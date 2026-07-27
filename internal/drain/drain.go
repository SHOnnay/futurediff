package drain

import (
	"context"
	"errors"
	"sync"
	"time"
)

type Status struct {
	Draining  bool       `json:"draining"`
	Active    int        `json:"active_mutations"`
	Reason    string     `json:"reason,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
}

type Manager struct {
	mu        sync.Mutex
	draining  bool
	active    int
	reason    string
	startedAt *time.Time
	zero      chan struct{}
}

func New() *Manager { ch := make(chan struct{}); close(ch); return &Manager{zero: ch} }

func (m *Manager) BeginMutation() (func(), error) {
	if m == nil {
		return func() {}, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.draining {
		return nil, errors.New("daemon is draining and rejects new mutations")
	}
	if m.active == 0 {
		m.zero = make(chan struct{})
	}
	m.active++
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.active--
			if m.active == 0 {
				close(m.zero)
			}
		})
	}, nil
}

func (m *Manager) Start(reason string, now time.Time) Status {
	if m == nil {
		return Status{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.draining {
		n := now.UTC().Truncate(time.Second)
		m.draining = true
		m.reason = reason
		m.startedAt = &n
	}
	return m.statusLocked()
}
func (m *Manager) Status() Status {
	if m == nil {
		return Status{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}
func (m *Manager) Wait(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	ch := m.zero
	m.mu.Unlock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *Manager) statusLocked() Status {
	return Status{Draining: m.draining, Active: m.active, Reason: m.reason, StartedAt: m.startedAt}
}
