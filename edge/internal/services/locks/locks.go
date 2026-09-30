package locks

import "github.com/3to1go/shared/keylock"

// JobLockManager issues non-blocking per-key mutexes.
type JobLockManager struct{ locks *keylock.Manager }

func NewJobLockManager() *JobLockManager { return &JobLockManager{locks: keylock.New()} }

func (m *JobLockManager) Acquire(key string) func() {
	lock := m.locks.Lock(key)
	if !lock.TryLock() {
		return nil
	}
	return lock.Unlock
}
