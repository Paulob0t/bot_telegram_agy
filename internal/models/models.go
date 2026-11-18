package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"telegram-paulobot/internal/config"
	"telegram-paulobot/internal/state"
)

type ModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon"`
}

var DefaultCatalog = []ModelInfo{
	{ID: "gemini-4-argon-medium", Name: "Gemini 4 Argon (Medium)", Icon: "🧪"},
	{ID: "gemini-4-argon-high", Name: "Gemini 4 Argon (High)", Icon: "🧪"},
	{ID: "gemini-4-argon-low", Name: "Gemini 4 Argon (Low)", Icon: "🧪"},
	{ID: "gemini-4-hydrogen-xhigh", Name: "Gemini 4 Hydrogen (XHigh)", Icon: "🔥"},
	{ID: "gemini-4-hydrogen-high", Name: "Gemini 4 Hydrogen (High)", Icon: "🧬"},
	{ID: "gemini-4-hydrogen-medium", Name: "Gemini 4 Hydrogen (Medium)", Icon: "🧬"},
	{ID: "gemini-4-hydrogen-low", Name: "Gemini 4 Hydrogen (Low)", Icon: "🧬"},
	{ID: "gemini-3.8-flash-high", Name: "Gemini 3.8 Flash (High)", Icon: "⚡"},
	{ID: "gemini-3.8-flash-medium", Name: "Gemini 3.8 Flash (Medium)", Icon: "⚡"},
	{ID: "gemini-3.8-flash-low", Name: "Gemini 3.8 Flash (Low)", Icon: "⚡"},
	{ID: "claude-opus-5-5-high", Name: "Claude Opus 5.5 (High)", Icon: "👑"},
	{ID: "claude-opus-5-5-medium", Name: "Claude Opus 5.5 (Medium)", Icon: "👑"},
	{ID: "claude-opus-5-5-low", Name: "Claude Opus 5.5 (Low)", Icon: "👑"},
	{ID: "claude-sonnet-5-5-high", Name: "Claude Sonnet 5.5 (High)", Icon: "🎯"},
	{ID: "claude-sonnet-5-5-medium", Name: "Claude Sonnet 5.5 (Medium)", Icon: "🎯"},
	{ID: "claude-sonnet-5-5-low", Name: "Claude Sonnet 5.5 (Low)", Icon: "🎯"},
	{ID: "gpt-oss-120b-medium", Name: "GPT-OSS 120B (Medium)", Icon: "🤖"},
}

var ClaudeCatalog = []ModelInfo{
	{ID: "sonnet", Name: "Claude Sonnet 4.6 (Balanceado)", Icon: "🎯"},
	{ID: "opus", Name: "Claude Opus 4.6 (Máxima inteligencia)", Icon: "👑"},
	{ID: "haiku", Name: "Claude Haiku 4.5 (Ultraligero)", Icon: "⚡"},
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
	case strings.Contains(m, "opus"):
		return "👑"
	case strings.Contains(m, "hydrogen"):
		if strings.Contains(m, "xhigh") {
			return "🔥"
		}
		return "🧬"
	case strings.Contains(m, "argon"):
		return "🧪"
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
	settingsPath := filepath.Join(cfg.BaseCLIDir, "settings.json")
	raw, err := os.ReadFile(settingsPath)
	if err == nil {
		var data map[string]any
		if json.Unmarshal(raw, &data) == nil {
			if m, ok := data["model"].(string); ok && strings.TrimSpace(m) != "" {
				return strings.TrimSpace(m)
			}
		}
	}
	return "Gemini 4 Argon (Medium)"
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

	if st.CurrentModel != "" {
		for _, m := range DefaultCatalog {
			if m.ID == st.CurrentModel {
				return fmt.Sprintf("%s %s (`%s`)", m.Icon, m.Name, m.ID), m.ID, GetContextWindowForModel(m.ID)
			}
		}
		return fmt.Sprintf("`%s`", st.CurrentModel), st.CurrentModel, GetContextWindowForModel(st.CurrentModel)
	}

	cliDef := GetCLIDefaultModel(cfg)
	// Buscar si cliDef coincide con el Name o ID de algún modelo del catálogo
	for _, m := range DefaultCatalog {
		if strings.EqualFold(m.Name, cliDef) || strings.EqualFold(m.ID, cliDef) {
			return fmt.Sprintf("Por defecto AGY CLI: %s %s (`%s`)", m.Icon, m.Name, m.ID), m.ID, GetContextWindowForModel(m.ID)
		}
	}
	return fmt.Sprintf("Por defecto AGY CLI: **%s**", cliDef), cliDef, GetContextWindowForModel(cliDef)
}
