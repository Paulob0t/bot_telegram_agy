package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"telegram-paulobot/internal/buffer"
	"telegram-paulobot/internal/chats"
	"telegram-paulobot/internal/config"
	"telegram-paulobot/internal/desktop"
	"telegram-paulobot/internal/executor"
	"telegram-paulobot/internal/guard"
	"telegram-paulobot/internal/hooks"
	"telegram-paulobot/internal/models"
	"telegram-paulobot/internal/obsidian"
	"telegram-paulobot/internal/projects"
	"telegram-paulobot/internal/state"
	"telegram-paulobot/internal/telegram"
	"telegram-paulobot/internal/tokens"
	"telegram-paulobot/internal/utils"
)

type queuedJob struct {
	chatID  int64
	prompt  string
	temp    []string
	noticeM int64
}

var (
	startTime = time.Now()
	runMu     sync.Mutex
	isBusy    bool
	runCancel context.CancelFunc
	jobQueue  []queuedJob
)

func main() {
	cwd, _ := os.Getwd()
	cfg, err := config.Load(cwd)
	if err != nil {
		log.Fatalf("✕ %v", err)
	}

	st := state.New(cfg)

	// Subcomandos CLI unificados (0 Python): dialog, stats, hook agy/claude, notify
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "dialog", "token-dialog":
			tokens.ShowRofiDialog(cfg, true)
			return
		case "stats", "ai-token-stats":
			useCache := len(os.Args) > 2 && os.Args[2] == "--cached"
			var q tokens.Quotas
			if useCache {
				q = tokens.LoadCachedOrFetch(cfg, time.Minute)
			} else {
				q = tokens.FetchAll(cfg)
				_ = tokens.WriteWaybarJSON(cfg)
			}
			raw, _ := json.Marshal(q)
			fmt.Println(string(raw))
			return
		case "hook":
			if len(os.Args) > 2 && os.Args[2] == "claude" {
				hooks.RunClaudeHook(cfg, st)
			} else {
				hooks.RunAgyHook(cfg, st)
			}
			return
		case "notify", "notify-telegram":
			os.Exit(hooks.RunNotifyCLI(cfg, st, os.Args[2:]))
		}
	}

	tg := telegram.New(cfg.Token)
	cg := guard.New(cfg.ChatIdleMinutes)

	msgBuf := buffer.New(cfg.LongMessageThreshold, cfg.LongMessageWaitSeconds, func(full string) {
		submitWithGuard(cfg, st, tg, cg, cfg.AuthorizedUserID, full, nil)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("◆ Paulobot Go iniciado · Motor: %s · CWD: %s", st.CurrentEngine, st.CurrentCWD)

	go startWaybarTokensLoop(ctx, cfg)

	var offset int64
	for {
		select {
		case <-ctx.Done():
			log.Println("◇ Deteniendo Paulobot Go...")
			return
		default:
		}

		updates, err := tg.GetUpdates(offset, 25)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}

		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if u.CallbackQuery != nil {
				if u.CallbackQuery.From.ID != cfg.AuthorizedUserID {
					continue
				}
				handleCallback(cfg, st, tg, cg, u.CallbackQuery)
				continue
			}
			if u.Message == nil || u.Message.From == nil {
				continue
			}
			if u.Message.From.ID != cfg.AuthorizedUserID {
				continue
			}

			text := strings.TrimSpace(u.Message.Text)
			if text == "" {
				text = strings.TrimSpace(u.Message.Caption)
			}

			// Descargar foto o documento adjunto si viene en el mensaje
			var tempFiles []string
			if len(u.Message.Photo) > 0 {
				best := u.Message.Photo[len(u.Message.Photo)-1]
				tmpImg := filepath.Join(os.TempDir(), fmt.Sprintf("tg_photo_%s.jpg", best.FileUniqueID))
				if err := tg.DownloadFile(best.FileID, tmpImg); err == nil {
					tempFiles = append(tempFiles, tmpImg)
					if text != "" {
						text = fmt.Sprintf("[Foto adjunta guardada en: %s]\n\n%s", tmpImg, text)
					} else {
						text = fmt.Sprintf("[Foto adjunta guardada en: %s]\nPor favor analiza esta imagen.", tmpImg)
					}
				}
			}
			if u.Message.Document != nil {
				doc := u.Message.Document
				fn := filepath.Base(doc.FileName)
				if fn == "" || fn == "." {
					fn = "doc_" + doc.FileUniqueID
				}
				tmpDoc := filepath.Join(os.TempDir(), fmt.Sprintf("tg_doc_%s_%s", doc.FileUniqueID, fn))
				if err := tg.DownloadFile(doc.FileID, tmpDoc); err == nil {
					tempFiles = append(tempFiles, tmpDoc)
					if text != "" {
						text = fmt.Sprintf("[Archivo adjunto: %s]\n\n%s", tmpDoc, text)
					} else {
						text = fmt.Sprintf("[Archivo adjunto: %s]\nRevisa este archivo.", tmpDoc)
					}
				}
			}

			if text == "" {
				continue
			}

			if strings.HasPrefix(text, "/") && len(tempFiles) == 0 {
				handleCommand(cfg, st, tg, cg, u.Message.Chat.ID, text)
				continue
			}

			if st.FeatureEnabled("long_buffer") && len(tempFiles) == 0 {
				if ready, ok := msgBuf.Push(text); ok {
					submitWithGuard(cfg, st, tg, cg, u.Message.Chat.ID, ready, nil)
				}
			} else {
				submitWithGuard(cfg, st, tg, cg, u.Message.Chat.ID, text, tempFiles)
			}
		}
	}
}

