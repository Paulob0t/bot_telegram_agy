// Package commands handles the app's slash and control commands once, for both the
// direct mobile server and the Hub relay worker.
package commands

import (
	"fmt"
	"strings"

	"agente-bot/internal/agent"
	"agente-bot/internal/chats"
	"agente-bot/internal/config"
	"agente-bot/internal/models"
	"agente-bot/internal/projects"
	"agente-bot/internal/state"
	"agente-bot/internal/tokens"
)

// OptionsPrefix asks for the picker data: "__options__:/model", "__options__:/resume"...
const OptionsPrefix = "__options__:"

// Env is everything a command needs from the transport that received it.
type Env struct {
	Cfg   *config.Config
	Agent *agent.Service
	// Global is the persisted state the hooks read (features such as mobile_approvals).
	Global *state.BotSessionState
	// Session is the state of the chat that sent the command; it may equal Global.
	Session   *state.BotSessionState
	SessionID string
	// Send writes one JSON message back to the client.
	Send func(v any) error
}

func (e Env) reply(text string) {
	_ = e.Send(agent.Event{Type: agent.EventTypeChunk, Text: text, Done: true, SessionID: e.SessionID})
}

func (e Env) fail(msg string) {
	_ = e.Send(agent.Event{Type: agent.EventTypeError, Message: msg, SessionID: e.SessionID})
}

// Handle runs text when it is a command and reports whether it was one.
// Anything else is a prompt for the agent.
func Handle(e Env, text string) bool {
	text = strings.TrimSpace(text)
	if e.Agent.HandlePermissionCommand(text) {
		return true
	}
	if strings.HasPrefix(text, OptionsPrefix) {
		_ = e.Send(map[string]any{"type": "options", "data": BuildOptions(e.Cfg, e.Session)})
		return true
	}
	if !strings.HasPrefix(text, "/") {
		return false
	}

	fields := strings.Fields(text)
	cmd := strings.ToLower(fields[0])
	arg := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))

	switch cmd {
	case "/stop", "/cancel":
		e.Agent.CancelActive()
	case "/approvals", "/aprobaciones":
		e.reply(agent.ApprovalsToggleReply(e.Global.ToggleFeature("mobile_approvals")))
	case "/status", "/start":
		e.status()
	case "/agy", "/claude":
		e.setEngine(strings.TrimPrefix(cmd, "/"))
	case "/new", "/nuevo":
		resetChat(e.Session, e.Session.CurrentCWD)
		e.reply("Nuevo chat limpio preparado en " + e.Session.GetCWDShortName(e.Session.CurrentCWD))
	case "/cd", "/project":
		e.changeProject(arg)
	case "/model", "/modelo":
		e.setModel(arg)
	case "/resume", "/chats":
		e.resume(arg)
	case "/tokens", "/usage":
		go func() {
			q := tokens.FetchAll(e.Cfg)
			_ = tokens.WriteWaybarJSON(e.Cfg)
			e.reply(tokens.FormatDashboard(q))
		}()
	case "/scan":
		e.reply(FormatScan(Scan(e.Cfg, e.Session)))
	default:
		return false
	}
	return true
}

func (e Env) status() {
	effDisp, _, ctxWin := models.GetEffectiveModelInfo(e.Cfg, e.Session)
	e.reply(fmt.Sprintf(
		"◆ Motor: %s\n• Modelo: %s (%s · %dk ctx)\n• Proyecto:\n%s",
		e.Session.CurrentEngine, effDisp, strings.ToUpper(e.Session.CurrentEffort), ctxWin/1000,
		projects.FormatActiveCWDSummary(e.Session.CurrentCWD),
	))
}

func (e Env) setEngine(engine string) {
	e.Session.CurrentEngine = engine
	_ = e.Session.Save()
	if engine == "claude" {
		e.reply("Motor cambiado a Claude Code.")
		return
	}
	e.reply("Motor cambiado a Antigravity (AGY).")
}

func (e Env) changeProject(target string) {
	if target == "" {
		e.fail("Indica el proyecto: /cd <nombre>")
		return
	}
	p, ok := findProject(e.Cfg, target)
	if !ok {
		e.fail("Proyecto no encontrado: " + target)
		return
	}
	e.Session.SetCWD(p.Path)
	resetChat(e.Session, p.Path)
	e.reply("Proyecto cambiado a " + p.Name)
}

func (e Env) setModel(id string) {
	if id == "" {
		e.fail("Indica el modelo: /model <id>")
		return
	}
	e.Session.CurrentModel = id
	_ = e.Session.Save()
	effDisp, _, _ := models.GetEffectiveModelInfo(e.Cfg, e.Session)
	e.reply("Modelo cambiado a " + effDisp)
}

func (e Env) resume(id string) {
	st := e.Session
	if id == "" || id == NewChatID {
		resetChat(st, st.CurrentCWD)
		e.reply("Nuevo chat limpio preparado en " + st.GetCWDShortName(st.CurrentCWD))
		return
	}
	if st.CurrentEngine == "claude" {
		st.SetClaudeSession(id, id, st.CurrentCWD)
		e.reply("Sesión reanudada.")
		return
	}
	title := chats.GetAgyConversationTitle(e.Cfg, id)
	st.SetAgyConversation(id, title, st.CurrentCWD)
	msg := "Sesión reanudada: " + title
	if preview := chats.GetAgyConversationPreview(id, 4); preview != "" {
		msg += "\n\nÚltimos mensajes:\n" + preview
	}
	e.reply(msg)
}

// resetChat leaves the engine's next prompt starting a fresh conversation in cwd.
func resetChat(st *state.BotSessionState, cwd string) {
	if st.CurrentEngine == "claude" {
		st.SetClaudeSession(NewChatID, "Nuevo chat Claude", cwd)
		return
	}
	st.SetAgyConversation(NewChatID, "Nuevo chat independiente", cwd)
}

// findProject matches by exact name or path first, then by partial name.
func findProject(cfg *config.Config, target string) (projects.Project, bool) {
	list := projects.ListAvailableProjects(cfg)
	for _, p := range list {
		if strings.EqualFold(p.Name, target) || p.Path == target {
			return p, true
		}
	}
	lower := strings.ToLower(target)
	for _, p := range list {
		if strings.Contains(strings.ToLower(p.Name), lower) {
			return p, true
		}
	}
	return projects.Project{}, false
}
