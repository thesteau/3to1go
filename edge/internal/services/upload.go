package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/3to1go/edge/internal/config"
	"github.com/3to1go/edge/internal/encryption"
	"github.com/3to1go/edge/internal/identity"
)

// UploadFailure is a structured error returned by UploadClient.
type UploadFailure struct {
	Message           string
	Category          string
	Retryable         bool
	StatusCode        int
	NextOffset        *int64
	RetryAfterSeconds *int
	Phase             string
}

func (e *UploadFailure) Error() string {
	if e.Phase != "" {
		return e.Phase + ": " + e.Message
	}
	return e.Message
}

// CircuitBreaker tracks consecutive failures and opens after a threshold.
type CircuitBreaker struct {
	mu                   sync.Mutex
	failureThreshold     int
	cooldownSeconds      int
	consecutiveFailures  int
	openedUntilMonotonic float64 // 0 = closed
}

func NewCircuitBreaker(threshold, cooldownSeconds int) *CircuitBreaker {
	return &CircuitBreaker{
		failureThreshold: threshold,
		cooldownSeconds:  cooldownSeconds,
	}
}

func (cb *CircuitBreaker) BeforeRequest() *UploadFailure {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.openedUntilMonotonic == 0 {
		return nil
	}
	now := monoNow()
	if now >= cb.openedUntilMonotonic {
		cb.openedUntilMonotonic = 0
		return nil
	}
	remaining := int(math.Ceil(cb.openedUntilMonotonic - now))
	if remaining < 1 {
		remaining = 1
	}
	return &UploadFailure{
		Message:           "central circuit breaker is open",
		Category:          "circuit_open",
		Retryable:         true,
		RetryAfterSeconds: &remaining,
	}
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveFailures = 0
	cb.openedUntilMonotonic = 0
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveFailures++
	if cb.consecutiveFailures >= cb.failureThreshold {
		cb.openedUntilMonotonic = monoNow() + float64(cb.cooldownSeconds)
	}
}

// Snapshot returns a JSON-ready status of the circuit breaker.
func (cb *CircuitBreaker) Snapshot() map[string]interface{} {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.openedUntilMonotonic == 0 {
		return map[string]interface{}{
			"state":                      "closed",
			"consecutive_failures":       cb.consecutiveFailures,
			"cooldown_remaining_seconds": 0,
		}
	}
	remaining := int(math.Max(0, cb.openedUntilMonotonic-monoNow()))
	state := "open"
	if remaining == 0 {
		state = "closed"
	}
	return map[string]interface{}{
		"state":                      state,
		"consecutive_failures":       cb.consecutiveFailures,
		"cooldown_remaining_seconds": remaining,
	}
}

// ProgressCallback is called with (uploadID, offset, currentChunkSize) after each chunk.
type ProgressCallback func(uploadID string, offset, chunkSize int64)

// UploadClient sends backup archives to the central server.
type UploadClient struct {
	centralURL                  string
	advertisedURL               string
	edgeCredential              string
	edgeInstanceID              string
	encryptionKeyFingerprint    string
	chunkSizeBytes              int64
	minChunkSizeBytes           int64
	maxChunkSizeBytes           int64
	maxRetryAttempts            int
	retryBaseDelay              time.Duration
	retryMaxDelay               time.Duration
	connectTimeout              time.Duration
	readTimeoutPadding          time.Duration
	minThroughputBytesPerSecond int64
	http                        *http.Client
	CircuitBreaker              *CircuitBreaker
}

