package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"agente-bot/internal/config"
	"agente-bot/internal/executor"
	"agente-bot/internal/state"
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
	EventTypeState  EventType = "state"
	EventTypeChunk  EventType = "chunk"
	EventTypeNotify EventType = "notify"
	EventTypeError  EventType = "error"
	// EventTypeAgentStatus drives the 3D avatar: granular state + exact file/command.
	EventTypeAgentStatus EventType = "agent_status"
)

// Event modela el contrato de salida por WebSocket hacia la app (Flutter).
type Event struct {
	Type       EventType `json:"type"`
	State      State     `json:"state,omitempty"`
	Text       string    `json:"text,omitempty"`
	Title      string    `json:"title,omitempty"`
	ToolStatus string    `json:"tool_status,omitempty"`
	Done       bool      `json:"done"`
	Message    string    `json:"message,omitempty"`
	Label      string    `json:"label,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	Engine     string    `json:"engine,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
}

type Request struct {
	UserID    string `json:"user_id"`
	Text      string `json:"text"`
	Timestamp int64  `json:"timestamp"`
	Source    string `json:"source,omitempty"` // "mobile"
	SessionID string `json:"session_id,omitempty"`
}

type RunnerFunc func(ctx context.Context, cfg *config.Config, st *state.BotSessionState, prompt string, cb executor.StreamCallbacks) (string, error)

// Service desacopla la ejecución del agente (AGY / Claude) del transporte (servidor directo y Hub relay).
type Service struct {
	cfg    *config.Config
	st     *state.BotSessionState
	runner RunnerFunc

	execMu       sync.Mutex
	sessMuMap    sync.Map
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

// Subscribe registra un listener global de eventos (sincroniza todos los clientes móviles conectados).
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

// EmitExternalNotification envía una notificación push de terminal/IDE sin ensuciar el chat activo.
func (s *Service) EmitExternalNotification(title, body string) {
	s.broadcast(Event{
		Type:  EventTypeNotify,
		Title: title,
		Text:  body,
		Done:  true,
	}, nil)
	engine := "agy"
	if strings.Contains(strings.ToLower(title+" "+body), "claude") {
		engine = "claude"
	}
	s.broadcast(Event{
		Type:   EventTypeAgentStatus,
		State:  State(executor.ActivitySuccess),
		Label:  "Listo · tarea completada",
		Engine: engine,
		Done:   true,
	}, nil)
}

func (s *Service) emitActivity(a executor.Activity, perReq func(Event)) {
	s.broadcast(Event{
		Type:   EventTypeAgentStatus,
		State:  State(a.State),
		Label:  a.Label,
		Detail: a.Detail,
		Engine: s.st.CurrentEngine,
	}, perReq)
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

func (s *Service) getSessionMutex(sessionID string) *sync.Mutex {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return &s.execMu
	}
	v, _ := s.sessMuMap.LoadOrStore(sid, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// Execute procesa un prompt de forma serializada entre todos los transportes y emite eventos de estado y chunks.
func (s *Service) Execute(parentCtx context.Context, req Request, onEvent func(Event)) (string, error) {
	return s.ExecuteWithState(parentCtx, s.st, req, onEvent)
}

// ExecuteWithState procesa un prompt usando un estado de sesión aislado (CWD y ConversationID propios por pestaña móvil).
func (s *Service) ExecuteWithState(parentCtx context.Context, st *state.BotSessionState, req Request, onEvent func(Event)) (string, error) {
	if st == nil {
		st = s.st
	}
	prompt := strings.TrimSpace(req.Text)
	if prompt == "" {
		err := fmt.Errorf("el texto del mensaje está vacío")
		s.broadcast(Event{Type: EventTypeError, Message: err.Error()}, onEvent)
		return "", err
	}

	mu := s.getSessionMutex(req.SessionID)
	if mu.TryLock() {
		defer mu.Unlock()
	} else {
		s.emitActivity(executor.Activity{State: executor.ActivityStarting, Label: "Mensaje en cola · esperando turno"}, onEvent)
		mu.Lock()
		defer mu.Unlock()
	}

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
	s.emitActivity(executor.Activity{State: executor.ActivityStarting, Label: "Iniciando tarea"}, onEvent)
	startedAt := time.Now()

	formattedPrompt := prompt
	if !strings.Contains(formattedPrompt, "[Mobile:") {
		formattedPrompt += "\n\n[Mobile: responde claro y conciso para interfaz móvil con asistente visual]"
	}

	out, err := s.runner(ctx, s.cfg, st, formattedPrompt, executor.StreamCallbacks{
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
		OnActivity: func(a executor.Activity) {
			s.emitActivity(a, onEvent)
		},
		OnChunk: func(text, toolStatus string, done bool) {
			finalText := text
			if done && req.Source == "mobile" && finalText != "" {
				elapsed := time.Since(startedAt).Round(100 * time.Millisecond)
				mod := st.CurrentModel
				if mod == "" {
					mod = "gemini-4-hydrogen-low"
				}
				finalText = fmt.Sprintf("%s\n\n— %s · %s · %s", finalText, elapsed, st.GetCWDShortName(st.CurrentCWD), mod)
			}
			s.broadcast(Event{
				Type:       EventTypeChunk,
				Text:       finalText,
				ToolStatus: toolStatus,
				Done:       done,
				SessionID:  req.SessionID,
			}, onEvent)
		},
	})

	elapsed := time.Since(startedAt).Round(100 * time.Millisecond)
	if err != nil && ctx.Err() == nil {
		s.broadcast(Event{
			Type:      EventTypeError,
			Message:   err.Error(),
			SessionID: req.SessionID,
		}, onEvent)
		s.emitActivity(executor.Activity{State: executor.ActivityError, Label: "Error en ejecución", Detail: err.Error()}, onEvent)
	} else if ctx.Err() != nil {
		s.emitActivity(executor.Activity{State: executor.ActivityError, Label: "Ejecución detenida"}, onEvent)
	} else {
		s.emitActivity(executor.Activity{State: executor.ActivitySuccess, Label: fmt.Sprintf("Listo · terminado en %s", elapsed)}, onEvent)
	}

	return out, err
}
