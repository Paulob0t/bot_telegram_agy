package models

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"agente-bot/internal/config"
	"agente-bot/internal/state"
)

const (
	DefaultBotModelID   = "gemini-4-hydrogen-low"
	DefaultBotModelName = "Gemini 4 Hydrogen (Low)"
)

type ModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon"`
}

// DefaultCatalog contiene los 23 modelos actuales de AGY CLI con Gemini 4 Hydrogen (Low) al frente.
var DefaultCatalog = []ModelInfo{
	{ID: "gemini-4-hydrogen-low", Name: "Gemini 4 Hydrogen (Low)", Icon: "🧬"},
	{ID: "gemini-4-hydrogen-medium", Name: "Gemini 4 Hydrogen (Medium)", Icon: "🧬"},
	{ID: "gemini-4-hydrogen-high", Name: "Gemini 4 Hydrogen (High)", Icon: "🧬"},
	{ID: "gemini-4-hydrogen-xhigh", Name: "Gemini 4 Hydrogen (XHigh)", Icon: "🔥"},
	{ID: "gemini-4-neon-low", Name: "Gemini 4 Neon (Low)", Icon: "💠"},
	{ID: "gemini-4-neon-medium", Name: "Gemini 4 Neon (Medium)", Icon: "💠"},
	{ID: "gemini-4-neon-high", Name: "Gemini 4 Neon (High)", Icon: "💠"},
	{ID: "gemini-4-neon-xhigh", Name: "Gemini 4 Neon (XHigh)", Icon: "🔥"},
	{ID: "gemini-4-argon-low", Name: "Gemini 4 Argon (Low)", Icon: "🧪"},
	{ID: "gemini-4-argon-medium", Name: "Gemini 4 Argon (Medium)", Icon: "🧪"},
	{ID: "gemini-4-argon-high", Name: "Gemini 4 Argon (High)", Icon: "🧪"},
	{ID: "gemini-pro-orchestrator-neon", Name: "Orchestrator (Neon)", Icon: "🧭"},
	{ID: "gemini-pro-orchestrator-g1-pro", Name: "Orchestrator (Argon)", Icon: "🧭"},
	{ID: "gemini-3.8-flash-low", Name: "Gemini 3.8 Flash (Low)", Icon: "⚡"},
	{ID: "gemini-3.8-flash-medium", Name: "Gemini 3.8 Flash (Medium)", Icon: "⚡"},
	{ID: "gemini-3.8-flash-high", Name: "Gemini 3.8 Flash (High)", Icon: "⚡"},
	{ID: "claude-opus-5-5-low", Name: "Claude Opus 5.5 (Low)", Icon: "👑"},
	{ID: "claude-opus-5-5-medium", Name: "Claude Opus 5.5 (Medium)", Icon: "👑"},
	{ID: "claude-opus-5-5-high", Name: "Claude Opus 5.5 (High)", Icon: "👑"},
	{ID: "claude-sonnet-5-5-low", Name: "Claude Sonnet 5.5 (Low)", Icon: "🎯"},
	{ID: "claude-sonnet-5-5-medium", Name: "Claude Sonnet 5.5 (Medium)", Icon: "🎯"},
	{ID: "claude-sonnet-5-5-high", Name: "Claude Sonnet 5.5 (High)", Icon: "🎯"},
	{ID: "gpt-oss-120b-medium", Name: "GPT-OSS 120B (Medium)", Icon: "🤖"},
}

var ClaudeCatalog = []ModelInfo{
	{ID: "sonnet", Name: "Claude Sonnet 4.6 (Balanceado)", Icon: "🎯"},
	{ID: "opus", Name: "Claude Opus 4.6 (Máxima inteligencia)", Icon: "👑"},
	{ID: "haiku", Name: "Claude Haiku 4.5 (Ultraligero)", Icon: "⚡"},
}

var (
	ansiRe      = regexp.MustCompile(`\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])`)
	modelLineRe = regexp.MustCompile(`^([a-zA-Z0-9\.\-_]+)\s+(.+)$`)
)

func GetCatalog(cfg *config.Config) []ModelInfo {
	cachePath := filepath.Join(cfg.BaseCLIDir, "telegram_models_cache.json")
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		return DefaultCatalog
	}
	var cached struct {
		Models []ModelInfo `json:"models"`
	}
	if json.Unmarshal(raw, &cached) != nil || len(cached.Models) == 0 {
		return DefaultCatalog
	}
	byID := make(map[string]ModelInfo, len(cached.Models))
	for _, m := range cached.Models {
		if m.ID != "" {
			byID[m.ID] = m
		}
	}
	var merged []ModelInfo
	seen := make(map[string]bool)
	for _, base := range DefaultCatalog {
		if cm, ok := byID[base.ID]; ok {
			if cm.Icon == "" {
				cm.Icon = base.Icon
			}
			merged = append(merged, cm)
		} else {
			merged = append(merged, base)
		}
		seen[base.ID] = true
	}
	for _, m := range cached.Models {
		if m.ID != "" && !seen[m.ID] {
			if m.Icon == "" {
				m.Icon = PickIconForModel(m.ID)
			}
			merged = append(merged, m)
			seen[m.ID] = true
		}
	}
	return merged
}

