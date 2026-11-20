package bot

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"telegram-paulobot/internal/agent"
	"telegram-paulobot/internal/telegram"
	"telegram-paulobot/internal/utils"
)

type queuedJob struct {
	id      string
	chatID  int64
	prompt  string
	temp    []string
	noticeM int64
}

func (a *App) submitWithGuard(chatID int64, prompt string, tempFiles []string) {
	need, reason := a.Guard.NeedsConfirmation(
		a.State.FeatureEnabled("chat_guard"),
		a.State.CurrentEngine,
		a.State.CurrentConversationID,
		a.State.ClaudeSessionID,
	)
	if !need {
		a.Guard.Touch()
		a.enqueuePrompt(chatID, prompt, tempFiles)
		return
	}

	holdID := a.Guard.Hold(chatID, prompt, tempFiles...)
	text, kb := a.buildChatGuardView(holdID, prompt, reason)
	_, _ = a.TG.SendMessage(chatID, text, kb)
}

func (a *App) enqueuePrompt(chatID int64, prompt string, tempFiles []string) {
	a.runMu.Lock()
	jobID := fmt.Sprintf("%x", time.Now().UnixNano()&0xffffffff)
	job := queuedJob{id: jobID, chatID: chatID, prompt: prompt, temp: tempFiles}
	if a.isBusy {
		pos := len(a.jobQueue) + 1
		preview := strings.Join(strings.Fields(prompt), " ")
		if len([]rune(preview)) > 75 {
			preview = string([]rune(preview)[:75]) + "…"
		}
		kb := &telegram.InlineKeyboard{
			InlineKeyboard: [][]telegram.InlineButton{
				{
					{Text: "⚡ Interrumpir y ejecutar ya", CallbackData: "q_force:" + jobID},
					{Text: "🗑️ Quitar de cola", CallbackData: "q_drop:" + jobID},
				},
				{
					{Text: "🛑 Cancelar todo", CallbackData: "q_cancel_all"},
				},
			},
		}
		noticeID, _ := a.TG.SendMessage(
			chatID,
			fmt.Sprintf("⏳ **Ocupado** · Mensaje en cola (`#%d` • 📂 `%s`):\n`%s`", pos, a.State.GetCWDShortName(a.State.CurrentCWD), preview),
			kb,
		)
		job.noticeM = noticeID
		a.jobQueue = append(a.jobQueue, job)
		a.runMu.Unlock()
		return
	}
	a.isBusy = true
	a.runMu.Unlock()

	go a.workerLoop(job)
}

func (a *App) workerLoop(first queuedJob) {
	curr := first
	for {
		a.executeJob(curr)

		a.runMu.Lock()
		if len(a.jobQueue) == 0 {
			a.isBusy = false
			a.runCancel = nil
			a.runMu.Unlock()
			return
		}
		curr = a.jobQueue[0]
		a.jobQueue = a.jobQueue[1:]
		a.runMu.Unlock()
	}
}

