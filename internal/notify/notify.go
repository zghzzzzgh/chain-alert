// Package notify delivers alert messages to one or more sinks.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Notifier is the minimal sink interface.
type Notifier interface {
	Send(text string) error
}

// Multi fans one message out to every configured sink.
type Multi struct{ sinks []Notifier }

// NewMulti builds a fan-out notifier.
func NewMulti(sinks ...Notifier) *Multi { return &Multi{sinks: sinks} }

// Send delivers to every sink; a single failure does not stop the others.
func (m *Multi) Send(text string) error {
	var firstErr error
	for _, s := range m.sinks {
		if err := s.Send(text); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Stdout prints alerts to the local log.
type Stdout struct{}

// NewStdout returns a stdout-backed notifier.
func NewStdout() *Stdout { return &Stdout{} }

// Send implements Notifier.
func (s *Stdout) Send(text string) error {
	log.Printf("[ALERT] %s", toPlain(text))
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
	log.Printf("telegram ready: @%s -> chat %s", bot.Self.UserName, chatID)
	return &Telegram{bot: bot, chatID: chatID}, nil
}

// Send implements Notifier.
func (t *Telegram) Send(text string) error {
	msg := tgbotapi.NewMessageToChannel(t.chatID, text)
	msg.ParseMode = "HTML"
	msg.DisableWebPagePreview = true
	sent, err := t.bot.Send(msg)
	if err != nil {
		return err
	}
	log.Printf("[SENT:telegram] msg_id=%d | %s", sent.MessageID, firstLine(text))
	return nil
}

// Discord posts alerts through a channel webhook.
type Discord struct {
	webhook string
	client  *http.Client
}

// NewDiscord validates the webhook URL format.
func NewDiscord(webhook string) (*Discord, error) {
	if !strings.HasPrefix(webhook, "https://discord.com/api/webhooks/") &&
		!strings.HasPrefix(webhook, "https://discordapp.com/api/webhooks/") {
		return nil, fmt.Errorf("discord webhook_url looks invalid")
	}
	log.Printf("discord ready: webhook configured")
	return &Discord{webhook: webhook, client: &http.Client{Timeout: 15 * time.Second}}, nil
}

// Send implements Notifier.
func (d *Discord) Send(text string) error {
	payload, _ := json.Marshal(map[string]string{"content": truncate(toPlain(text), 1900)})
	req, err := http.NewRequest(http.MethodPost, d.webhook, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook returned %s", resp.Status)
	}
	log.Printf("[SENT:discord] %s", firstLine(text))
	return nil
}

// toPlain strips the small subset of HTML used in alert bodies.
func toPlain(s string) string {
	r := strings.NewReplacer(
		"<b>", "", "</b>", "",
		"<code>", "`", "</code>", "`",
		"<a href=\"", "", "\">", " ", "</a>", "",
	)
	return r.Replace(s)
}

func firstLine(s string) string {
	s = toPlain(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
