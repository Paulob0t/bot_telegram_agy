package state

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"telegram-paulobot/internal/config"
)

var ValidEfforts = map[string]bool{
	"low": true, "medium": true, "high": true, "xhigh": true, "max": true,
}

type BotSessionState struct {
	mu                       sync.RWMutex
	cfg                      *config.Config
	CurrentEngine            string            `json:"current_engine"`
	CurrentConversationID    string            `json:"current_conversation_id,omitempty"`
	CurrentConversationTitle string            `json:"current_conversation_title"`
	ClaudeSessionID          string            `json:"claude_session_id,omitempty"`
	ClaudeSessionTitle       string            `json:"claude_session_title"`
	ClaudeModel              string            `json:"claude_model,omitempty"`
	CurrentModel             string            `json:"current_model,omitempty"`
	CurrentEffort            string            `json:"current_effort"`
	CurrentCWD               string            `json:"current_cwd"`
	ConversationCWDMap       map[string]string `json:"conversation_cwd_map"`
	Features                 map[string]bool   `json:"features"`
	UpdatedAt                float64           `json:"updated_at"`
	LastUsageData            map[string]any    `json:"-"`
}

func New(cfg *config.Config) *BotSessionState {
	s := &BotSessionState{
		cfg:                      cfg,
		CurrentEngine:            "agy",
		CurrentConversationTitle: "▸ Continuar último chat (-c)",
		ClaudeSessionTitle:       "▸ Último chat Claude (-c)",
		CurrentModel:             "gemini-4-hydrogen-low",
		CurrentEffort:            "low",
		CurrentCWD:               cfg.HomeDir,
		ConversationCWDMap:       make(map[string]string),
		Features:                 defaultFeatures(cfg),
		LastUsageData:            make(map[string]any),
	}
	s.loadState()
	s.loadUsage()
	return s
}

func defaultFeatures(cfg *config.Config) map[string]bool {
	_, obsErr := os.Stat(cfg.ObsidianVaultDir)
	hasObsidian := obsErr == nil
	_, claudeErr := exec.LookPath(cfg.ClaudeBin)
	hasClaude := claudeErr == nil
	return map[string]bool{
		"claude":          hasClaude,
		"obsidian":        hasObsidian,
		"prompts":         hasObsidian,
		"screen":          true,
		"chat_guard":      true,
		"long_buffer":     true,
		"notify_telegram": true,
		"notify_desktop":  true,
		// Off by default: when on, risky Claude tool calls block until the phone answers.
		"mobile_approvals": false,
	}
}

func (s *BotSessionState) loadState() {
	raw, err := os.ReadFile(s.cfg.StateCachePath)
	if err != nil {
		_ = s.Save()
		return
	}
	var loaded BotSessionState
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if eff := strings.ToLower(loaded.CurrentEffort); ValidEfforts[eff] {
		s.CurrentEffort = eff
	}
	s.CurrentModel = strings.TrimSpace(loaded.CurrentModel)
	if s.CurrentModel == "" {
		s.CurrentModel = "gemini-4-hydrogen-low"
	}
	if loaded.CurrentCWD != "" {
		if info, err := os.Stat(loaded.CurrentCWD); err == nil && info.IsDir() {
			s.CurrentCWD = loaded.CurrentCWD
		}
	}
	if loaded.CurrentEngine == "claude" || loaded.CurrentEngine == "agy" {
		s.CurrentEngine = loaded.CurrentEngine
	}
	s.CurrentConversationID = loaded.CurrentConversationID
	if loaded.CurrentConversationTitle != "" {
		s.CurrentConversationTitle = loaded.CurrentConversationTitle
	}
	s.ClaudeSessionID = loaded.ClaudeSessionID
	if loaded.ClaudeSessionTitle != "" {
		s.ClaudeSessionTitle = loaded.ClaudeSessionTitle
	}
	s.ClaudeModel = loaded.ClaudeModel
	if loaded.ConversationCWDMap != nil {
		s.ConversationCWDMap = loaded.ConversationCWDMap
	}
	for k, v := range loaded.Features {
		s.Features[k] = v
	}
	s.UpdatedAt = loaded.UpdatedAt
}

func (s *BotSessionState) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.UpdatedAt = float64(time.Now().UnixNano()) / 1e9
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.cfg.StateCachePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := s.cfg.StateCachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.cfg.StateCachePath)
}

func (s *BotSessionState) loadUsage() {
	raw, err := os.ReadFile(s.cfg.UsageCachePath)
	if err != nil {
		return
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err == nil {
		s.mu.Lock()
		s.LastUsageData = m
		s.mu.Unlock()
	}
}

func (s *BotSessionState) FeatureEnabled(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.Features[key]
	return ok && v
}

func (s *BotSessionState) ToggleFeature(key string) bool {
	s.mu.Lock()
	cur := s.Features[key]
	next := !cur
	s.Features[key] = next
	if key == "claude" && !next && s.CurrentEngine == "claude" {
		s.CurrentEngine = "agy"
	}
	s.mu.Unlock()
	_ = s.Save()
	return next
}

func (s *BotSessionState) SetCWD(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	s.mu.Lock()
	s.CurrentCWD = abs
	if s.CurrentEngine == "agy" && s.CurrentConversationID != "" && s.CurrentConversationID != "NEW" && s.CurrentConversationID != "LATEST" {
		if s.ConversationCWDMap == nil {
			s.ConversationCWDMap = make(map[string]string)
		}
		s.ConversationCWDMap[s.CurrentConversationID] = abs
	}
	s.mu.Unlock()
	_ = s.Save()
	return true
}

func (s *BotSessionState) SetAgyConversation(convID, title, cwd string) {
	s.mu.Lock()
	s.CurrentEngine = "agy"
	s.CurrentConversationID = convID
	s.CurrentConversationTitle = title
	if s.ConversationCWDMap == nil {
		s.ConversationCWDMap = make(map[string]string)
	}
	if cwd != "" {
		if info, err := os.Stat(cwd); err == nil && info.IsDir() {
			s.CurrentCWD = cwd
			if convID != "" && convID != "NEW" && convID != "LATEST" {
				s.ConversationCWDMap[convID] = cwd
			}
		}
	} else if convID != "" && convID != "NEW" && convID != "LATEST" {
		if mapped, ok := s.ConversationCWDMap[convID]; ok {
			if info, err := os.Stat(mapped); err == nil && info.IsDir() {
				s.CurrentCWD = mapped
			}
		} else {
			s.ConversationCWDMap[convID] = s.CurrentCWD
		}
	}
	s.mu.Unlock()
	_ = s.Save()
}

func (s *BotSessionState) SetClaudeSession(sessionID, title, cwd string) {
	s.mu.Lock()
	s.CurrentEngine = "claude"
	s.ClaudeSessionID = sessionID
	s.ClaudeSessionTitle = title
	if cwd != "" {
		if info, err := os.Stat(cwd); err == nil && info.IsDir() {
			s.CurrentCWD = cwd
		}
	}
	s.mu.Unlock()
	_ = s.Save()
}

func (s *BotSessionState) GetCWDShortName(cwd string) string {
	if cwd == "" {
		cwd = s.CurrentCWD
	}
	if filepath.Clean(cwd) == filepath.Clean(s.cfg.HomeDir) {
		return "~ (" + s.cfg.HomeDir + ")"
	}
	base := filepath.Base(cwd)
	if base == "" || base == "." {
		return cwd
	}
	return base
}
