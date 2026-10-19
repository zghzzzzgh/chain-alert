// Package notify delivers alert messages to a sink (Telegram or stdout).
package notify

import (
"fmt"
"log"

tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Notifier is the minimal sink interface so the monitor can be unit-tested
// without a real Telegram bot.
type Notifier interface {
Send(text string) error
}

// Stdout prints alerts to the local log. Used when Telegram is not configured.
type Stdout struct{}

// NewStdout returns a stdout-backed notifier.
func NewStdout() *Stdout { return &Stdout{} }

// Send implements Notifier.
func (s *Stdout) Send(text string) error {
log.Printf("[ALERT] %s", text)
return nil
}

// Telegram pushes alerts to a chat or channel.
type Telegram struct {
bot    *tgbotapi.BotAPI
chatID string
}

// NewTelegram validates the bot token against the Telegram API.
func NewTelegram(token, chatID string) (*Telegram, error) {
bot, err := tgbotapi.NewBotAPI(token)
if err != nil {
return nil, fmt.Errorf("telegram auth: %w", err)
}
return &Telegram{bot: bot, chatID: chatID}, nil
}

// Send implements Notifier.
func (t *Telegram) Send(text string) error {
msg := tgbotapi.NewMessageToChannel(t.chatID, text)
msg.ParseMode = "HTML"
msg.DisableWebPagePreview = true
_, err := t.bot.Send(msg)
return err
}