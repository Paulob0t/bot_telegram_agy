package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agente-bot/internal/config"
	"agente-bot/internal/state"
)

// approvalTools are the Claude tools that change the machine; read-only tools never ask.
var approvalTools = map[string]bool{
	"Bash":         true,
	"Write":        true,
	"Edit":         true,
	"MultiEdit":    true,
	"NotebookEdit": true,
}

// RunClaudePermissionHook is a Claude Code PreToolUse hook. With the "mobile_approvals"
// feature on, it asks the phone to approve risky tool calls and prints the decision.
// Printing nothing leaves Claude's normal permission flow untouched.
func RunClaudePermissionHook(cfg *config.Config, st *state.BotSessionState) {
	if !st.FeatureEnabled("mobile_approvals") {
		return
	}
	raw, _ := io.ReadAll(os.Stdin)
	var payload struct {
		ToolName  string         `json:"tool_name"`
		ToolInput map[string]any `json:"tool_input"`
		Cwd       string         `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || !approvalTools[payload.ToolName] {
		return
	}
	tok := strings.TrimSpace(cfg.MobileWSToken)
	if tok == "" {
		return
	}

	reqBody, _ := json.Marshal(map[string]string{
		"tool":      payload.ToolName,
		"detail":    describeToolInput(payload.ToolName, payload.ToolInput),
		"workspace": filepath.Base(strings.TrimRight(payload.Cwd, `/\`)),
	})
	req, err := http.NewRequest(http.MethodPost, cfg.LocalMobileURL()+"/permission", bytes.NewReader(reqBody))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	// Server waits up to 120s; leave headroom so the server answers "timeout" first.
	client := &http.Client{Timeout: 130 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var out struct {
		Decision string `json:"decision"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&out) != nil {
		return
	}

	var decision, reason string
	switch out.Decision {
	case "allow":
		decision, reason = "allow", "Aprobado desde el móvil"
	case "deny":
		decision, reason = "deny", "Rechazado desde el móvil"
	case "timeout":
		decision, reason = "deny", "Sin respuesta del móvil en 120s"
	default: // no_clients: nobody to ask, fall back to Claude's own flow
		return
	}
	result, _ := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]string{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       decision,
			"permissionDecisionReason": reason,
		},
	})
	fmt.Println(string(result))
}

func describeToolInput(tool string, in map[string]any) string {
	str := func(k string) string { v, _ := in[k].(string); return v }
	switch tool {
	case "Bash":
		return clip(str("command"), 600)
	case "Write", "Edit", "MultiEdit":
		return str("file_path")
	case "NotebookEdit":
		return str("notebook_path")
	}
	b, _ := json.Marshal(in)
	return clip(string(b), 600)
}
