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
	id      string
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
			caption := strings.TrimSpace(u.Message.Caption)

			// 1. Foto recibida desde Telegram -> PC
			if len(u.Message.Photo) > 0 {
				best := u.Message.Photo[len(u.Message.Photo)-1]
				uploadsDir := guard.PrepareUploadsDir(cfg.HomeDir)
				fn := fmt.Sprintf("foto_%s_%s.jpg", time.Now().Format("20060102_150405"), best.FileUniqueID)
				destImg := filepath.Join(uploadsDir, fn)
				if err := tg.DownloadFile(best.FileID, destImg); err != nil {
					_, _ = tg.SendMessage(u.Message.Chat.ID, fmt.Sprintf("✕ Error descargando foto: `%v`", err), nil)
					continue
				}
				if caption != "" {
					prompt := fmt.Sprintf("[Foto adjunta guardada en: %s]\n\n%s", destImg, caption)
					submitWithGuard(cfg, st, tg, cg, u.Message.Chat.ID, prompt, []string{destImg})
				} else {
					upID := cg.HoldUpload(guard.PendingUpload{
						ChatID:   u.Message.Chat.ID,
						FilePath: destImg,
						FileName: fn,
						IsPhoto:  true,
					})
					up, _ := cg.GetUpload(upID)
					msg, kb := buildUploadActionView(cfg, st, up)
					_, _ = tg.SendMessage(u.Message.Chat.ID, msg, kb)
				}
				continue
			}

			// 2. PDF o documento recibido desde Telegram -> PC
			if u.Message.Document != nil {
				doc := u.Message.Document
				uploadsDir := guard.PrepareUploadsDir(cfg.HomeDir)
				fn := strings.ReplaceAll(filepath.Base(doc.FileName), " ", "_")
				if fn == "" || fn == "." {
					fn = "archivo_" + doc.FileUniqueID
				}
				destDoc := filepath.Join(uploadsDir, fmt.Sprintf("%s_%s", doc.FileUniqueID, fn))
				if err := tg.DownloadFile(doc.FileID, destDoc); err != nil {
					_, _ = tg.SendMessage(u.Message.Chat.ID, fmt.Sprintf("✕ Error descargando archivo: `%v`", err), nil)
					continue
				}
				ext := strings.ToLower(filepath.Ext(fn))
				isPDF := ext == ".pdf" || strings.EqualFold(doc.MimeType, "application/pdf")
				upID := cg.HoldUpload(guard.PendingUpload{
					ChatID:   u.Message.Chat.ID,
					FilePath: destDoc,
					FileName: fn,
					Caption:  caption,
					IsPDF:    isPDF,
				})
				up, _ := cg.GetUpload(upID)
				msg, kb := buildUploadActionView(cfg, st, up)
				_, _ = tg.SendMessage(u.Message.Chat.ID, msg, kb)
				continue
			}

			if text == "" {
				continue
			}

			if strings.HasPrefix(text, "/") {
				handleCommand(cfg, st, tg, cg, u.Message.Chat.ID, text)
				continue
			}

			if st.FeatureEnabled("long_buffer") {
				if ready, ok := msgBuf.Push(text); ok {
					submitWithGuard(cfg, st, tg, cg, u.Message.Chat.ID, ready, nil)
				}
			} else {
				submitWithGuard(cfg, st, tg, cg, u.Message.Chat.ID, text, nil)
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

	holdID := cg.Hold(chatID, prompt, tempFiles...)
	text, kb := buildChatGuardView(st, holdID, prompt, reason)
	_, _ = tg.SendMessage(chatID, text, kb)
}

