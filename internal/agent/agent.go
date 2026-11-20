package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"telegram-paulobot/internal/config"
	"telegram-paulobot/internal/executor"
	"telegram-paulobot/internal/state"
)

type State string

const (
	StateIdle      State = "idle"
	StateListening State = "listening"
	StateThinking  State = "thinking"
	StateSpeaking  State = "speaking"
	StateSleeping  State = "sleeping"
)

type EventType string

const (
	EventTypeState EventType = "state"
	EventTypeChunk EventType = "chunk"
	EventTypeError EventType = "error"
)

// Event modela el contrato unificado de salida para WebSocket (Flutter + Rive) y Telegram.
type Event struct {
	Type       EventType `json:"type"`
	State      State     `json:"state,omitempty"`
	Text       string    `json:"text,omitempty"`
	ToolStatus string    `json:"tool_status,omitempty"`
	Done       bool      `json:"done"`
	Message    string    `json:"message,omitempty"`
}

type Request struct {
	UserID    string `json:"user_id"`
	Text      string `json:"text"`
	Timestamp int64  `json:"timestamp"`
	Source    string `json:"source,omitempty"` // "telegram" | "mobile"
}

type RunnerFunc func(ctx context.Context, cfg *config.Config, st *state.BotSessionState, prompt string, cb executor.StreamCallbacks) (string, error)

// Service desacopla la ejecución del agente (AGY / Claude) de los canales de transporte (Telegram y WebSocket móvil).
type Service struct {
	cfg    *config.Config
	st     *state.BotSessionState
	runner RunnerFunc

	execMu       sync.Mutex
	stateMu      sync.RWMutex
	currentState State
	activeCancel context.CancelFunc

	subMu       sync.RWMutex
	nextSubID   int
	subscribers map[int]func(Event)
}

func New(cfg *config.Config, st *state.BotSessionState) *Service {
	return &Service{
		cfg:          cfg,
		st:           st,
		runner:       executor.RunPromptWithEvents,
		currentState: StateIdle,
		subscribers:  make(map[int]func(Event)),
	}
}

// SetRunner permite inyectar un runner simulado en tests unitarios sin invocar el binario real.
func (s *Service) SetRunner(fn RunnerFunc) {
	s.runner = fn
}

func (s *Service) CurrentState() State {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.currentState
}

func (s *Service) IsBusy() bool {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.activeCancel != nil
}

func (s *Service) CancelActive() bool {
	s.stateMu.Lock()
	cancel := s.activeCancel
	s.stateMu.Unlock()
	if cancel != nil {
		cancel()
		return true
	}
	return false
}

// Subscribe registra un listener global de eventos (ideal para sincronizar Rive en móvil aunque el prompt venga de Telegram).
func (s *Service) Subscribe(fn func(Event)) func() {
	s.subMu.Lock()
	s.nextSubID++
	id := s.nextSubID
	s.subscribers[id] = fn
	s.subMu.Unlock()

	return func() {
		s.subMu.Lock()
		delete(s.subscribers, id)
		s.subMu.Unlock()
	}
}

func (s *Service) broadcast(ev Event, perReq func(Event)) {
	if perReq != nil {
		perReq(ev)
	}
	s.subMu.RLock()
	subs := make([]func(Event), 0, len(s.subscribers))
	for _, fn := range s.subscribers {
		subs = append(subs, fn)
	}
	s.subMu.RUnlock()

	for _, fn := range subs {
		fn(ev)
	}
}

func (s *Service) setState(next State, perReq func(Event)) {
	s.stateMu.Lock()
	changed := s.currentState != next
	s.currentState = next
	s.stateMu.Unlock()

	if changed {
		s.broadcast(Event{
			Type:  EventTypeState,
			State: next,
		}, perReq)
	}
}

// Execute procesa un prompt de forma serializada entre todos los transportes y emite eventos de estado y chunks.
func (s *Service) Execute(parentCtx context.Context, req Request, onEvent func(Event)) (string, error) {
	prompt := strings.TrimSpace(req.Text)
	if prompt == "" {
		err := fmt.Errorf("el texto del mensaje está vacío")
		s.broadcast(Event{Type: EventTypeError, Message: err.Error()}, onEvent)
		return "", err
	}

	s.execMu.Lock()
	defer s.execMu.Unlock()

	ctx, cancel := context.WithCancel(parentCtx)
	s.stateMu.Lock()
	s.activeCancel = cancel
	s.stateMu.Unlock()

	defer func() {
		s.stateMu.Lock()
		s.activeCancel = nil
		s.stateMu.Unlock()
		cancel()
		s.setState(StateIdle, onEvent)
	}()

	s.setState(StateThinking, onEvent)

	formattedPrompt := prompt
	if req.Source == "mobile" && !strings.Contains(formattedPrompt, "[Mobile:") {
		formattedPrompt += "\n\n[Mobile: responde claro y conciso para interfaz móvil con asistente visual]"
	}

	out, err := s.runner(ctx, s.cfg, s.st, formattedPrompt, executor.StreamCallbacks{
		OnState: func(stStr string) {
			switch State(stStr) {
			case StateThinking:
				s.setState(StateThinking, onEvent)
			case StateSpeaking:
				s.setState(StateSpeaking, onEvent)
			case StateIdle:
				s.setState(StateIdle, onEvent)
			}
		},
		OnChunk: func(text, toolStatus string, done bool) {
			s.broadcast(Event{
				Type:       EventTypeChunk,
				Text:       text,
				ToolStatus: toolStatus,
				Done:       done,
			}, onEvent)
		},
	})

	if err != nil && ctx.Err() == nil {
		s.broadcast(Event{
			Type:    EventTypeError,
			Message: err.Error(),
		}, onEvent)
	}

	return out, err
}
