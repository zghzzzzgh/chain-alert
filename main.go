// Command chain-alert watches one or more EVM chains and pushes matching
// on-chain events to Telegram and/or Discord.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/zghzzzzgh/chain-alert/internal/config"
	"github.com/zghzzzzgh/chain-alert/internal/monitor"
	"github.com/zghzzzzgh/chain-alert/internal/notify"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to the YAML config file")
	once := flag.Bool("once", false, "scan the latest block of every chain once and exit")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// --- build the notification fan-out ---
	var sinks []notify.Notifier
	if cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID != "" {
		tg, err := notify.NewTelegram(cfg.Telegram.BotToken, cfg.Telegram.ChatID)
		if err != nil {
			log.Fatalf("init telegram: %v", err)
		}
		sinks = append(sinks, tg)
	}
	if cfg.Discord.WebhookURL != "" {
		dc, err := notify.NewDiscord(cfg.Discord.WebhookURL)
		if err != nil {
			log.Fatalf("init discord: %v", err)
		}
		sinks = append(sinks, dc)
	}
	if len(sinks) == 0 {
		log.Println("WARN: no notifier configured - alerts will be printed to stdout")
		sinks = append(sinks, notify.NewStdout())
	}
	notifier := notify.NewMulti(sinks...)

	log.Printf("chain-alert starting | chains=%d notifiers=%d", len(cfg.Chains), len(sinks))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	for _, ch := range cfg.Chains {
		mon, err := monitor.New(ch, notifier)
		if err != nil {
			log.Fatalf("chain %q: %v", ch.Name, err)
		}
		wg.Add(1)
		go func(m *monitor.Monitor, name string) {
			defer wg.Done()
			if *once {
				if err := m.RunOnce(ctx); err != nil {
					log.Printf("chain %q: %v", name, err)
				}
				return
			}
			if err := m.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("chain %q stopped: %v", name, err)
			}
		}(mon, ch.Name)
	}

	wg.Wait()
	log.Println("chain-alert stopped")
}
