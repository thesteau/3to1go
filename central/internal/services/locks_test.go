package services

import (
	"sync"
	"testing"
)

func TestNewNamespaceLockManager(t *testing.T) {
	m := NewNamespaceLockManager()
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestNamespaceLockManager_SameKeyReturnsSameMutex(t *testing.T) {
	m := NewNamespaceLockManager()
	l1 := m.Lock("ns/a")
	l2 := m.Lock("ns/a")
	if l1 != l2 {
		t.Error("same key should return same mutex")
	}
}

func TestNamespaceLockManager_DifferentKeysReturnDifferentMutexes(t *testing.T) {
	m := NewNamespaceLockManager()
	l1 := m.Lock("ns/a")
	l2 := m.Lock("ns/b")
	if l1 == l2 {
		t.Error("different keys should return different mutexes")
	}
}

func TestNamespaceLockManager_ConcurrentAccess(t *testing.T) {
	m := NewNamespaceLockManager()
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			l := m.Lock("shared-ns")
			l.Lock()
			l.Unlock()
		})
	}
	wg.Wait()
}
