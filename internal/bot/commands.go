package bot

import (
	"fmt"
	"os"
	"strings"
	"time"

	"telegram-paulobot/internal/desktop"
	"telegram-paulobot/internal/models"
	"telegram-paulobot/internal/obsidian"
	"telegram-paulobot/internal/projects"
	"telegram-paulobot/internal/telegram"
	"telegram-paulobot/internal/tokens"
	"telegram-paulobot/internal/transport/mobile"
	"telegram-paulobot/internal/utils"
)

func (a *App) handleCommand(chatID int64, text string) {
	fields := strings.Fields(text)
	cmd := fields[0]
	arg := ""
	if len(fields) > 1 {
		arg = strings.TrimSpace(strings.TrimPrefix(text, cmd))
	}

	switch cmd {
	case "/start", "/help", "/ayuda", "/status", "/current":
		up := utils.FormatUptime(int64(time.Since(a.StartTime).Seconds()))
		summary := projects.FormatActiveCWDSummary(a.State.CurrentCWD)
		chatLabel := a.State.CurrentConversationTitle
		if a.State.CurrentEngine == "claude" {
			chatLabel = a.State.ClaudeSessionTitle
		}
		effDisp, _, ctxWin := models.GetEffectiveModelInfo(a.Cfg, a.State)
		msg := fmt.Sprintf(
			"◆ **Paulobot Go** · `%s`\n• 💬 **Chat:** `%s`\n• 🤖 **Modelo activo:** %s\n• ⚡ **Esfuerzo:** `%s` · 🪟 `%dk` ctx\n%s\n• ◷ **Uptime:** `%s`",
			a.State.CurrentEngine, chatLabel, effDisp, strings.ToUpper(a.State.CurrentEffort), ctxWin/1000, summary, up,
		)
		_, _ = a.TG.SendMessage(chatID, msg, nil)

	case "/engine":
		kb := &telegram.InlineKeyboard{
			InlineKeyboard: [][]telegram.InlineButton{
				{
					{Text: "◆ AGY", CallbackData: "eng:agy"},
					{Text: "◇ Claude", CallbackData: "eng:claude"},
				},
			},
		}
		_, _ = a.TG.SendMessage(chatID, fmt.Sprintf("⌘ **Motor actual:** `%s`", a.State.CurrentEngine), kb)

	case "/agy":
		a.State.CurrentEngine = "agy"
		_ = a.State.Save()
		a.Guard.Touch()
		_, _ = a.TG.SendMessage(chatID, fmt.Sprintf("🔵 **Motor Antigravity (AGY) activo**\n• 💬 `%s`\n• 📂 `%s`", a.State.CurrentConversationTitle, a.State.GetCWDShortName(a.State.CurrentCWD)), nil)

	case "/claude":
		a.State.CurrentEngine = "claude"
		_ = a.State.Save()
		a.Guard.Touch()
		_, _ = a.TG.SendMessage(chatID, fmt.Sprintf("🟠 **Motor Claude Code activo**\n• 💬 `%s`\n• 📂 `%s`", a.State.ClaudeSessionTitle, a.State.GetCWDShortName(a.State.CurrentCWD)), nil)

	case "/new", "/nuevo":
		if a.State.CurrentEngine == "claude" {
			a.State.SetClaudeSession("NEW", "➕ Nuevo chat Claude", a.State.CurrentCWD)
		} else {
			a.State.SetAgyConversation("NEW", "➕ Nuevo chat independiente", a.State.CurrentCWD)
		}
		a.Guard.Touch()
		_, _ = a.TG.SendMessage(chatID, fmt.Sprintf("🆕 **Nuevo chat preparado** en 📂 `%s`.\nEl próximo mensaje abrirá una conversación limpia.", a.State.GetCWDShortName(a.State.CurrentCWD)), nil)

	case "/continue", "/continuar":
		if a.State.CurrentEngine == "claude" {
			a.State.SetClaudeSession("", "▸ Último chat Claude (-c)", a.State.CurrentCWD)
		} else {
			a.State.SetAgyConversation("", "▸ Continuar último chat (-c)", a.State.CurrentCWD)
		}
		a.Guard.Touch()
		_, _ = a.TG.SendMessage(chatID, "🔄 **Modo continuar último chat activo (`-c`) activado.**", nil)

	case "/project", "/cd", "/workspace", "/dir", "/repo":
		if arg != "" {
			for _, p := range projects.ListAvailableProjects(a.Cfg) {
				if strings.EqualFold(p.Name, arg) || strings.Contains(strings.ToLower(p.Name), strings.ToLower(arg)) {
					a.State.SetCWD(p.Path)
					_, _ = a.TG.SendMessage(chatID, "✓ "+projects.FormatActiveCWDSummary(p.Path), nil)
					return
				}
			}
		}
		kb := a.buildProjectsKeyboard("proj:")
		_, _ = a.TG.SendMessage(chatID, projects.FormatActiveCWDSummary(a.State.CurrentCWD), kb)

	case "/model", "/modelo":
		msg, kb := a.buildModelsView()
		_, _ = a.TG.SendMessage(chatID, msg, kb)

	case "/effort", "/esfuerzo":
		effs := []string{"low", "medium", "high", "xhigh"}
		var row []telegram.InlineButton
		for _, e := range effs {
			lbl := strings.ToUpper(e)
			if a.State.CurrentEffort == e {
				lbl = "● " + lbl
			}
			row = append(row, telegram.InlineButton{Text: lbl, CallbackData: "eff:" + e})
		}
		_, _ = a.TG.SendMessage(chatID, fmt.Sprintf("⚡ **Nivel de esfuerzo actual:** `%s`", strings.ToUpper(a.State.CurrentEffort)), &telegram.InlineKeyboard{InlineKeyboard: [][]telegram.InlineButton{row}})

	case "/config", "/ajustes":
		msg, kb := a.buildConfigKeyboard()
		_, _ = a.TG.SendMessage(chatID, msg, kb)

	case "/screen", "/ss", "/captura":
		p, err := desktop.CaptureScreen()
		if err != nil {
			_, _ = a.TG.SendMessage(chatID, fmt.Sprintf("✕ Error capturando pantalla: `%v`", err), nil)
			return
		}
		_ = a.TG.SendPhoto(chatID, p, "◆ Captura actual")
		_ = os.Remove(p)

	case "/tokens", "/usage", "/consumo":
		q := tokens.FetchAll(a.Cfg)
		_ = tokens.WriteWaybarJSON(a.Cfg)
		kb := &telegram.InlineKeyboard{
			InlineKeyboard: [][]telegram.InlineButton{
				{{Text: "🔄 Sincronizar en vivo", CallbackData: "tok_sync"}},
			},
		}
		_, _ = a.TG.SendMessage(chatID, tokens.FormatTelegramDashboard(q), kb)

	case "/obsidian", "/prompts", "/vault", "/notas":
		notes := obsidian.ListMarkdownNotes(a.Cfg, 10)
		if len(notes) == 0 {
			_, _ = a.TG.SendMessage(chatID, "○ Sin notas en Obsidian.", nil)
			return
		}
		var rows [][]telegram.InlineButton
		for _, n := range notes {
			rows = append(rows, []telegram.InlineButton{{Text: "▸ " + n.Name, CallbackData: "obs:" + n.Name}})
		}
		_, _ = a.TG.SendMessage(chatID, "◆ **Notas / Prompts (Obsidian)**", &telegram.InlineKeyboard{InlineKeyboard: rows})

	case "/resume", "/chats", "/historial":
		msg, kb := a.buildResumeKeyboard("chat:", "res_page:", 0)
		_, _ = a.TG.SendMessage(chatID, msg, kb)

	case "/mobile", "/ws", "/tailscale":
		_, _ = a.TG.SendMessage(chatID, mobile.FormatConnectionInfo(a.Cfg), nil)

	case "/stop", "/cancel", "/cancelar":
		a.cancelActiveExecution(chatID, false)
	}
}
