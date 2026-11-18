package config

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Config struct {
	Token                   string
	AuthorizedUserID        int64
	HomeDir                 string
	ProjectsBaseDir         string
	ObsidianVaultDir        string
	BaseCLIDir              string
	DBPath                  string
	UsageCachePath          string
	StateCachePath          string
	ClaudeProjectsDir       string
	AgyBin                  string
	ClaudeBin               string
	LongMessageThreshold    int
	LongMessageWaitSeconds  float64
	ChatIdleMinutes         float64
	IsWindows               bool
}

func Load(projectRoot string) (*Config, error) {
	loadDotenvFiles(projectRoot)

	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	authStr := strings.TrimSpace(os.Getenv("TELEGRAM_AUTHORIZED_USER_ID"))
	authID, _ := strconv.ParseInt(authStr, 10, 64)
	if token == "" || authID == 0 {
		return nil, errors.New("faltan credenciales TELEGRAM_BOT_TOKEN o TELEGRAM_AUTHORIZED_USER_ID en .env")
	}

	isWin := runtime.GOOS == "windows"
	homeDir := envOr("BOT_HOME_DIR", userHome())

	defaultProjects := "/mnt/datos/Projects"
	defaultObsidian := "/mnt/datos/Documents/wicode"
	if isWin {
		defaultProjects = `C:\Proyectos`
		defaultObsidian = filepath.Join(homeDir, "Documents", "wicode")
	}

	baseCLIDir := filepath.Join(homeDir, ".gemini", "antigravity-cli")

	return &Config{
		Token:                  token,
		AuthorizedUserID:       authID,
		HomeDir:                homeDir,
		ProjectsBaseDir:        envOr("BOT_PROJECTS_DIR", defaultProjects),
		ObsidianVaultDir:       envOr("BOT_OBSIDIAN_DIR", defaultObsidian),
		BaseCLIDir:             baseCLIDir,
		DBPath:                 filepath.Join(baseCLIDir, "conversation_summaries.db"),
		UsageCachePath:         filepath.Join(baseCLIDir, "last_token_usage.json"),
		StateCachePath:         filepath.Join(baseCLIDir, "telegram_bot_state.json"),
		ClaudeProjectsDir:      filepath.Join(homeDir, ".claude", "projects"),
		AgyBin:                 resolveBinary("AGY_BIN", binName("agy", isWin), homeDir, isWin),
		ClaudeBin:              resolveBinary("CLAUDE_BIN", binName("claude", isWin), homeDir, isWin),
		LongMessageThreshold:   envInt("BOT_LONG_MESSAGE_THRESHOLD", 400),
		LongMessageWaitSeconds: envFloat("BOT_LONG_MESSAGE_WAIT_SECONDS", 4.0),
		ChatIdleMinutes:        envFloat("BOT_CHAT_IDLE_MINUTES", 30.0),
		IsWindows:              isWin,
	}, nil
}

func loadDotenvFiles(projectRoot string) {
	envName := envOr("BOT_ENV_FILE", ".env")
	home := userHome()
	candidates := []string{
		filepath.Join(projectRoot, envName),
		filepath.Join(projectRoot, ".env"),
		filepath.Join(home, ".config", "telegram-bot", ".env"),
	}
	for _, p := range candidates {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			k := strings.TrimSpace(parts[0])
			v := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
			if k != "" && os.Getenv(k) == "" {
				_ = os.Setenv(k, v)
			}
		}
		_ = f.Close()
	}
}

func resolveBinary(envVar, name, home string, isWin bool) string {
	if v := strings.TrimSpace(os.Getenv(envVar)); v != "" {
		return v
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	var fallbacks []string
	if isWin {
		fallbacks = []string{
			filepath.Join(home, "AppData", "Local", "agy", "bin", name),
			filepath.Join(home, ".local", "bin", name),
		}
	} else {
		fallbacks = []string{
			filepath.Join(home, ".local", "bin", name),
			filepath.Join(home, ".gemini", "antigravity-cli", "bin", name),
		}
	}
	for _, c := range fallbacks {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	return name
}

func binName(base string, isWin bool) string {
	if isWin {
		return base + ".exe"
	}
	return base
}

func userHome() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return os.Getenv("HOME")
	}
	return h
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(k string, def float64) float64 {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return def
}
