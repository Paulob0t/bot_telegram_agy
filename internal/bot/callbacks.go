package bot

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"telegram-paulobot/internal/chats"
	"telegram-paulobot/internal/models"
	"telegram-paulobot/internal/obsidian"
	"telegram-paulobot/internal/projects"
	"telegram-paulobot/internal/telegram"
	"telegram-paulobot/internal/tokens"
)

func (a *App) handleCallback(cb *telegram.CallbackQuery) {
	data := cb.Data
	if a.handleQueueCallback(cb, data) ||
		a.handleUploadCallback(cb, data) ||
		a.handleChatGuardCallback(cb, data) ||
		a.handleSystemCallback(cb, data) {
		return
	}
}

func (a *App) handleQueueCallback(cb *telegram.CallbackQuery, data string) bool {
	switch {
	case data == "q_cancel":
		a.TG.AnswerCallback(cb.ID, "Cancelando árbol de procesos…")
		if cb.Message != nil {
			a.cancelActiveExecution(cb.Message.Chat.ID, false)
		}
		return true

	case data == "q_cancel_all":
		a.TG.AnswerCallback(cb.ID, "Cancelando todo…")
		if cb.Message != nil {
			a.cancelActiveExecution(cb.Message.Chat.ID, true)
		}
		return true

	case strings.HasPrefix(data, "q_force:"):
		jobID := strings.TrimPrefix(data, "q_force:")
		a.forceJobNow(jobID)
		a.TG.AnswerCallback(cb.ID, "Interrumpiendo tarea actual…")
		return true

	case strings.HasPrefix(data, "q_drop:"):
		jobID := strings.TrimPrefix(data, "q_drop:")
		removed := a.dropQueuedJob(jobID)
		a.TG.AnswerCallback(cb.ID, "Retirado de la cola")
		if removed && cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, "🗑️ **Mensaje retirado de la cola.**", nil)
		}
		return true
	}
	return false
}

