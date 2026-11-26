package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"agente-bot/internal/chats"
	"agente-bot/internal/config"
	"agente-bot/internal/state"
)

type StreamCallbacks struct {
	OnState func(state string)
	OnChunk func(text, toolStatus string, done bool)
	// OnActivity reports granular tool activity (CODING_EDIT, EXECUTING, ...).
	OnActivity func(a Activity)
}

// RunPrompt mantiene compatibilidad hacia atrás con callbacks simples de texto parcial.
// botRunEnv marks CLI runs started by the bot so the Stop hooks skip a duplicate
// notification: the app already streams that reply live.
const botRunEnv = "AGENTE_BOT_RUN=1"

// backgroundPrefix starts the label shown while AGY waits on a background task.
const backgroundPrefix = "En segundo plano"

func RunPrompt(ctx context.Context, cfg *config.Config, st *state.BotSessionState, prompt string, onChunk func(partial string)) (string, error) {
	return RunPromptWithEvents(ctx, cfg, st, prompt, StreamCallbacks{
		OnChunk: func(text, toolStatus string, done bool) {
			if onChunk == nil || done {
				return
			}
			rendered := text
			if toolStatus != "" {
				if rendered != "" {
					rendered += "\n\n" + toolStatus
				} else {
					rendered = toolStatus
				}
			}
			if rendered != "" {
				onChunk(rendered + "\n\n◷ ▍")
			}
		},
	})
}

