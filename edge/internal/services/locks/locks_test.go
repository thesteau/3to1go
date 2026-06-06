package locks

import (
	"sync"
	"testing"
)

func TestAcquire_ReturnsUnlockFunc(t *testing.T) {
	m := NewJobLockManager()
	unlock := m.Acquire("job1")
	if unlock == nil {
		t.Fatal("Acquire should return a non-nil unlock func on first call")
	}
	unlock()
}

func TestAcquire_BlockedWhenHeld(t *testing.T) {
	m := NewJobLockManager()
	unlock := m.Acquire("job1")
	defer unlock()

	second := m.Acquire("job1")
	if second != nil {
		second()
		t.Fatal("Acquire should return nil when the key is already locked")
	}
}

func TestAcquire_DifferentKeysIndependent(t *testing.T) {
	m := NewJobLockManager()
	u1 := m.Acquire("job1")
	u2 := m.Acquire("job2")
	if u1 == nil || u2 == nil {
		t.Fatal("different keys should each be acquirable independently")
	}
	u1()
	u2()
}

func TestAcquire_ReacquirableAfterUnlock(t *testing.T) {
	m := NewJobLockManager()
	unlock := m.Acquire("job1")
	unlock()

	second := m.Acquire("job1")
	if second == nil {
		t.Fatal("lock should be reacquirable after being released")
	}
	second()
}

func TestAcquire_ConcurrentGoroutinesOnlyOneWins(t *testing.T) {
	m := NewJobLockManager()

	var (
		wg      sync.WaitGroup
		winners int
		mu      sync.Mutex
	)

	// Hold the lock long enough for all goroutines to attempt acquisition.
	hold := m.Acquire("job1")

	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u := m.Acquire("job1")
			if u != nil {
				mu.Lock()
				winners++
				mu.Unlock()
				u()
			}
		}()
	}

	hold()
	wg.Wait()

	// After releasing hold, exactly one goroutine should have won.
	// (The others raced against hold and lost — 0 or 1 winners is valid.)
	if winners > 1 {
		t.Errorf("expected at most 1 winner, got %d", winners)
	}
}
