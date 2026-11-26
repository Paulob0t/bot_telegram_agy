package mobile

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"agente-bot/internal/agent"
	"agente-bot/internal/commands"
	"agente-bot/internal/config"
	"agente-bot/internal/obsidian"
	"agente-bot/internal/state"
	"agente-bot/internal/uploads"
)

const (
	defaultPort  = "8080"
	pingInterval = 25 * time.Second
	pongWait     = 60 * time.Second
)

type Server struct {
	cfg   *config.Config
	st    *state.BotSessionState
	agent *agent.Service

	mu          sync.RWMutex
	activeAddrs []string
}

func NewServer(cfg *config.Config, ag *agent.Service, stOpt ...*state.BotSessionState) *Server {
	var st *state.BotSessionState
	if len(stOpt) > 0 && stOpt[0] != nil {
		st = stOpt[0]
	} else {
		st = state.New(cfg)
	}
	return &Server{
		cfg:   cfg,
		st:    st,
		agent: ag,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/notify", s.handleNotify)
	mux.HandleFunc("/permission", s.handlePermission)
	mux.HandleFunc("/upload", s.handleUpload)
	mux.HandleFunc("/scan", s.handleScan)
	mux.HandleFunc("/options", s.handleOptions)
	mux.HandleFunc("/version", s.handleVersion)
	mux.HandleFunc("/apk", s.handleAPK)
	mux.HandleFunc("/avatar/from-photo", s.handleAvatarFromPhoto)
	mux.HandleFunc("/obsidian", s.handleObsidian)
	return mux
}

func (s *Server) ActiveAddrs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.activeAddrs...)
}

// Run escucha únicamente en interfaces privadas seguras:
//   - Si MOBILE_WS_ADDR está definido en .env, escucha exclusivamente ahí.
//   - Si no, escucha en loopback (127.0.0.1:8080 para pruebas locales/emulador) y se enlaza automáticamente
//     a la IP de Tailscale (100.x.y.z:8080) en cuanto la interfaz tailscale0 está activa (jamás expone en LAN 0.0.0.0).
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

func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthorized(r) {
		http.Error(w, "no autorizado", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "método no permitido", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "JSON inválido", http.StatusBadRequest)
		return
	}
	if title := strings.TrimSpace(body.Title); title != "" || strings.TrimSpace(body.Body) != "" {
		s.agent.EmitExternalNotification(title, strings.TrimSpace(body.Body))
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// permissionWait bounds how long a hook blocks waiting for the phone to answer.
const permissionWait = 120 * time.Second

func (s *Server) handlePermission(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthorized(r) {
		http.Error(w, "no autorizado", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "método no permitido", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Tool      string `json:"tool"`
		Detail    string `json:"detail"`
		Workspace string `json:"workspace"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&body); err != nil {
		http.Error(w, "JSON inválido", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), permissionWait)
	defer cancel()
	decision := s.agent.RequestPermission(ctx, body.Tool, body.Detail, body.Workspace)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"decision": string(decision)})
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthorized(r) {
		http.Error(w, "no autorizado", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "método no permitido", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "multipart inválido", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "falta campo file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	target := strings.ToLower(strings.TrimSpace(r.FormValue("target")))
	fn := strings.ReplaceAll(filepath.Base(header.Filename), " ", "_")
	if fn == "" || fn == "." {
		fn = fmt.Sprintf("upload_%d", time.Now().Unix())
	}

	uploadsDir := uploads.PrepareDir(s.cfg.HomeDir)
	cachePath := filepath.Join(uploadsDir, fmt.Sprintf("%d_%s", time.Now().Unix(), fn))
	out, err := os.Create(cachePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		_ = out.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = out.Close()

	finalPath := cachePath
	switch target {
	case "cwd":
		if dest, err := obsidian.SaveFileToDir(cachePath, s.st.CurrentCWD, fn); err == nil {
			finalPath = dest
		}
	case "downloads":
		dlDir := filepath.Join(s.cfg.HomeDir, "Downloads")
		if dest, err := obsidian.SaveFileToDir(cachePath, dlDir, fn); err == nil {
			finalPath = dest
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":        true,
		"path":      finalPath,
		"file_name": fn,
	})
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthorized(r) {
		http.Error(w, "no autorizado", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(commands.Scan(s.cfg, s.st))
}

func (s *Server) handleOptions(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthorized(r) {
		http.Error(w, "no autorizado", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(commands.BuildOptions(s.cfg, s.st))
}

func (s *Server) apkFilePath() string {
	return filepath.Join("/mnt/datos/Projects/agente-bot/mobile_app/build/app/outputs/flutter-apk/app-release.apk")
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthorized(r) {
		http.Error(w, "no autorizado", http.StatusUnauthorized)
		return
	}
	verStr := "1.0.0+1"
	buildNum := 1
	if raw, err := os.ReadFile("/mnt/datos/Projects/agente-bot/mobile_app/pubspec.yaml"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "version:") {
				verStr = strings.TrimSpace(strings.TrimPrefix(line, "version:"))
				if parts := strings.Split(verStr, "+"); len(parts) == 2 {
					fmt.Sscanf(parts[1], "%d", &buildNum)
				}
				break
			}
		}
	}
	_, err := os.Stat(s.apkFilePath())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":         true,
		"version":    verStr,
		"build":      buildNum,
		"apk_exists": err == nil,
	})
}

func (s *Server) handleAPK(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthorized(r) {
		http.Error(w, "no autorizado", http.StatusUnauthorized)
		return
	}
	apkPath := s.apkFilePath()
	if _, err := os.Stat(apkPath); err != nil {
		http.Error(w, "APK no compilado aún (ejecuta flutter build apk --release)", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="paulobot-mobile.apk"`)
	http.ServeFile(w, r, apkPath)
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

	if commands.Handle(commands.Env{
		Cfg:       s.cfg,
		Agent:     s.agent,
		Global:    s.st,
		Session:   s.st,
		SessionID: req.SessionID,
		Send:      ws.WriteJSON,
	}, text) {
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
