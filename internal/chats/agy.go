package chats

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"telegram-paulobot/internal/config"
)

type AgyConversation struct {
	ConversationID   string `json:"conversation_id"`
	Title            string `json:"title"`
	LastModifiedTime string `json:"last_modified_time"`
}

func ListAgyConversations(cfg *config.Config, limit int) []AgyConversation {
	if _, err := os.Stat(cfg.DBPath); err != nil {
		return nil
	}
	query := "SELECT conversation_id, title, last_modified_time FROM conversation_summaries WHERE title != '' ORDER BY last_modified_time DESC"
	cmd := exec.Command("sqlite3", "-json", cfg.DBPath, query)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	var rows []AgyConversation
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil
	}
	for i := range rows {
		rows[i].Title = strings.Join(strings.Fields(rows[i].Title), " ")
	}
	if limit > 0 && len(rows) > limit {
		return rows[:limit]
	}
	return rows
}

func GetAgyConversationTitle(cfg *config.Config, convID string) string {
	for _, c := range ListAgyConversations(cfg, 0) {
		if c.ConversationID == convID && c.Title != "" {
			return c.Title
		}
	}
	if len(convID) >= 8 {
		return "Chat (" + convID[:8] + "…)"
	}
	return "Chat AGY"
}

// GetAgyConversationPreview extracts the last few user/assistant messages from
// the local agy transcript.jsonl without spending any LLM tokens.
func GetAgyConversationPreview(convID string, maxMsgs int) string {
	home, err := os.UserHomeDir()
	if err != nil || convID == "" {
		return ""
	}
	p := filepath.Join(home, ".gemini", "antigravity-cli", "brain", convID, ".system_generated", "logs", "transcript.jsonl")
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()

	type entry struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	var msgs []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for sc.Scan() {
		var e entry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		text := strings.Join(strings.Fields(e.Content), " ")
		if text == "" {
			continue
		}
		if len([]rune(text)) > 140 {
			text = string([]rune(text)[:140]) + "…"
		}
		switch e.Type {
		case "USER_INPUT":
			msgs = append(msgs, "Tú: "+text)
		case "PLANNER_RESPONSE":
			msgs = append(msgs, "IA: "+text)
		}
	}
	if len(msgs) == 0 {
		return ""
	}
	if maxMsgs > 0 && len(msgs) > maxMsgs {
		msgs = msgs[len(msgs)-maxMsgs:]
	}
	return strings.Join(msgs, "\n")
}