func (a *App) handleUploadCallback(cb *telegram.CallbackQuery, data string) bool {
	switch {
	case strings.HasPrefix(data, "up_ai:"):
		upID := strings.TrimPrefix(data, "up_ai:")
		up, ok := a.Guard.PopUpload(upID)
		if !ok {
			a.TG.AnswerCallback(cb.ID, "Archivo expirado, reenvíalo")
			return true
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
		a.TG.AnswerCallback(cb.ID, "Analizando con IA…")
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("🤖 **Enviando `%s` a IA** • 📂 `%s`", up.FileName, a.State.GetCWDShortName(a.State.CurrentCWD)), nil)
		}
		a.submitWithGuard(up.ChatID, prompt, []string{up.FilePath})
		return true

	case strings.HasPrefix(data, "up_cwd:"):
		upID := strings.TrimPrefix(data, "up_cwd:")
		up, ok := a.Guard.PopUpload(upID)
		if !ok {
			a.TG.AnswerCallback(cb.ID, "Archivo expirado, reenvíalo")
			return true
		}
		dest, err := obsidian.SaveFileToDir(up.FilePath, a.State.CurrentCWD, up.FileName)
		if err != nil {
			a.TG.AnswerCallback(cb.ID, "Error guardando")
			if cb.Message != nil {
				a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✕ Error guardando archivo: `%v`", err), nil)
			}
			return true
		}
		a.TG.AnswerCallback(cb.ID, "Guardado en proyecto")
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✅ **Guardado en proyecto (0 tokens):**\n`%s`", dest), nil)
		}
		return true

	case strings.HasPrefix(data, "up_dl:"):
		upID := strings.TrimPrefix(data, "up_dl:")
		up, ok := a.Guard.PopUpload(upID)
		if !ok {
			a.TG.AnswerCallback(cb.ID, "Archivo expirado, reenvíalo")
			return true
		}
		dlDir := filepath.Join(a.Cfg.HomeDir, "Downloads")
		dest, err := obsidian.SaveFileToDir(up.FilePath, dlDir, up.FileName)
		if err != nil {
			a.TG.AnswerCallback(cb.ID, "Error guardando")
			if cb.Message != nil {
				a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✕ Error guardando en Downloads: `%v`", err), nil)
			}
			return true
		}
		a.TG.AnswerCallback(cb.ID, "Guardado en Downloads")
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✅ **Guardado en Downloads (0 tokens):**\n`%s`", dest), nil)
		}
		return true

	case strings.HasPrefix(data, "up_pask:"):
		upID := strings.TrimPrefix(data, "up_pask:")
		up, ok := a.Guard.GetUpload(upID)
		if !ok {
			a.TG.AnswerCallback(cb.ID, "Archivo expirado, reenvíalo")
			return true
		}
		kb := a.buildProjectsKeyboard("up_psel:" + upID + ":")
		kb.InlineKeyboard = append(kb.InlineKeyboard, []telegram.InlineButton{
			{Text: "🔙 Volver", CallbackData: "up_back:" + upID},
		})
		a.TG.AnswerCallback(cb.ID, "Elige proyecto destino")
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("📁 **¿En qué proyecto quieres guardar `%s`?**", up.FileName), kb)
		}
		return true

	case strings.HasPrefix(data, "up_psel:"):
		rest := strings.TrimPrefix(data, "up_psel:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return true
		}
		upID, projName := parts[0], parts[1]
		up, ok := a.Guard.PopUpload(upID)
		if !ok {
			a.TG.AnswerCallback(cb.ID, "Archivo expirado, reenvíalo")
			return true
		}
		targetDir := a.Cfg.HomeDir
		if projName != "HOME" {
			for _, p := range projects.ListAvailableProjects(a.Cfg) {
				if p.Name == projName {
					targetDir = p.Path
					break
				}
			}
		}
		dest, err := obsidian.SaveFileToDir(up.FilePath, targetDir, up.FileName)
		if err != nil {
			a.TG.AnswerCallback(cb.ID, "Error guardando")
			return true
		}
		a.TG.AnswerCallback(cb.ID, "Guardado en "+projName)
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✅ **Guardado en `%s` (0 tokens):**\n`%s`", projName, dest), nil)
		}
		return true

	case strings.HasPrefix(data, "up_obs:"):
		rest := strings.TrimPrefix(data, "up_obs:")
		parts := strings.SplitN(rest, ":", 3)
		if len(parts) != 3 {
			return true
		}
		upID, projName, action := parts[0], parts[1], parts[2]
		up, ok := a.Guard.PopUpload(upID)
		if !ok {
			a.TG.AnswerCallback(cb.ID, "Archivo expirado, reenvíalo")
			return true
		}
		dest, err := obsidian.SavePDFToVaultProject(a.Cfg, up.FilePath, projName, up.FileName)
		if err != nil {
			a.TG.AnswerCallback(cb.ID, "Error guardando en Obsidian")
			if cb.Message != nil {
				a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✕ Error guardando PDF en Obsidian: `%v`", err), nil)
			}
			return true
		}
		if action == "save" {
			a.TG.AnswerCallback(cb.ID, "Guardado en Obsidian")
			if cb.Message != nil {
				a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✅ **PDF guardado en Obsidian (0 tokens):**\n`%s`", dest), nil)
			}
			return true
		}
		prompt, targetCWD := obsidian.BuildWicodeGeneratorPrompt(a.Cfg, projName, dest, a.State.CurrentCWD)
		a.State.SetCWD(targetCWD)
		a.Guard.Touch()
		a.TG.AnswerCallback(cb.ID, "Iniciando Wicode Generator…")
		if cb.Message != nil {
			a.TG.EditMessage(
				cb.Message.Chat.ID,
				cb.Message.MessageID,
				fmt.Sprintf("⚡ **PDF guardado en `%s/PDF/%s`**\n🚀 _Iniciando `wicode-prompt-generator` (Nota Markdown + Prompts T01..TXX)…_", projName, up.FileName),
				nil,
			)
		}
		a.enqueuePrompt(up.ChatID, prompt, []string{dest})
		return true

	case strings.HasPrefix(data, "up_back:"):
		upID := strings.TrimPrefix(data, "up_back:")
		up, ok := a.Guard.GetUpload(upID)
		if !ok {
			a.TG.AnswerCallback(cb.ID, "Archivo expirado")
			return true
		}
		msg, kb := a.buildUploadActionView(up)
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, msg, kb)
		}
		return true

	case strings.HasPrefix(data, "up_drop:"):
		upID := strings.TrimPrefix(data, "up_drop:")
		if up, ok := a.Guard.PopUpload(upID); ok {
			_ = os.Remove(up.FilePath)
		}
		a.TG.AnswerCallback(cb.ID, "Archivo descartado")
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, "🗑️ Archivo descartado y eliminado de caché.", nil)
		}
		return true
	}
	return false
}

