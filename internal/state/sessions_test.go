package state

import (
	"path/filepath"
	"testing"

	"agente-bot/internal/config"
)

func TestSessionStoreSurvivesRestart(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		HomeDir:          tmp,
		ObsidianVaultDir: filepath.Join(tmp, "none"),
		StateCachePath:   filepath.Join(tmp, "state.json"),
		UsageCachePath:   filepath.Join(tmp, "usage.json"),
	}
	global := New(cfg)

	store := NewSessionStore(global)
	if store.Get("default") != global {
		t.Fatal("default session must share the global state")
	}
	store.Get("chat-1").SetAgyConversation("conv-123", "Chat", tmp)

	restored := NewSessionStore(New(cfg)).Get("chat-1")
	if restored.CurrentConversationID != "conv-123" || restored.CurrentCWD != tmp {
		t.Fatalf("restored conv=%q cwd=%q", restored.CurrentConversationID, restored.CurrentCWD)
	}
	if New(cfg).CurrentConversationID == "conv-123" {
		t.Fatal("chat state leaked into the global state file")
	}
}
