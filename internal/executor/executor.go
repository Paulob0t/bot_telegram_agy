package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"telegram-paulobot/internal/chats"
	"telegram-paulobot/internal/config"
	"telegram-paulobot/internal/state"
)

// RunPrompt ejecuta agy o claude con --output-format stream-json y --dangerously-skip-permissions,
// mata el árbol de procesos completo (PGID) al cancelar y fija el chat/cwd tras ejecutar.
func RunPrompt(ctx context.Context, cfg *config.Config, st *state.BotSessionState, prompt string, onChunk func(partial string)) (string, error) {
	fullPrompt := prompt + "\n\n[Telegram: responde ultracave, máx 4 líneas cortas salvo que pida detalle]"

	var bin string
	var args []string
	var envExtra []string
	isClaude := st.CurrentEngine == "claude"
	usedConvID := st.CurrentConversationID

	if isClaude {
		bin = cfg.ClaudeBin
		args = []string{
			"-p", fullPrompt,
			"--output-format", "stream-json",
			"--verbose",
			"--dangerously-skip-permissions",
		}
		if st.ClaudeModel != "" {
			args = append(args, "--model", st.ClaudeModel)
		}
		if st.CurrentEffort != "" {
			eff := st.CurrentEffort
			if eff != "low" && eff != "medium" && eff != "high" {
				eff = "high"
			}
			args = append(args, "--effort", eff)
		}
		if st.ClaudeSessionID == "" || st.ClaudeSessionID == "LATEST" {
			args = append(args, "-c")
		} else if st.ClaudeSessionID != "NEW" {
			args = append(args, "-r", st.ClaudeSessionID)
		}
		envExtra = []string{"CLAUDE_FROM_TELEGRAM=1", "AGY_FROM_TELEGRAM=1"}
	} else {
		bin = cfg.AgyBin
		args = []string{
			"-p", fullPrompt,
			"--output-format", "stream-json",
			"--dangerously-skip-permissions",
		}
		if st.CurrentModel != "" {
			args = append(args, "--model", st.CurrentModel)
			lowMod := strings.ToLower(st.CurrentModel)
			hasSuffix := strings.HasSuffix(lowMod, "-low") ||
				strings.HasSuffix(lowMod, "-medium") ||
				strings.HasSuffix(lowMod, "-high") ||
				strings.HasSuffix(lowMod, "-xhigh")
			if !hasSuffix && st.CurrentEffort != "" {
				args = append(args, "--effort", st.CurrentEffort)
			}
		} else if st.CurrentEffort != "" {
			args = append(args, "--effort", st.CurrentEffort)
		}
		if st.CurrentConversationID == "" || st.CurrentConversationID == "LATEST" {
			args = append(args, "-c")
		} else if st.CurrentConversationID != "NEW" {
			args = append(args, "--conversation", st.CurrentConversationID)
		}
		envExtra = []string{"AGY_FROM_TELEGRAM=1", "CLAUDE_FROM_TELEGRAM=1"}
	}

	// Si estaba en NEW, el siguiente mensaje continuará el hilo recién creado
	if isClaude && st.ClaudeSessionID == "NEW" {
		st.ClaudeSessionID = ""
		st.ClaudeSessionTitle = "▸ Último chat Claude (-c)"
		_ = st.Save()
	} else if !isClaude && st.CurrentConversationID == "NEW" {
		st.CurrentConversationID = ""
		st.CurrentConversationTitle = "▸ Continuar último chat (-c)"
		_ = st.Save()
	}

	execCWD := st.CurrentCWD
	if info, err := os.Stat(execCWD); err != nil || !info.IsDir() {
		execCWD = cfg.HomeDir
	}

	cmd := exec.Command(bin, args...)
	cmd.Dir = execCWD
	cmd.Env = append(os.Environ(), envExtra...)
	configureProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("error iniciando %s: %w", bin, err)
	}

	doneCh := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			killProcessTree(cmd)
			_ = stdout.Close()
		case <-doneCh:
		}
	}()

	var accumulated strings.Builder
	var toolStatus string
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 2*1024*1024)

	lastEmit := time.Now()
	emitIfNeeded := func() {
		if onChunk == nil || time.Since(lastEmit) < 1200*time.Millisecond {
			return
		}
		body := strings.TrimSpace(accumulated.String())
		if body == "" && toolStatus == "" {
			return
		}
		rendered := body
		if toolStatus != "" {
			if rendered != "" {
				rendered += "\n\n" + toolStatus
			} else {
				rendered = toolStatus
			}
		}
		onChunk(rendered + "\n\n◷ ▍")
		lastEmit = time.Now()
	}

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "{") {
			accumulated.WriteString(line)
			accumulated.WriteByte('\n')
			emitIfNeeded()
			continue
		}

		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			accumulated.WriteString(line)
			accumulated.WriteByte('\n')
			emitIfNeeded()
			continue
		}

		evType, _ := ev["event"].(string)
		if evType == "" {
			evType, _ = ev["type"].(string)
		}

		switch evType {
		case "step_update":
			if step, ok := ev["step_update"].(map[string]any); ok {
				if delta, ok := step["text_delta"].(string); ok && delta != "" {
					accumulated.WriteString(delta)
					toolStatus = ""
				}
				if ts := extractStepToolStatus(step); ts != "" {
					toolStatus = ts
				} else if (step["thinking_delta"] != nil || step["thinking"] != nil) && accumulated.Len() == 0 {
					toolStatus = "◈ _Razonando…_"
				}
			}
		case "assistant":
			if msgObj, ok := ev["message"].(map[string]any); ok {
				if contentList, ok := msgObj["content"].([]any); ok {
					var parts []string
					for _, item := range contentList {
						blk, ok := item.(map[string]any)
						if !ok {
							continue
						}
						btype, _ := blk["type"].(string)
						if btype == "text" {
							if t, ok := blk["text"].(string); ok && t != "" {
								parts = append(parts, t)
							}
						} else if btype == "tool_use" {
							tname, _ := blk["name"].(string)
							if tname != "" {
								toolStatus = fmt.Sprintf("⚙ _Ejecutando `%s`…_", tname)
							}
						}
					}
					if len(parts) > 0 {
						accumulated.Reset()
						accumulated.WriteString(strings.Join(parts, "\n"))
						toolStatus = ""
					}
				}
			}
		case "result":
			switch res := ev["result"].(type) {
			case map[string]any:
				if finalResp, ok := res["response"].(string); ok && strings.TrimSpace(finalResp) != "" {
					accumulated.Reset()
					accumulated.WriteString(finalResp)
				}
			case string:
				if strings.TrimSpace(res) != "" {
					accumulated.Reset()
					accumulated.WriteString(strings.TrimSpace(res))
				}
			}
			if sid, ok := ev["session_id"].(string); ok && sid != "" && isClaude && st.ClaudeSessionID == "" {
				st.ClaudeSessionID = sid
				_ = st.Save()
			}
			toolStatus = ""
		}

		emitIfNeeded()
	}

	close(doneCh)
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	var waitErr error
	if ctx.Err() != nil {
		select {
		case waitErr = <-waitCh:
		case <-time.After(250 * time.Millisecond):
			killProcessTree(cmd)
			waitErr = ctx.Err()
		}
		return strings.TrimSpace(accumulated.String()), waitErr
	}
	waitErr = <-waitCh

	// Fijar conversación AGY y asociarla a su proyecto (cwd)
	if !isClaude {
		if usedConvID == "" || usedConvID == "NEW" || usedConvID == "LATEST" {
			latest := chats.ListAgyConversations(cfg, 1)
			if len(latest) > 0 {
				st.SetAgyConversation(latest[0].ConversationID, "💬 "+latest[0].Title, execCWD)
			}
		} else {
			st.SetAgyConversation(usedConvID, st.CurrentConversationTitle, execCWD)
		}
	}

	return strings.TrimSpace(accumulated.String()), waitErr
}

