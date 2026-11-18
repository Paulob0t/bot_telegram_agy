package obsidian

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"telegram-paulobot/internal/config"
)

type Note struct {
	Name    string
	RelPath string
	AbsPath string
	ModTime time.Time
}

func ListMarkdownNotes(cfg *config.Config, limit int) []Note {
	if _, err := os.Stat(cfg.ObsidianVaultDir); err != nil {
		return nil
	}
	var notes []Note
	_ = filepath.WalkDir(cfg.ObsidianVaultDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			rel, _ := filepath.Rel(cfg.ObsidianVaultDir, path)
			var mt time.Time
			if info, err := d.Info(); err == nil {
				mt = info.ModTime()
			}
			notes = append(notes, Note{
				Name:    strings.TrimSuffix(d.Name(), ".md"),
				RelPath: rel,
				AbsPath: path,
				ModTime: mt,
			})
		}
		return nil
	})
	sort.Slice(notes, func(i, j int) bool {
		return notes[i].ModTime.After(notes[j].ModTime)
	})
	if limit > 0 && len(notes) > limit {
		return notes[:limit]
	}
	return notes
}

func ReadNote(absPath string, maxChars int) (string, error) {
	b, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	s := string(b)
	if maxChars > 0 {
		runes := []rune(s)
		if len(runes) > maxChars {
			s = string(runes[:maxChars]) + "\n…"
		}
	}
	return s, nil
}
