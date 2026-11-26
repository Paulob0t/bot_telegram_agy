package projects

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agente-bot/internal/config"
	"agente-bot/internal/utils"
)

type Project struct {
	Name    string
	Path    string
	ModTime time.Time
}

func hasProjectMarker(dir string) bool {
	for _, marker := range []string{".git", "CLAUDE.md", "AGENTS.md", ".project"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}

func ListAvailableProjects(cfg *config.Config) []Project {
	entries, err := os.ReadDir(cfg.ProjectsBaseDir)
	if err != nil {
		return nil
	}
	var out []Project
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(cfg.ProjectsBaseDir, e.Name())
		var mt time.Time
		if info, err := e.Info(); err == nil {
			mt = info.ModTime()
		}
		if !hasProjectMarker(full) {
			if subs, err := os.ReadDir(full); err == nil {
				addedSub := false
				for _, sub := range subs {
					if !sub.IsDir() || strings.HasPrefix(sub.Name(), ".") {
						continue
					}
					subFull := filepath.Join(full, sub.Name())
					subMt := mt
					if info, err := sub.Info(); err == nil {
						subMt = info.ModTime()
					}
					if hasProjectMarker(subFull) {
						out = append(out, Project{
							Name:    e.Name() + "/" + sub.Name(),
							Path:    subFull,
							ModTime: subMt,
						})
						addedSub = true
						continue
					}
					if subs3, err := os.ReadDir(subFull); err == nil {
						for _, s3 := range subs3 {
							if !s3.IsDir() || strings.HasPrefix(s3.Name(), ".") {
								continue
							}
							s3Full := filepath.Join(subFull, s3.Name())
							if hasProjectMarker(s3Full) {
								s3Mt := subMt
								if info, err := s3.Info(); err == nil {
									s3Mt = info.ModTime()
								}
								out = append(out, Project{
									Name:    e.Name() + "/" + sub.Name() + "/" + s3.Name(),
									Path:    s3Full,
									ModTime: s3Mt,
								})
								addedSub = true
							}
						}
					}
				}
				if addedSub {
					continue
				}
			}
		}
		out = append(out, Project{
			Name:    e.Name(),
			Path:    full,
			ModTime: mt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ModTime.After(out[j].ModTime)
	})
	if home, err := os.UserHomeDir(); err == nil && home != "" && home != cfg.ProjectsBaseDir {
		out = append([]Project{{
			Name: "~ (" + filepath.Base(home) + ")",
			Path: home,
		}}, out...)
	}
	return out
}

func FormatActiveCWDSummary(cwd string) string {
	short := filepath.Base(cwd)
	if short == "" || short == "." {
		short = cwd
	}
	branch := utils.GetGitBranch(cwd)
	if branch != "" {
		return fmt.Sprintf("◆ **%s** · `%s`\n`%s`", short, branch, cwd)
	}
	return fmt.Sprintf("◆ **%s**\n`%s`", short, cwd)
}
