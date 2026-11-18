package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"telegram-paulobot/internal/config"
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

func PickIconForModel(modelID string) string {
	m := strings.ToLower(modelID)
	switch {
	case strings.Contains(m, "opus"):
		return "◆"
	case strings.Contains(m, "hydrogen"):
		if strings.Contains(m, "xhigh") {
			return "◈"
		}
		return "◇"
	case strings.Contains(m, "argon"), strings.Contains(m, "sonnet"), strings.Contains(m, "claude"):
		return "●"
	case strings.Contains(m, "flash"):
		return "▸"
	default:
		return "○"
	}
}

func GetContextWindowForModel(modelID string) int {
	m := strings.ToLower(modelID)
	switch {
	case strings.Contains(m, "deepseek"):
		return 64_000
	case strings.Contains(m, "claude"), strings.Contains(m, "sonnet"), strings.Contains(m, "opus"):
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
	if err != nil {
		return ""
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return ""
	}
	if m, ok := data["model"].(string); ok {
		return strings.TrimSpace(m)
	}
	return ""
}
