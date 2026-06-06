package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sync"
	"time"

	"github.com/3to1go/central/internal/store"
)

type snapshotLister interface {
	ListNamespaces(ctx context.Context) ([]store.NamespaceEntry, error)
	ListNamespaceEntries(ctx context.Context, namespace string) ([]store.SnapshotEntry, error)
}

type snapshotReader interface {
	Open(namespace, filename string) (io.ReadCloser, error)
}

// Result holds the outcome of a single verification run.
type Result struct {
	CheckedAt      time.Time `json:"checked_at"`
	TotalChecked   int       `json:"total_checked"`
	FailureCount   int       `json:"failure_count"`
	LastFailure    string    `json:"last_failure,omitempty"`
	LastFailureMsg string    `json:"last_failure_msg,omitempty"`
}

// Service runs periodic SHA-256 integrity checks on stored snapshots.
type Service struct {
	idx    snapshotLister
	store  snapshotReader
	mu     sync.RWMutex
	latest *Result
}

// New creates a Service. Neither idx nor store is called until Run or RunOnce is invoked.
func New(idx snapshotLister, store snapshotReader) *Service {
	return &Service{idx: idx, store: store}
}

// Latest returns the most recent verification result, or nil if none has run yet.
func (s *Service) Latest() *Result {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.latest == nil {
		return nil
	}
	copy := *s.latest
	return &copy
}

// RunOnce checks the latest snapshot for every job in every namespace.
// It is intentionally lightweight: one snapshot per job, not the full history.
func (s *Service) RunOnce(ctx context.Context) *Result {
	namespaces, err := s.idx.ListNamespaces(ctx)
	result := &Result{CheckedAt: time.Now().UTC()}
	if err != nil {
		result.FailureCount = 1
		result.LastFailureMsg = "failed to list namespaces: " + err.Error()
		s.setLatest(result)
		return result
	}

	for _, ns := range namespaces {
		for _, job := range ns.Jobs {
			if ctx.Err() != nil {
				break
			}
			namespace := ns.EdgeID + "/" + ns.EdgeInstanceID + "/" + job.JobName
			entries, err := s.idx.ListNamespaceEntries(ctx, namespace)
			if err != nil || len(entries) == 0 {
				continue
			}
			latest := entries[0]
			if latest.ArchiveSHA == "" {
				continue
			}
			result.TotalChecked++
			if fail := s.checkOne(ctx, namespace, latest); fail != "" {
				result.FailureCount++
				result.LastFailure = namespace + "/" + latest.StoredAs
				result.LastFailureMsg = fail
			}
		}
	}

	s.setLatest(result)
	return result
}

// Run blocks and runs verification every intervalHours. Call with a cancellable context.
func (s *Service) Run(ctx context.Context, intervalHours int) {
	if intervalHours <= 0 {
		return
	}
	ticker := time.NewTicker(time.Duration(intervalHours) * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.RunOnce(ctx)
		}
	}
}

func (s *Service) checkOne(ctx context.Context, namespace string, entry store.SnapshotEntry) string {
	rc, err := s.store.Open(namespace, entry.StoredAs)
	if err != nil {
		return "open failed: " + err.Error()
	}
	defer rc.Close()

	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return "read failed: " + err.Error()
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != entry.ArchiveSHA {
		return "sha256 mismatch: stored=" + entry.ArchiveSHA[:8] + " actual=" + actual[:8]
	}
	return ""
}

func (s *Service) setLatest(r *Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = r
}
