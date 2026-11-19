package bot

import (
	"fmt"
	"path/filepath"
	"strings"

	"telegram-paulobot/internal/chats"
	"telegram-paulobot/internal/guard"
	"telegram-paulobot/internal/models"
	"telegram-paulobot/internal/obsidian"
	"telegram-paulobot/internal/projects"
	"telegram-paulobot/internal/telegram"
)

func (a *App) buildUploadActionView(up guard.PendingUpload) (string, *telegram.InlineKeyboard) {
	cwdShort := a.State.GetCWDShortName(a.State.CurrentCWD)
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

	if up.IsPDF && a.State.FeatureEnabled("obsidian") {
		for _, vp := range obsidian.ListVaultProjects(a.Cfg) {
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

func (a *App) buildChatGuardView(holdID, prompt, reason string) (string, *telegram.InlineKeyboard) {
	title := a.State.CurrentConversationTitle
	if a.State.CurrentEngine == "claude" {
		title = a.State.ClaudeSessionTitle
	}
	if title == "" {
		title = "Último chat activo (-c)"
	}
	cwdShort := a.State.GetCWDShortName(a.State.CurrentCWD)
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
		reason, title, cwdShort, a.State.CurrentCWD, preview,
	)
	return msg, kb
}

func (a *App) buildModelsView() (string, *telegram.InlineKeyboard) {
	effDisp, _, ctxWin := models.GetEffectiveModelInfo(a.Cfg, a.State)
	var rows [][]telegram.InlineButton

	if a.State.CurrentEngine == "claude" {
		cliDef := models.GetClaudeDefaultModel(a.Cfg)
		defPrefix := "🔄 "
		if a.State.ClaudeModel == "" {
			defPrefix = "✅ "
		}
		rows = append(rows, []telegram.InlineButton{
			{Text: fmt.Sprintf("%sPor defecto Claude CLI (%s)", defPrefix, cliDef), CallbackData: "mod:DEFAULT"},
		})
		for _, m := range models.ClaudeCatalog {
			prefix := m.Icon + " "
			if a.State.ClaudeModel == m.ID {
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
			effDisp, strings.ToUpper(a.State.CurrentEffort), ctxWin/1000,
		)
		return msg, &telegram.InlineKeyboard{InlineKeyboard: rows}
	}

	cliDef := models.GetCLIDefaultModel(a.Cfg)
	defPrefix := "🔄 "
	if a.State.CurrentModel == "" {
		defPrefix = "✅ "
	}
	rows = append(rows, []telegram.InlineButton{
		{Text: fmt.Sprintf("%sPor defecto CLI (%s)", defPrefix, cliDef), CallbackData: "mod:DEFAULT"},
	})
	for _, m := range models.DefaultCatalog {
		prefix := m.Icon + " "
		if a.State.CurrentModel == m.ID {
			prefix = "✅ "
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
		effDisp, strings.ToUpper(a.State.CurrentEffort), ctxWin/1000,
	)
	return msg, &telegram.InlineKeyboard{InlineKeyboard: rows}
}

func (a *App) buildProjectsKeyboard(prefix string) *telegram.InlineKeyboard {
	activeCWD := a.State.CurrentCWD
	list := projects.ListAvailableProjects(a.Cfg)
	var rows [][]telegram.InlineButton
	homeMark := "🏠 "
	if filepath.Clean(activeCWD) == filepath.Clean(a.Cfg.HomeDir) {
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

func (a *App) buildResumeKeyboard(prefix, pagePrefix string, page int) (string, *telegram.InlineKeyboard) {
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

	if a.State.CurrentEngine == "claude" {
		header = "⌘ **Sesiones recientes (Claude Code)**"
		for _, s := range chats.ListClaudeSessions(a.Cfg, 0) {
			title := s.Title
			if len([]rune(title)) > 34 {
				title = string([]rune(title)[:34]) + "…"
			}
			items = append(items, chatItem{id: s.SessionID, label: "🟠 " + title})
		}
	} else {
		for _, c := range chats.ListAgyConversations(a.Cfg, 0) {
			title := c.Title
			projTag := ""
			if mapped, ok := a.State.ConversationCWDMap[c.ConversationID]; ok && mapped != "" {
				projTag = " [" + a.State.GetCWDShortName(mapped) + "]"
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

func (a *App) buildConfigKeyboard() (string, *telegram.InlineKeyboard) {
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
		on := a.State.FeatureEnabled(item.key)
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