func buildUploadActionView(cfg *config.Config, st *state.BotSessionState, up guard.PendingUpload) (string, *telegram.InlineKeyboard) {
	cwdShort := st.GetCWDShortName(st.CurrentCWD)
	kind := "📄 Archivo"
	if up.IsPhoto {
		kind = "🖼️ Foto"
	} else if up.IsPDF {
		kind = "📕 PDF"
	}

	var rows [][]telegram.InlineButton
	rows = append(rows, []telegram.InlineButton{
		{Text: fmt.Sprintf("🤖 Analizar con IA (%s)", cwdShort), CallbackData: "up_ai:" + up.ID},
	})
	rows = append(rows, []telegram.InlineButton{
		{Text: fmt.Sprintf("📂 Guardar en %s", cwdShort), CallbackData: "up_cwd:" + up.ID},
		{Text: "📁 Otro proyecto…", CallbackData: "up_pask:" + up.ID},
	})
	rows = append(rows, []telegram.InlineButton{
		{Text: "📥 Guardar en ~/Downloads", CallbackData: "up_dl:" + up.ID},
	})

	if up.IsPDF && st.FeatureEnabled("obsidian") {
		for _, vp := range obsidian.ListVaultProjects(cfg) {
			rows = append(rows, []telegram.InlineButton{
				{Text: fmt.Sprintf("📒 Obsidian %s/PDF (0 tokens)", vp.Name), CallbackData: fmt.Sprintf("up_obs:%s:%s:save", up.ID, vp.Name)},
			})
			rows = append(rows, []telegram.InlineButton{
				{Text: fmt.Sprintf("⚡ %s: Guardar + Generar Prompts Wicode", vp.Name), CallbackData: fmt.Sprintf("up_obs:%s:%s:gen", up.ID, vp.Name)},
			})
		}
	}

	rows = append(rows, []telegram.InlineButton{
		{Text: "🗑️ Descartar archivo", CallbackData: "up_drop:" + up.ID},
	})

	msg := fmt.Sprintf(
		"%s **recibido en tu PC:** `%s`\n• 📂 **Proyecto activo (`cwd`):** `%s`\n• 💾 **Caché 24h:** `%s`",
		kind, up.FileName, cwdShort, up.FilePath,
	)
	if up.Caption != "" {
		msg += fmt.Sprintf("\n• 📝 **Nota:** `%s`", up.Caption)
	}
	msg += "\n\n*¿Qué quieres hacer con este archivo?*"
	return msg, &telegram.InlineKeyboard{InlineKeyboard: rows}
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
	jobID := fmt.Sprintf("%x", time.Now().UnixNano()&0xffffffff)
	job := queuedJob{id: jobID, chatID: chatID, prompt: prompt, temp: tempFiles}
	if isBusy {
		pos := len(jobQueue) + 1
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
		if ctx.Err() != nil {
			return
		}
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
		effDisp, _, ctxWin := models.GetEffectiveModelInfo(cfg, st)
		msg := fmt.Sprintf(
			"◆ **Paulobot Go** · `%s`\n• 💬 **Chat:** `%s`\n• 🤖 **Modelo activo:** %s\n• ⚡ **Esfuerzo:** `%s` · 🪟 `%dk` ctx\n%s\n• ◷ **Uptime:** `%s`",
			st.CurrentEngine, chatLabel, effDisp, strings.ToUpper(st.CurrentEffort), ctxWin/1000, summary, up,
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
		msg, kb := buildModelsView(cfg, st)
		_, _ = tg.SendMessage(chatID, msg, kb)

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
		msg, kb := buildResumeKeyboard(cfg, st, "chat:", "res_page:", 0)
		_, _ = tg.SendMessage(chatID, msg, kb)

	case "/stop", "/cancel", "/cancelar":
		cancelActiveExecution(tg, chatID, false)
	}
}

func buildModelsView(cfg *config.Config, st *state.BotSessionState) (string, *telegram.InlineKeyboard) {
	effDisp, _, ctxWin := models.GetEffectiveModelInfo(cfg, st)
	var rows [][]telegram.InlineButton

	if st.CurrentEngine == "claude" {
		cliDef := models.GetClaudeDefaultModel(cfg)
		defPrefix := "🔄 "
		if st.ClaudeModel == "" {
			defPrefix = "✅ "
		}
		rows = append(rows, []telegram.InlineButton{
			{Text: fmt.Sprintf("%sPor defecto Claude CLI (%s)", defPrefix, cliDef), CallbackData: "mod:DEFAULT"},
		})
		for _, m := range models.ClaudeCatalog {
			prefix := m.Icon + " "
			if st.ClaudeModel == m.ID {
				prefix = "✅ "
			}
			rows = append(rows, []telegram.InlineButton{
				{Text: prefix + m.Name, CallbackData: "mod:" + m.ID},
			})
		}
		msg := fmt.Sprintf(
			"🟠 **Selector de Modelo (Claude Code CLI)**\n\n"+
				"• 📌 **En uso ahora:** %s\n"+
				"• ⚡ **Esfuerzo:** `%s`\n"+
				"• 🪟 **Ventana de contexto:** `%dk` tokens\n\n"+
				"*Toca un modelo para cambiarlo:*",
			effDisp, strings.ToUpper(st.CurrentEffort), ctxWin/1000,
		)
		return msg, &telegram.InlineKeyboard{InlineKeyboard: rows}
	}

	cliDef := models.GetCLIDefaultModel(cfg)
	defPrefix := "🔄 "
	if st.CurrentModel == "" {
		defPrefix = "✅ "
	}
	rows = append(rows, []telegram.InlineButton{
		{Text: fmt.Sprintf("%sPor defecto CLI (%s)", defPrefix, cliDef), CallbackData: "mod:DEFAULT"},
	})
	for _, m := range models.DefaultCatalog {
		prefix := m.Icon + " "
		if st.CurrentModel == m.ID || (st.CurrentModel == "" && strings.EqualFold(m.Name, cliDef)) {
			if st.CurrentModel == m.ID {
				prefix = "✅ "
			}
		}
		rows = append(rows, []telegram.InlineButton{
			{Text: prefix + m.Name, CallbackData: "mod:" + m.ID},
		})
	}
	msg := fmt.Sprintf(
		"🤖 **Selector de Modelo (Antigravity AGY)**\n\n"+
			"• 📌 **En uso ahora:** %s\n"+
			"• ⚡ **Esfuerzo:** `%s`\n"+
			"• 🪟 **Ventana de contexto:** `%dk` tokens\n\n"+
			"*Toca un modelo de la lista para activarlo:*",
		effDisp, strings.ToUpper(st.CurrentEffort), ctxWin/1000,
	)
	return msg, &telegram.InlineKeyboard{InlineKeyboard: rows}
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

func buildResumeKeyboard(cfg *config.Config, st *state.BotSessionState, prefix, pagePrefix string, page int) (string, *telegram.InlineKeyboard) {
	const pageSize = 6
	var rows [][]telegram.InlineButton
	rows = append(rows, []telegram.InlineButton{
		{Text: "🔄 Último activo (-c)", CallbackData: prefix + "LATEST"},
		{Text: "🆕 Nuevo chat", CallbackData: prefix + "NEW"},
	})

	type chatItem struct {
		id    string
		label string
	}
	var items []chatItem
	header := "🗂️ **Historial de Chats (Antigravity AGY)**"

	if st.CurrentEngine == "claude" {
		header = "⌘ **Sesiones recientes (Claude Code)**"
		for _, s := range chats.ListClaudeSessions(cfg, 0) {
			title := s.Title
			if len([]rune(title)) > 34 {
				title = string([]rune(title)[:34]) + "…"
			}
			items = append(items, chatItem{id: s.SessionID, label: "🟠 " + title})
		}
	} else {
		for _, c := range chats.ListAgyConversations(cfg, 0) {
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
			items = append(items, chatItem{id: c.ConversationID, label: "💬 " + title + projTag})
		}
	}

	total := len(items)
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	start := page * pageSize
	end := start + pageSize
	if end > total {
		end = total
	}
	for _, it := range items[start:end] {
		rows = append(rows, []telegram.InlineButton{{Text: it.label, CallbackData: prefix + it.id}})
	}

	if totalPages > 1 {
		var nav []telegram.InlineButton
		if page > 0 {
			nav = append(nav, telegram.InlineButton{Text: "◀️ Anterior", CallbackData: fmt.Sprintf("%s%d", pagePrefix, page-1)})
		}
		nav = append(nav, telegram.InlineButton{Text: fmt.Sprintf("📄 %d/%d", page+1, totalPages), CallbackData: "res_noop"})
		if page < totalPages-1 {
			nav = append(nav, telegram.InlineButton{Text: "Siguiente ▶️", CallbackData: fmt.Sprintf("%s%d", pagePrefix, page+1)})
		}
		rows = append(rows, nav)
	}

	msg := fmt.Sprintf("%s\n*%d conversaciones · Página %d/%d*", header, total, page+1, totalPages)
	return msg, &telegram.InlineKeyboard{InlineKeyboard: rows}
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
		tg.AnswerCallback(cb.ID, "Cancelando árbol de procesos…")
		if cb.Message != nil {
			cancelActiveExecution(tg, cb.Message.Chat.ID, false)
		}

	case data == "q_cancel_all":
		tg.AnswerCallback(cb.ID, "Cancelando todo…")
		if cb.Message != nil {
			cancelActiveExecution(tg, cb.Message.Chat.ID, true)
		}

	case strings.HasPrefix(data, "q_force:"):
		jobID := strings.TrimPrefix(data, "q_force:")
		runMu.Lock()
		foundIdx := -1
		for i, j := range jobQueue {
			if j.id == jobID {
				foundIdx = i
				break
			}
		}
		if foundIdx >= 0 {
			target := jobQueue[foundIdx]
			jobQueue = append(jobQueue[:foundIdx], jobQueue[foundIdx+1:]...)
			jobQueue = append([]queuedJob{target}, jobQueue...)
		}
		cancelFn := runCancel
		runMu.Unlock()

		if cancelFn != nil {
			cancelFn()
		}
		tg.AnswerCallback(cb.ID, "Interrumpiendo tarea actual…")

	case strings.HasPrefix(data, "q_drop:"):
		jobID := strings.TrimPrefix(data, "q_drop:")
		runMu.Lock()
		removed := false
		for i, j := range jobQueue {
			if j.id == jobID {
				jobQueue = append(jobQueue[:i], jobQueue[i+1:]...)
				removed = true
				break
			}
		}
		runMu.Unlock()
		tg.AnswerCallback(cb.ID, "Retirado de la cola")
		if removed && cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, "🗑️ **Mensaje retirado de la cola.**", nil)
		}

	case strings.HasPrefix(data, "up_ai:"):
		upID := strings.TrimPrefix(data, "up_ai:")
		up, ok := cg.PopUpload(upID)
		if !ok {
			tg.AnswerCallback(cb.ID, "Archivo expirado, reenvíalo")
			return
		}
		instruction := up.Caption
		if instruction == "" {
			if up.IsPhoto {
				instruction = "Por favor analiza esta imagen."
			} else {
				instruction = "Revisa y analiza este archivo."
			}
		}
		prompt := fmt.Sprintf("[Archivo adjunto guardado en: %s]\n\n%s", up.FilePath, instruction)
		tg.AnswerCallback(cb.ID, "Analizando con IA…")
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("🤖 **Enviando `%s` a IA** • 📂 `%s`", up.FileName, st.GetCWDShortName(st.CurrentCWD)), nil)
		}
		submitWithGuard(cfg, st, tg, cg, up.ChatID, prompt, []string{up.FilePath})

	case strings.HasPrefix(data, "up_cwd:"):
		upID := strings.TrimPrefix(data, "up_cwd:")
		up, ok := cg.PopUpload(upID)
		if !ok {
			tg.AnswerCallback(cb.ID, "Archivo expirado, reenvíalo")
			return
		}
		dest, err := obsidian.SaveFileToDir(up.FilePath, st.CurrentCWD, up.FileName)
		if err != nil {
			tg.AnswerCallback(cb.ID, "Error guardando")
			if cb.Message != nil {
				tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✕ Error guardando archivo: `%v`", err), nil)
			}
			return
		}
		tg.AnswerCallback(cb.ID, "Guardado en proyecto")
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✅ **Guardado en proyecto (0 tokens):**\n`%s`", dest), nil)
		}

	case strings.HasPrefix(data, "up_dl:"):
		upID := strings.TrimPrefix(data, "up_dl:")
		up, ok := cg.PopUpload(upID)
		if !ok {
			tg.AnswerCallback(cb.ID, "Archivo expirado, reenvíalo")
			return
		}
		dlDir := filepath.Join(cfg.HomeDir, "Downloads")
		dest, err := obsidian.SaveFileToDir(up.FilePath, dlDir, up.FileName)
		if err != nil {
			tg.AnswerCallback(cb.ID, "Error guardando")
			if cb.Message != nil {
				tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✕ Error guardando en Downloads: `%v`", err), nil)
			}
			return
		}
		tg.AnswerCallback(cb.ID, "Guardado en Downloads")
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✅ **Guardado en Downloads (0 tokens):**\n`%s`", dest), nil)
		}

	case strings.HasPrefix(data, "up_pask:"):
		upID := strings.TrimPrefix(data, "up_pask:")
		up, ok := cg.GetUpload(upID)
		if !ok {
			tg.AnswerCallback(cb.ID, "Archivo expirado, reenvíalo")
			return
		}
		kb := buildProjectsKeyboard(cfg, st.CurrentCWD, "up_psel:"+upID+":")
		kb.InlineKeyboard = append(kb.InlineKeyboard, []telegram.InlineButton{
			{Text: "🔙 Volver", CallbackData: "up_back:" + upID},
		})
		tg.AnswerCallback(cb.ID, "Elige proyecto destino")
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("📁 **¿En qué proyecto quieres guardar `%s`?**", up.FileName), kb)
		}

	case strings.HasPrefix(data, "up_psel:"):
		rest := strings.TrimPrefix(data, "up_psel:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return
		}
		upID, projName := parts[0], parts[1]
		up, ok := cg.PopUpload(upID)
		if !ok {
			tg.AnswerCallback(cb.ID, "Archivo expirado, reenvíalo")
			return
		}
		targetDir := cfg.HomeDir
		if projName != "HOME" {
			for _, p := range projects.ListAvailableProjects(cfg) {
				if p.Name == projName {
					targetDir = p.Path
					break
				}
			}
		}
		dest, err := obsidian.SaveFileToDir(up.FilePath, targetDir, up.FileName)
		if err != nil {
			tg.AnswerCallback(cb.ID, "Error guardando")
			return
		}
		tg.AnswerCallback(cb.ID, "Guardado en "+projName)
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✅ **Guardado en `%s` (0 tokens):**\n`%s`", projName, dest), nil)
		}

	case strings.HasPrefix(data, "up_obs:"):
		rest := strings.TrimPrefix(data, "up_obs:")
		parts := strings.SplitN(rest, ":", 3)
		if len(parts) != 3 {
			return
		}
		upID, projName, action := parts[0], parts[1], parts[2]
		up, ok := cg.PopUpload(upID)
		if !ok {
			tg.AnswerCallback(cb.ID, "Archivo expirado, reenvíalo")
			return
		}
		dest, err := obsidian.SavePDFToVaultProject(cfg, up.FilePath, projName, up.FileName)
		if err != nil {
			tg.AnswerCallback(cb.ID, "Error guardando en Obsidian")
			if cb.Message != nil {
				tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✕ Error guardando PDF en Obsidian: `%v`", err), nil)
			}
			return
		}
		if action == "save" {
			tg.AnswerCallback(cb.ID, "Guardado en Obsidian")
			if cb.Message != nil {
				tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✅ **PDF guardado en Obsidian (0 tokens):**\n`%s`", dest), nil)
			}
			return
		}
		prompt, targetCWD := obsidian.BuildWicodeGeneratorPrompt(cfg, projName, dest, st.CurrentCWD)
		st.SetCWD(targetCWD)
		cg.Touch()
		tg.AnswerCallback(cb.ID, "Iniciando Wicode Generator…")
		if cb.Message != nil {
			tg.EditMessage(
				cb.Message.Chat.ID,
				cb.Message.MessageID,
				fmt.Sprintf("⚡ **PDF guardado en `%s/PDF/%s`**\n🚀 _Iniciando `wicode-prompt-generator` (Nota Markdown + Prompts T01..TXX)…_", projName, up.FileName),
				nil,
			)
		}
		enqueuePrompt(cfg, st, tg, up.ChatID, prompt, []string{dest})

	case strings.HasPrefix(data, "up_back:"):
		upID := strings.TrimPrefix(data, "up_back:")
		up, ok := cg.GetUpload(upID)
		if !ok {
			tg.AnswerCallback(cb.ID, "Archivo expirado")
			return
		}
		msg, kb := buildUploadActionView(cfg, st, up)
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, msg, kb)
		}

	case strings.HasPrefix(data, "up_drop:"):
		upID := strings.TrimPrefix(data, "up_drop:")
		if up, ok := cg.PopUpload(upID); ok {
			_ = os.Remove(up.FilePath)
		}
		tg.AnswerCallback(cb.ID, "Archivo descartado")
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, "🗑️ Archivo descartado y eliminado de caché.", nil)
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
		enqueuePrompt(cfg, st, tg, held.ChatID, held.Prompt, held.TempFiles)

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
		enqueuePrompt(cfg, st, tg, held.ChatID, held.Prompt, held.TempFiles)

	case strings.HasPrefix(data, "cg_drop:"):
		holdID := strings.TrimPrefix(data, "cg_drop:")
		if held, ok := cg.Pop(holdID); ok {
			for _, tf := range held.TempFiles {
				_ = os.Remove(tf)
			}
		}
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
		rest := strings.TrimPrefix(data, "cg_pick:")
		holdID := rest
		page := 0
		if parts := strings.SplitN(rest, ":", 2); len(parts) == 2 {
			holdID = parts[0]
			page, _ = strconv.Atoi(parts[1])
		}
		if _, ok := cg.Get(holdID); !ok {
			tg.AnswerCallback(cb.ID, "Expiró, reenvía el mensaje")
			return
		}
		msg, kb := buildResumeKeyboard(cfg, st, "cg_csel:"+holdID+":", "cg_pick:"+holdID+":", page)
		kb.InlineKeyboard = append(kb.InlineKeyboard, []telegram.InlineButton{
			{Text: "🔙 Volver", CallbackData: "cg_back:" + holdID},
		})
		tg.AnswerCallback(cb.ID, "Elige chat")
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, msg, kb)
		}

	case strings.HasPrefix(data, "res_page:"):
		page, _ := strconv.Atoi(strings.TrimPrefix(data, "res_page:"))
		msg, kb := buildResumeKeyboard(cfg, st, "chat:", "res_page:", page)
		tg.AnswerCallback(cb.ID, "")
		if cb.Message != nil {
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, msg, kb)
		}

	case data == "res_noop":
		tg.AnswerCallback(cb.ID, "")

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
		enqueuePrompt(cfg, st, tg, held.ChatID, held.Prompt, held.TempFiles)

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
		if st.CurrentEngine == "claude" {
			st.ClaudeModel = mod
		} else {
			st.CurrentModel = mod
			for _, eff := range []string{"xhigh", "low", "medium", "high"} {
				if strings.HasSuffix(strings.ToLower(mod), "-"+eff) {
					st.CurrentEffort = eff
					break
				}
			}
		}
		_ = st.Save()
		effDisp, _, _ := models.GetEffectiveModelInfo(cfg, st)
		tg.AnswerCallback(cb.ID, "Modelo actualizado")
		if cb.Message != nil {
			msg, kb := buildModelsView(cfg, st)
			tg.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✅ **Modelo cambiado a:** %s\n\n%s", effDisp, msg), kb)
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
