package services

import "sync"

// NamespaceLockManager provides per-namespace mutexes.
type NamespaceLockManager struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewNamespaceLockManager() *NamespaceLockManager {
	return &NamespaceLockManager{locks: make(map[string]*sync.Mutex)}
}

func (m *NamespaceLockManager) Lock(namespace string) *sync.Mutex {
	m.mu.Lock()
	l := m.locks[namespace]
	if l == nil {
		l = &sync.Mutex{}
		m.locks[namespace] = l
	}
	m.mu.Unlock()
	return l
}
