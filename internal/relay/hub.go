package relay

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	pingInterval = 10 * time.Second
	pongWait     = 25 * time.Second
	writeWait    = 5 * time.Second
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type safeConn struct {
	conn     *websocket.Conn
	mu       sync.Mutex
	forceCel bool
}

func (sc *safeConn) writeMessage(msgType int, data []byte) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	_ = sc.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return sc.conn.WriteMessage(msgType, data)
}

func (sc *safeConn) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return sc.writeMessage(websocket.TextMessage, b)
}

func (sc *safeConn) close() {
	_ = sc.conn.Close()
}

// HubServer routes WebSocket messages between mobile clients (/ws/client) and workers (/ws/worker),
// preferring the primary PC worker and failing over to the cel fallback worker when PC is offline.
type HubServer struct {
	secret         string
	mu             sync.RWMutex
	primaryWorker  *safeConn
	fallbackWorker *safeConn
	clients        map[*safeConn]struct{}
}

// NewHubServer creates a new HubServer protected by HUB_SECRET.
func NewHubServer(secret string) *HubServer {
	return &HubServer{
		secret:  strings.TrimSpace(secret),
		clients: make(map[*safeConn]struct{}),
	}
}

func (h *HubServer) isAuthorized(r *http.Request) bool {
	if h.secret == "" {
		return false
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			token = strings.TrimSpace(auth[7:])
		}
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(h.secret)) == 1
}

// Handler returns the HTTP handler for the Hub.
func (h *HubServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		h.mu.RLock()
		_, activeRole := h.activeWorkerLocked()
		workerOnline := activeRole != "offline"
		primaryOnline := h.primaryWorker != nil
		fallbackOnline := h.fallbackWorker != nil
		clientCount := len(h.clients)
		h.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":              true,
			"mode":            "hub",
			"worker_online":   workerOnline,
			"active_worker":   activeRole,
			"primary_online":  primaryOnline,
			"fallback_online": fallbackOnline,
			"clients":         clientCount,
		})
	})
	mux.HandleFunc("/ws/worker", h.handleWorkerWS)
	mux.HandleFunc("/ws/client", h.handleClientWS)
	return mux
}

// Run starts the Hub HTTP/WebSocket server and the cloudflared Quick Tunnel loop.
func (h *HubServer) Run(ctx context.Context, port int, gistToken, gistID string) error {
	go RunCloudflaredLoop(ctx, port, gistToken, gistID)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: h.Handler(),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("relay hub: escuchando en :%d (/ws/client, /ws/worker)", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (h *HubServer) handleWorkerWS(w http.ResponseWriter, r *http.Request) {
	if !h.isAuthorized(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("relay hub: upgrade worker falló: %v", err)
		return
	}

	role := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("role")))
	if role != "cel" {
		role = "pc"
	}

	sc := &safeConn{conn: conn}
	h.mu.Lock()
	if role == "cel" {
		if h.fallbackWorker != nil {
			h.fallbackWorker.close()
		}
		h.fallbackWorker = sc
	} else {
		if h.primaryWorker != nil {
			h.primaryWorker.close()
		}
		h.primaryWorker = sc
	}
	_, activeRole := h.activeWorkerLocked()
	h.mu.Unlock()

	h.broadcastHubStatus(activeRole)
	log.Printf("relay hub: worker (%s) conectado desde %s (activo: %s)", role, r.RemoteAddr, activeRole)
	defer func() {
		h.mu.Lock()
		if role == "cel" {
			if h.fallbackWorker == sc {
				h.fallbackWorker = nil
			}
		} else {
			if h.primaryWorker == sc {
				h.primaryWorker = nil
			}
		}
		_, nextRole := h.activeWorkerLocked()
		h.mu.Unlock()
		sc.close()
		h.broadcastHubStatus(nextRole)
		log.Printf("relay hub: worker (%s) desconectado (activo: %s)", role, nextRole)
	}()

	runKeepAlive(sc)

	for {
		msgType, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if msgType != websocket.TextMessage && msgType != websocket.BinaryMessage {
			continue
		}
		h.broadcastToClients(msgType, payload)
	}
}

