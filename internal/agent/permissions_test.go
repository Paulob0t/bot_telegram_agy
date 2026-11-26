package agent

import (
	"context"
	"testing"
	"time"
)

func TestRequestPermissionRoundTrip(t *testing.T) {
	s := &Service{subscribers: make(map[int]func(Event))}

	if d := s.RequestPermission(context.Background(), "Bash", "ls", "ws"); d != PermissionNoClients {
		t.Fatalf("sin clientes: got %q", d)
	}

	idCh := make(chan string, 1)
	s.Subscribe(func(ev Event) {
		if ev.Type == EventTypePermission && !ev.Done {
			idCh <- ev.Message
		}
	})
	go func() {
		id := <-idCh
		if !s.HandlePermissionCommand(PermissionCommandPrefix + id + ":allow") {
			t.Error("comando no reconocido")
		}
	}()
	if d := s.RequestPermission(context.Background(), "Bash", "ls", "ws"); d != PermissionAllow {
		t.Fatalf("got %q, want allow", d)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if d := s.RequestPermission(ctx, "Bash", "rm -rf x", "ws"); d != PermissionTimeout {
		t.Fatalf("got %q, want timeout", d)
	}
	if s.HandlePermissionCommand("hola") {
		t.Fatal("texto normal tratado como comando")
	}
}
