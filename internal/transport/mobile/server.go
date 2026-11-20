package mobile

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"telegram-paulobot/internal/agent"
	"telegram-paulobot/internal/config"
)

const (
	defaultPort  = "8080"
	pingInterval = 25 * time.Second
	pongWait     = 60 * time.Second
)

type Server struct {
	cfg   *config.Config
	agent *agent.Service

	mu          sync.RWMutex
	activeAddrs []string
}

func NewServer(cfg *config.Config, ag *agent.Service) *Server {
	return &Server{
		cfg:   cfg,
		agent: ag,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/health", s.handleHealth)
	return mux
}

func (s *Server) ActiveAddrs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.activeAddrs...)
}

// Run escucha únicamente en interfaces privadas seguras:
// - Si MOBILE_WS_ADDR está definido en .env, escucha exclusivamente ahí.
// - Si no, escucha en loopback (127.0.0.1:8080 para pruebas locales/emulador) y se enlaza automáticamente
//   a la IP de Tailscale (100.x.y.z:8080) en cuanto la interfaz tailscale0 está activa (jamás expone en LAN 0.0.0.0).
func (s *Server) Run(ctx context.Context) {
	if explicit := strings.TrimSpace(s.cfg.MobileWSAddr); explicit != "" {
		s.serveOnAddr(ctx, explicit)
		return
	}

	// 1. Loopback local privado (127.0.0.1:8080) para pruebas de Flutter en PC
	go s.serveOnAddr(ctx, net.JoinHostPort("127.0.0.1", defaultPort))

	// 2. Vigilar interfaz Tailscale (100.x.y.z:8080) y enlazar en cuanto levante
	var boundTailscaleIP string
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		tsIP := config.DetectTailscaleIPv4()
		if tsIP != "" && tsIP != boundTailscaleIP {
			boundTailscaleIP = tsIP
			addr := net.JoinHostPort(tsIP, defaultPort)
			go s.serveOnAddr(ctx, addr)
		}
		time.Sleep(10 * time.Second)
	}
}

func (s *Server) serveOnAddr(ctx context.Context, addr string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		ln, err := net.Listen("tcp", addr)
		if err != nil {
			time.Sleep(10 * time.Second)
			continue
		}

		s.mu.Lock()
		s.activeAddrs = append(s.activeAddrs, addr)
		s.mu.Unlock()

		log.Printf("◆ WebSocket Móvil escuchando en ws://%s/ws", addr)
		httpSrv := &http.Server{
			Handler:           s.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
		}

		go func() {
			<-ctx.Done()
			_ = httpSrv.Close()
		}()

		err = httpSrv.Serve(ln)
		if ctx.Err() != nil || errors.Is(err, http.ErrServerClosed) {
			return
		}
		time.Sleep(5 * time.Second)
	}
}

func (s *Server) isAuthorized(r *http.Request) bool {
	expected := strings.TrimSpace(s.cfg.MobileWSToken)
	if expected == "" {
		return false
	}
	var provided string
	authHdr := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHdr), "bearer ") {
		provided = strings.TrimSpace(authHdr[7:])
	}
	if provided == "" {
		provided = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthorized(r) {
		http.Error(w, "no autorizado", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":    true,
		"state": s.agent.CurrentState(),
	})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthorized(r) {
		http.Error(w, "Bearer token inválido o ausente", http.StatusUnauthorized)
		return
	}

	ws, err := upgradeWebSocket(w, r)
	if err != nil {
		return
	}
	defer ws.Close()

	connCtx, cancelConn := context.WithCancel(r.Context())
	defer cancelConn()

	// 1. Enviar estado inicial de la State Machine de Rive al conectar
	_ = ws.WriteJSON(agent.Event{
		Type:  agent.EventTypeState,
		State: s.agent.CurrentState(),
	})

	// 2. Suscribir este cliente móvil a todos los cambios de estado y chunks del agente
	unsub := s.agent.Subscribe(func(ev agent.Event) {
		_ = ws.WriteJSON(ev)
	})
	defer unsub()

	// 3. Heartbeat Ping cada 25s
	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-connCtx.Done():
				return
			case <-ticker.C:
				if err := ws.WriteFrame(opPing, []byte("ping")); err != nil {
					_ = ws.Close()
					return
				}
			}
		}
	}()

	// 4. Loop de lectura con deadline renovado en cada frame o Pong
	var msgBuf []byte
	for {
		_ = ws.SetReadDeadline(time.Now().Add(pongWait))
		opcode, payload, err := ws.ReadFrame()
		if err != nil {
			return
		}

		switch opcode {
		case opClose:
			_ = ws.WriteFrame(opClose, nil)
			return
		case opPing:
			_ = ws.WriteFrame(opPong, payload)
		case opPong:
		case opText, opContinuation:
			if opcode == opText {
				msgBuf = payload
			} else {
				msgBuf = append(msgBuf, payload...)
			}
			s.processClientMessage(connCtx, ws, msgBuf)
			msgBuf = nil
		}
	}
}

func (s *Server) processClientMessage(ctx context.Context, ws *wsConn, raw []byte) {
	var req agent.Request
	if err := json.Unmarshal(raw, &req); err != nil {
		_ = ws.WriteJSON(agent.Event{
			Type:    agent.EventTypeError,
			Message: fmt.Sprintf("JSON inválido: %v", err),
		})
		return
	}

	text := strings.TrimSpace(req.Text)
	if text == "" {
		_ = ws.WriteJSON(agent.Event{
			Type:    agent.EventTypeError,
			Message: "el campo text es obligatorio",
		})
		return
	}

	if text == "/stop" || text == "/cancel" {
		s.agent.CancelActive()
		return
	}

	req.Source = "mobile"
	go func() {
		_, _ = s.agent.Execute(ctx, req, nil)
	}()
}

func FormatConnectionInfo(cfg *config.Config) string {
	tsIP := config.DetectTailscaleIPv4()
	var sb strings.Builder
	sb.WriteString("📱 **Conexión WebSocket Móvil (Flutter + Rive)**\n\n")
	if tsIP != "" {
		sb.WriteString(fmt.Sprintf("• 🟢 **Tailscale IP:** `%s`\n", tsIP))
		sb.WriteString(fmt.Sprintf("• 🔗 **URL Móvil (Tailscale):** `ws://%s:8080/ws`\n", tsIP))
	} else {
		sb.WriteString("• 🟡 **Tailscale:** `No detectado aún` *(ejecuta `scripts/setup-tailscale`)*\n")
	}
	sb.WriteString("• 💻 **URL Local (PC / Emulador):** `ws://127.0.0.1:8080/ws`\n")
	sb.WriteString(fmt.Sprintf("• 🔑 **Bearer Token (`MOBILE_WS_TOKEN`):** `%s`", cfg.MobileWSToken))
	return sb.String()
}