func (h *HubServer) handleClientWS(w http.ResponseWriter, r *http.Request) {
	if !h.isAuthorized(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("relay hub: upgrade client falló: %v", err)
		return
	}

	sc := &safeConn{conn: conn}
	h.mu.Lock()
	h.clients[sc] = struct{}{}
	_, activeRole := h.activeWorkerLocked()
	h.mu.Unlock()

	_ = sc.writeJSON(map[string]string{
		"type":          "hub_status",
		"active_worker": activeRole,
	})

	log.Printf("relay hub: cliente conectado desde %s", r.RemoteAddr)
	defer func() {
		h.mu.Lock()
		delete(h.clients, sc)
		h.mu.Unlock()
		sc.close()
		log.Printf("relay hub: cliente desconectado")
	}()

	runKeepAlive(sc)

	for {
		msgType, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if msgType != websocket.TextMessage && msgType != websocket.BinaryMessage {
			continue
		}

		h.mu.RLock()
		worker, activeRole := h.activeWorkerLocked()
		fallback := h.fallbackWorker
		h.mu.RUnlock()

		var ctrl struct {
			Type    string `json:"type"`
			Enabled bool   `json:"enabled"`
		}
		if json.Unmarshal(payload, &ctrl) == nil && ctrl.Type == "set_debug" {
			sc.mu.Lock()
			sc.forceCel = ctrl.Enabled
			sc.mu.Unlock()
			nextRole := activeRole
			if ctrl.Enabled && fallback != nil {
				nextRole = "cel"
			}
			_ = sc.writeJSON(map[string]string{
				"type":          "hub_status",
				"active_worker": nextRole,
			})
			continue
		}

		sc.mu.Lock()
		forceCel := sc.forceCel
		sc.mu.Unlock()
		if forceCel && fallback != nil {
			worker = fallback
			activeRole = "cel"
		}

		if worker == nil {
			_ = sc.writeJSON(map[string]string{
				"type":    "error",
				"error":   "worker_offline",
				"message": "Worker no conectado",
			})
			continue
		}

		if err := worker.writeMessage(msgType, payload); err != nil {
			if activeRole == "pc" && fallback != nil {
				if fbErr := fallback.writeMessage(msgType, payload); fbErr == nil {
					h.broadcastHubStatus("cel")
					continue
				}
			}
			_ = sc.writeJSON(map[string]string{
				"type":    "error",
				"error":   "worker_offline",
				"message": "Worker no conectado",
			})
		}
	}
}

func (h *HubServer) activeWorkerLocked() (*safeConn, string) {
	if h.primaryWorker != nil {
		return h.primaryWorker, "pc"
	}
	if h.fallbackWorker != nil {
		return h.fallbackWorker, "cel"
	}
	return nil, "offline"
}

func (h *HubServer) broadcastHubStatus(activeRole string) {
	b, err := json.Marshal(map[string]string{
		"type":          "hub_status",
		"active_worker": activeRole,
	})
	if err != nil {
		return
	}
	h.broadcastToClients(websocket.TextMessage, b)
}

func (h *HubServer) broadcastToClients(msgType int, payload []byte) {
	h.mu.RLock()
	clients := make([]*safeConn, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		if err := c.writeMessage(msgType, payload); err != nil {
			c.close()
		}
	}
}

func runKeepAlive(sc *safeConn) {
	_ = sc.conn.SetReadDeadline(time.Now().Add(pongWait))
	sc.conn.SetPongHandler(func(string) error {
		return sc.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for range ticker.C {
			if err := sc.writeMessage(websocket.PingMessage, nil); err != nil {
				sc.close()
				return
			}
		}
	}()
}
