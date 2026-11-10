package notify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
)

func TestSendHitsTelegramAPIShape(t *testing.T) {
	var gotPath, gotChat, gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		v, _ := url.ParseQuery(string(body))
		gotChat = v.Get("chat_id")
		gotText = v.Get("text")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	old := APIBase
	APIBase = srv.URL
	defer func() { APIBase = old }()

	n := FromConfig(&config.Config{Telegram: &config.Telegram{
		Enabled: true, Token: "123:ABC", ChatID: "777",
	}})
	if n == nil {
		t.Fatal("notifier should be enabled")
	}
	n.Send("hello")

	if !strings.Contains(gotPath, "/bot123:ABC/sendMessage") {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if gotChat != "777" || gotText != "hello" {
		t.Fatalf("chat=%q text=%q", gotChat, gotText)
	}
}

func TestDisabledReturnsNil(t *testing.T) {
	if FromConfig(&config.Config{}) != nil {
		t.Fatal("nil telegram config must yield no notifier")
	}
	if FromConfig(&config.Config{Telegram: &config.Telegram{Enabled: false, Token: "x", ChatID: "y"}}) != nil {
		t.Fatal("disabled must yield no notifier")
	}
}

func TestSendOnceDeDupes(t *testing.T) {
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	old := APIBase
	APIBase = srv.URL
	defer func() { APIBase = old }()

	n := FromConfig(&config.Config{Telegram: &config.Telegram{Enabled: true, Token: "t", ChatID: "c"}})
	n.SendOnce("same")
	n.SendOnce("same")
	n.SendOnce("different")
	time.Sleep(50 * time.Millisecond)
	if count != 2 {
		t.Fatalf("expected 2 sends (dedupe identical), got %d", count)
	}
}

func TestFormatUpDown(t *testing.T) {
	if !strings.Contains(Format(StatusLine{Up: true, Transport: "cquic", Ping: 42 * time.Millisecond}), "42ms") {
		t.Fatal("up format should include ping")
	}
	if !strings.Contains(Format(StatusLine{Up: false}), "قطع") {
		t.Fatal("down format should say disconnected")
	}
}
