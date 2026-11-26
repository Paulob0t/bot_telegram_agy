package config

import (
	"bufio"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Config struct {
	MobileWSAddr           string
	MobileWSToken          string
	HomeDir                string
	ProjectsBaseDir        string
	ObsidianVaultDir       string
	BaseCLIDir             string
	DBPath                 string
	UsageCachePath         string
	StateCachePath         string
	ClaudeProjectsDir      string
	AgyBin                 string
	ClaudeBin              string
	LongMessageThreshold   int
	LongMessageWaitSeconds float64
	ChatIdleMinutes        float64
	IsWindows              bool
}

func Load(projectRoot string) (*Config, error) {
	loadDotenvFiles(projectRoot)

	mobileToken := strings.TrimSpace(os.Getenv("MOBILE_WS_TOKEN"))
	if mobileToken == "" {
		return nil, errors.New("falta MOBILE_WS_TOKEN en .env")
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
		MobileWSAddr:           envOr("MOBILE_WS_ADDR", ""),
		MobileWSToken:          mobileToken,
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
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		if realExe, err := filepath.EvalSymlinks(exe); err == nil {
			exeDir = filepath.Dir(realExe)
		} else {
			exeDir = filepath.Dir(exe)
		}
	}
	candidates := []string{
		filepath.Join(projectRoot, envName),
		filepath.Join(projectRoot, ".env"),
		filepath.Join(exeDir, envName),
		filepath.Join(exeDir, ".env"),
		// Hooks run from ~/.local/bin inside any project, so they need a fixed location.
		filepath.Join(home, ".config", "agente-bot", ".env"),
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

// DetectTailscaleIPv4 busca la IP privada de Tailscale (rango CGNAT 100.64.0.0/10 o interfaz tailscale0).
func DetectTailscaleIPv4() string {
	_, tailscaleNet, _ := net.ParseCIDR("100.64.0.0/10")
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil {
				continue
			}
			ip4 := ip.To4()
			if ip4 == nil {
				continue
			}
			if strings.HasPrefix(strings.ToLower(iface.Name), "tailscale") || (tailscaleNet != nil && tailscaleNet.Contains(ip4)) {
				return ip4.String()
			}
		}
	}
	return ""
}

// LocalMobileURL is the base URL hooks use to reach this machine's mobile server.
func (c *Config) LocalMobileURL() string {
	if addr := strings.TrimSpace(c.MobileWSAddr); addr != "" {
		if host, port, err := net.SplitHostPort(addr); err == nil {
			if host == "" || host == "0.0.0.0" || host == "::" {
				host = "127.0.0.1"
			}
			return "http://" + net.JoinHostPort(host, port)
		}
	}
	return "http://127.0.0.1:8080"
}
