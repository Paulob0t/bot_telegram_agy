package chats

import (
	"encoding/json"
	"os"
	"os/exec"
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
