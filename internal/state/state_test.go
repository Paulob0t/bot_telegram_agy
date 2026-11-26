package state

import (
	"path/filepath"
	"testing"

	"agente-bot/internal/config"
)

func TestStateSaveAndLoad(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		HomeDir:          tmp,
		ObsidianVaultDir: filepath.Join(tmp, "none"),
		StateCachePath:   filepath.Join(tmp, "state.json"),
		UsageCachePath:   filepath.Join(tmp, "usage.json"),
		ClaudeBin:        "claude",
	}

	s1 := New(cfg)
	s1.CurrentEngine = "claude"
	s1.CurrentModel = "gemini-4-argon-low"
	if err := s1.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	s2 := New(cfg)
	if s2.CurrentEngine != "claude" {
		t.Fatalf("engine = %q, want claude", s2.CurrentEngine)
	}
	if s2.CurrentModel != "gemini-4-argon-low" {
		t.Fatalf("model = %q, want gemini-4-argon-low", s2.CurrentModel)
	}
}

func TestNewSessionNeverWritesStateFile(t *testing.T) {
	cfg := &config.Config{HomeDir: t.TempDir()}
	cfg.StateCachePath = filepath.Join(cfg.HomeDir, "state.json")
	global := New(cfg)
	global.Features["mobile_approvals"] = true
	if err := global.Save(); err != nil {
		t.Fatal(err)
	}

	sess := global.NewSession()
	sess.Features["mobile_approvals"] = false
	sess.SetClaudeSession("abc", "x", "")

	reloaded := New(cfg)
	if !reloaded.FeatureEnabled("mobile_approvals") {
		t.Fatal("la sesión efímera sobrescribió el estado global")
	}
}