func SyncFromCLI(cfg *config.Config) ([]ModelInfo, error) {
	cmd := exec.Command(cfg.AgyBin, "models")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return GetCatalog(cfg), err
	}
	clean := ansiRe.ReplaceAllString(string(out), "")
	var parsed []ModelInfo
	seen := make(map[string]bool)

	for _, rawLine := range strings.Split(clean, "\n") {
		segments := strings.Split(rawLine, "\r")
		line := strings.TrimSpace(segments[len(segments)-1])
		if line == "" || strings.Contains(strings.ToLower(line), "fetching available models") {
			continue
		}
		if m := modelLineRe.FindStringSubmatch(line); len(m) == 3 {
			id := strings.TrimSpace(m[1])
			name := strings.TrimSpace(m[2])
			if !seen[id] {
				seen[id] = true
				parsed = append(parsed, ModelInfo{
					ID:   id,
					Name: name,
					Icon: PickIconForModel(id),
				})
			}
		}
	}
	if len(parsed) > 0 {
		cachePath := filepath.Join(cfg.BaseCLIDir, "telegram_models_cache.json")
		payload, _ := json.MarshalIndent(map[string]any{
			"updated_at": time.Now().Unix(),
			"models":     parsed,
		}, "", "  ")
		_ = os.WriteFile(cachePath, payload, 0o644)
	}
	return GetCatalog(cfg), nil
}

func PickIconForModel(modelID string) string {
	for _, m := range DefaultCatalog {
		if m.ID == modelID && m.Icon != "" {
			return m.Icon
		}
	}
	for _, m := range ClaudeCatalog {
		if m.ID == modelID && m.Icon != "" {
			return m.Icon
		}
	}
	m := strings.ToLower(modelID)
	switch {
	case strings.Contains(m, "orchestrator"):
		return "🧭"
	case strings.Contains(m, "neon"):
		if strings.Contains(m, "xhigh") {
			return "🔥"
		}
		return "💠"
	case strings.Contains(m, "hydrogen"):
		if strings.Contains(m, "xhigh") {
			return "🔥"
		}
		return "🧬"
	case strings.Contains(m, "argon"):
		return "🧪"
	case strings.Contains(m, "opus"):
		return "👑"
	case strings.Contains(m, "sonnet"), strings.Contains(m, "claude"):
		return "🎯"
	case strings.Contains(m, "flash"), strings.Contains(m, "haiku"):
		return "⚡"
	default:
		return "🤖"
	}
}

func GetContextWindowForModel(modelID string) int {
	m := strings.ToLower(modelID)
	switch {
	case strings.Contains(m, "deepseek"):
		return 64_000
	case strings.Contains(m, "claude"), strings.Contains(m, "sonnet"), strings.Contains(m, "opus"), strings.Contains(m, "haiku"):
		return 200_000
	case strings.Contains(m, "gpt"):
		return 128_000
	default:
		return 1_048_576
	}
}

func GetCLIDefaultModel(cfg *config.Config) string {
	return DefaultBotModelName
}

func GetClaudeDefaultModel(cfg *config.Config) string {
	settingsPath := filepath.Join(cfg.HomeDir, ".claude", "settings.json")
	raw, err := os.ReadFile(settingsPath)
	if err == nil {
		var data map[string]any
		if json.Unmarshal(raw, &data) == nil {
			if m, ok := data["model"].(string); ok && strings.TrimSpace(m) != "" {
				return strings.TrimSpace(m)
			}
		}
	}
	return "sonnet"
}

// GetEffectiveModelInfo retorna (nombre_amigable, id_efectivo, ventana_contexto) según el motor y modelo actual.
func GetEffectiveModelInfo(cfg *config.Config, st *state.BotSessionState) (string, string, int) {
	if st.CurrentEngine == "claude" {
		if st.ClaudeModel != "" {
			for _, m := range ClaudeCatalog {
				if m.ID == st.ClaudeModel {
					return fmt.Sprintf("%s %s (`%s`)", m.Icon, m.Name, m.ID), m.ID, 200_000
				}
			}
			return fmt.Sprintf("🟠 `%s`", st.ClaudeModel), st.ClaudeModel, 200_000
		}
		defCl := GetClaudeDefaultModel(cfg)
		for _, m := range ClaudeCatalog {
			if m.ID == defCl {
				return fmt.Sprintf("Por defecto Claude CLI: %s %s (`%s`)", m.Icon, m.Name, m.ID), m.ID, 200_000
			}
		}
		return fmt.Sprintf("Por defecto Claude CLI (`%s`)", defCl), defCl, 200_000
	}

	activeID := st.CurrentModel
	isDefault := false
	if activeID == "" || activeID == "DEFAULT" {
		activeID = DefaultBotModelID
		isDefault = true
	}

	catalog := GetCatalog(cfg)
	for _, m := range catalog {
		if m.ID == activeID {
			if isDefault {
				return fmt.Sprintf("%s %s (`%s` · Default)", m.Icon, m.Name, m.ID), m.ID, GetContextWindowForModel(m.ID)
			}
			return fmt.Sprintf("%s %s (`%s`)", m.Icon, m.Name, m.ID), m.ID, GetContextWindowForModel(m.ID)
		}
	}
	return fmt.Sprintf("`%s`", activeID), activeID, GetContextWindowForModel(activeID)
}
