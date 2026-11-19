package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"telegram-paulobot/internal/bot"
	"telegram-paulobot/internal/config"
	"telegram-paulobot/internal/hooks"
	"telegram-paulobot/internal/state"
	"telegram-paulobot/internal/tokens"
)

func main() {
	cwd, _ := os.Getwd()
	cfg, err := config.Load(cwd)
	if err != nil {
		log.Fatalf("✕ %v", err)
	}

	st := state.New(cfg)

	// Subcomandos CLI unificados (0 Python): dialog, stats, hook agy/claude, notify
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "dialog", "token-dialog":
			tokens.ShowRofiDialog(cfg, true)
			return
		case "stats", "ai-token-stats":
			useCache := len(os.Args) > 2 && os.Args[2] == "--cached"
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
			if len(os.Args) > 2 && os.Args[2] == "claude" {
				hooks.RunClaudeHook(cfg, st)
			} else {
				hooks.RunAgyHook(cfg, st)
			}
			return
		case "notify", "notify-telegram":
			os.Exit(hooks.RunNotifyCLI(cfg, st, os.Args[2:]))
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bot.New(cfg, st).Run(ctx)
}