// NewUploadClient constructs an UploadClient from settings and the loaded encryption key.
func NewUploadClient(cfg *config.Settings, encKey []byte, certs *CertManager) *UploadClient {
	instID := identity.LoadOrCreate(config.InstallationIDPath())
	keyFP := encryption.KeyFingerprint(encKey)

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if certs != nil {
		if tlsCfg := certs.tlsConfig(); tlsCfg != nil {
			transport.TLSClientConfig = tlsCfg
		}
	}
	httpClient := &http.Client{Transport: transport}

	maxChunk := cfg.MaxUploadChunkSizeBytes()
	if maxChunk < cfg.UploadChunkSizeBytes() {
		maxChunk = cfg.UploadChunkSizeBytes()
	}

	return &UploadClient{
		centralURL:                  cfg.CentralURL,
		advertisedURL:               cfg.AdvertisedURL,
		edgeCredential:              cfg.EdgeCredential,
		edgeInstanceID:              instID,
		encryptionKeyFingerprint:    keyFP,
		chunkSizeBytes:              cfg.UploadChunkSizeBytes(),
		minChunkSizeBytes:           cfg.MinUploadChunkSizeBytes(),
		maxChunkSizeBytes:           maxChunk,
		maxRetryAttempts:            cfg.UploadRetryMaxAttempts,
		retryBaseDelay:              time.Duration(cfg.UploadRetryBaseDelaySeconds) * time.Second,
		retryMaxDelay:               time.Duration(cfg.UploadRetryMaxDelaySeconds) * time.Second,
		connectTimeout:              time.Duration(cfg.UploadConnectTimeoutSeconds) * time.Second,
		readTimeoutPadding:          time.Duration(cfg.UploadReadTimeoutPaddingSeconds) * time.Second,
		minThroughputBytesPerSecond: int64(cfg.UploadMinThroughputBytesPerSecond),
		http:                        httpClient,
		CircuitBreaker:              NewCircuitBreaker(cfg.CircuitBreakerFailureThreshold, cfg.CircuitBreakerCooldownSeconds),
	}
}

// UploadResult is returned on a successful upload.
type UploadResult struct {
	Status    string `json:"status"`
	StoredAs  string `json:"stored_as"`
	Pruned    int    `json:"pruned"`
	Duplicate bool   `json:"duplicate"`
	UploadID  string `json:"upload_id"`
}

// UploadArchive sends the archive at archivePath to central, resuming if uploadID/offset are set.
func (c *UploadClient) UploadArchive(
	ctx context.Context,
	edgeID, jobName, fingerprint, timestamp, archivePath string,
	archiveSHA256 string,
	uploadID string,
	uploadOffset int64,
	preferredChunkSize int64,
	progress ProgressCallback,
) (*UploadResult, error) {
	fi, err := os.Stat(archivePath)
	if err != nil {
		return nil, err
	}
	archiveSize := fi.Size()

	if archiveSHA256 == "" {
		archiveSHA256, err = sha256File(archivePath)
		if err != nil {
			return nil, err
		}
	}

	idempotencyKey := buildIdempotencyKey(edgeID, jobName, fingerprint, timestamp, archiveSize, archiveSHA256)

	initiate := func() (map[string]interface{}, error) {
		return c.initiateSession(ctx, edgeID, jobName, fingerprint, timestamp, archiveSize, archiveSHA256, idempotencyKey)
	}
	sessionInfo, err := c.retryPhase("initiate", initiate)
	if err != nil {
		return nil, err
	}

	uploadID = stringField(sessionInfo, "upload_id")
	offset := maxInt64(uploadOffset, int64Field(sessionInfo, "next_offset"))

	if stringField(sessionInfo, "status") == "completed" {
		return &UploadResult{
			Status:    "ok",
			StoredAs:  stringField(sessionInfo, "stored_as"),
			Pruned:    intField(sessionInfo, "pruned"),
			Duplicate: boolField(sessionInfo, "duplicate"),
			UploadID:  uploadID,
		}, nil
	}

	chunkSize := c.initialChunkSize(preferredChunkSize, int64Field(sessionInfo, "recommended_chunk_size_bytes"), archiveSize)
	if progress != nil {
		progress(uploadID, offset, chunkSize)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	successStreak := 0
	attempt := 0
	for offset < archiveSize {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, err
		}
		buf := make([]byte, min64(chunkSize, archiveSize-offset))
		n, err := io.ReadFull(f, buf)
		if err != nil && err != io.ErrUnexpectedEOF {
			return nil, err
		}
		buf = buf[:n]
		if len(buf) == 0 {
			break
		}

		resp, err := c.sendChunk(ctx, uploadID, offset, buf)
		if err != nil {
			if uf, ok := err.(*UploadFailure); ok {
				if uf.NextOffset != nil && *uf.NextOffset > offset {
					offset = *uf.NextOffset
					successStreak = 0
					if progress != nil {
						progress(uploadID, offset, chunkSize)
					}
					attempt = 0
					continue
				}
				attempt++
				if !uf.Retryable || attempt >= c.maxRetryAttempts {
					return nil, uf
				}
				reconciled, rerr := c.retryPhase("reconcile", initiate)
				if rerr != nil {
					return nil, rerr
				}
				newOff := int64Field(reconciled, "next_offset")
				if newOff > offset {
					offset = newOff
				}
				if chunkSize > c.minChunkSizeBytes*2 {
					chunkSize /= 2
				} else {
					chunkSize = c.minChunkSizeBytes
				}
				if progress != nil {
					progress(uploadID, offset, chunkSize)
				}
				if offset >= archiveSize {
					break
				}
				c.sleepBeforeRetry(attempt, uf.RetryAfterSeconds)
				continue
			}
			return nil, err
		}

		offset = maxInt64(offset+int64(len(buf)), int64Field(resp, "next_offset"))
		successStreak++
		if successStreak >= 2 {
			chunkSize = min64(c.maxChunkSizeBytes, chunkSize*2)
			successStreak = 0
		}
		if progress != nil {
			progress(uploadID, offset, chunkSize)
		}
		attempt = 0
	}

	finalResp, err := c.retryPhase("finalize", func() (map[string]interface{}, error) {
		return c.finalizeSession(ctx, uploadID)
	})
	if err != nil {
		return nil, err
	}
	return &UploadResult{
		Status:    stringField(finalResp, "status"),
		StoredAs:  stringField(finalResp, "stored_as"),
		Pruned:    intField(finalResp, "pruned"),
		Duplicate: boolField(finalResp, "duplicate"),
		UploadID:  uploadID,
	}, nil
}

