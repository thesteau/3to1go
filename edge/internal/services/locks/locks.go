package locks

import "sync"

// JobLockManager issues non-blocking per-key mutexes.
// Acquire returns a non-nil unlock func if the lock was acquired, nil if already held.
type JobLockManager struct {
	mu    sync.Mutex
	locks map[string]*jobLock
}

type jobLock struct {
	mu   sync.Mutex
	held bool
}

// NewJobLockManager creates a new manager.
func NewJobLockManager() *JobLockManager {
	return &JobLockManager{locks: make(map[string]*jobLock)}
}

// Acquire tries to lock key. Returns an unlock func on success, nil if already locked.
func (m *JobLockManager) Acquire(key string) func() {
	m.mu.Lock()
	jl, ok := m.locks[key]
	if !ok {
		jl = &jobLock{}
		m.locks[key] = jl
	}
	m.mu.Unlock()

	if !jl.mu.TryLock() {
		return nil
	}
	jl.held = true
	return func() {
		jl.held = false
		jl.mu.Unlock()
	}
}
