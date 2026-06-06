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

func TestAcquire_NeverMoreThanOneConcurrentHolder(t *testing.T) {
	m := NewJobLockManager()

	const n = 20
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		active   int
		violated bool
	)

	start := make(chan struct{})

	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			unlock := m.Acquire("job1")
			if unlock == nil {
				return
			}
			mu.Lock()
			active++
			if active > 1 {
				violated = true
			}
			mu.Unlock()

			mu.Lock()
			active--
			mu.Unlock()
			unlock()
		}()
	}

	close(start) // release all goroutines at once
	wg.Wait()

	if violated {
		t.Error("more than one goroutine held the lock concurrently")
	}
}
