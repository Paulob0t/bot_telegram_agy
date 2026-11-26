package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"agente-bot/internal/agent"
	"agente-bot/internal/commands"
	"agente-bot/internal/config"
	"agente-bot/internal/state"
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

	getSessionState := func(sid string) *state.BotSessionState {
		return sessionStore(st).Get(sid)
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

		sessSt := getSessionState(req.SessionID)
		if commands.Handle(commands.Env{
			Cfg:       cfg,
			Agent:     ag,
			Global:    st,
			Session:   sessSt,
			SessionID: req.SessionID,
			Send:      sc.writeJSON,
		}, text) {
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

// chatSessions outlives each hub connection and, through its file, restarts:
// a phone, tunnel or service restart must not reset the app's open chats.
var (
	chatSessionsOnce sync.Once
	chatSessions     *state.SessionStore
)

func sessionStore(global *state.BotSessionState) *state.SessionStore {
	chatSessionsOnce.Do(func() { chatSessions = state.NewSessionStore(global) })
	return chatSessions
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
