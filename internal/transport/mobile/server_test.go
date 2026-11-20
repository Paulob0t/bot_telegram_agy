package mobile

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"telegram-paulobot/internal/agent"
	"telegram-paulobot/internal/config"
	"telegram-paulobot/internal/executor"
	"telegram-paulobot/internal/state"
)

func sendMaskedClientFrame(conn net.Conn, opcode byte, payload []byte) error {
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	header := []byte{0x80 | opcode, 0x80 | byte(len(payload))}
	if len(payload) >= 126 {
		header = make([]byte, 4)
		header[0] = 0x80 | opcode
		header[1] = 0x80 | 126
		binary.BigEndian.PutUint16(header[2:4], uint16(len(payload)))
	}
	header = append(header, mask[:]...)
	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := conn.Write(header); err != nil {
		return err
	}
	_, err := conn.Write(masked)
	return err
}

func TestMobileWSAuthAndLifecycle(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		HomeDir:        tmp,
		StateCachePath: tmp + "/state.json",
		UsageCachePath: tmp + "/usage.json",
		MobileWSToken:  "secreto-tailscale-123",
	}
	st := state.New(cfg)
	ag := agent.New(cfg, st)

	ag.SetRunner(func(ctx context.Context, cfg *config.Config, st *state.BotSessionState, prompt string, cb executor.StreamCallbacks) (string, error) {
		cb.OnState("thinking")
		cb.OnState("speaking")
		cb.OnChunk("Respuesta parcial", "", false)
		cb.OnChunk("Respuesta final móvil", "", true)
		return "Respuesta final móvil", nil
	})

	srv := NewServer(cfg, ag)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Verificar rechazo 401 sin token
	resp, err := http.Get(ts.URL + "/ws")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("esperaba 401 sin token, recibió %d", resp.StatusCode)
	}

	// 2. Conectar por WebSocket con Authorization: Bearer
	hostAddr := strings.TrimPrefix(ts.URL, "http://")
	conn, err := net.DialTimeout("tcp", hostAddr, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	handshake := "GET /ws HTTP/1.1\r\n" +
		"Host: " + hostAddr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Authorization: Bearer secreto-tailscale-123\r\n\r\n"

	if _, err := conn.Write([]byte(handshake)); err != nil {
		t.Fatal(err)
	}

	br := bufio.NewReader(conn)
	statusLine, err := br.ReadString('\n')
	if err != nil || !strings.Contains(statusLine, "101") {
		t.Fatalf("handshake falló: %q (%v)", statusLine, err)
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil || line == "\r\n" {
			break
		}
	}

	clientWS := &wsConn{conn: conn, br: br}

	// 3. Leer frame inicial de estado idle
	op, payload, err := clientWS.ReadFrame()
	if err != nil || op != opText {
		t.Fatalf("error leyendo estado inicial: op=%d err=%v", op, err)
	}
	var initEv agent.Event
	if err := json.Unmarshal(payload, &initEv); err != nil || initEv.Type != agent.EventTypeState || initEv.State != agent.StateIdle {
		t.Fatalf("evento inicial inesperado: %+v", initEv)
	}

	// 4. Enviar Ping desde cliente y verificar Pong
	if err := sendMaskedClientFrame(conn, opPing, []byte("hb")); err != nil {
		t.Fatal(err)
	}
	op, pongPayload, err := clientWS.ReadFrame()
	if err != nil || op != opPong || string(pongPayload) != "hb" {
		t.Fatalf("esperaba Pong 'hb', recibió op=%d payload=%q err=%v", op, string(pongPayload), err)
	}

	// 5. Enviar mensaje JSON desde cliente Flutter -> Servidor Go
	clientMsg, _ := json.Marshal(map[string]any{
		"user_id":   "paulo_flutter",
		"text":      "hola agente",
		"timestamp": time.Now().Unix(),
	})
	if err := sendMaskedClientFrame(conn, opText, clientMsg); err != nil {
		t.Fatal(err)
	}

	// 6. Recibir secuencia de estados y chunks hasta regresar a idle
	var sawThinking, sawSpeaking, sawDoneChunk, sawFinalIdle bool
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for !(sawThinking && sawSpeaking && sawDoneChunk && sawFinalIdle) {
		op, data, err := clientWS.ReadFrame()
		if err != nil {
			t.Fatalf("error leyendo stream WS: %v", err)
		}
		if op != opText {
			continue
		}
		var ev agent.Event
		if err := json.Unmarshal(data, &ev); err != nil {
			t.Fatalf("JSON inválido del servidor: %v", err)
		}
		switch ev.Type {
		case agent.EventTypeState:
			switch ev.State {
			case agent.StateThinking:
				sawThinking = true
			case agent.StateSpeaking:
				sawSpeaking = true
			case agent.StateIdle:
				if sawDoneChunk {
					sawFinalIdle = true
				}
			}
		case agent.EventTypeChunk:
			if ev.Done && ev.Text == "Respuesta final móvil" {
				sawDoneChunk = true
			}
		}
	}

	// Verificar cálculo RFC 6455 Sec-WebSocket-Accept
	expectedAccept := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if got := computeAcceptKey(base64.StdEncoding.EncodeToString([]byte("the sample nonce"))); got != expectedAccept {
		t.Fatalf("accept key = %q, want %q", got, expectedAccept)
	}
}