// DownloadLatestSnapshot downloads the latest snapshot for a job from central.
func (c *UploadClient) DownloadLatestSnapshot(ctx context.Context, edgeID, jobName, destPath string) (string, error) {
	path := fmt.Sprintf("/backup/recovery/%s/%s/%s/latest", edgeID, c.edgeInstanceID, jobName)
	return c.downloadSnapshot(ctx, path, destPath, nil)
}

// DownloadSnapshotByFingerprint downloads a specific snapshot by fingerprint.
func (c *UploadClient) DownloadSnapshotByFingerprint(ctx context.Context, edgeID, jobName, fingerprint, destPath string) (string, error) {
	path := fmt.Sprintf("/backup/recovery/%s/%s/%s/by-fingerprint", edgeID, c.edgeInstanceID, jobName)
	return c.downloadSnapshot(ctx, path, destPath, map[string]string{"fp": fingerprint})
}

func (c *UploadClient) downloadSnapshot(ctx context.Context, path, destPath string, params map[string]string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.centralURL+path, nil)
	if err != nil {
		return "", err
	}
	q := req.URL.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+c.edgeCredential)

	resp, err := c.doRequest(req, "recovery_download")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	filename := resp.Header.Get("X-Relay-Snapshot-Filename")
	if filename == "" {
		filename = destPath
	}

	if err := os.MkdirAll(getDir(destPath), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", err
	}
	return filename, nil
}

func (c *UploadClient) retryPhase(phase string, op func() (map[string]interface{}, error)) (map[string]interface{}, error) {
	for attempt := 0; ; attempt++ {
		result, err := op()
		if err == nil {
			return result, nil
		}
		uf, ok := err.(*UploadFailure)
		if !ok {
			return nil, err
		}
		if uf.Phase == "" {
			uf.Phase = phase
		}
		if !uf.Retryable || attempt+1 >= c.maxRetryAttempts {
			return nil, uf
		}
		c.sleepBeforeRetry(attempt+1, uf.RetryAfterSeconds)
	}
}

