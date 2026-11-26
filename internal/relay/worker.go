package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"telegram-paulobot/internal/agent"
	"telegram-paulobot/internal/chats"
	"telegram-paulobot/internal/config"
	"telegram-paulobot/internal/models"
	"telegram-paulobot/internal/projects"
	"telegram-paulobot/internal/state"
)

// RunWorkerWithURL connects outbound to an explicit Hub WebSocket URL (e.g. ws://127.0.0.1:8080/ws/worker?role=cel)
// and reconnects with exponential backoff on disconnect.
func RunWorkerWithURL(ctx context.Context, cfg *config.Config, st *state.BotSessionState, ag *agent.Service, wsURL, hubSecret string) {
	backoff := 2 * time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}
		connectedAt := time.Now()
		err := connectAndServeWorker(ctx, cfg, st, ag, wsURL, hubSecret)
		if ctx.Err() != nil {
			return
		}
		if time.Since(connectedAt) > 30*time.Second {
			backoff = 2 * time.Second
		}
		log.Printf("relay worker: desconectado (%v), reintentando en %s...", err, backoff)
		if !sleepWithContext(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff, maxBackoff)
	}
}

// RunWorker connects outbound to the Hub's /ws/worker endpoint using the URL from GitHub Gist,
// executes incoming prompts via ag.Execute, and streams events back to the Hub.
func RunWorker(ctx context.Context, cfg *config.Config, st *state.BotSessionState, ag *agent.Service, hubSecret, gistToken, gistID string) {
	backoff := 2 * time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		hubURL, err := FetchActiveHubURL(ctx, gistToken, gistID)
		if err != nil {
			log.Printf("relay worker: error obteniendo hub_url del Gist: %v", err)
			if !sleepWithContext(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}

		wsURL, err := buildWorkerWSURL(hubURL)
		if err != nil {
			log.Printf("relay worker: URL inválida (%q): %v", hubURL, err)
			if !sleepWithContext(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}

		connectedAt := time.Now()
		err = connectAndServeWorker(ctx, cfg, st, ag, wsURL, hubSecret)
		if ctx.Err() != nil {
			return
		}

		if time.Since(connectedAt) > 30*time.Second {
			backoff = 2 * time.Second
		}
		log.Printf("relay worker: desconectado (%v), reintentando en %s...", err, backoff)
		if !sleepWithContext(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff, maxBackoff)
	}
}

func connectAndServeWorker(ctx context.Context, cfg *config.Config, st *state.BotSessionState, ag *agent.Service, wsURL, hubSecret string) error {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+hubSecret)

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}
	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return err
	}

	sc := &safeConn{conn: conn}
	defer sc.close()

	log.Printf("relay worker: conectado a %s", wsURL)
	runKeepAlive(sc)

	unsub := ag.Subscribe(func(ev agent.Event) {
		if ev.Type == agent.EventTypeNotify || ev.Type == agent.EventTypePermission {
			_ = sc.writeJSON(ev)
		}
	})
	defer unsub()

	sessionStates := make(map[string]*state.BotSessionState)
	getSessionState := func(sid string) *state.BotSessionState {
		sid = strings.TrimSpace(sid)
		if sid == "" || sid == "default" {
			return st
		}
		if existing, ok := sessionStates[sid]; ok {
			return existing
		}
		cloned := state.New(cfg)
		cloned.CurrentCWD = st.CurrentCWD
		cloned.CurrentEngine = st.CurrentEngine
		cloned.CurrentModel = st.CurrentModel
		cloned.CurrentConversationID = "NEW"
		cloned.CurrentConversationTitle = "Nuevo chat independiente"
		sessionStates[sid] = cloned
		return cloned
	}

	for {
		msgType, payload, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if msgType != websocket.TextMessage {
			continue
		}

		var req agent.Request
		if err := json.Unmarshal(payload, &req); err != nil {
			_ = sc.writeJSON(agent.Event{
				Type:    agent.EventTypeError,
				Message: fmt.Sprintf("JSON inválido: %v", err),
			})
			continue
		}

		text := strings.TrimSpace(req.Text)
		if text == "" {
			_ = sc.writeJSON(agent.Event{
				Type:    agent.EventTypeError,
				Message: "el campo text es obligatorio",
			})
			continue
		}

		if text == "/stop" || text == "/cancel" {
			ag.CancelActive()
			continue
		}

		if ag.HandlePermissionCommand(text) {
			continue
		}

		// Global state, not the per-session clone: the hook process reads the saved state file.
		if text == "/approvals" || text == "/aprobaciones" {
			_ = sc.writeJSON(agent.Event{
				Type:      agent.EventTypeChunk,
				Text:      agent.ApprovalsToggleReply(st.ToggleFeature("mobile_approvals")),
				Done:      true,
				SessionID: req.SessionID,
			})
			continue
		}

		sessSt := getSessionState(req.SessionID)

		if text == "/new" || text == "/nuevo" {
			if sessSt.CurrentEngine == "claude" {
				sessSt.SetClaudeSession("NEW", "Nuevo chat Claude", sessSt.CurrentCWD)
			} else {
				sessSt.SetAgyConversation("NEW", "Nuevo chat independiente", sessSt.CurrentCWD)
			}
			_ = sc.writeJSON(agent.Event{
				Type:      agent.EventTypeChunk,
				Text:      "Nuevo chat limpio preparado.",
				Done:      true,
				SessionID: req.SessionID,
			})
			continue
		}

		if strings.HasPrefix(text, "__options__:") {
			_ = sc.writeJSON(map[string]any{
				"type": "options",
				"data": buildWorkerOptions(cfg, sessSt),
			})
			continue
		}

		if strings.HasPrefix(text, "/cd ") {
			target := strings.TrimSpace(strings.TrimPrefix(text, "/cd "))
			resolved := ""
			for _, p := range projects.ListAvailableProjects(cfg) {
				if strings.EqualFold(p.Name, target) || p.Path == target {
					resolved = p.Path
					break
				}
			}
			if resolved != "" {
				sessSt.CurrentCWD = resolved
				if sessSt.CurrentEngine == "claude" {
					sessSt.SetClaudeSession("NEW", "Nuevo chat Claude", resolved)
				} else {
					sessSt.SetAgyConversation("NEW", "Nuevo chat independiente", resolved)
				}
				_ = sc.writeJSON(agent.Event{
					Type:      agent.EventTypeChunk,
					Text:      "Proyecto cambiado a " + target,
					Done:      true,
					SessionID: req.SessionID,
				})
			} else {
				_ = sc.writeJSON(agent.Event{
					Type:      agent.EventTypeError,
					Message:   "Proyecto no encontrado: " + target,
					SessionID: req.SessionID,
				})
			}
			continue
		}

		if strings.HasPrefix(text, "/model ") {
			mod := strings.TrimSpace(strings.TrimPrefix(text, "/model "))
			sessSt.CurrentModel = mod
			_ = sc.writeJSON(agent.Event{
				Type:      agent.EventTypeChunk,
				Text:      "Modelo cambiado a " + mod,
				Done:      true,
				SessionID: req.SessionID,
			})
			continue
		}

		if strings.HasPrefix(text, "/resume ") {
			id := strings.TrimSpace(strings.TrimPrefix(text, "/resume "))
			if id == "NEW" {
				if sessSt.CurrentEngine == "claude" {
					sessSt.SetClaudeSession("NEW", "Nuevo chat Claude", sessSt.CurrentCWD)
				} else {
					sessSt.SetAgyConversation("NEW", "Nuevo chat independiente", sessSt.CurrentCWD)
				}
				_ = sc.writeJSON(agent.Event{
					Type:      agent.EventTypeChunk,
					Text:      "Nuevo chat limpio preparado en " + sessSt.GetCWDShortName(sessSt.CurrentCWD),
					Done:      true,
					SessionID: req.SessionID,
				})
				continue
			}
			if sessSt.CurrentEngine == "claude" {
				sessSt.SetClaudeSession(id, id, sessSt.CurrentCWD)
			} else {
				sessSt.SetAgyConversation(id, id, sessSt.CurrentCWD)
			}
			msg := "Sesión reanudada: " + id
			if preview := chats.GetAgyConversationPreview(id, 4); preview != "" {
				msg += "\n\n📜 Últimos mensajes:\n" + preview
			}
			_ = sc.writeJSON(agent.Event{
				Type:      agent.EventTypeChunk,
				Text:      msg,
				Done:      true,
				SessionID: req.SessionID,
			})
			continue
		}

		req.Source = "mobile"
		go func(r agent.Request, sState *state.BotSessionState) {
			_, _ = ag.ExecuteWithState(ctx, sState, r, func(ev agent.Event) {
				ev.SessionID = r.SessionID
				_ = sc.writeJSON(ev)
			})
		}(req, sessSt)
	}
}