func (a *App) handleChatGuardCallback(cb *telegram.CallbackQuery, data string) bool {
	switch {
	case strings.HasPrefix(data, "cg_cont:"):
		holdID := strings.TrimPrefix(data, "cg_cont:")
		held, ok := a.Guard.Pop(holdID)
		if !ok {
			a.TG.AnswerCallback(cb.ID, "Expiró, reenvía el mensaje")
			return true
		}
		a.Guard.Touch()
		a.TG.AnswerCallback(cb.ID, "Enviando…")
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✅ Enviado a **%s** • 📂 `%s`", a.State.CurrentConversationTitle, a.State.GetCWDShortName(a.State.CurrentCWD)), nil)
		}
		a.enqueuePrompt(held.ChatID, held.Prompt, held.TempFiles)
		return true

	case strings.HasPrefix(data, "cg_new:"):
		holdID := strings.TrimPrefix(data, "cg_new:")
		held, ok := a.Guard.Pop(holdID)
		if !ok {
			a.TG.AnswerCallback(cb.ID, "Expiró, reenvía el mensaje")
			return true
		}
		if a.State.CurrentEngine == "claude" {
			a.State.SetClaudeSession("NEW", "➕ Nuevo chat Claude", a.State.CurrentCWD)
		} else {
			a.State.SetAgyConversation("NEW", "➕ Nuevo chat independiente", a.State.CurrentCWD)
		}
		a.Guard.Touch()
		a.TG.AnswerCallback(cb.ID, "Nuevo chat…")
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✅ Enviado a **🆕 Nuevo chat** • 📂 `%s`", a.State.GetCWDShortName(a.State.CurrentCWD)), nil)
		}
		a.enqueuePrompt(held.ChatID, held.Prompt, held.TempFiles)
		return true

	case strings.HasPrefix(data, "cg_drop:"):
		holdID := strings.TrimPrefix(data, "cg_drop:")
		if held, ok := a.Guard.Pop(holdID); ok {
			for _, tf := range held.TempFiles {
				_ = os.Remove(tf)
			}
		}
		a.TG.AnswerCallback(cb.ID, "Descartado")
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, "🗑️ Mensaje descartado.", nil)
		}
		return true

	case strings.HasPrefix(data, "cg_proj:"):
		holdID := strings.TrimPrefix(data, "cg_proj:")
		held, ok := a.Guard.Get(holdID)
		if !ok {
			a.TG.AnswerCallback(cb.ID, "Expiró, reenvía el mensaje")
			return true
		}
		kb := a.buildProjectsKeyboard("cg_psel:" + holdID + ":")
		kb.InlineKeyboard = append(kb.InlineKeyboard, []telegram.InlineButton{
			{Text: "🔙 Volver al mensaje", CallbackData: "cg_back:" + holdID},
		})
		a.TG.AnswerCallback(cb.ID, "Elige proyecto")
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("📁 **Selecciona el proyecto (`cwd`) para tu mensaje:**\n`%s`", held.Prompt), kb)
		}
		return true

	case strings.HasPrefix(data, "cg_psel:"):
		rest := strings.TrimPrefix(data, "cg_psel:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return true
		}
		holdID, projName := parts[0], parts[1]
		if projName == "HOME" {
			a.State.SetCWD(a.Cfg.HomeDir)
		} else {
			for _, p := range projects.ListAvailableProjects(a.Cfg) {
				if p.Name == projName {
					a.State.SetCWD(p.Path)
					break
				}
			}
		}
		held, ok := a.Guard.Get(holdID)
		if !ok {
			a.TG.AnswerCallback(cb.ID, "Proyecto cambiado")
			return true
		}
		a.TG.AnswerCallback(cb.ID, "Proyecto: "+projName)
		if cb.Message != nil {
			text, kb := a.buildChatGuardView(holdID, held.Prompt, "proyecto actualizado")
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, text, kb)
		}
		return true

	case strings.HasPrefix(data, "cg_pick:"):
		rest := strings.TrimPrefix(data, "cg_pick:")
		holdID := rest
		page := 0
		if parts := strings.SplitN(rest, ":", 2); len(parts) == 2 {
			holdID = parts[0]
			page, _ = strconv.Atoi(parts[1])
		}
		if _, ok := a.Guard.Get(holdID); !ok {
			a.TG.AnswerCallback(cb.ID, "Expiró, reenvía el mensaje")
			return true
		}
		msg, kb := a.buildResumeKeyboard("cg_csel:"+holdID+":", "cg_pick:"+holdID+":", page)
		kb.InlineKeyboard = append(kb.InlineKeyboard, []telegram.InlineButton{
			{Text: "🔙 Volver", CallbackData: "cg_back:" + holdID},
		})
		a.TG.AnswerCallback(cb.ID, "Elige chat")
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, msg, kb)
		}
		return true

	case strings.HasPrefix(data, "cg_csel:"):
		rest := strings.TrimPrefix(data, "cg_csel:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return true
		}
		holdID, targetID := parts[0], parts[1]
		held, ok := a.Guard.Pop(holdID)
		if !ok {
			a.TG.AnswerCallback(cb.ID, "Expiró, reenvía el mensaje")
			return true
		}
		a.selectConversation(targetID)
		a.Guard.Touch()
		a.TG.AnswerCallback(cb.ID, "Enviando…")
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✅ Enviado a **%s** • 📂 `%s`", a.State.CurrentConversationTitle, a.State.GetCWDShortName(a.State.CurrentCWD)), nil)
		}
		a.enqueuePrompt(held.ChatID, held.Prompt, held.TempFiles)
		return true

	case strings.HasPrefix(data, "cg_back:"):
		holdID := strings.TrimPrefix(data, "cg_back:")
		held, ok := a.Guard.Get(holdID)
		if !ok {
			a.TG.AnswerCallback(cb.ID, "Expiró")
			return true
		}
		text, kb := a.buildChatGuardView(holdID, held.Prompt, "confirmación")
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, text, kb)
		}
		return true
	}
	return false
}

