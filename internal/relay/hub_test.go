package relay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestHubAuthRejectsUnauthorized(t *testing.T) {
	hub := NewHubServer("secret-123")
	srv := httptest.NewServer(hub.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ws/client")
	if err != nil {
		t.Fatalf("http.Get error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	resp2, err := http.Get(srv.URL + "/ws/worker?token=wrong")
	if err != nil {
		t.Fatalf("http.Get error: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp2.StatusCode)
	}
}

func TestHubReturnsWorkerOffline(t *testing.T) {
	hub := NewHubServer("secret-123")
	srv := httptest.NewServer(hub.Handler())
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/client?token=secret-123"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial error: %v", err)
	}
	defer conn.Close()

	// Consume initial hub_status frame sent on connect.
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read initial hub_status error: %v", err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"text":"hola"}`)); err != nil {
		t.Fatalf("write error: %v", err)
	}

	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read error: %v", err)
	}

	var out map[string]string
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	if out["error"] != "worker_offline" {
		t.Fatalf("expected worker_offline, got %+v", out)
	}
}

func TestBuildWorkerWSURL(t *testing.T) {
	got, err := buildWorkerWSURL("https://abc-123.trycloudflare.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "wss://abc-123.trycloudflare.com/ws/worker"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