func submitWithGuard(cfg *config.Config, st *state.BotSessionState, tg *telegram.Client, cg *guard.ChatGuard, chatID int64, prompt string, tempFiles []string) {
	need, reason := cg.NeedsConfirmation(
		st.FeatureEnabled("chat_guard"),
		st.CurrentEngine,
		st.CurrentConversationID,
		st.ClaudeSessionID,
	)
	if !need {
		cg.Touch()
		enqueuePrompt(cfg, st, tg, chatID, prompt, tempFiles)
		return
	}

	holdID := cg.Hold(chatID, prompt)
	text, kb := buildChatGuardView(st, holdID, prompt, reason)
	_, _ = tg.SendMessage(chatID, text, kb)
}

func buildChatGuardView(st *state.BotSessionState, holdID, prompt, reason string) (string, *telegram.InlineKeyboard) {
	title := st.CurrentConversationTitle
	if st.CurrentEngine == "claude" {
		title = st.ClaudeSessionTitle
	}
	if title == "" {
		title = "Último chat activo (-c)"
	}
	cwdShort := st.GetCWDShortName(st.CurrentCWD)
	preview := strings.Join(strings.Fields(prompt), " ")
	if len([]rune(preview)) > 80 {
		preview = string([]rune(preview)[:80]) + "…"
	}
	shortTitle := title
	if len([]rune(shortTitle)) > 28 {
		shortTitle = string([]rune(shortTitle)[:28]) + "…"
	}

	kb := &telegram.InlineKeyboard{
		InlineKeyboard: [][]telegram.InlineButton{
			{{Text: fmt.Sprintf("➡️ Continuar: %s (%s)", shortTitle, cwdShort), CallbackData: "cg_cont:" + holdID}},
			{
				{Text: "🆕 Nuevo chat", CallbackData: "cg_new:" + holdID},
				{Text: "📂 Elegir otro chat", CallbackData: "cg_pick:" + holdID},
			},
			{
				{Text: fmt.Sprintf("📁 Cambiar Proyecto (%s)", cwdShort), CallbackData: "cg_proj:" + holdID},
				{Text: "🗑️ Descartar", CallbackData: "cg_drop:" + holdID},
			},
		},
	}
	msg := fmt.Sprintf(
		"🧭 **¿En qué chat y proyecto va esto?** (%s)\n\n• 💬 **Chat:** **%s**\n• 📂 **Proyecto (`cwd`):** `%s` (`%s`)\n• 📝 **Mensaje:** `%s`",
		reason, title, cwdShort, st.CurrentCWD, preview,
	)
	return msg, kb
}

func enqueuePrompt(cfg *config.Config, st *state.BotSessionState, tg *telegram.Client, chatID int64, prompt string, tempFiles []string) {
	runMu.Lock()
	job := queuedJob{chatID: chatID, prompt: prompt, temp: tempFiles}
	if isBusy {
		pos := len(jobQueue) + 1
		preview := strings.Join(strings.Fields(prompt), " ")
		if len([]rune(preview)) > 75 {
			preview = string([]rune(preview)[:75]) + "…"
		}
		kb := &telegram.InlineKeyboard{
			InlineKeyboard: [][]telegram.InlineButton{
				{{Text: "🛑 Cancelar todo", CallbackData: "q_cancel_all"}},
			},
		}
		noticeID, _ := tg.SendMessage(
			chatID,
			fmt.Sprintf("⏳ **Ocupado** · Mensaje en cola (`#%d` • 📂 `%s`):\n`%s`", pos, st.GetCWDShortName(st.CurrentCWD), preview),
			kb,
		)
		job.noticeM = noticeID
		jobQueue = append(jobQueue, job)
		runMu.Unlock()
		return
	}
	isBusy = true
	runMu.Unlock()

	go workerLoop(cfg, st, tg, job)
}

