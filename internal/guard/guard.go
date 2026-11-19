package guard

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type HeldPrompt struct {
	ID        string
	ChatID    int64
	Prompt    string
	TempFiles []string
	CreatedAt time.Time
}

type PendingUpload struct {
	ID        string
	ChatID    int64
	FilePath  string
	FileName  string
	Caption   string
	IsPDF     bool
	IsPhoto   bool
	CreatedAt time.Time
}

type ChatGuard struct {
	mu           sync.Mutex
	lastActivity time.Time
	idleLimit    time.Duration
	held         map[string]HeldPrompt
	uploads      map[string]PendingUpload
	seq          uint64
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
		uploads:      make(map[string]PendingUpload),
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

func (g *ChatGuard) Hold(chatID int64, prompt string, tempFiles ...string) string {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	for k, v := range g.held {
		if now.Sub(v.CreatedAt) > time.Hour {
			delete(g.held, k)
		}
	}
	g.seq++
	id := fmt.Sprintf("%x%x", now.UnixNano()&0xfffffff, g.seq&0xff)
	g.held[id] = HeldPrompt{
		ID:        id,
		ChatID:    chatID,
		Prompt:    prompt,
		TempFiles: append([]string(nil), tempFiles...),
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

func (g *ChatGuard) HoldUpload(u PendingUpload) string {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	for k, v := range g.uploads {
		if now.Sub(v.CreatedAt) > 12*time.Hour {
			delete(g.uploads, k)
		}
	}
	g.seq++
	id := fmt.Sprintf("%x%x", now.UnixNano()&0xfffffff, g.seq&0xff)
	u.ID = id
	u.CreatedAt = now
	g.uploads[id] = u
	return id
}

func (g *ChatGuard) GetUpload(id string) (PendingUpload, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	u, ok := g.uploads[id]
	return u, ok
}

func (g *ChatGuard) PopUpload(id string) (PendingUpload, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	u, ok := g.uploads[id]
	if ok {
		delete(g.uploads, id)
	}
	return u, ok
}

// PrepareUploadsDir devuelve ~/.cache/telegram-paulobot/uploads y borra archivos con más de 24h.
func PrepareUploadsDir(homeDir string) string {
	dir := filepath.Join(homeDir, ".cache", "telegram-paulobot", "uploads")
	_ = os.MkdirAll(dir, 0o755)
	entries, err := os.ReadDir(dir)
	if err == nil {
		cutoff := time.Now().Add(-24 * time.Hour)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	return dir
}
