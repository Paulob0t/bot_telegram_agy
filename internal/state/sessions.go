package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// sessionTTL drops app chats untouched for this long.
const sessionTTL = 7 * 24 * time.Hour

// SessionStore keeps the per-chat states of the app's open sessions on disk, so
// the project and conversation of each chat survive a restart of the service.
type SessionStore struct {
	mu     sync.Mutex
	path   string
	global *BotSessionState
	byID   map[string]*BotSessionState
}

// NewSessionStore loads the saved chats next to the global state file.
func NewSessionStore(global *BotSessionState) *SessionStore {
	r := &SessionStore{
		path:   filepath.Join(filepath.Dir(global.cfg.StateCachePath), "mobile_sessions.json"),
		global: global,
		byID:   map[string]*BotSessionState{},
	}
	r.load()
	return r
}

// Get returns the state of chat sid, creating a fresh one on first use.
// The empty and "default" ids share the global state.
func (r *SessionStore) Get(sid string) *BotSessionState {
	sid = strings.TrimSpace(sid)
	if sid == "" || sid == "default" {
		return r.global
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.byID[sid]; ok {
		return existing
	}
	sess := r.global.NewSession()
	sess.onSave = r.persist
	r.byID[sid] = sess
	return sess
}

func (r *SessionStore) load() {
	raw, err := os.ReadFile(r.path)
	if err != nil {
		return
	}
	var saved map[string]*BotSessionState
	if json.Unmarshal(raw, &saved) != nil {
		return
	}
	cutoff := time.Now().Add(-sessionTTL)
	for sid, sess := range saved {
		if sess == nil || sess.LastUsed.Before(cutoff) {
			continue
		}
		sess.cfg = r.global.cfg
		sess.ephemeral = true
		sess.onSave = r.persist
		if sess.ConversationCWDMap == nil {
			sess.ConversationCWDMap = map[string]string{}
		}
		if sess.Features == nil {
			sess.Features = map[string]bool{}
		}
		sess.LastUsageData = map[string]any{}
		r.byID[sid] = sess
	}
}

func (r *SessionStore) persist() {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := make(map[string]json.RawMessage, len(r.byID))
	for sid, sess := range r.byID {
		sess.mu.RLock()
		data, err := json.Marshal(sess)
		sess.mu.RUnlock()
		if err == nil {
			snapshot[sid] = data
		}
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return
	}
	tmp := r.path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, r.path)
	}
}
