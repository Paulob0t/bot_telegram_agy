package executor

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"encoding/json"
	"telegram-paulobot/internal/config"
)

// watchAgyTranscript tails the local AGY `transcript.jsonl` file (0 extra tokens, identical to Ctrl+O)
// and emits live Activity updates while the CLI subprocess is running.
func watchAgyTranscript(done <-chan struct{}, cfg *config.Config, convID string, emit func(Activity)) {
	brainDir := filepath.Join(cfg.HomeDir, ".gemini", "antigravity-cli", "brain")
	resolveLogFile := func() string {
		if convID != "" && convID != "NEW" && convID != "LATEST" {
			p := filepath.Join(brainDir, convID, ".system_generated", "logs", "transcript.jsonl")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		entries, err := os.ReadDir(brainDir)
		if err != nil {
			return ""
		}
		type cand struct {
			path string
			mod  time.Time
		}
		var list []cand
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(brainDir, e.Name(), ".system_generated", "logs", "transcript.jsonl")
			if st, err := os.Stat(p); err == nil {
				list = append(list, cand{p, st.ModTime()})
			}
		}
		if len(list) == 0 {
			return ""
		}
		sort.Slice(list, func(i, j int) bool { return list[i].mod.After(list[j].mod) })
		return list[0].path
	}

	var trackedPath string
	var offset int64

	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			p := resolveLogFile()
			if p == "" {
				continue
			}
			if p != trackedPath {
				trackedPath = p
				if st, err := os.Stat(p); err == nil {
					offset = st.Size()
				}
				continue
			}
			f, err := os.Open(p)
			if err != nil {
				continue
			}
			st, err := f.Stat()
			if err != nil || st.Size() <= offset {
				_ = f.Close()
				continue
			}
			_, _ = f.Seek(offset, 0)
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 64*1024), 512*1024)
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if line == "" {
					continue
				}
				var row map[string]any
				if err := json.Unmarshal([]byte(line), &row); err != nil {
					continue
				}
				content, _ := row["content"].(string)
				stepType, _ := row["type"].(string)
				status, _ := row["status"].(string)
				if act, ok := classifyAgyTranscriptLine(content, stepType, status); ok {
					emit(act)
				}
			}
			offset = st.Size()
			_ = f.Close()
		}
	}
}
