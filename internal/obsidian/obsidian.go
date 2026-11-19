package obsidian

import (
	"fmt"
	"io"
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

type VaultProject struct {
	Name string
	Path string
}

var ProjectCodeMap = map[string]string{
	"Stover": "/mnt/datos/Projects/Stover",
	"Wicode": "/mnt/datos/Projects/wicode",
}

func ListVaultProjects(cfg *config.Config) []VaultProject {
	entries, err := os.ReadDir(cfg.ObsidianVaultDir)
	if err != nil {
		return nil
	}
	var out []VaultProject
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, VaultProject{
			Name: e.Name(),
			Path: filepath.Join(cfg.ObsidianVaultDir, e.Name()),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func SaveFileToDir(srcPath, destDir, fileName string) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	safeName := filepath.Base(fileName)
	if safeName == "" || safeName == "." {
		safeName = filepath.Base(srcPath)
	}
	destPath := filepath.Join(destDir, safeName)
	if filepath.Clean(srcPath) == filepath.Clean(destPath) {
		return destPath, nil
	}
	in, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer in.Close()

	out, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return "", err
	}
	return destPath, nil
}

func SavePDFToVaultProject(cfg *config.Config, srcPath, projName, fileName string) (string, error) {
	destDir := filepath.Join(cfg.ObsidianVaultDir, projName, "PDF")
	return SaveFileToDir(srcPath, destDir, fileName)
}

func BuildWicodeGeneratorPrompt(cfg *config.Config, projName, pdfDestPath, currentCWD string) (string, string) {
	targetCWD := currentCWD
	if mapped, ok := ProjectCodeMap[projName]; ok {
		if info, err := os.Stat(mapped); err == nil && info.IsDir() {
			targetCWD = mapped
		}
	}
	vaultProjDir := filepath.Join(cfg.ObsidianVaultDir, projName)
	prompt := fmt.Sprintf(
		"Usa la skill `wicode-prompt-generator` para procesar el PDF `%s` del proyecto `%s` (ubicado en `%s`). "+
			"Ejecuta Fase 1 (transcribir PDF a nota Markdown de contexto en `%s`) y Fase 2 (generar prompts técnicos T01..TXX en `%s/Prompts/`).",
		pdfDestPath, projName, vaultProjDir, vaultProjDir, vaultProjDir,
	)
	return prompt, targetCWD
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