func extractStepToolStatus(step map[string]any) string {
	stateStr, _ := step["state"].(string)
	icon := "⚙"
	if strings.Contains(strings.ToUpper(stateStr), "DONE") {
		icon = "✓"
	}
	if rc, ok := step["run_command"].(map[string]any); ok {
		cmdLine, _ := rc["command_line"].(string)
		if cmdLine == "" {
			cmdLine, _ = rc["command"].(string)
		}
		if cmdLine != "" {
			return fmt.Sprintf("%s *Comando:* `%s`", icon, cleanSnippet(cmdLine, 60))
		}
	}
	for _, key := range []string{"edit_file", "create_file", "view_file"} {
		if m, ok := step[key].(map[string]any); ok {
			if fp, ok := m["file_path"].(string); ok && fp != "" {
				return fmt.Sprintf("%s *Archivo:* `%s`", icon, cleanSnippet(filepath.Base(fp), 48))
			}
		}
	}
	for _, k := range []string{"tool_summary", "toolSummary", "tool_action", "toolAction"} {
		if v, ok := step[k].(string); ok && v != "" {
			return fmt.Sprintf("%s _%s_", icon, cleanSnippet(v, 55))
		}
	}
	return ""
}

func cleanSnippet(s string, max int) string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "`", "'")), " ")
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max-1]) + "…"
	}
	return s
}
