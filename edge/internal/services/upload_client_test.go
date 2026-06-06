package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testUploadClient(serverURL string) *UploadClient {
	return &UploadClient{
		centralURL:                  serverURL,
		advertisedURL:               "http://edge.local",
		edgeCredential:              "edge-secret",
		edgeInstanceID:              "instance-1",
		encryptionKeyFingerprint:    "key-fp",
		chunkSizeBytes:              4,
		minChunkSizeBytes:           2,
		maxChunkSizeBytes:           8,
		maxRetryAttempts:            2,
		retryBaseDelay:              time.Nanosecond,
		retryMaxDelay:               time.Nanosecond,
		connectTimeout:              time.Second,
		readTimeoutPadding:          time.Nanosecond,
		minThroughputBytesPerSecond: 1024,
		http:                        http.DefaultClient,
		CircuitBreaker:              NewCircuitBreaker(3, 1),
	}
}

func TestUploadArchiveSuccess(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "archive.tar.zst")
	if err := os.WriteFile(archivePath, []byte("abcdef"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	var gotInitiate map[string]any
	var chunkOffsets []int64
	finalized := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer edge-secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/backup/uploads/initiate":
			if err := json.NewDecoder(r.Body).Decode(&gotInitiate); err != nil {
				t.Fatalf("decode initiate: %v", err)
			}
			writeTestJSON(t, w, map[string]any{
				"upload_id":                    "upload-1",
				"status":                       "initiated",
				"next_offset":                  0,
				"recommended_chunk_size_bytes": 3,
			})
		case r.Method == http.MethodPut && r.URL.Path == "/backup/uploads/upload-1/chunk":
			offset := mustParseOffset(t, r.URL.Query().Get("offset"))
			chunkOffsets = append(chunkOffsets, offset)
			buf := new(bytes.Buffer)
			buf.ReadFrom(r.Body)
			writeTestJSON(t, w, map[string]any{
				"upload_id":       "upload-1",
				"status":          "in_progress",
				"received_bytes":  offset + int64(buf.Len()),
				"next_offset":     offset + int64(buf.Len()),
				"duplicate":       false,
				"recommendation":  "continue",
				"chunk_len_echo":  buf.Len(),
				"content_len_hdr": r.ContentLength,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/backup/uploads/upload-1/finalize":
			finalized = true
			writeTestJSON(t, w, map[string]any{
				"status":    "ok",
				"stored_as": "job__2024-01-01T00-00-00Z__abcdef12.tar.zst",
				"pruned":    2,
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := testUploadClient(server.URL)
	client.http = server.Client()
	var progress []int64
	result, err := client.UploadArchive(context.Background(), "edge-1", "job", "abcdef123456", "2024-01-01T00:00:00Z", archivePath, "", "", 0, 0, func(_ string, offset, _ int64) {
		progress = append(progress, offset)
	})
	if err != nil {
		t.Fatalf("UploadArchive: %v", err)
	}
	if result.Status != "ok" || result.Pruned != 2 || result.UploadID != "upload-1" {
		t.Fatalf("result = %+v", result)
	}
	if !finalized {
		t.Fatal("finalize endpoint was not called")
	}
	if len(chunkOffsets) != 2 || chunkOffsets[0] != 0 || chunkOffsets[1] != 3 {
		t.Fatalf("chunk offsets = %+v", chunkOffsets)
	}
	if gotInitiate["edge_instance_id"] != "instance-1" || gotInitiate["advertised_url"] != "http://edge.local" {
		t.Fatalf("initiate body = %+v", gotInitiate)
	}
	if len(progress) == 0 || progress[len(progress)-1] != 6 {
		t.Fatalf("progress = %+v", progress)
	}
}

func TestUploadArchiveCompletedDuplicateSkipsChunks(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "archive.tar.zst")
	if err := os.WriteFile(archivePath, []byte("abc"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backup/uploads/initiate" {
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		writeTestJSON(t, w, map[string]any{
			"upload_id":   "upload-dup",
			"status":      "completed",
			"stored_as":   "already.tar.zst",
			"duplicate":   true,
			"pruned":      1,
			"next_offset": 3,
		})
	}))
	defer server.Close()

	client := testUploadClient(server.URL)
	client.http = server.Client()
	result, err := client.UploadArchive(context.Background(), "edge-1", "job", "fp", "ts", archivePath, "sha", "", 0, 0, nil)
	if err != nil {
		t.Fatalf("UploadArchive: %v", err)
	}
	if !result.Duplicate || result.StoredAs != "already.tar.zst" || result.Pruned != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestDownloadSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fp") != "abcdef12" {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		w.Header().Set("X-Relay-Snapshot-Filename", "snapshot.tar.zst")
		fmt.Fprint(w, "snapshot bytes")
	}))
	defer server.Close()
	client := testUploadClient(server.URL)
	client.http = server.Client()
	dest := filepath.Join(t.TempDir(), "restore", "snapshot.tar.zst")
	filename, err := client.DownloadSnapshotByFingerprint(context.Background(), "edge-1", "job", "abcdef12", dest)
	if err != nil {
		t.Fatalf("DownloadSnapshotByFingerprint: %v", err)
	}
	if filename != "snapshot.tar.zst" {
		t.Fatalf("filename = %q", filename)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "snapshot bytes" {
		t.Fatalf("downloaded = %q, %v", got, err)
	}
}

func TestRetryPhaseAndDoRequestFailures(t *testing.T) {
	client := testUploadClient("http://example.invalid")
	attempts := 0
	one := 1
	result, err := client.retryPhase("retryable", func() (map[string]any, error) {
		attempts++
		if attempts == 1 {
			return nil, &UploadFailure{Message: "try again", Category: "server", Retryable: true, RetryAfterSeconds: &one}
		}
		return map[string]any{"status": "ok"}, nil
	})
	if err != nil || result["status"] != "ok" || attempts != 2 {
		t.Fatalf("retryPhase success = %+v, %v, attempts=%d", result, err, attempts)
	}
	_, err = client.retryPhase("validation", func() (map[string]any, error) {
		return nil, &UploadFailure{Message: "bad", Category: "validation", Retryable: false}
	})
	if err == nil || !strings.Contains(err.Error(), "validation: bad") {
		t.Fatalf("retryPhase nonretryable err = %v", err)
	}

	client.CircuitBreaker.RecordFailure()
	client.CircuitBreaker.RecordFailure()
	client.CircuitBreaker.RecordFailure()
	req := httptest.NewRequest(http.MethodGet, "http://example.invalid", nil)
	if _, err := client.doRequest(req, "phase"); err == nil {
		t.Fatal("expected circuit-open failure")
	}
}

func TestUploadFailureClassificationAndHelpers(t *testing.T) {
	cases := []struct {
		status    int
		body      string
		retry     string
		category  string
		retryable bool
	}{
		{409, `{"detail":{"status":"checksum_mismatch","message":"bad checksum"}}`, "", "integrity", true},
		{409, `{"detail":{"status":"offset_mismatch","next_offset":4}}`, "", "offset_mismatch", true},
		{429, `{"detail":"slow down"}`, "5", "rate_limited", true},
		{503, `service down`, "", "server", true},
		{507, `capacity`, "", "capacity", false},
		{413, `too big`, "", "too_large", false},
		{401, `nope`, "", "unauthorized", false},
		{418, `teapot`, "", "permanent", false},
	}
	for _, tc := range cases {
		resp := &http.Response{
			StatusCode: tc.status,
			Header:     make(http.Header),
			Body:       ioNopCloser{strings.NewReader(tc.body)},
		}
		if tc.retry != "" {
			resp.Header.Set("Retry-After", tc.retry)
		}
		err := buildUploadFailure(resp, "phase")
		if err.Category != tc.category || err.Retryable != tc.retryable {
			t.Fatalf("status %d classified as %+v", tc.status, err)
		}
		if tc.retry != "" && (err.RetryAfterSeconds == nil || *err.RetryAfterSeconds != 5) {
			t.Fatalf("retry-after = %+v", err.RetryAfterSeconds)
		}
	}

	sumPath := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(sumPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := sha256File(sumPath)
	if err != nil {
		t.Fatalf("sha256File: %v", err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte("hello")))
	if got != want {
		t.Fatalf("sha256File = %q", got)
	}
	if _, err := sha256File(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected missing file error")
	}

	if v := int64Field(map[string]any{"a": float64(3), "b": int64(4), "c": 5}, "a"); v != 3 {
		t.Fatalf("int64Field float = %d", v)
	}
	if int64Field(map[string]any{"b": int64(4)}, "b") != 4 || intField(map[string]any{"c": 5}, "c") != 5 {
		t.Fatal("numeric helpers failed")
	}
	if !boolField(map[string]any{"ok": true}, "ok") || stringField(map[string]any{"s": "x"}, "s") != "x" {
		t.Fatal("field helpers failed")
	}
}

func TestChunkSizingTimeoutAndRequestTimeout(t *testing.T) {
	client := testUploadClient("http://example.invalid")
	if got := client.initialChunkSize(0, 3, 10); got != 3 {
		t.Fatalf("recommended chunk = %d", got)
	}
	if got := client.initialChunkSize(1, 0, 10); got != 2 {
		t.Fatalf("min chunk = %d", got)
	}
	if got := client.initialChunkSize(99, 0, 10); got != 8 {
		t.Fatalf("max chunk = %d", got)
	}
	if got := client.initialChunkSize(99, 0, 3); got != 3 {
		t.Fatalf("archive cap = %d", got)
	}
	client.minThroughputBytesPerSecond = 0
	if got := client.timeoutForBytes(10); got != client.readTimeoutPadding {
		t.Fatalf("timeout no throughput = %v", got)
	}
	client.minThroughputBytesPerSecond = 5
	if got := client.timeoutForBytes(6); got < 2*time.Second {
		t.Fatalf("timeout throughput = %v", got)
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.test", nil)
	withTimeout, cancel := requestWithTimeout(req, time.Nanosecond)
	defer cancel()
	if _, ok := withTimeout.Context().Deadline(); !ok {
		t.Fatal("expected request deadline")
	}
	noTimeout, cancel := requestWithTimeout(req, 0)
	defer cancel()
	if noTimeout != req {
		t.Fatal("zero timeout should keep original request")
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, payload map[string]any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode JSON: %v", err)
	}
}

func mustParseOffset(t *testing.T, value string) int64 {
	t.Helper()
	var offset int64
	if _, err := fmt.Sscanf(value, "%d", &offset); err != nil {
		t.Fatalf("offset %q: %v", value, err)
	}
	return offset
}

type ioNopCloser struct {
	*strings.Reader
}

func (c ioNopCloser) Close() error { return nil }
