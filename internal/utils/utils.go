package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func FormatUptime(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%dm %ds", seconds/60, seconds%60)
	}
	return fmt.Sprintf("%dh %dm", seconds/3600, (seconds%3600)/60)
}

// GetGitBranch lee .git/HEAD directo sin lanzar subprocesos.
func GetGitBranch(dir string) string {
	if dir == "" {
		return ""
	}
	headPath := filepath.Join(dir, ".git", "HEAD")
	raw, err := os.ReadFile(headPath)
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(raw))
	if strings.HasPrefix(ref, "ref: refs/heads/") {
		return strings.TrimPrefix(ref, "ref: refs/heads/")
	}
	if len(ref) >= 7 {
		return ref[:7]
	}
	return ref
}

// SanitizeStreamingMarkdown trunca y cierra bloques ``` abiertos para que Telegram no falle al renderizar.
func SanitizeStreamingMarkdown(text string, maxLen int) string {
	if text == "" {
		return ""
	}
	if maxLen <= 0 {
		maxLen = 3800
	}
	runes := []rune(text)
	if len(runes) > maxLen {
		text = string(runes[:maxLen])
	}
	if strings.HasSuffix(text, "``") && !strings.HasSuffix(text, "```") {
		text = strings.TrimSuffix(text, "``")
	}
	if strings.Count(text, "```")%2 == 1 {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += "```"
	}
	return text
}
