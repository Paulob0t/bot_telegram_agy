package sessions

import (
	"context"
	"sync"
	"time"
)

const DefaultIdleTTL = time.Hour

// Session represents an active workspace/resume session tracked by the PC worker.
type Session struct {
	ID           string    `json:"id"`
	Project      string    `json:"project"`
	Title        string    `json:"title"`
	LastActivity time.Time `json:"last_activity"`
	cancel       context.CancelFunc
}

// Manager tracks active sessions, prunes them after DefaultIdleTTL, and cancels their processes.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
}

// NewManager creates a SessionManager with the given idle TTL (defaults to 1h).
func NewManager(ttl time.Duration) *Manager {
	if ttl <= 0 {
		ttl = DefaultIdleTTL
	}
	return &Manager{
		sessions: make(map[string]*Session),
		ttl:      ttl,
	}
}

// Upsert creates or refreshes a session's last activity timestamp.
func (m *Manager) Upsert(id, project, title string, cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.sessions[id]; ok {
		if project != "" {
			existing.Project = project
		}
		if title != "" {
			existing.Title = title
		}
		if cancel != nil {
			existing.cancel = cancel
		}
		existing.LastActivity = time.Now()
		return
	}
	m.sessions[id] = &Session{
		ID:           id,
		Project:      project,
		Title:        title,
		LastActivity: time.Now(),
		cancel:       cancel,
	}
}

// Touch updates the session's last activity timestamp.
func (m *Manager) Touch(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.LastActivity = time.Now()
	}
}

// Kill cancels the session's underlying context/process and removes it.
func (m *Manager) Kill(id string) bool {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if ok && s.cancel != nil {
		s.cancel()
	}
	return ok
}

// List returns a snapshot of all active sessions.
func (m *Manager) List() []Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, *s)
	}
	return out
}

// PruneExpired cancels and removes all sessions idle longer than m.ttl.
func (m *Manager) PruneExpired(now time.Time) []string {
	m.mu.Lock()
	var expired []*Session
	var ids []string
	for id, s := range m.sessions {
		if now.Sub(s.LastActivity) >= m.ttl {
			expired = append(expired, s)
			ids = append(ids, id)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	for _, s := range expired {
		if s.cancel != nil {
			s.cancel()
		}
	}
	return ids
}

// StartReaper periodically prunes expired sessions until ctx is cancelled.
func (m *Manager) StartReaper(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			m.PruneExpired(now)
		}
	}
}
