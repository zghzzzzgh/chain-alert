// Command chain-alert watches an EVM chain and pushes matching on-chain
// events to Telegram.
package main

import (
"context"
"errors"
"flag"
"log"
"os"
"os/signal"
"syscall"

"github.com/zghzzzzgh/chain-alert/internal/config"
"github.com/zghzzzzgh/chain-alert/internal/monitor"
"github.com/zghzzzzgh/chain-alert/internal/notify"
)

func main() {
configPath := flag.String("config", "config.yaml", "path to the YAML config file")
once := flag.Bool("once", false, "scan the latest block once and exit (useful for testing)")
flag.Parse()

cfg, err := config.Load(*configPath)
if err != nil {
log.Fatalf("load config: %v", err)
}

var notifier notify.Notifier
if cfg.Telegram.BotToken == "" || cfg.Telegram.ChatID == "" {
log.Println("WARN: telegram not configured - alerts will be printed to stdout")
notifier = notify.NewStdout()
} else {
tg, err := notify.NewTelegram(cfg.Telegram.BotToken, cfg.Telegram.ChatID)
if err != nil {
log.Fatalf("init telegram: %v", err)
}
notifier = tg
}

mon, err := monitor.New(cfg, notifier)
if err != nil {
log.Fatalf("init monitor: %v", err)
}

ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

log.Printf("chain-alert starting | chain=%s rules=%d", cfg.Chain.Name, len(cfg.Rules))

if *once {
if err := mon.RunOnce(ctx); err != nil {
log.Fatalf("run once: %v", err)
}
return
}

if err := mon.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
log.Fatalf("monitor stopped: %v", err)
}
log.Println("chain-alert stopped")
}