func workerLoop(cfg *config.Config, st *state.BotSessionState, tg *telegram.Client, first queuedJob) {
	curr := first
	for {
		executeJob(cfg, st, tg, curr)

		runMu.Lock()
		if len(jobQueue) == 0 {
			isBusy = false
			runCancel = nil
			runMu.Unlock()
			return
		}
		curr = jobQueue[0]
		jobQueue = jobQueue[1:]
		runMu.Unlock()
	}
}

func executeJob(cfg *config.Config, st *state.BotSessionState, tg *telegram.Client, job queuedJob) {
	defer func() {
		for _, tf := range job.temp {
			_ = os.Remove(tf)
		}
	}()

	cwdShort := st.GetCWDShortName(st.CurrentCWD)
	chatTitle := st.CurrentConversationTitle
	if st.CurrentEngine == "claude" {
		chatTitle = st.ClaudeSessionTitle
	}
	cancelKB := &telegram.InlineKeyboard{
		InlineKeyboard: [][]telegram.InlineButton{
			{{Text: "🛑 Cancelar ejecución", CallbackData: "q_cancel"}},
		},
	}
	header := fmt.Sprintf("⚡ **[%s]** • 📂 `%s`\n⏳ _Pensando…_", chatTitle, cwdShort)

	msgID := job.noticeM
	if msgID != 0 {
		tg.EditMessage(job.chatID, msgID, header, cancelKB)
	} else {
		msgID, _ = tg.SendMessage(job.chatID, header, cancelKB)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runMu.Lock()
	runCancel = cancel
	runMu.Unlock()
	defer cancel()

	out, err := executor.RunPrompt(ctx, cfg, st, job.prompt, func(partial string) {
		if msgID != 0 {
			tg.EditMessage(job.chatID, msgID, utils.SanitizeStreamingMarkdown(partial, 3750), cancelKB)
		}
	})

	if ctx.Err() != nil {
		if out != "" {
			out = utils.SanitizeStreamingMarkdown(out, 3700) + "\n\n🛑 **Ejecución cancelada.**"
		} else {
			out = "🛑 **Ejecución cancelada por el usuario.**"
		}
		if msgID != 0 {
			tg.EditMessage(job.chatID, msgID, out, nil)
		}
		return
	}

	if err != nil && out == "" {
		out = fmt.Sprintf("✕ Error: `%v`", err)
	}
	if out == "" {
		out = "✓ Tarea completada."
	}

	finalTitle := st.CurrentConversationTitle
	if st.CurrentEngine == "claude" {
		finalTitle = st.ClaudeSessionTitle
	}
	footer := fmt.Sprintf("\n\n— 💬 _%s_ • 📂 `%s`", strings.TrimLeft(finalTitle, "💬🟠▸🔄➕ "), st.GetCWDShortName(st.CurrentCWD))

	chunks := utils.SplitMarkdownChunks(out, 3800)
	if len([]rune(chunks[len(chunks)-1]))+len([]rune(footer)) <= 3950 {
		chunks[len(chunks)-1] += footer
	} else {
		chunks = append(chunks, strings.TrimSpace(footer))
	}

	if msgID != 0 {
		tg.EditMessage(job.chatID, msgID, chunks[0], nil)
	} else {
		_, _ = tg.SendMessage(job.chatID, chunks[0], nil)
	}
	for _, extra := range chunks[1:] {
		_, _ = tg.SendMessage(job.chatID, extra, nil)
	}
}

func handleCommand(cfg *config.Config, st *state.BotSessionState, tg *telegram.Client, cg *guard.ChatGuard, chatID int64, text string) {
	fields := strings.Fields(text)
	cmd := fields[0]
	arg := ""
	if len(fields) > 1 {
		arg = strings.TrimSpace(strings.TrimPrefix(text, cmd))
	}

	switch cmd {
	case "/start", "/help", "/ayuda", "/status", "/current":
		up := utils.FormatUptime(int64(time.Since(startTime).Seconds()))
		summary := projects.FormatActiveCWDSummary(st.CurrentCWD)
		chatLabel := st.CurrentConversationTitle
		if st.CurrentEngine == "claude" {
			chatLabel = st.ClaudeSessionTitle
		}
		mod := st.CurrentModel
		if mod == "" {
			mod = "DEFAULT"
		}
		msg := fmt.Sprintf(
			"◆ **Paulobot Go** · `%s`\n• 💬 **Chat:** `%s`\n• 🤖 **Modelo:** `%s` (`%s`)\n%s\n• ◷ **Uptime:** `%s`",
			st.CurrentEngine, chatLabel, mod, st.CurrentEffort, summary, up,
		)
		_, _ = tg.SendMessage(chatID, msg, nil)

	case "/engine":
		kb := &telegram.InlineKeyboard{
			InlineKeyboard: [][]telegram.InlineButton{
				{
					{Text: "◆ AGY", CallbackData: "eng:agy"},
					{Text: "◇ Claude", CallbackData: "eng:claude"},
				},
			},
		}
		_, _ = tg.SendMessage(chatID, fmt.Sprintf("⌘ **Motor actual:** `%s`", st.CurrentEngine), kb)

	case "/agy":
		st.CurrentEngine = "agy"
		_ = st.Save()
		cg.Touch()
		_, _ = tg.SendMessage(chatID, fmt.Sprintf("🔵 **Motor Antigravity (AGY) activo**\n• 💬 `%s`\n• 📂 `%s`", st.CurrentConversationTitle, st.GetCWDShortName(st.CurrentCWD)), nil)

	case "/claude":
		st.CurrentEngine = "claude"
		_ = st.Save()
		cg.Touch()
		_, _ = tg.SendMessage(chatID, fmt.Sprintf("🟠 **Motor Claude Code activo**\n• 💬 `%s`\n• 📂 `%s`", st.ClaudeSessionTitle, st.GetCWDShortName(st.CurrentCWD)), nil)

	case "/new", "/nuevo":
		if st.CurrentEngine == "claude" {
			st.SetClaudeSession("NEW", "➕ Nuevo chat Claude", st.CurrentCWD)
		} else {
			st.SetAgyConversation("NEW", "➕ Nuevo chat independiente", st.CurrentCWD)
		}
		cg.Touch()
		_, _ = tg.SendMessage(chatID, fmt.Sprintf("🆕 **Nuevo chat preparado** en 📂 `%s`.\nEl próximo mensaje abrirá una conversación limpia.", st.GetCWDShortName(st.CurrentCWD)), nil)

	case "/continue", "/continuar":
		if st.CurrentEngine == "claude" {
			st.SetClaudeSession("", "▸ Último chat Claude (-c)", st.CurrentCWD)
		} else {
			st.SetAgyConversation("", "▸ Continuar último chat (-c)", st.CurrentCWD)
		}
		cg.Touch()
		_, _ = tg.SendMessage(chatID, "🔄 **Modo continuar último chat activo (`-c`) activado.**", nil)

	case "/project", "/cd", "/workspace", "/dir", "/repo":
		if arg != "" {
			for _, p := range projects.ListAvailableProjects(cfg) {
				if strings.EqualFold(p.Name, arg) || strings.Contains(strings.ToLower(p.Name), strings.ToLower(arg)) {
					st.SetCWD(p.Path)
					_, _ = tg.SendMessage(chatID, "✓ "+projects.FormatActiveCWDSummary(p.Path), nil)
					return
				}
			}
		}
		kb := buildProjectsKeyboard(cfg, st.CurrentCWD, "proj:")
		_, _ = tg.SendMessage(chatID, projects.FormatActiveCWDSummary(st.CurrentCWD), kb)

	case "/model", "/modelo":
		var rows [][]telegram.InlineButton
		defMark := "○ "
		if st.CurrentModel == "" {
			defMark = "● "
		}
		rows = append(rows, []telegram.InlineButton{{Text: defMark + "Default CLI", CallbackData: "mod:DEFAULT"}})
		for _, m := range models.DefaultCatalog {
			icon := models.PickIconForModel(m.ID)
			if st.CurrentModel == m.ID {
				icon = "●"
			}
			rows = append(rows, []telegram.InlineButton{{Text: icon + " " + m.Name, CallbackData: "mod:" + m.ID}})
		}
		curMod := st.CurrentModel
		if curMod == "" {
			curMod = "DEFAULT"
		}
		_, _ = tg.SendMessage(chatID, fmt.Sprintf("◈ **Modelo (%d disponibles):** `%s` · Esfuerzo: `%s`", len(models.DefaultCatalog), curMod, st.CurrentEffort), &telegram.InlineKeyboard{InlineKeyboard: rows})

	case "/effort", "/esfuerzo":
		effs := []string{"low", "medium", "high", "xhigh"}
		var row []telegram.InlineButton
		for _, e := range effs {
			lbl := strings.ToUpper(e)
			if st.CurrentEffort == e {
				lbl = "● " + lbl
			}
			row = append(row, telegram.InlineButton{Text: lbl, CallbackData: "eff:" + e})
		}
		_, _ = tg.SendMessage(chatID, fmt.Sprintf("⚡ **Nivel de esfuerzo actual:** `%s`", strings.ToUpper(st.CurrentEffort)), &telegram.InlineKeyboard{InlineKeyboard: [][]telegram.InlineButton{row}})

	case "/config", "/ajustes":
		msg, kb := buildConfigKeyboard(st)
		_, _ = tg.SendMessage(chatID, msg, kb)

	case "/screen", "/ss", "/captura":
		p, err := desktop.CaptureScreen()
		if err != nil {
			_, _ = tg.SendMessage(chatID, fmt.Sprintf("✕ Error capturando pantalla: `%v`", err), nil)
			return
		}
		_ = tg.SendPhoto(chatID, p, "◆ Captura actual")
		_ = os.Remove(p)

	case "/tokens", "/usage", "/consumo":
		q := tokens.FetchAll(cfg)
		_ = tokens.WriteWaybarJSON(cfg)
		kb := &telegram.InlineKeyboard{
			InlineKeyboard: [][]telegram.InlineButton{
				{{Text: "🔄 Sincronizar en vivo", CallbackData: "tok_sync"}},
			},
		}
		_, _ = tg.SendMessage(chatID, tokens.FormatTelegramDashboard(q), kb)

	case "/obsidian", "/prompts", "/vault", "/notas":
		notes := obsidian.ListMarkdownNotes(cfg, 10)
		if len(notes) == 0 {
			_, _ = tg.SendMessage(chatID, "○ Sin notas en Obsidian.", nil)
			return
		}
		var rows [][]telegram.InlineButton
		for _, n := range notes {
			rows = append(rows, []telegram.InlineButton{{Text: "▸ " + n.Name, CallbackData: "obs:" + n.Name}})
		}
		_, _ = tg.SendMessage(chatID, "◆ **Notas / Prompts (Obsidian)**", &telegram.InlineKeyboard{InlineKeyboard: rows})

	case "/resume", "/chats", "/historial":
		msg, kb := buildResumeKeyboard(cfg, st, "chat:")
		_, _ = tg.SendMessage(chatID, msg, kb)

	case "/stop", "/cancel", "/cancelar":
		cancelActiveExecution(tg, chatID, false)
	}
}

func buildProjectsKeyboard(cfg *config.Config, activeCWD, prefix string) *telegram.InlineKeyboard {
	list := projects.ListAvailableProjects(cfg)
	var rows [][]telegram.InlineButton
	homeMark := "🏠 "
	if filepath.Clean(activeCWD) == filepath.Clean(cfg.HomeDir) {
		homeMark = "● "
	}
	rows = append(rows, []telegram.InlineButton{{Text: homeMark + "Home (~)", CallbackData: prefix + "HOME"}})

	var pair []telegram.InlineButton
	for i, p := range list {
		if i >= 12 {
			break
		}
		mark := "📁 "
		if filepath.Clean(p.Path) == filepath.Clean(activeCWD) {
			mark = "● "
		}
		pair = append(pair, telegram.InlineButton{Text: mark + p.Name, CallbackData: prefix + p.Name})
		if len(pair) == 2 {
			rows = append(rows, pair)
			pair = nil
		}
	}
	if len(pair) > 0 {
		rows = append(rows, pair)
	}
	return &telegram.InlineKeyboard{InlineKeyboard: rows}
}

func buildResumeKeyboard(cfg *config.Config, st *state.BotSessionState, prefix string) (string, *telegram.InlineKeyboard) {
	var rows [][]telegram.InlineButton
	rows = append(rows, []telegram.InlineButton{
		{Text: "🔄 Último activo (-c)", CallbackData: prefix + "LATEST"},
		{Text: "🆕 Nuevo chat", CallbackData: prefix + "NEW"},
	})

	if st.CurrentEngine == "claude" {
		sessions := chats.ListClaudeSessions(cfg, 8)
		for _, s := range sessions {
			title := s.Title
			if len([]rune(title)) > 34 {
				title = string([]rune(title)[:34]) + "…"
			}
			rows = append(rows, []telegram.InlineButton{{Text: "🟠 " + title, CallbackData: prefix + s.SessionID}})
		}
		return "⌘ **Sesiones recientes (Claude Code)**", &telegram.InlineKeyboard{InlineKeyboard: rows}
	}

	convs := chats.ListAgyConversations(cfg, 8)
	for _, c := range convs {
		title := c.Title
		projTag := ""
		if mapped, ok := st.ConversationCWDMap[c.ConversationID]; ok && mapped != "" {
			projTag = " [" + st.GetCWDShortName(mapped) + "]"
		}
		maxLen := 32
		if projTag != "" {
			maxLen = 24
		}
		if len([]rune(title)) > maxLen {
			title = string([]rune(title)[:maxLen]) + "…"
		}
		rows = append(rows, []telegram.InlineButton{{Text: "💬 " + title + projTag, CallbackData: prefix + c.ConversationID}})
	}
	return "🗂️ **Conversaciones recientes (Antigravity AGY)**", &telegram.InlineKeyboard{InlineKeyboard: rows}
}

func buildConfigKeyboard(st *state.BotSessionState) (string, *telegram.InlineKeyboard) {
	keys := []struct {
		key   string
		label string
	}{
		{"chat_guard", "🧭 ChatGuard"},
		{"long_buffer", "📝 Buffer Mensajes Largos"},
		{"claude", "🟠 Motor Claude Code"},
		{"obsidian", "📒 Bóveda Obsidian"},
		{"notify_telegram", "🔔 Notificar Telegram"},
		{"notify_desktop", "🖥️ Notificar Escritorio"},
	}
	var rows [][]telegram.InlineButton
	for _, item := range keys {
		on := st.FeatureEnabled(item.key)
		stateIcon := "🔴 OFF"
		if on {
			stateIcon = "🟢 ON"
		}
		rows = append(rows, []telegram.InlineButton{
			{Text: fmt.Sprintf("%s: %s", item.label, stateIcon), CallbackData: "cfg:" + item.key},
		})
	}
	return "⚙️ **Configuración de Módulos (Paulobot Go)**\n• 🔵 **AGY:** `Siempre activo`", &telegram.InlineKeyboard{InlineKeyboard: rows}
}

func cancelActiveExecution(tg *telegram.Client, chatID int64, clearAll bool) {
	runMu.Lock()
	cleared := 0
	if clearAll && len(jobQueue) > 0 {
		cleared = len(jobQueue)
		for _, q := range jobQueue {
			for _, tf := range q.temp {
				_ = os.Remove(tf)
			}
		}
		jobQueue = nil
	}
	cancelFn := runCancel
	runMu.Unlock()

	if cancelFn != nil {
		cancelFn()
		if clearAll {
			_, _ = tg.SendMessage(chatID, fmt.Sprintf("🛑 Ejecución cancelada y `%d` mensajes en cola descartados.", cleared), nil)
		} else {
			_, _ = tg.SendMessage(chatID, "🛑 Ejecución cancelada.", nil)
		}
	} else {
		_, _ = tg.SendMessage(chatID, "○ No hay ninguna tarea en curso.", nil)
	}
}

func handleCallback(cfg *config.Config, st *state.BotSessionState, tg *telegram.Client, cg *guard.ChatGuard, cb *telegram.CallbackQuery) {
	data := cb.Data
	switch {
	case data == "tok_sync":
		tg.AnswerCallback(cb.ID, "Sincronizando cuotas…")
		q := tokens.FetchAll(cfg)
		_ = tokens.WriteWaybarJSON(cfg)
		kb := &telegram.InlineKeyboard{
			InlineKeyboard: [][]telegram.InlineButton{
				{{Text: "🔄 Sincronizar en vivo", CallbackData: "tok_sync"}},
			},
		}
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, tokens.FormatTelegramDashboard(q), kb)
		}

	case data == "q_cancel":
		tg.AnswerCallback(cb.ID, "Cancelando…")
		if cb.Message != nil {
			cancelActiveExecution(tg, cb.Message.Chat.ID, false)
		}

	case data == "q_cancel_all":
		tg.AnswerCallback(cb.ID, "Cancelando todo…")
		if cb.Message != nil {
			cancelActiveExecution(tg, cb.Message.Chat.ID, true)
		}

	case strings.HasPrefix(data, "cg_cont:"):
		holdID := strings.TrimPrefix(data, "cg_cont:")
		held, ok := cg.Pop(holdID)
		if !ok {
			tg.AnswerCallback(cb.ID, "Expiró, reenvía el mensaje")
			return
		}
		cg.Touch()
		tg.AnswerCallback(cb.ID, "Enviando…")
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✅ Enviado a **%s** • 📂 `%s`", st.CurrentConversationTitle, st.GetCWDShortName(st.CurrentCWD)), nil)
		}
		enqueuePrompt(cfg, st, tg, held.ChatID, held.Prompt, nil)

	case strings.HasPrefix(data, "cg_new:"):
		holdID := strings.TrimPrefix(data, "cg_new:")
		held, ok := cg.Pop(holdID)
		if !ok {
			tg.AnswerCallback(cb.ID, "Expiró, reenvía el mensaje")
			return
		}
		if st.CurrentEngine == "claude" {
			st.SetClaudeSession("NEW", "➕ Nuevo chat Claude", st.CurrentCWD)
		} else {
			st.SetAgyConversation("NEW", "➕ Nuevo chat independiente", st.CurrentCWD)
		}
		cg.Touch()
		tg.AnswerCallback(cb.ID, "Nuevo chat…")
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✅ Enviado a **🆕 Nuevo chat** • 📂 `%s`", st.GetCWDShortName(st.CurrentCWD)), nil)
		}
		enqueuePrompt(cfg, st, tg, held.ChatID, held.Prompt, nil)

	case strings.HasPrefix(data, "cg_drop:"):
		holdID := strings.TrimPrefix(data, "cg_drop:")
		cg.Pop(holdID)
		tg.AnswerCallback(cb.ID, "Descartado")
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, "🗑️ Mensaje descartado.", nil)
		}

	case strings.HasPrefix(data, "cg_proj:"):
		holdID := strings.TrimPrefix(data, "cg_proj:")
		held, ok := cg.Get(holdID)
		if !ok {
			tg.AnswerCallback(cb.ID, "Expiró, reenvía el mensaje")
			return
		}
		kb := buildProjectsKeyboard(cfg, st.CurrentCWD, "cg_psel:"+holdID+":")
		kb.InlineKeyboard = append(kb.InlineKeyboard, []telegram.InlineButton{
			{Text: "🔙 Volver al mensaje", CallbackData: "cg_back:" + holdID},
		})
		tg.AnswerCallback(cb.ID, "Elige proyecto")
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("📁 **Selecciona el proyecto (`cwd`) para tu mensaje:**\n`%s`", held.Prompt), kb)
		}

	case strings.HasPrefix(data, "cg_psel:"):
		rest := strings.TrimPrefix(data, "cg_psel:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return
		}
		holdID, projName := parts[0], parts[1]
		if projName == "HOME" {
			st.SetCWD(cfg.HomeDir)
		} else {
			for _, p := range projects.ListAvailableProjects(cfg) {
				if p.Name == projName {
					st.SetCWD(p.Path)
					break
				}
			}
		}
		held, ok := cg.Get(holdID)
		if !ok {
			tg.AnswerCallback(cb.ID, "Proyecto cambiado")
			return
		}
		tg.AnswerCallback(cb.ID, "Proyecto: "+projName)
		if cb.Message != nil {
			text, kb := buildChatGuardView(st, holdID, held.Prompt, "proyecto actualizado")
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, text, kb)
		}

	case strings.HasPrefix(data, "cg_pick:"):
		holdID := strings.TrimPrefix(data, "cg_pick:")
		if _, ok := cg.Get(holdID); !ok {
			tg.AnswerCallback(cb.ID, "Expiró, reenvía el mensaje")
			return
		}
		_, kb := buildResumeKeyboard(cfg, st, "cg_csel:"+holdID+":")
		kb.InlineKeyboard = append(kb.InlineKeyboard, []telegram.InlineButton{
			{Text: "🔙 Volver", CallbackData: "cg_back:" + holdID},
		})
		tg.AnswerCallback(cb.ID, "Elige chat")
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, "📂 **Elige en qué chat enviar tu mensaje:**", kb)
		}

	case strings.HasPrefix(data, "cg_csel:"):
		rest := strings.TrimPrefix(data, "cg_csel:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return
		}
		holdID, targetID := parts[0], parts[1]
		held, ok := cg.Pop(holdID)
		if !ok {
			tg.AnswerCallback(cb.ID, "Expiró, reenvía el mensaje")
			return
		}
		selectConversation(cfg, st, targetID)
		cg.Touch()
		tg.AnswerCallback(cb.ID, "Enviando…")
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✅ Enviado a **%s** • 📂 `%s`", st.CurrentConversationTitle, st.GetCWDShortName(st.CurrentCWD)), nil)
		}
		enqueuePrompt(cfg, st, tg, held.ChatID, held.Prompt, nil)

	case strings.HasPrefix(data, "cg_back:"):
		holdID := strings.TrimPrefix(data, "cg_back:")
		held, ok := cg.Get(holdID)
		if !ok {
			tg.AnswerCallback(cb.ID, "Expiró")
			return
		}
		text, kb := buildChatGuardView(st, holdID, held.Prompt, "confirmación")
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, text, kb)
		}

	case strings.HasPrefix(data, "eng:"):
		eng := strings.TrimPrefix(data, "eng:")
		st.CurrentEngine = eng
		_ = st.Save()
		cg.Touch()
		tg.AnswerCallback(cb.ID, "Motor: "+eng)
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✓ **Motor cambiado a:** `%s`", eng), nil)
		}

	case strings.HasPrefix(data, "proj:"):
		name := strings.TrimPrefix(data, "proj:")
		if name == "HOME" {
			st.SetCWD(cfg.HomeDir)
			tg.AnswerCallback(cb.ID, "Proyecto: Home")
			if cb.Message != nil {
				tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, "✓ "+projects.FormatActiveCWDSummary(cfg.HomeDir), nil)
			}
			return
		}
		for _, p := range projects.ListAvailableProjects(cfg) {
			if p.Name == name {
				st.SetCWD(p.Path)
				tg.AnswerCallback(cb.ID, "Proyecto: "+name)
				if cb.Message != nil {
					tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, "✓ "+projects.FormatActiveCWDSummary(p.Path), nil)
				}
				return
			}
		}

	case strings.HasPrefix(data, "obs:"):
		name := strings.TrimPrefix(data, "obs:")
		notes := obsidian.ListMarkdownNotes(cfg, 25)
		for idx, n := range notes {
			if n.Name == name {
				content, err := obsidian.ReadNote(n.AbsPath, 3300)
				if err != nil {
					tg.AnswerCallback(cb.ID, "Error leyendo nota")
					return
				}
				kb := &telegram.InlineKeyboard{
					InlineKeyboard: [][]telegram.InlineButton{
						{{Text: "🚀 Lanzar como Prompt en AGY/Claude", CallbackData: fmt.Sprintf("obs_run:%d", idx)}},
					},
				}
				tg.AnswerCallback(cb.ID, n.Name)
				if cb.Message != nil {
					tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("◆ **%s**\n\n%s", n.Name, content), kb)
				}
				return
			}
		}

	case strings.HasPrefix(data, "obs_run:"):
		idxStr := strings.TrimPrefix(data, "obs_run:")
		idx, err := strconv.Atoi(idxStr)
		notes := obsidian.ListMarkdownNotes(cfg, 25)
		if err != nil || idx < 0 || idx >= len(notes) {
			tg.AnswerCallback(cb.ID, "Nota no encontrada")
			return
		}
		n := notes[idx]
		content, err := obsidian.ReadNote(n.AbsPath, 0)
		if err != nil || strings.TrimSpace(content) == "" {
			tg.AnswerCallback(cb.ID, "Nota vacía")
			return
		}
		cg.Touch()
		tg.AnswerCallback(cb.ID, "Lanzando "+n.Name)
		if cb.Message != nil {
			enqueuePrompt(cfg, st, tg, cb.Message.Chat.ID, content, nil)
		}

	case strings.HasPrefix(data, "chat:"):
		sid := strings.TrimPrefix(data, "chat:")
		selectConversation(cfg, st, sid)
		cg.Touch()
		tg.AnswerCallback(cb.ID, "Sesión seleccionada")
		if cb.Message != nil {
			label := st.CurrentConversationTitle
			if st.CurrentEngine == "claude" {
				label = st.ClaudeSessionTitle
			}
			tg.EditMessage(
				cb.Message.Chat.ID,
				cb.Message.MessageID,
				fmt.Sprintf("✓ **Chat activo:** `%s`\n📂 **Proyecto (`cwd`):** `%s`", label, st.GetCWDShortName(st.CurrentCWD)),
				nil,
			)
		}

	case strings.HasPrefix(data, "mod:"):
		mod := strings.TrimPrefix(data, "mod:")
		if mod == "DEFAULT" {
			mod = ""
		}
		st.CurrentModel = mod
		for _, eff := range []string{"xhigh", "low", "medium", "high"} {
			if strings.HasSuffix(strings.ToLower(mod), "-"+eff) {
				st.CurrentEffort = eff
				break
			}
		}
		_ = st.Save()
		disp := mod
		if disp == "" {
			disp = "DEFAULT"
		}
		tg.AnswerCallback(cb.ID, "Modelo: "+disp)
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✓ **Modelo:** `%s` · Esfuerzo: `%s`", disp, st.CurrentEffort), nil)
		}

	case strings.HasPrefix(data, "eff:"):
		eff := strings.TrimPrefix(data, "eff:")
		st.CurrentEffort = eff
		_ = st.Save()
		tg.AnswerCallback(cb.ID, "Esfuerzo: "+eff)
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✓ **Esfuerzo:** `%s`", strings.ToUpper(eff)), nil)
		}

	case strings.HasPrefix(data, "cfg:"):
		key := strings.TrimPrefix(data, "cfg:")
		st.ToggleFeature(key)
		tg.AnswerCallback(cb.ID, "Actualizado")
		if cb.Message != nil {
			msg, kb := buildConfigKeyboard(st)
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, msg, kb)
		}
	}
}