func (c *UploadClient) initiateSession(ctx context.Context, edgeID, jobName, fingerprint, timestamp string, archiveSize int64, sha256sum, idempotencyKey string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"edge_id":                    edgeID,
		"edge_instance_id":           c.edgeInstanceID,
		"job_name":                   jobName,
		"fingerprint":                fingerprint,
		"timestamp":                  timestamp,
		"archive_format":             "tar.zst",
		"archive_size_bytes":         archiveSize,
		"archive_sha256":             sha256sum,
		"idempotency_key":            idempotencyKey,
		"encryption_key_fingerprint": c.encryptionKeyFingerprint,
		"advertised_url":             c.advertisedURL,
	}
	return c.jsonPost(ctx, "/backup/uploads/initiate", body, "initiate",
		c.timeoutForBytes(c.chunkSizeBytes))
}

func (c *UploadClient) sendChunk(ctx context.Context, uploadID string, offset int64, chunk []byte) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/backup/uploads/%s/chunk?offset=%d", c.centralURL, uploadID, offset)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(chunk))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.edgeCredential)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(chunk))

	timeout := c.connectTimeout + c.timeoutForBytes(int64(len(chunk)))
	req, cancel := requestWithTimeout(req, timeout)
	defer cancel()

	resp, err := c.doRequest(req, "chunk")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return decodeJSON(resp.Body)
}

func (c *UploadClient) finalizeSession(ctx context.Context, uploadID string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/backup/uploads/%s/finalize", c.centralURL, uploadID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(nil))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.edgeCredential)
	req.Header.Set("Content-Type", "application/octet-stream")

	timeout := c.connectTimeout + c.timeoutForBytes(c.chunkSizeBytes)
	req, cancel := requestWithTimeout(req, timeout)
	defer cancel()

	resp, err := c.doRequest(req, "finalize")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return decodeJSON(resp.Body)
}

func (c *UploadClient) jsonPost(ctx context.Context, path string, body interface{}, phase string, readTimeout time.Duration) (map[string]interface{}, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.centralURL+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.edgeCredential)
	req.Header.Set("Content-Type", "application/json")

	req, cancel := requestWithTimeout(req, c.connectTimeout+readTimeout)
	defer cancel()

	resp, err := c.doRequest(req, phase)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return decodeJSON(resp.Body)
}

func requestWithTimeout(req *http.Request, timeout time.Duration) (*http.Request, context.CancelFunc) {
	if timeout <= 0 {
		return req, func() {}
	}
	ctx, cancel := context.WithTimeout(req.Context(), timeout)
	return req.WithContext(ctx), cancel
}

func (c *UploadClient) doRequest(req *http.Request, phase string) (*http.Response, error) {
	if uf := c.CircuitBreaker.BeforeRequest(); uf != nil {
		uf.Phase = phase
		return nil, uf
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.CircuitBreaker.RecordFailure()
		return nil, &UploadFailure{Message: err.Error(), Category: "network", Retryable: true, Phase: phase}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c.CircuitBreaker.RecordSuccess()
		return resp, nil
	}
	uf := buildUploadFailure(resp, phase)
	if uf.Category == "network" || uf.Category == "server" || uf.Category == "rate_limited" {
		c.CircuitBreaker.RecordFailure()
	} else {
		c.CircuitBreaker.RecordSuccess()
	}
	resp.Body.Close()
	return nil, uf
}

func buildUploadFailure(resp *http.Response, phase string) *UploadFailure {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var payload map[string]interface{}
	_ = json.Unmarshal(body, &payload)

	var detail interface{}
	if payload != nil {
		detail = payload["detail"]
	}

	message := detailMessage(detail)
	if message == "" {
		message = string(bytes.TrimSpace(body))
	}
	if message == "" {
		message = fmt.Sprintf("http %d", resp.StatusCode)
	}

	nextOffset := detailNextOffset(detail)
	detailStatus := detailStatusStr(detail)
	retryAfter := retryAfterSeconds(resp)

	sc := resp.StatusCode
	uf := &UploadFailure{
		Message:           message,
		StatusCode:        sc,
		NextOffset:        nextOffset,
		RetryAfterSeconds: retryAfter,
		Phase:             phase,
	}

	switch {
	case sc == 409 && detailStatus == "checksum_mismatch":
		uf.Category, uf.Retryable = "integrity", true
	case sc == 409 && nextOffset != nil:
		uf.Category, uf.Retryable = "offset_mismatch", true
	case sc == 429:
		uf.Category, uf.Retryable = "rate_limited", true
	case sc == 408 || sc == 425 || sc == 500 || sc == 502 || sc == 503 || sc == 504:
		uf.Category, uf.Retryable = "server", true
	case sc == 507:
		uf.Category, uf.Retryable = "capacity", false
	case sc == 413:
		uf.Category, uf.Retryable = "too_large", false
	case sc == 400 || sc == 404 || sc == 409 || sc == 422:
		uf.Category, uf.Retryable = "validation", false
	case sc == 401 || sc == 403:
		uf.Category, uf.Retryable = "unauthorized", false
	default:
		uf.Category, uf.Retryable = "permanent", false
	}
	return uf
}