// RunPromptWithEvents ejecuta agy o claude con --output-format stream-json y --dangerously-skip-permissions,
// emitiendo transiciones de estado ("thinking", "speaking", "idle") y fragmentos para la app móvil.
func RunPromptWithEvents(ctx context.Context, cfg *config.Config, st *state.BotSessionState, prompt string, cb StreamCallbacks) (string, error) {
	currentState := ""
	emitState := func(next string) {
		if next == "" || next == currentState {
			return
		}
		currentState = next
		if cb.OnState != nil {
			cb.OnState(next)
		}
	}
	defer emitState("idle")

	// The transcript watcher emits from its own goroutine.
	var activityMu sync.Mutex
	var lastActivity Activity
	var lastTool string // last tool label, names the task AGY waits on in background
	emitActivity := func(a Activity) {
		activityMu.Lock()
		defer activityMu.Unlock()
		if a.State == ActivityExecuting && !strings.HasPrefix(a.Label, backgroundPrefix) {
			lastTool = a.Label
		}
		if cb.OnActivity == nil || a == lastActivity {
			return
		}
		lastActivity = a
		cb.OnActivity(a)
	}
	backgroundLabel := func() string {
		activityMu.Lock()
		defer activityMu.Unlock()
		if lastTool == "" {
			return backgroundPrefix
		}
		return backgroundPrefix + " · " + lastTool
	}

	fullPrompt := prompt

	var bin string
	var args []string
	var envExtra []string
	isClaude := st.CurrentEngine == "claude"
	usedConvID := st.CurrentConversationID
	runStart := time.Now().Add(-time.Second)

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
		envExtra = []string{botRunEnv}
	} else {
		bin = cfg.AgyBin
		args = []string{
			"-p", fullPrompt,
			"--output-format", "stream-json",
			"--dangerously-skip-permissions",
		}
		activeModel := st.CurrentModel
		if activeModel == "" || activeModel == "DEFAULT" {
			activeModel = "gemini-4-hydrogen-low"
		}
		args = append(args, "--model", activeModel)
		lowMod := strings.ToLower(activeModel)
		hasSuffix := strings.HasSuffix(lowMod, "-low") ||
			strings.HasSuffix(lowMod, "-medium") ||
			strings.HasSuffix(lowMod, "-high") ||
			strings.HasSuffix(lowMod, "-xhigh")
		if !hasSuffix && st.CurrentEffort != "" {
			args = append(args, "--effort", st.CurrentEffort)
		}
		convID := st.CurrentConversationID
		if convID != "" && convID != "LATEST" && convID != "NEW" && !chats.AgyConversationExists(cfg, convID) {
			// Deleted conversation: AGY would warn and ignore the flag, so start clean instead.
			st.CurrentConversationID = "NEW"
			usedConvID = "NEW"
		}
		if st.CurrentConversationID == "" || st.CurrentConversationID == "LATEST" {
			args = append(args, "-c")
		} else if st.CurrentConversationID != "NEW" {
			args = append(args, "--conversation", st.CurrentConversationID)
		}
		envExtra = []string{botRunEnv}
	}

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
	emitState("thinking")

	doneCh := make(chan struct{})
	if !isClaude {
		go watchAgyTranscript(doneCh, cfg, usedConvID, emitActivity)
	}
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
		if cb.OnChunk == nil || time.Since(lastEmit) < 800*time.Millisecond {
			return
		}
		body := strings.TrimSpace(accumulated.String())
		if body == "" && toolStatus == "" {
			return
		}
		cb.OnChunk(body, toolStatus, false)
		lastEmit = time.Now()
	}

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "root agent idle; waiting for") {
			emitState("thinking")
			emitActivity(Activity{State: ActivityExecuting, Label: backgroundLabel()})
			continue
		}
		if !strings.HasPrefix(line, "{") {
			accumulated.WriteString(line)
			accumulated.WriteByte('\n')
			emitState("speaking")
			emitIfNeeded()
			continue
		}

		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			accumulated.WriteString(line)
			accumulated.WriteByte('\n')
			emitState("speaking")
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
					emitState("speaking")
					emitActivity(Activity{State: ActivityWrappingUp, Label: "Casi listo · redactando respuesta"})
				}
				if act, ok := classifyAgyStep(step); ok {
					emitActivity(act)
				}
				if ts := extractStepToolStatus(step); ts != "" {
					toolStatus = ts
					emitState("thinking")
				} else if (step["thinking_delta"] != nil || step["thinking"] != nil) && accumulated.Len() == 0 {
					toolStatus = "Analizando contexto"
					emitState("thinking")
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
								input, _ := blk["input"].(map[string]any)
								act := classifyClaudeTool(tname, input)
								toolStatus = act.Label
								emitActivity(act)
								emitState("thinking")
							}
						}
					}
					if len(parts) > 0 {
						accumulated.Reset()
						accumulated.WriteString(strings.Join(parts, "\n"))
						toolStatus = ""
						emitState("speaking")
						emitActivity(Activity{State: ActivityWrappingUp, Label: "Casi listo · redactando respuesta"})
					}
				}
			}
		case "result":
			switch res := ev["result"].(type) {
			case map[string]any:
				if finalResp, ok := res["response"].(string); ok && strings.TrimSpace(finalResp) != "" {
					accumulated.Reset()
					accumulated.WriteString(finalResp)
					emitState("speaking")
				}
			case string:
				if strings.TrimSpace(res) != "" {
					accumulated.Reset()
					accumulated.WriteString(strings.TrimSpace(res))
					emitState("speaking")
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

	if !isClaude {
		if usedConvID == "" || usedConvID == "NEW" || usedConvID == "LATEST" {
			if id := chats.NewestAgyConversationSince(cfg, runStart); id != "" {
				st.SetAgyConversation(id, "💬 "+chats.GetAgyConversationTitle(cfg, id), execCWD)
			}
		} else {
			st.SetAgyConversation(usedConvID, st.CurrentConversationTitle, execCWD)
		}
	}

	finalOut := strings.TrimSpace(accumulated.String())
	if cb.OnChunk != nil {
		cb.OnChunk(finalOut, "", true)
	}
	return finalOut, waitErr
}

func extractStepToolStatus(step map[string]any) string {
	if act, ok := classifyAgyStep(step); ok {
		return act.Label
	}
	for _, k := range []string{"tool_summary", "toolSummary", "tool_action", "toolAction"} {
		if v, ok := step[k].(string); ok && v != "" {
			return cleanSnippet(v, 55)
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
