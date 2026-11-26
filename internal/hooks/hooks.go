package hooks

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"agente-bot/internal/config"
	"agente-bot/internal/desktop"
	"agente-bot/internal/state"
)

var (
	userReqRe  = regexp.MustCompile(`(?s)<USER_REQUEST>(.*?)</USER_REQUEST>`)
	anyTagRe   = regexp.MustCompile(`(?s)<[^>]+>.*?</[^>]+>|<[^>]+>`)
	fileLinkRe = regexp.MustCompile(`\[\x60?([^\]\x60]+)\x60?\]\((?:file|conversation)://[^\)]+\)`)
)

func RunAgyHook(cfg *config.Config, st *state.BotSessionState) {
	defer fmt.Println("{}")

	raw, err := io.ReadAll(os.Stdin)
	if err != nil || len(strings.TrimSpace(string(raw))) == 0 {
		return
	}
	var payload struct {
		Error          string   `json:"error"`
		WorkspacePaths []string `json:"workspacePaths"`
		TranscriptPath string   `json:"transcriptPath"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}

	wsName := "Workspace"
	if len(payload.WorkspacePaths) > 0 && payload.WorkspacePaths[0] != "" {
		wsName = filepath.Base(strings.TrimRight(payload.WorkspacePaths[0], `/\`))
	}

	if st.FeatureEnabled("notify_desktop") {
		desktop.Notify("Antigravity", wsName)
	}

	if !shouldNotifyMobile(st) {
		return
	}

	title, body := extractAgyTranscript(payload.TranscriptPath)
	if title == "" {
		title = "Tarea finalizada"
	}
	icon := "✓"
	body = formatFullResponse(body)
	if payload.Error != "" {
		icon = "⚠"
		body = strings.TrimSpace(body + "\n\nError: " + clip(payload.Error, 120))
	}
	notifyMobileApp(cfg, fmt.Sprintf("%s [AGY] %s · %s", icon, title, wsName), body)
}

func RunClaudeHook(cfg *config.Config, st *state.BotSessionState) {
	raw, _ := io.ReadAll(os.Stdin)
	if len(strings.TrimSpace(string(raw))) == 0 {
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	if active, _ := payload["stop_hook_active"].(bool); active {
		return
	}

	transcriptPath, _ := payload["transcript_path"].(string)
	if transcriptPath == "" {
		transcriptPath, _ = payload["transcriptPath"].(string)
	}
	cwd, _ := payload["cwd"].(string)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	wsName := filepath.Base(strings.TrimRight(cwd, `/\`))
	if wsName == "" || wsName == "." {
		wsName = "Workspace"
	}

	if st.FeatureEnabled("notify_desktop") {
		desktop.Notify("Claude", wsName)
	}

	if !shouldNotifyMobile(st) {
		return
	}

	title, body := extractClaudeTranscript(transcriptPath)
	if title == "" {
		title = "Tarea Claude lista"
	}
	engineLabel := "Claude"
	if !strings.Contains(transcriptPath, ".claude") {
		engineLabel = "AGY"
	}
	notifyMobileApp(cfg, fmt.Sprintf("✓ [%s] %s · %s", engineLabel, title, wsName), formatFullResponse(body))
}

// RunNotifyCLI sends a manual notification to the app:
// `agente-bot notify "Título" [cuerpo...]`, with optional piped stdin as extra body.
func RunNotifyCLI(cfg *config.Config, st *state.BotSessionState, args []string) int {
	if !st.FeatureEnabled("notify_mobile") {
		return 0
	}
	var stdinText string
	if info, err := os.Stdin.Stat(); err == nil && (info.Mode()&os.ModeCharDevice) == 0 {
		b, _ := io.ReadAll(os.Stdin)
		stdinText = strings.TrimSpace(string(b))
	}
	if len(args) == 0 && stdinText == "" {
		return 1
	}

	title := "Notificación de PC"
	if len(args) > 0 {
		title = args[0]
	}
	parts := []string{}
	if len(args) > 1 {
		parts = append(parts, strings.Join(args[1:], " "))
	}
	if stdinText != "" {
		parts = append(parts, clip(stdinText, 3500))
	}
	if !notifyMobileApp(cfg, title, strings.Join(parts, "\n\n")) {
		return 1
	}
	return 0
}

// shouldNotifyMobile skips runs started by the bot itself: the app already streams them live.
func shouldNotifyMobile(st *state.BotSessionState) bool {
	return os.Getenv("AGENTE_BOT_RUN") != "1" && st.FeatureEnabled("notify_mobile")
}

func extractAgyTranscript(path string) (string, string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	var userTask, lastResponse string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var data map[string]any
		if json.Unmarshal([]byte(line), &data) != nil {
			continue
		}
		t, _ := data["type"].(string)
		if t == "USER_INPUT" {
			content, _ := data["content"].(string)
			if m := userReqRe.FindStringSubmatch(content); len(m) > 1 {
				userTask = strings.TrimSpace(m[1])
			} else {
				userTask = strings.TrimSpace(content)
			}
		} else if t == "PLANNER_RESPONSE" {
			if content, ok := data["content"].(string); ok && strings.TrimSpace(content) != "" {
				lastResponse = strings.TrimSpace(content)
			}
		}
	}
	return shortenTitle(userTask), lastResponse
}

func extractClaudeTranscript(path string) (string, string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	var userTask, lastResponse string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var data map[string]any
		if json.Unmarshal([]byte(line), &data) != nil {
			continue
		}
		t, _ := data["type"].(string)
		if isMeta, _ := data["isMeta"].(bool); t == "user" && !isMeta {
			if msg, ok := data["message"].(map[string]any); ok {
				if content, ok := msg["content"].(string); ok {
					clean := strings.TrimSpace(anyTagRe.ReplaceAllString(content, ""))
					if clean != "" && !strings.HasPrefix(clean, "/") {
						userTask = clean
					}
				}
			}
		} else if t == "assistant" {
			if msg, ok := data["message"].(map[string]any); ok {
				if list, ok := msg["content"].([]any); ok {
					var parts []string
					for _, it := range list {
						if m, ok := it.(map[string]any); ok && m["type"] == "text" {
							if txt, ok := m["text"].(string); ok && strings.TrimSpace(txt) != "" {
								parts = append(parts, strings.TrimSpace(txt))
							}
						}
					}
					if len(parts) > 0 {
						lastResponse = strings.Join(parts, "\n")
					}
				}
			}
		}
	}
	return shortenTitle(userTask), lastResponse
}

func shortenTitle(s string) string {
	clean := strings.TrimSpace(anyTagRe.ReplaceAllString(s, ""))
	first := strings.Split(clean, "\n")[0]
	return clip(strings.TrimSpace(first), 50)
}

func formatFullResponse(resp string) string {
	if strings.TrimSpace(resp) == "" {
		return ""
	}
	text := fileLinkRe.ReplaceAllString(strings.TrimSpace(resp), "`$1`")
	return clip(text, 3500)
}

func clip(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return strings.TrimRight(string(runes[:limit]), " ,;:.") + "…"
}

// notifyMobileApp posts to the local mobile server, which relays to every connected app.
func notifyMobileApp(cfg *config.Config, title, body string) bool {
	tok := strings.TrimSpace(cfg.MobileWSToken)
	if tok == "" {
		return false
	}
	payload, _ := json.Marshal(map[string]string{
		"title": title,
		"body":  body,
	})
	req, err := http.NewRequest(http.MethodPost, cfg.LocalMobileURL()+"/notify", bytes.NewReader(payload))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
