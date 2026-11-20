package bot

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"telegram-paulobot/internal/agent"
	"telegram-paulobot/internal/buffer"
	"telegram-paulobot/internal/config"
	"telegram-paulobot/internal/guard"
	"telegram-paulobot/internal/state"
	"telegram-paulobot/internal/telegram"
	"telegram-paulobot/internal/tokens"
)

type App struct {
	Cfg       *config.Config
	State     *state.BotSessionState
	Agent     *agent.Service
	TG        *telegram.Client
	Guard     *guard.ChatGuard
	Buffer    *buffer.MessageBuffer
	StartTime time.Time

	runMu     sync.Mutex
	isBusy    bool
	runCancel context.CancelFunc
	jobQueue  []queuedJob
}

func New(cfg *config.Config, st *state.BotSessionState, ag ...*agent.Service) *App {
	var svc *agent.Service
	if len(ag) > 0 && ag[0] != nil {
		svc = ag[0]
	} else {
		svc = agent.New(cfg, st)
	}
	app := &App{
		Cfg:       cfg,
		State:     st,
		Agent:     svc,
		TG:        telegram.New(cfg.Token),
		Guard:     guard.New(cfg.ChatIdleMinutes),
		StartTime: time.Now(),
	}
	app.Buffer = buffer.New(cfg.LongMessageThreshold, cfg.LongMessageWaitSeconds, func(full string) {
		app.submitWithGuard(cfg.AuthorizedUserID, full, nil)
	})
	return app
}

func (a *App) Run(ctx context.Context) {
	log.Printf("◆ Paulobot Go iniciado · Motor: %s · CWD: %s", a.State.CurrentEngine, a.State.CurrentCWD)
	go a.startWaybarTokensLoop(ctx)

	var offset int64
	for {
		select {
		case <-ctx.Done():
			log.Println("◇ Deteniendo Paulobot Go...")
			return
		default:
		}

		updates, err := a.TG.GetUpdates(offset, 25)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}

		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if u.CallbackQuery != nil {
				if u.CallbackQuery.From.ID != a.Cfg.AuthorizedUserID {
					continue
				}
				a.handleCallback(u.CallbackQuery)
				continue
			}
			if u.Message == nil || u.Message.From == nil {
				continue
			}
			if u.Message.From.ID != a.Cfg.AuthorizedUserID {
				continue
			}
			a.handleIncomingMessage(u.Message)
		}
	}
}

func (a *App) handleIncomingMessage(m *telegram.Message) {
	text := strings.TrimSpace(m.Text)
	caption := strings.TrimSpace(m.Caption)

	// 1. Foto recibida desde Telegram -> PC
	if len(m.Photo) > 0 {
		best := m.Photo[len(m.Photo)-1]
		uploadsDir := guard.PrepareUploadsDir(a.Cfg.HomeDir)
		fn := fmt.Sprintf("foto_%s_%s.jpg", time.Now().Format("20060102_150405"), best.FileUniqueID)
		destImg := filepath.Join(uploadsDir, fn)
		if err := a.TG.DownloadFile(best.FileID, destImg); err != nil {
			_, _ = a.TG.SendMessage(m.Chat.ID, fmt.Sprintf("✕ Error descargando foto: `%v`", err), nil)
			return
		}
		if caption != "" {
			prompt := fmt.Sprintf("[Foto adjunta guardada en: %s]\n\n%s", destImg, caption)
			a.submitWithGuard(m.Chat.ID, prompt, []string{destImg})
		} else {
			upID := a.Guard.HoldUpload(guard.PendingUpload{
				ChatID:   m.Chat.ID,
				FilePath: destImg,
				FileName: fn,
				IsPhoto:  true,
			})
			up, _ := a.Guard.GetUpload(upID)
			msg, kb := a.buildUploadActionView(up)
			_, _ = a.TG.SendMessage(m.Chat.ID, msg, kb)
		}
		return
	}

	// 2. PDF o documento recibido desde Telegram -> PC
	if m.Document != nil {
		doc := m.Document
		uploadsDir := guard.PrepareUploadsDir(a.Cfg.HomeDir)
		fn := strings.ReplaceAll(filepath.Base(doc.FileName), " ", "_")
		if fn == "" || fn == "." {
			fn = "archivo_" + doc.FileUniqueID
		}
		destDoc := filepath.Join(uploadsDir, fmt.Sprintf("%s_%s", doc.FileUniqueID, fn))
		if err := a.TG.DownloadFile(doc.FileID, destDoc); err != nil {
			_, _ = a.TG.SendMessage(m.Chat.ID, fmt.Sprintf("✕ Error descargando archivo: `%v`", err), nil)
			return
		}
		ext := strings.ToLower(filepath.Ext(fn))
		isPDF := ext == ".pdf" || strings.EqualFold(doc.MimeType, "application/pdf")
		upID := a.Guard.HoldUpload(guard.PendingUpload{
			ChatID:   m.Chat.ID,
			FilePath: destDoc,
			FileName: fn,
			Caption:  caption,
			IsPDF:    isPDF,
		})
		up, _ := a.Guard.GetUpload(upID)
		msg, kb := a.buildUploadActionView(up)
		_, _ = a.TG.SendMessage(m.Chat.ID, msg, kb)
		return
	}

	if text == "" {
		return
	}

	if strings.HasPrefix(text, "/") {
		a.handleCommand(m.Chat.ID, text)
		return
	}

	if a.State.FeatureEnabled("long_buffer") {
		if ready, ok := a.Buffer.Push(text); ok {
			a.submitWithGuard(m.Chat.ID, ready, nil)
		}
	} else {
		a.submitWithGuard(m.Chat.ID, text, nil)
	}
}

func (a *App) startWaybarTokensLoop(ctx context.Context) {
	_ = tokens.WriteWaybarJSON(a.Cfg)
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = tokens.WriteWaybarJSON(a.Cfg)
		}
	}
}
