// Package notify sends optional C Tunnel status/ping updates to a Telegram chat. It is best-effort:
// a failed send never disturbs the tunnel. Enable it in the config (telegram.enabled) with a bot token
// and chat id.
package notify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
)

// APIBase is the Telegram Bot API root; overridable in tests.
var APIBase = "https://api.telegram.org"

// Notifier posts messages to a Telegram chat.
type Notifier struct {
	token  string
	chat   string
	client *http.Client

	mu       sync.Mutex
	lastSent string // de-dupe identical consecutive state messages
}

// FromConfig returns a Notifier when telegram is enabled and configured, else nil.
func FromConfig(cfg *config.Config) *Notifier {
	t := cfg.Telegram
	if t == nil || !t.Enabled || t.Token == "" || t.ChatID == "" {
		return nil
	}
	return &Notifier{
		token:  t.Token,
		chat:   t.ChatID,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Send posts text to the chat (best-effort).
func (n *Notifier) Send(text string) {
	if n == nil {
		return
	}
	form := url.Values{}
	form.Set("chat_id", n.chat)
	form.Set("text", text)
	form.Set("parse_mode", "HTML")
	form.Set("disable_web_page_preview", "true")
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", APIBase, n.token)
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := n.client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// SendOnce sends text only if it differs from the previous SendOnce (avoids repeating "still up").
func (n *Notifier) SendOnce(text string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	same := text == n.lastSent
	n.lastSent = text
	n.mu.Unlock()
	if !same {
		n.Send(text)
	}
}

// StatusLine is the kind of fact the heartbeat reports.
type StatusLine struct {
	Up        bool
	Transport string
	Mode      string
	Peer      string
	Ping      time.Duration
}

// Format renders a StatusLine as a Telegram HTML message.
func Format(s StatusLine) string {
	if !s.Up {
		return "⚠️ <b>C Tunnel</b> — قطع شد؛ در حال اتصال مجدد…"
	}
	ping := "—"
	if s.Ping > 0 {
		ping = fmt.Sprintf("%dms", s.Ping.Milliseconds())
	}
	return fmt.Sprintf("✅ <b>C Tunnel</b> — متصل\nترنسپورت: <code>%s</code>\nحالت: <code>%s</code>\nمقصد: <code>%s</code>\nپینگ: <b>%s</b>",
		s.Transport, s.Mode, s.Peer, ping)
}

// Heartbeat periodically reports status using the provided probe until ctx is done. interval<=0 uses
// a 5-minute default. It sends a change immediately via SendOnce so up/down transitions are prompt.
func (n *Notifier) Heartbeat(ctx context.Context, interval time.Duration, probe func() StatusLine) {
	if n == nil {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	n.SendOnce(Format(probe()))
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.SendOnce(Format(probe()))
		}
	}
}
