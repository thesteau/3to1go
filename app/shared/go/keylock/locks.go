package keylock

import "sync"

// Manager provides per-namespace mutexes.
type Manager struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func New() *Manager {
	return &Manager{locks: make(map[string]*sync.Mutex)}
}

func (m *Manager) Lock(namespace string) *sync.Mutex {
	m.mu.Lock()
	l := m.locks[namespace]
	if l == nil {
		l = &sync.Mutex{}
		m.locks[namespace] = l
	}
	m.mu.Unlock()
	return l
}
