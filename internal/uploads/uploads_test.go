package uploads

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrepareDirPrunesOldFiles(t *testing.T) {
	tmpHome := t.TempDir()
	dir := PrepareDir(tmpHome)

	oldFile := filepath.Join(dir, "old.pdf")
	recentFile := filepath.Join(dir, "recent.jpg")
	_ = os.WriteFile(oldFile, []byte("old"), 0o644)
	_ = os.WriteFile(recentFile, []byte("new"), 0o644)

	past := time.Now().Add(-30 * time.Hour)
	_ = os.Chtimes(oldFile, past, past)

	_ = PrepareDir(tmpHome)

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("expected oldFile (>24h) to be pruned")
	}
	if _, err := os.Stat(recentFile); err != nil {
		t.Fatalf("expected recentFile to remain: %v", err)
	}
}