func (a *App) handleSystemCallback(cb *telegram.CallbackQuery, data string) bool {
	switch {
	case data == "tok_sync":
		a.TG.AnswerCallback(cb.ID, "Sincronizando cuotas…")
		q := tokens.FetchAll(a.Cfg)
		_ = tokens.WriteWaybarJSON(a.Cfg)
		kb := &telegram.InlineKeyboard{
			InlineKeyboard: [][]telegram.InlineButton{
				{{Text: "🔄 Sincronizar en vivo", CallbackData: "tok_sync"}},
			},
		}
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, tokens.FormatTelegramDashboard(q), kb)
		}
		return true

	case strings.HasPrefix(data, "res_page:"):
		page, _ := strconv.Atoi(strings.TrimPrefix(data, "res_page:"))
		msg, kb := a.buildResumeKeyboard("chat:", "res_page:", page)
		a.TG.AnswerCallback(cb.ID, "")
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, msg, kb)
		}
		return true

	case data == "res_noop":
		a.TG.AnswerCallback(cb.ID, "")
		return true

	case strings.HasPrefix(data, "eng:"):
		eng := strings.TrimPrefix(data, "eng:")
		a.State.CurrentEngine = eng
		_ = a.State.Save()
		a.Guard.Touch()
		a.TG.AnswerCallback(cb.ID, "Motor: "+eng)
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✓ **Motor cambiado a:** `%s`", eng), nil)
		}
		return true

	case strings.HasPrefix(data, "proj:"):
		name := strings.TrimPrefix(data, "proj:")
		if name == "HOME" {
			a.State.SetCWD(a.Cfg.HomeDir)
			a.TG.AnswerCallback(cb.ID, "Proyecto: Home")
			if cb.Message != nil {
				a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, "✓ "+projects.FormatActiveCWDSummary(a.Cfg.HomeDir), nil)
			}
			return true
		}
		for _, p := range projects.ListAvailableProjects(a.Cfg) {
			if p.Name == name {
				a.State.SetCWD(p.Path)
				a.TG.AnswerCallback(cb.ID, "Proyecto: "+name)
				if cb.Message != nil {
					a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, "✓ "+projects.FormatActiveCWDSummary(p.Path), nil)
				}
				return true
			}
		}
		return true

	case strings.HasPrefix(data, "obs:"):
		name := strings.TrimPrefix(data, "obs:")
		notes := obsidian.ListMarkdownNotes(a.Cfg, 25)
		for idx, n := range notes {
			if n.Name == name {
				content, err := obsidian.ReadNote(n.AbsPath, 3300)
				if err != nil {
					a.TG.AnswerCallback(cb.ID, "Error leyendo nota")
					return true
				}
				kb := &telegram.InlineKeyboard{
					InlineKeyboard: [][]telegram.InlineButton{
						{{Text: "🚀 Lanzar como Prompt en AGY/Claude", CallbackData: fmt.Sprintf("obs_run:%d", idx)}},
					},
				}
				a.TG.AnswerCallback(cb.ID, n.Name)
				if cb.Message != nil {
					a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("◆ **%s**\n\n%s", n.Name, content), kb)
				}
				return true
			}
		}
		return true

	case strings.HasPrefix(data, "obs_run:"):
		idxStr := strings.TrimPrefix(data, "obs_run:")
		idx, err := strconv.Atoi(idxStr)
		notes := obsidian.ListMarkdownNotes(a.Cfg, 25)
		if err != nil || idx < 0 || idx >= len(notes) {
			a.TG.AnswerCallback(cb.ID, "Nota no encontrada")
			return true
		}
		n := notes[idx]
		content, err := obsidian.ReadNote(n.AbsPath, 0)
		if err != nil || strings.TrimSpace(content) == "" {
			a.TG.AnswerCallback(cb.ID, "Nota vacía")
			return true
		}
		a.Guard.Touch()
		a.TG.AnswerCallback(cb.ID, "Lanzando "+n.Name)
		if cb.Message != nil {
			a.enqueuePrompt(cb.Message.Chat.ID, content, nil)
		}
		return true

	case strings.HasPrefix(data, "chat:"):
		sid := strings.TrimPrefix(data, "chat:")
		a.selectConversation(sid)
		a.Guard.Touch()
		a.TG.AnswerCallback(cb.ID, "Sesión seleccionada")
		if cb.Message != nil {
			label := a.State.CurrentConversationTitle
			if a.State.CurrentEngine == "claude" {
				label = a.State.ClaudeSessionTitle
			}
			a.TG.EditMessage(
				cb.Message.Chat.ID,
				cb.Message.MessageID,
				fmt.Sprintf("✓ **Chat activo:** `%s`\n📂 **Proyecto (`cwd`):** `%s`", label, a.State.GetCWDShortName(a.State.CurrentCWD)),
				nil,
			)
		}
		return true

	case strings.HasPrefix(data, "mod:"):
		mod := strings.TrimPrefix(data, "mod:")
		if mod == "DEFAULT" {
			mod = ""
		}
		if a.State.CurrentEngine == "claude" {
			a.State.ClaudeModel = mod
		} else {
			a.State.CurrentModel = mod
			for _, eff := range []string{"xhigh", "low", "medium", "high"} {
				if strings.HasSuffix(strings.ToLower(mod), "-"+eff) {
					a.State.CurrentEffort = eff
					break
				}
			}
		}
		_ = a.State.Save()
		effDisp, _, ctxWin := models.GetEffectiveModelInfo(a.Cfg, a.State)
		a.TG.AnswerCallback(cb.ID, "Modelo actualizado")
		if cb.Message != nil {
			summary := fmt.Sprintf(
				"✅ **Modelo configurado y guardado:**\n\n"+
					"• 🤖 **Modelo activo:** %s\n"+
					"• ⚡ **Esfuerzo sincronizado:** `%s`\n"+
					"• 🪟 **Ventana de contexto:** `%dk` tokens",
				effDisp, strings.ToUpper(a.State.CurrentEffort), ctxWin/1000,
			)
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, summary, nil)
		}
		return true

	case strings.HasPrefix(data, "eff:"):
		eff := strings.TrimPrefix(data, "eff:")
		a.State.CurrentEffort = eff
		_ = a.State.Save()
		a.TG.AnswerCallback(cb.ID, "Esfuerzo: "+eff)
		if cb.Message != nil {
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, fmt.Sprintf("✓ **Esfuerzo:** `%s`", strings.ToUpper(eff)), nil)
		}
		return true

	case strings.HasPrefix(data, "cfg:"):
		key := strings.TrimPrefix(data, "cfg:")
		a.State.ToggleFeature(key)
		a.TG.AnswerCallback(cb.ID, "Actualizado")
		if cb.Message != nil {
			msg, kb := a.buildConfigKeyboard()
			a.TG.EditMessage(cb.Message.Chat.ID, cb.Message.MessageID, msg, kb)
		}
		return true
	}
	return false
}

func (a *App) selectConversation(targetID string) {
	if a.State.CurrentEngine == "claude" {
		switch targetID {
		case "LATEST":
			a.State.SetClaudeSession("", "▸ Último chat Claude (-c)", a.State.CurrentCWD)
		case "NEW":
			a.State.SetClaudeSession("NEW", "➕ Nuevo chat Claude", a.State.CurrentCWD)
		default:
			title := "Sesión Claude (" + targetID + ")"
			cwd := a.State.CurrentCWD
			for _, s := range chats.ListClaudeSessions(a.Cfg, 30) {
				if s.SessionID == targetID {
					title = s.Title
					if s.CWD != "" {
						cwd = s.CWD
					}
					break
				}
			}
			a.State.SetClaudeSession(targetID, title, cwd)
		}
		return
	}

	switch targetID {
	case "LATEST":
		a.State.SetAgyConversation("", "▸ Continuar último chat (-c)", "")
	case "NEW":
		a.State.SetAgyConversation("NEW", "➕ Nuevo chat independiente", "")
	default:
		title := chats.GetAgyConversationTitle(a.Cfg, targetID)
		a.State.SetAgyConversation(targetID, "💬 "+title, "")
	}
}
