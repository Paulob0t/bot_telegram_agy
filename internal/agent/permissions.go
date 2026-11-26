package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
)

// EventTypePermission asks connected mobile clients to approve or reject a tool call.
const EventTypePermission EventType = "permission_request"

// PermissionCommandPrefix is the text command a client sends back: "__perm__:<id>:allow|deny".
const PermissionCommandPrefix = "__perm__:"

// ApprovalsToggleReply builds the chat reply after flipping the mobile_approvals feature.
func ApprovalsToggleReply(on bool) string {
	if on {
		return "Aprobaciones desde el móvil: ACTIVADAS. Bash/Write/Edit esperarán tu OK (120s)."
	}
	return "Aprobaciones desde el móvil: DESACTIVADAS."
}

type PermissionDecision string

const (
	PermissionAllow     PermissionDecision = "allow"
	PermissionDeny      PermissionDecision = "deny"
	PermissionTimeout   PermissionDecision = "timeout"
	PermissionNoClients PermissionDecision = "no_clients"
)

type permissionRegistry struct {
	mu      sync.Mutex
	pending map[string]chan PermissionDecision
}

var perms = &permissionRegistry{pending: make(map[string]chan PermissionDecision)}

func newPermissionID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// RequestPermission broadcasts a permission_request and blocks until a client answers,
// ctx expires, or there is nobody subscribed to answer.
func (s *Service) RequestPermission(ctx context.Context, tool, detail, workspace string) PermissionDecision {
	s.subMu.RLock()
	n := len(s.subscribers)
	s.subMu.RUnlock()
	if n == 0 {
		return PermissionNoClients
	}

	id := newPermissionID()
	ch := make(chan PermissionDecision, 1)
	perms.mu.Lock()
	perms.pending[id] = ch
	perms.mu.Unlock()
	defer func() {
		perms.mu.Lock()
		delete(perms.pending, id)
		perms.mu.Unlock()
	}()

	s.broadcast(Event{
		Type:    EventTypePermission,
		Label:   tool,
		Detail:  detail,
		Title:   workspace,
		Message: id,
	}, nil)

	select {
	case d := <-ch:
		return d
	case <-ctx.Done():
		// Tell clients to drop the stale prompt.
		s.broadcast(Event{Type: EventTypePermission, Message: id, Done: true}, nil)
		return PermissionTimeout
	}
}

// HandlePermissionCommand resolves a pending request from a "__perm__:<id>:allow|deny" text.
// Returns false when text is not a permission command.
func (s *Service) HandlePermissionCommand(text string) bool {
	if !strings.HasPrefix(text, PermissionCommandPrefix) {
		return false
	}
	parts := strings.SplitN(strings.TrimPrefix(text, PermissionCommandPrefix), ":", 2)
	if len(parts) != 2 {
		return true
	}
	decision := PermissionDeny
	if parts[1] == string(PermissionAllow) {
		decision = PermissionAllow
	}
	perms.mu.Lock()
	ch, ok := perms.pending[parts[0]]
	if ok {
		delete(perms.pending, parts[0])
	}
	perms.mu.Unlock()
	if ok {
		ch <- decision
		// Close the prompt on every other connected device.
		s.broadcast(Event{Type: EventTypePermission, Message: parts[0], Done: true}, nil)
	}
	return true
}
