// Package uploads manages the temporary folder for files sent from the app.
package uploads

import (
	"os"
	"path/filepath"
	"time"
)

// maxAge is how long an uploaded file is kept before it is pruned.
const maxAge = 24 * time.Hour

// PrepareDir creates the uploads folder, prunes files older than maxAge and returns its path.
func PrepareDir(homeDir string) string {
	dir := filepath.Join(homeDir, ".cache", "telegram-paulobot", "uploads")
	_ = os.MkdirAll(dir, 0o755)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dir
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return dir
}
