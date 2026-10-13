package recovery

import (
	"context"
	"crypto/rand"
)

// Progress describes one restore. Percent measures the current phase; extraction
// has no known total and reports bytes read and files restored instead.
type Progress struct {
	OperationID   string `json:"operation_id,omitempty"`
	Status        string `json:"status"`
	Phase         string `json:"phase,omitempty"`
	Percent       *int   `json:"percent"`
	BytesDone     int64  `json:"bytes_done"`
	BytesTotal    int64  `json:"bytes_total"`
	RestoredFiles int    `json:"restored_files"`
}

type operationKey struct{}

func WithOperationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, operationKey{}, id)
}

func (r *RecoveryService) Progress() Progress {
	r.progressMu.RLock()
	defer r.progressMu.RUnlock()
	result := r.progress
	if result.Percent != nil {
		percent := *result.Percent
		result.Percent = &percent
	}
	if result.Status == "" {
		result.Status = "idle"
	}
	return result
}

func (r *RecoveryService) beginProgress(ctx context.Context) {
	id, _ := ctx.Value(operationKey{}).(string)
	if id == "" {
		id = rand.Text()
	}
	r.progressMu.Lock()
	defer r.progressMu.Unlock()
	r.progress = Progress{OperationID: id, Status: "running", Phase: "downloading"}
}

func (r *RecoveryService) setProgressPhase(phase string) {
	r.progressMu.Lock()
	defer r.progressMu.Unlock()
	r.progress.Phase = phase
	r.progress.Percent = nil
	r.progress.BytesDone, r.progress.BytesTotal = 0, 0
}

func (r *RecoveryService) reportBytes(done, total int64) {
	r.progressMu.Lock()
	defer r.progressMu.Unlock()
	r.progress.BytesDone, r.progress.BytesTotal = done, total
	r.progress.Percent = nil
	if total > 0 {
		percent := int(min(99, float64(done)/float64(total)*100))
		r.progress.Percent = &percent
	}
}

func (r *RecoveryService) reportFiles(count int) {
	r.progressMu.Lock()
	defer r.progressMu.Unlock()
	r.progress.RestoredFiles = count
}

func (r *RecoveryService) finishProgress(success bool) {
	r.progressMu.Lock()
	defer r.progressMu.Unlock()
	r.progress.Status = "failed"
	if success {
		percent := 100
		r.progress.Status = "completed"
		r.progress.Percent = &percent
	}
}
