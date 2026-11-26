package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"telegram-paulobot/internal/agent"
	"telegram-paulobot/internal/bot"
	"telegram-paulobot/internal/config"
	"telegram-paulobot/internal/hooks"
	"telegram-paulobot/internal/relay"
	"telegram-paulobot/internal/state"
	"telegram-paulobot/internal/tokens"
	"telegram-paulobot/internal/transport/mobile"
)

func main() {
	modeFlag := flag.String("mode", "", "Modo de operación relay: hub o worker")
	portFlag := flag.Int("port", 0, "Puerto HTTP/WebSocket para modo hub (default 8080 o PORT)")
	roleFlag := flag.String("role", "pc", "Rol del worker: pc (primario) o cel (respaldo)")
	hubURLFlag := flag.String("hub-url", "", "URL directa del Hub para worker (ej. ws://127.0.0.1:8080/ws/worker?role=cel)")
	flag.Parse()

	mode := strings.ToLower(strings.TrimSpace(*modeFlag))
	if mode == "hub" {
		hubSecret := strings.TrimSpace(os.Getenv("HUB_SECRET"))
		gistToken := strings.TrimSpace(os.Getenv("GITHUB_GIST_TOKEN"))
		gistID := strings.TrimSpace(os.Getenv("GITHUB_GIST_ID"))
		if hubSecret == "" || gistToken == "" || gistID == "" {
			log.Fatalf("✕ HUB_SECRET, GITHUB_GIST_TOKEN y GITHUB_GIST_ID son requeridos en modo hub")
		}
		port := *portFlag
		if port <= 0 {
			if p, err := strconv.Atoi(strings.TrimSpace(os.Getenv("PORT"))); err == nil && p > 0 {
				port = p
			} else {
				port = 8080
			}
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := relay.NewHubServer(hubSecret).Run(ctx, port, gistToken, gistID); err != nil {
			log.Fatalf("✕ relay hub: %v", err)
		}
		return
	}

	cwd, _ := os.Getwd()
	cfg, err := config.Load(cwd)
	if err != nil {
		log.Fatalf("✕ %v", err)
	}

	st := state.New(cfg)

	// Subcomandos CLI unificados (0 Python): dialog, stats, hook agy/claude, notify
	if args := flag.Args(); len(args) > 0 {
		switch args[0] {
		case "dialog", "token-dialog":
			tokens.ShowRofiDialog(cfg, true)
			return
		case "stats", "ai-token-stats":
			useCache := len(args) > 1 && args[1] == "--cached"
			var q tokens.Quotas
			if useCache {
				q = tokens.LoadCachedOrFetch(cfg, time.Minute)
			} else {
				q = tokens.FetchAll(cfg)
				_ = tokens.WriteWaybarJSON(cfg)
			}
			raw, _ := json.Marshal(q)
			fmt.Println(string(raw))
			return
		case "hook":
			if len(args) > 1 && args[1] == "claude-perm" {
				hooks.RunClaudePermissionHook(cfg, st)
			} else if len(args) > 1 && args[1] == "claude" {
				hooks.RunClaudeHook(cfg, st)
			} else {
				hooks.RunAgyHook(cfg, st)
			}
			return
		case "notify", "notify-telegram":
			os.Exit(hooks.RunNotifyCLI(cfg, st, args[1:]))
		case "mobile-info":
			fmt.Println(mobile.FormatConnectionInfo(cfg))
			return
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if mode == "worker" {
		hubSecret := strings.TrimSpace(os.Getenv("HUB_SECRET"))
		if hubSecret == "" {
			log.Fatalf("✕ HUB_SECRET es requerido en modo worker")
		}
		ag := agent.New(cfg, st)
		if directURL := strings.TrimSpace(*hubURLFlag); directURL != "" {
			if !strings.Contains(directURL, "role=") {
				sep := "?"
				if strings.Contains(directURL, "?") {
					sep = "&"
				}
				directURL += sep + "role=" + strings.TrimSpace(*roleFlag)
			}
			relay.RunWorkerWithURL(ctx, cfg, st, ag, directURL, hubSecret)
			return
		}
		gistToken := strings.TrimSpace(os.Getenv("GITHUB_GIST_TOKEN"))
		gistID := strings.TrimSpace(os.Getenv("GITHUB_GIST_ID"))
		if gistToken == "" || gistID == "" {
			log.Fatalf("✕ GITHUB_GIST_TOKEN y GITHUB_GIST_ID son requeridos en modo worker")
		}
		relay.RunWorker(ctx, cfg, st, ag, hubSecret, gistToken, gistID)
		return
	}

	ag := agent.New(cfg, st)
	if hubSecret := strings.TrimSpace(os.Getenv("HUB_SECRET")); hubSecret != "" {
		gistToken := strings.TrimSpace(os.Getenv("GITHUB_GIST_TOKEN"))
		gistID := strings.TrimSpace(os.Getenv("GITHUB_GIST_ID"))
		if gistToken != "" && gistID != "" {
			go relay.RunWorker(ctx, cfg, st, ag, hubSecret, gistToken, gistID)
		}
	}
	wsSrv := mobile.NewServer(cfg, ag, st)
	if !cfg.EnableTelegram {
		wsSrv.Run(ctx)
		return
	}

	go wsSrv.Run(ctx)
	bot.New(cfg, st, ag).Run(ctx)
}