func buildWorkerWSURL(hubURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(hubURL))
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	case "wss", "ws":
	default:
		return "", fmt.Errorf("esquema no soportado: %s", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/ws/worker"
	return u.String(), nil
}

func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// maxResumeChats caps the resume list sent to the phone.
const maxResumeChats = 30

func buildWorkerOptions(cfg *config.Config, st *state.BotSessionState) map[string]any {
	projs := projects.ListAvailableProjects(cfg)
	type projItem struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	var projList []projItem
	for _, p := range projs {
		projList = append(projList, projItem{Name: p.Name, Path: p.Path})
	}

	type chatItem struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	var chatList []chatItem
	chatList = append(chatList, chatItem{ID: "NEW", Title: "➕ Nuevo chat limpio en este proyecto"})
	if st.CurrentEngine == "claude" {
		// Filter the full history first: a global top-N cut leaves most projects empty.
		for _, cs := range chats.ListClaudeSessions(cfg, 0) {
			if st.CurrentCWD != "" && filepath.Clean(cs.CWD) != filepath.Clean(st.CurrentCWD) {
				continue
			}
			chatList = append(chatList, chatItem{ID: cs.SessionID, Title: cs.Title})
			if len(chatList) > maxResumeChats {
				break
			}
		}
	} else {
		allConvs := chats.ListAgyConversations(cfg, 0)
		var matching []chatItem
		for _, c := range allConvs {
			wsTag := ""
			isMatch := false
			// Exact workspace match: a substring test made the home folder match every chat.
			if st.CurrentCWD != "" && (strings.Contains(c.WorkspaceURIs, "file://"+filepath.Clean(st.CurrentCWD)+`"`) ||
				strings.Contains(c.WorkspaceURIs, "file://"+filepath.Clean(st.CurrentCWD)+`/"`)) {
				isMatch = true
			}
			if idx := strings.Index(c.WorkspaceURIs, "file://"); idx != -1 {
				rest := c.WorkspaceURIs[idx+len("file://"):]
				if end := strings.IndexAny(rest, `"]`); end != -1 {
					rest = rest[:end]
				}
				parts := strings.Split(strings.TrimRight(rest, "/"), "/")
				if len(parts) > 0 && parts[len(parts)-1] != "" {
					wsTag = "[" + parts[len(parts)-1] + "] "
				}
			}
			item := chatItem{ID: c.ConversationID, Title: wsTag + c.Title}
			if isMatch && len(matching) < maxResumeChats {
				matching = append(matching, item)
			}
		}
		// Only this project's chats: an empty list means the app opens a clean chat directly.
		chatList = append(chatList, matching...)
	}

	_, activeModID, _ := models.GetEffectiveModelInfo(cfg, st)
	return map[string]any{
		"ok":           true,
		"engine":       st.CurrentEngine,
		"active_model": activeModID,
		"active_cwd":   st.CurrentCWD,
		"models":       models.GetCatalog(cfg),
		"projects":     projList,
		"chats":        chatList,
	}
}
