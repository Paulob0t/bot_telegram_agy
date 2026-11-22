package projects

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"telegram-paulobot/internal/config"
	"telegram-paulobot/internal/utils"
)

type Project struct {
	Name    string
	Path    string
	ModTime time.Time
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
