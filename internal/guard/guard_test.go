package guard

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHoldPromptAndUploads(t *testing.T) {
	cg := New(30)

	// 1. Hold prompt with TempFiles
	hid := cg.Hold(12345, "analiza esto", "/tmp/a.jpg", "/tmp/b.pdf")
	held, ok := cg.Get(hid)
	if !ok || held.Prompt != "analiza esto" || len(held.TempFiles) != 2 {
		t.Fatalf("unexpected held prompt: %+v, ok=%v", held, ok)
	}
	popped, ok := cg.Pop(hid)
	if !ok || popped.ID != hid {
		t.Fatalf("expected pop to succeed")
	}
	if _, ok := cg.Get(hid); ok {
		t.Fatalf("expected prompt to be removed after Pop")
	}

	// 2. Hold upload (PDF/Photo)
	uid := cg.HoldUpload(PendingUpload{
		ChatID:   12345,
		FilePath: "/tmp/req.pdf",
		FileName: "req.pdf",
		Caption:  "modulo ventas",
		IsPDF:    true,
	})
	up, ok := cg.GetUpload(uid)
	if !ok || !up.IsPDF || up.FileName != "req.pdf" {
		t.Fatalf("unexpected upload: %+v", up)
	}
	if _, ok := cg.PopUpload(uid); !ok {
		t.Fatalf("expected PopUpload to succeed")
	}
	if _, ok := cg.GetUpload(uid); ok {
		t.Fatalf("expected upload removed after PopUpload")
	}
}

func TestPrepareUploadsDirPrunesOldFiles(t *testing.T) {
	tmpHome := t.TempDir()
	dir := PrepareUploadsDir(tmpHome)

	oldFile := filepath.Join(dir, "old.pdf")
	recentFile := filepath.Join(dir, "recent.jpg")
	_ = os.WriteFile(oldFile, []byte("old"), 0o644)
	_ = os.WriteFile(recentFile, []byte("new"), 0o644)

	past := time.Now().Add(-30 * time.Hour)
	_ = os.Chtimes(oldFile, past, past)

	_ = PrepareUploadsDir(tmpHome)

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("expected oldFile (>24h) to be pruned")
	}
	if _, err := os.Stat(recentFile); err != nil {
		t.Fatalf("expected recentFile to remain: %v", err)
	}
}