func (c *UploadClient) initialChunkSize(preferred, recommended, archiveSize int64) int64 {
	target := preferred
	if target == 0 {
		target = recommended
	}
	if target == 0 {
		target = c.chunkSizeBytes
	}
	if target < c.minChunkSizeBytes {
		target = c.minChunkSizeBytes
	}
	if target > c.maxChunkSizeBytes {
		target = c.maxChunkSizeBytes
	}
	if target > archiveSize && archiveSize > 0 {
		target = archiveSize
	}
	if target < 1 {
		target = 1
	}
	return target
}

func (c *UploadClient) timeoutForBytes(size int64) time.Duration {
	if c.minThroughputBytesPerSecond <= 0 {
		return c.readTimeoutPadding
	}
	seconds := int64(math.Ceil(float64(size) / float64(c.minThroughputBytesPerSecond)))
	if seconds < 1 {
		seconds = 1
	}
	return time.Duration(seconds)*time.Second + c.readTimeoutPadding
}

func (c *UploadClient) sleepBeforeRetry(attempt int, retryAfter *int) {
	var delay time.Duration
	if retryAfter != nil {
		delay = time.Duration(*retryAfter) * time.Second
	} else {
		exp := math.Pow(2, float64(attempt-1))
		secs := c.retryBaseDelay.Seconds() * exp
		if secs > c.retryMaxDelay.Seconds() {
			secs = c.retryMaxDelay.Seconds()
		}
		jitter := secs * 0.2 * rand.Float64()
		delay = time.Duration((secs + jitter) * float64(time.Second))
	}
	time.Sleep(delay)
}

func buildIdempotencyKey(edgeID, jobName, fingerprint, timestamp string, archiveSize int64, sha256sum string) string {
	raw := fmt.Sprintf("%s|%s|%s|%s|%d|%s", edgeID, jobName, fingerprint, timestamp, archiveSize, sha256sum)
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", sum)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func decodeJSON(r io.Reader) (map[string]interface{}, error) {
	var m map[string]interface{}
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func detailMessage(detail interface{}) string {
	if s, ok := detail.(string); ok {
		return s
	}
	if m, ok := detail.(map[string]interface{}); ok {
		if msg, ok := m["message"].(string); ok && msg != "" {
			return msg
		}
		if status, ok := m["status"].(string); ok {
			if off, ok := m["next_offset"]; ok {
				return fmt.Sprintf("%s next_offset=%v", status, off)
			}
			return status
		}
	}
	return ""
}

func detailNextOffset(detail interface{}) *int64 {
	if m, ok := detail.(map[string]interface{}); ok {
		if v, ok := m["next_offset"].(float64); ok {
			n := int64(v)
			return &n
		}
	}
	return nil
}

func detailStatusStr(detail interface{}) string {
	if m, ok := detail.(map[string]interface{}); ok {
		if s, ok := m["status"].(string); ok {
			return s
		}
	}
	return ""
}

func retryAfterSeconds(resp *http.Response) *int {
	header := resp.Header.Get("Retry-After")
	if header == "" {
		return nil
	}
	n, err := strconv.Atoi(header)
	if err != nil || n < 1 {
		return nil
	}
	return &n
}

func stringField(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func int64Field(m map[string]interface{}, key string) int64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

func intField(m map[string]interface{}, key string) int {
	return int(int64Field(m, key))
}

func boolField(m map[string]interface{}, key string) bool {
	if m == nil {
		return false
	}
	b, _ := m[key].(bool)
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func getDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}

func monoNow() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}