func selectConversation(cfg *config.Config, st *state.BotSessionState, targetID string) {
	if st.CurrentEngine == "claude" {
		switch targetID {
		case "LATEST":
			st.SetClaudeSession("", "▸ Último chat Claude (-c)", st.CurrentCWD)
		case "NEW":
			st.SetClaudeSession("NEW", "➕ Nuevo chat Claude", st.CurrentCWD)
		default:
			title := "Sesión Claude (" + targetID + ")"
			cwd := st.CurrentCWD
			for _, s := range chats.ListClaudeSessions(cfg, 30) {
				if s.SessionID == targetID {
					title = s.Title
					if s.CWD != "" {
						cwd = s.CWD
					}
					break
				}
			}
			st.SetClaudeSession(targetID, title, cwd)
		}
		return
	}

	switch targetID {
	case "LATEST":
		st.SetAgyConversation("", "▸ Continuar último chat (-c)", "")
	case "NEW":
		st.SetAgyConversation("NEW", "➕ Nuevo chat independiente", "")
	default:
		title := chats.GetAgyConversationTitle(cfg, targetID)
		st.SetAgyConversation(targetID, "💬 "+title, "")
	}
}

func startWaybarTokensLoop(ctx context.Context, cfg *config.Config) {
	_ = tokens.WriteWaybarJSON(cfg)
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = tokens.WriteWaybarJSON(cfg)
		}
	}
}
