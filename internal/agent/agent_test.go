package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agente-bot/internal/config"
	"agente-bot/internal/executor"
	"agente-bot/internal/state"
)

func TestServiceStateTransitionsAndChunks(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		HomeDir:        tmp,
		StateCachePath: tmp + "/state.json",
		UsageCachePath: tmp + "/usage.json",
	}
	st := state.New(cfg)
	svc := New(cfg, st)

	svc.SetRunner(func(ctx context.Context, cfg *config.Config, st *state.BotSessionState, prompt string, cb executor.StreamCallbacks) (string, error) {
		cb.OnState("thinking")
		cb.OnChunk("", "⚙ *Comando:* `go test`", false)
		cb.OnState("speaking")
		cb.OnChunk("Hola desde el core", "", false)
		cb.OnChunk("Hola desde el core desacoplado", "", true)
		return "Hola desde el core desacoplado", nil
	})

	var events []Event
	var globalEvents []Event
	unsub := svc.Subscribe(func(ev Event) {
		globalEvents = append(globalEvents, ev)
	})
	defer unsub()

	out, err := svc.Execute(context.Background(), Request{
		UserID: "paulo-mobile",
		Text:   "probar fase 1",
		Source: "mobile",
	}, func(ev Event) {
		events = append(events, ev)
	})

	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}
	if out != "Hola desde el core desacoplado" {
		t.Fatalf("unexpected output: %q", out)
	}
	if svc.CurrentState() != StateIdle {
		t.Fatalf("expected final state idle, got %q", svc.CurrentState())
	}

	// Verificar que se emitieron estados thinking -> speaking -> idle y chunk con Done=true
	var sawThinking, sawSpeaking, sawIdle, sawDoneChunk bool
	for _, ev := range events {
		if ev.Type == EventTypeState {
			switch ev.State {
			case StateThinking:
				sawThinking = true
			case StateSpeaking:
				sawSpeaking = true
			case StateIdle:
				sawIdle = true
			}
		}
		if ev.Type == EventTypeChunk && ev.Done && strings.HasPrefix(ev.Text, "Hola desde el core desacoplado") {
			sawDoneChunk = true
		}
	}
	if !sawThinking || !sawSpeaking || !sawIdle || !sawDoneChunk {
		t.Fatalf("missing expected lifecycle events: thinking=%v speaking=%v idle=%v doneChunk=%v (events=%+v)",
			sawThinking, sawSpeaking, sawIdle, sawDoneChunk, events)
	}
	if len(globalEvents) != len(events) {
		t.Fatalf("expected global subscriber to receive %d events, got %d", len(events), len(globalEvents))
	}
}

func TestServiceErrorEmission(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		HomeDir:        tmp,
		StateCachePath: tmp + "/state.json",
		UsageCachePath: tmp + "/usage.json",
	}
	st := state.New(cfg)
	svc := New(cfg, st)

	svc.SetRunner(func(ctx context.Context, cfg *config.Config, st *state.BotSessionState, prompt string, cb executor.StreamCallbacks) (string, error) {
		return "", errors.New("fallo simulado de CLI")
	})

	var sawErr bool
	_, err := svc.Execute(context.Background(), Request{Text: "hola"}, func(ev Event) {
		if ev.Type == EventTypeError && ev.Message == "fallo simulado de CLI" {
			sawErr = true
		}
	})
	if err == nil || !sawErr {
		t.Fatalf("expected error event emission, err=%v sawErr=%v", err, sawErr)
	}
}
