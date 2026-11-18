package chats

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"telegram-paulobot/internal/config"
)

const chunkBytes = 128 * 1024

type ClaudeSession struct {
	SessionID string
	CWD       string
	Title     string
	ModTime   time.Time
}

type cacheEntry struct {
	mtime int64
	meta  ClaudeSession
}

var (
	metaMu    sync.RWMutex
	metaCache = make(map[string]cacheEntry)
	xmlTagRe  = regexp.MustCompile(`</?[a-zA-Z0-9_\-]+(?:\s+[^>]*)?>`)
)

func ListClaudeSessions(cfg *config.Config, limit int) []ClaudeSession {
	pattern := filepath.Join(cfg.ClaudeProjectsDir, "*", "*.jsonl")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	var out []ClaudeSession
	for _, p := range files {
		if s, ok := extractSessionMeta(p); ok {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ModTime.After(out[j].ModTime)
	})
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func extractSessionMeta(jsonlPath string) (ClaudeSession, bool) {
	info, err := os.Stat(jsonlPath)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return ClaudeSession{}, false
	}
	mtime := info.ModTime().UnixNano()

	metaMu.RLock()
	if cached, ok := metaCache[jsonlPath]; ok && cached.mtime == mtime {
		metaMu.RUnlock()
		return cached.meta, true
	}
	metaMu.RUnlock()

	f, err := os.Open(jsonlPath)
	if err != nil {
		return ClaudeSession{}, false
	}
	defer f.Close()

	size := info.Size()
	var lines []string
	if size <= chunkBytes*2 {
		b, _ := io.ReadAll(f)
		lines = strings.Split(string(b), "\n")
	} else {
		head := make([]byte, chunkBytes)
		n1, _ := f.Read(head)
		_, _ = f.Seek(size-chunkBytes, io.SeekStart)
		tail := make([]byte, chunkBytes)
		n2, _ := f.Read(tail)
		lines = append(strings.Split(string(head[:n1]), "\n"), strings.Split(string(tail[:n2]), "\n")...)
	}

	var cwd, customTitle, firstUser, lastUser string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			continue
		}
		if cwd == "" {
			if c, ok := data["cwd"].(string); ok {
				cwd = c
			}
		}
		t, _ := data["type"].(string)
		if t == "custom-title" {
			if ct, ok := data["customTitle"].(string); ok && strings.TrimSpace(ct) != "" {
				customTitle = strings.TrimSpace(ct)
			}
		} else if t == "user" {
			if isMeta, _ := data["isMeta"].(bool); !isMeta {
				if msg, ok := data["message"].(map[string]any); ok {
					if clean := cleanUserText(msg["content"]); clean != "" {
						if firstUser == "" {
							firstUser = clean
						}
						lastUser = clean
					}
				}
			}
		}
	}

	title := customTitle
	if title == "" {
		title = lastUser
	}
	if title == "" {
		title = firstUser
	}
	if title == "" {
		title = "Sesión Claude"
	}

	sessionID := strings.TrimSuffix(filepath.Base(jsonlPath), ".jsonl")
	meta := ClaudeSession{
		SessionID: sessionID,
		CWD:       cwd,
		Title:     title,
		ModTime:   info.ModTime(),
	}

	metaMu.Lock()
	metaCache[jsonlPath] = cacheEntry{mtime: mtime, meta: meta}
	metaMu.Unlock()

	return meta, true
}

func cleanUserText(raw any) string {
	var text string
	switch v := raw.(type) {
	case string:
		text = v
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok && m["type"] == "text" {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		text = strings.Join(parts, "\n")
	default:
		return ""
	}

	text = xmlTagRe.ReplaceAllString(text, "")
	for _, line := range strings.Split(text, "\n") {
		clean := strings.Join(strings.Fields(strings.Trim(line, " #*>-•\t")), " ")
		if clean != "" && !strings.HasPrefix(clean, "/") {
			return clean
		}
	}
	return ""
}
