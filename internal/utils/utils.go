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
	// Balancear backticks simples o negritas abiertas fuera de fences
	lastFence := strings.LastIndex(text, "```")
	tail := text
	if lastFence != -1 {
		tail = text[lastFence+3:]
	}
	if strings.Count(tail, "`")%2 == 1 {
		text += "`"
		return text
	}
	if strings.Count(tail, "**")%2 == 1 {
		text += "**"
	} else if strings.Count(strings.ReplaceAll(tail, "**", ""), "*")%2 == 1 {
		text += "*"
	}
	return text
}

// SplitMarkdownChunks divide respuestas largas en bloques <= maxChars cerrando fences ``` si quedan abiertos.
func SplitMarkdownChunks(text string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = 3850
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return []string{text}
	}
	var chunks []string
	for len(runes) > 0 {
		if len(runes) <= maxChars {
			chunks = append(chunks, string(runes))
			break
		}
		cut := maxChars
		for i := maxChars; i > maxChars-600 && i > 0; i-- {
			if runes[i-1] == '\n' {
				cut = i
				break
			}
		}
		part := string(runes[:cut])
		runes = runes[cut:]
		if strings.Count(part, "```")%2 == 1 {
			part += "\n```"
			runes = append([]rune("```\n"), runes...)
		}
		chunks = append(chunks, strings.TrimSpace(part))
	}
	return chunks
}
