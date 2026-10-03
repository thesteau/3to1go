package runner

import (
	"sync"

	"github.com/3to1go/scout/internal/anomaly"
	"github.com/3to1go/scout/internal/services/state"
)

type mockStateStore struct {
	mu     sync.Mutex
	states map[string]state.JobState
}

func newMockStateStore() *mockStateStore {
	return &mockStateStore{states: make(map[string]state.JobState)}
}

func (m *mockStateStore) Get(key string) state.JobState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.states[key]
}

func (m *mockStateStore) Set(key string, s state.JobState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[key] = s
	return nil
}

func (m *mockStateStore) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, key)
	return nil
}

func (m *mockStateStore) ReferencedPendingArchives() map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	refs := make(map[string]bool)
	for _, s := range m.states {
		if s.PendingArchive != "" {
			refs[s.PendingArchive] = true
		}
	}
	return refs
}

type mockAnomalyStore struct {
	mu      sync.Mutex
	history map[string][]anomaly.Observation
	pending map[string]anomaly.Observation
}

func newMockAnomalyStore() *mockAnomalyStore {
	return &mockAnomalyStore{
		history: make(map[string][]anomaly.Observation),
		pending: make(map[string]anomaly.Observation),
	}
}

func (m *mockAnomalyStore) History(key string) ([]anomaly.Observation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]anomaly.Observation(nil), m.history[key]...), nil
}

func (m *mockAnomalyStore) SetPending(key string, obs anomaly.Observation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending[key] = obs
	return nil
}

func (m *mockAnomalyStore) Accept(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if obs, ok := m.pending[key]; ok {
		m.history[key] = append(m.history[key], obs)
		delete(m.pending, key)
	}
	return nil
}

func (m *mockAnomalyStore) ClearPending(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pending, key)
	return nil
}

func (m *mockAnomalyStore) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.history, key)
	delete(m.pending, key)
	return nil
}