func (a *App) executeJob(job queuedJob) {
	cwdShort := a.State.GetCWDShortName(a.State.CurrentCWD)
	chatTitle := a.State.CurrentConversationTitle
	if a.State.CurrentEngine == "claude" {
		chatTitle = a.State.ClaudeSessionTitle
	}
	cancelKB := &telegram.InlineKeyboard{
		InlineKeyboard: [][]telegram.InlineButton{
			{{Text: "🛑 Cancelar ejecución", CallbackData: "q_cancel"}},
		},
	}
	header := fmt.Sprintf("⚡ **[%s]** • 📂 `%s`\n⏳ _Pensando…_", chatTitle, cwdShort)

	msgID := job.noticeM
	if msgID != 0 {
		a.TG.EditMessage(job.chatID, msgID, header, cancelKB)
	} else {
		msgID, _ = a.TG.SendMessage(job.chatID, header, cancelKB)
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.runMu.Lock()
	a.runCancel = cancel
	a.runMu.Unlock()
	defer cancel()

	out, err := a.Agent.Execute(ctx, agent.Request{
		UserID: fmt.Sprintf("%d", job.chatID),
		Text:   job.prompt,
		Source: "telegram",
	}, func(ev agent.Event) {
		if ctx.Err() != nil || ev.Type != agent.EventTypeChunk || ev.Done || msgID == 0 {
			return
		}
		rendered := ev.Text
		if ev.ToolStatus != "" {
			if rendered != "" {
				rendered += "\n\n" + ev.ToolStatus
			} else {
				rendered = ev.ToolStatus
			}
		}
		if rendered != "" {
			a.TG.EditMessage(job.chatID, msgID, utils.SanitizeStreamingMarkdown(rendered+"\n\n◷ ▍", 3750), cancelKB)
		}
	})

	if ctx.Err() != nil {
		if out != "" {
			out = utils.SanitizeStreamingMarkdown(out, 3700) + "\n\n🛑 **Ejecución cancelada.**"
		} else {
			out = "🛑 **Ejecución cancelada por el usuario.**"
		}
		if msgID != 0 {
			a.TG.EditMessage(job.chatID, msgID, out, nil)
		}
		return
	}

	if err != nil && out == "" {
		out = fmt.Sprintf("✕ Error: `%v`", err)
	}
	if out == "" {
		out = "✓ Tarea completada."
	}

	finalTitle := a.State.CurrentConversationTitle
	if a.State.CurrentEngine == "claude" {
		finalTitle = a.State.ClaudeSessionTitle
	}
	footer := fmt.Sprintf("\n\n— 💬 _%s_ • 📂 `%s`", strings.TrimLeft(finalTitle, "💬🟠▸🔄➕ "), a.State.GetCWDShortName(a.State.CurrentCWD))

	chunks := utils.SplitMarkdownChunks(out, 3800)
	if len([]rune(chunks[len(chunks)-1]))+len([]rune(footer)) <= 3950 {
		chunks[len(chunks)-1] += footer
	} else {
		chunks = append(chunks, strings.TrimSpace(footer))
	}

	if msgID != 0 {
		a.TG.EditMessage(job.chatID, msgID, chunks[0], nil)
	} else {
		_, _ = a.TG.SendMessage(job.chatID, chunks[0], nil)
	}
	for _, extra := range chunks[1:] {
		_, _ = a.TG.SendMessage(job.chatID, extra, nil)
	}
}

func (a *App) cancelActiveExecution(chatID int64, clearAll bool) {
	a.runMu.Lock()
	cleared := 0
	if clearAll && len(a.jobQueue) > 0 {
		cleared = len(a.jobQueue)
		for _, q := range a.jobQueue {
			for _, tf := range q.temp {
				_ = os.Remove(tf)
			}
		}
		a.jobQueue = nil
	}
	cancelFn := a.runCancel
	a.runMu.Unlock()

	agentCancelled := a.Agent.CancelActive()
	if cancelFn != nil || agentCancelled {
		if cancelFn != nil {
			cancelFn()
		}
		if clearAll {
			_, _ = a.TG.SendMessage(chatID, fmt.Sprintf("🛑 Ejecución cancelada y `%d` mensajes en cola descartados.", cleared), nil)
		} else {
			_, _ = a.TG.SendMessage(chatID, "🛑 Ejecución cancelada.", nil)
		}
	} else {
		_, _ = a.TG.SendMessage(chatID, "○ No hay ninguna tarea en curso.", nil)
	}
}

func (a *App) forceJobNow(jobID string) {
	a.runMu.Lock()
	foundIdx := -1
	for i, j := range a.jobQueue {
		if j.id == jobID {
			foundIdx = i
			break
		}
	}
	if foundIdx >= 0 {
		target := a.jobQueue[foundIdx]
		a.jobQueue = append(a.jobQueue[:foundIdx], a.jobQueue[foundIdx+1:]...)
		a.jobQueue = append([]queuedJob{target}, a.jobQueue...)
	}
	cancelFn := a.runCancel
	a.runMu.Unlock()

	agentCancelled := a.Agent.CancelActive()
	if cancelFn != nil || agentCancelled {
		if cancelFn != nil {
			cancelFn()
		}
	}
}

func (a *App) dropQueuedJob(jobID string) bool {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	for i, j := range a.jobQueue {
		if j.id == jobID {
			a.jobQueue = append(a.jobQueue[:i], a.jobQueue[i+1:]...)
			return true
		}
	}
	return false
}
