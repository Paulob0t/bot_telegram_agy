package guard

import (
	"fmt"
	"sync"
	"time"
)

type HeldPrompt struct {
	ID        string
	ChatID    int64
	Prompt    string
	CreatedAt time.Time
}

type ChatGuard struct {
	mu           sync.Mutex
	lastActivity time.Time
	idleLimit    time.Duration
	held         map[string]HeldPrompt
}

func New(idleMinutes float64) *ChatGuard {
	if idleMinutes <= 0 {
		idleMinutes = 30
	}
	return &ChatGuard{
		// Arranca en zero para que el primer mensaje tras reiniciar el bot confirme chat y proyecto (cwd)
		lastActivity: time.Time{},
		idleLimit:    time.Duration(idleMinutes * float64(time.Minute)),
		held:         make(map[string]HeldPrompt),
	}
}

func (g *ChatGuard) Touch() {
	g.mu.Lock()
	g.lastActivity = time.Now()
	g.mu.Unlock()
}

func (g *ChatGuard) NeedsConfirmation(enabled bool, engine, convID, claudeID string) (bool, string) {
	if !enabled || g.idleLimit <= 0 {
		return false, ""
	}
	if engine == "claude" && claudeID == "NEW" {
		return false, ""
	}
	if engine != "claude" && convID == "NEW" {
		return false, ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.lastActivity.IsZero() {
		return true, "bot reiniciado"
	}
	elapsed := time.Since(g.lastActivity)
	if elapsed < g.idleLimit {
		return false, ""
	}
	mins := int(elapsed.Minutes())
	if mins >= 60 {
		return true, fmt.Sprintf("%dh %dm inactivo", mins/60, mins%60)
	}
	return true, fmt.Sprintf("%dm inactivo", mins)
}

func (g *ChatGuard) Hold(chatID int64, prompt string) string {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	for k, v := range g.held {
		if now.Sub(v.CreatedAt) > time.Hour {
			delete(g.held, k)
		}
	}
	id := fmt.Sprintf("%x", now.UnixNano()&0xffffffff)
	g.held[id] = HeldPrompt{
		ID:        id,
		ChatID:    chatID,
		Prompt:    prompt,
		CreatedAt: now,
	}
	return id
}

func (g *ChatGuard) Get(id string) (HeldPrompt, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	h, ok := g.held[id]
	return h, ok
}

func (g *ChatGuard) Pop(id string) (HeldPrompt, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	h, ok := g.held[id]
	if ok {
		delete(g.held, id)
	}
	return h, ok
}
