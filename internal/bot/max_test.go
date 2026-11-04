package bot

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/maxapi"
	"GitHub-GitLab-Notifier/storage"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMax is a minimal MAX Bot API used by the tests.
type fakeMax struct {
	mu    sync.Mutex
	sent  []sentMessage
	times []time.Time
}

type sentMessage struct {
	ChatID string
	Text   string
	Format string
}

func (f *fakeMax) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/me":
			_, _ = w.Write([]byte(`{"user_id":1,"first_name":"Git bot","username":"git_notifier_bot","is_bot":true}`))
		case "/messages":
			var body struct{ Text, Format string }
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &body)
			f.mu.Lock()
			f.sent = append(f.sent, sentMessage{r.URL.Query().Get("chat_id"), body.Text, body.Format})
			f.times = append(f.times, time.Now())
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"message":{"recipient":{"chat_id":-1,"chat_type":"chat"},"body":{"mid":"m"}}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func (f *fakeMax) last(t *testing.T) sentMessage {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		t.Fatal("no message sent")
	}
	return f.sent[len(f.sent)-1]
}

func newTestBot(t *testing.T) (*Bot, *fakeMax) {
	t.Helper()
	if err := i18n.Load("../../locales"); err != nil {
		t.Fatal(err)
	}
	fake := &fakeMax{}
	srv := httptest.NewServer(fake.handler(t))
	t.Cleanup(srv.Close)

	t.Setenv("BOT_TOKEN", "test-token")
	t.Setenv("MAX_API_URL", srv.URL)
	t.Setenv("DOMAIN", "notifier.example.local")
	t.Setenv("USE_HTTPS", "true")
	t.Setenv("PROXY_ENABLED", "false")

	store, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	b, err := NewBot(i18n.EN, store, "test")
	if err != nil {
		t.Fatal(err)
	}
	return b, fake
}

func update(chatID int64, text string, fromBot bool) maxapi.Update {
	return maxapi.Update{
		UpdateType: maxapi.UpdateMessageCreated,
		Message: &maxapi.Message{
			Sender:    &maxapi.User{UserID: 7, FirstName: "Amir", Username: "amir", IsBot: fromBot},
			Recipient: maxapi.Recipient{ChatID: &chatID, ChatType: "chat"},
			Body:      maxapi.MessageBody{Text: text},
		},
	}
}

func TestBotUsesMaxIdentity(t *testing.T) {
	b, _ := newTestBot(t)
	if b.Username() != "git_notifier_bot" {
		t.Errorf("Username() = %q", b.Username())
	}
}

func TestConnectStatusDisconnect(t *testing.T) {
	b, fake := newTestBot(t)
	const chat = int64(-1001)

	b.consume(update(chat, "/connect https://gitlab.example.local/infra/app", false))
	msg := fake.last(t)
	if msg.ChatID != "-1001" || msg.Format != "html" {
		t.Errorf("reply went to %q with format %q", msg.ChatID, msg.Format)
	}
	if !strings.Contains(msg.Text, "https://notifier.example.local/webhook") {
		t.Errorf("connect reply has no webhook URL: %s", msg.Text)
	}

	b.consume(update(chat, "/status", false))
	if msg := fake.last(t); !strings.Contains(msg.Text, "https://gitlab.example.local/infra/app") ||
		!strings.Contains(msg.Text, "-1001") {
		t.Errorf("status does not list the repo: %s", msg.Text)
	}

	b.consume(update(chat, "/disconnect https://gitlab.example.local/infra/app", false))
	if repos, _ := b.store.GetReposByChat(chat); len(repos) != 0 {
		t.Errorf("repo still connected: %+v", repos)
	}
}

func TestConfigureIsNotSupported(t *testing.T) {
	b, fake := newTestBot(t)
	b.consume(update(-5, "/configure", false))
	if msg := fake.last(t); msg.Text != i18n.T(i18n.EN, i18n.MsgConfigureNotSupported) {
		t.Errorf("unexpected reply: %s", msg.Text)
	}
}

func TestIgnoresBotsPlainTextAndOtherBots(t *testing.T) {
	b, fake := newTestBot(t)
	b.consume(update(-5, "/status", true))                   // from a bot
	b.consume(update(-5, "hello", false))                    // not a command
	b.consume(update(-5, "/status@someone_else_bot", false)) // other bot
	b.consume(maxapi.Update{UpdateType: "message_removed"})  // other update type
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.sent) != 0 {
		t.Errorf("expected no replies, got %+v", fake.sent)
	}
}

func TestSendRespectsRateLimit(t *testing.T) {
	b, fake := newTestBot(t)
	for i := 0; i < 3; i++ {
		if err := b.SendMessageToTopic(-9, 12345, "ping"); err != nil {
			t.Fatal(err)
		}
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for i := 1; i < len(fake.times); i++ {
		if gap := fake.times[i].Sub(fake.times[i-1]); gap < 500*time.Millisecond {
			t.Errorf("messages %d and %d only %v apart", i-1, i, gap)
		}
	}
	if fake.sent[0].ChatID != "-9" {
		t.Errorf("topic ID must be ignored, message went to %q", fake.sent[0].ChatID)
	}
}

func TestTruncateRunes(t *testing.T) {
	long := strings.Repeat("я", maxapi.MaxTextLength+10)
	got := truncateRunes(long, maxapi.MaxTextLength)
	if n := len([]rune(got)); n != maxapi.MaxTextLength || !strings.HasSuffix(got, "...") {
		t.Errorf("len = %d, suffix ok = %v", n, strings.HasSuffix(got, "..."))
	}
	if truncateRunes("short", 10) != "short" {
		t.Error("short text must not change")
	}
}
