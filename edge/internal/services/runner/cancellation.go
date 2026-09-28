package runner

import (
	"context"

	"github.com/3to1go/edge/internal/backup"
	"github.com/3to1go/edge/internal/services/state"
)

// beginOperation is called while holding cycleLock; completion releases the
// context only after all workers have stopped using it.
func (r *EdgeRunner) beginOperation(parent context.Context) func() {
	ctx, cancel := context.WithCancel(parent)
	r.mu.Lock()
	r.operationCtx, r.operationCancel = ctx, cancel
	r.mu.Unlock()
	return func() {
		cancel()
		r.mu.Lock()
		r.operationCtx, r.operationCancel = nil, nil
		r.mu.Unlock()
	}
}

func (r *EdgeRunner) operationContext() context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.operationCtx != nil {
		return r.operationCtx
	}
	return context.Background()
}

func (r *EdgeRunner) CancelOperation() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.operationCancel == nil {
		return false
	}
	r.operationCancel()
	return true
}

func (r *EdgeRunner) markCancelled(job *backup.JobDefinition, s *state.JobState) {
	s.LastStatus = "cancelled"
	s.ActivePhase = ""
	s.ActivePhasePercent = 0
	s.NextRetryAt = ""
	s.LastErrorCategory = ""
	s.LastErrorDetail = ""
	s.ManualInterventionRequired = false
	s.LastUploadUpdatedAt = utcNow()
	r.StateStore.Set(job.RootPath, *s)
}
