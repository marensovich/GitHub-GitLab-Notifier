package max

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
)

// handler records what the adapter passes to the core.
type handler struct {
	mu       sync.Mutex
	commands []messenger.Command
	presses  []messenger.Press
	got      chan struct{}
}

func newHandler() *handler { return &handler{got: make(chan struct{}, 10)} }

func (h *handler) HandleCommand(_ context.Context, c messenger.Command) {
	h.mu.Lock()
	h.commands = append(h.commands, c)
	h.mu.Unlock()
	h.got <- struct{}{}
}

func (h *handler) HandlePress(_ context.Context, p messenger.Press) {
	h.mu.Lock()
	h.presses = append(h.presses, p)
	h.mu.Unlock()
	h.got <- struct{}{}
}

func (h *handler) cmds() []messenger.Command {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]messenger.Command(nil), h.commands...)
}

func (h *handler) wait(t *testing.T) {
	t.Helper()
	select {
	case <-h.got:
	case <-time.After(2 * time.Second):
		t.Fatal("nothing reached the core")
	}
}

func TestWebhookURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://n.example.com":          "https://n.example.com/max/updates",
		"https://n.example.com:443/":     "https://n.example.com/max/updates",
		"https://n.example.com/notifier": "https://n.example.com/notifier/max/updates",
		"http://n.example.com":           "",
		"https://n.example.com:8443":     "",
		"":                               "",
	} {
		got, ok := webhookURL(in)
		if got != want || ok != (want != "") {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
}

const groupCommand = `{"update_type":"message_created","message":{"sender":{"user_id":42,"username":"alice"},
	"recipient":{"chat_id":-5,"chat_type":"chat"},"body":{"mid":"mid.c","text":"/status@notifier_bot  extra"}}}`

func TestWebhookSecret(t *testing.T) {
	m, f := newFake(t)
	f.answer("GET /chats/-5", `{"chat_id":-5,"type":"chat","title":"Team"}`)
	m.secret = secretFor(testToken)
	h := newHandler()
	m.setHandler(context.Background(), h)
	for _, secret := range []string{"", "wrong"} {
		req := httptest.NewRequest(http.MethodPost, UpdatesPath, strings.NewReader(groupCommand))
		if secret != "" {
			req.Header.Set("X-Max-Bot-Api-Secret", secret)
		}
		w := httptest.NewRecorder()
		m.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("secret %q: %d", secret, w.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, UpdatesPath, strings.NewReader(groupCommand))
	req.Header.Set("X-Max-Bot-Api-Secret", secretFor(testToken))
	w := httptest.NewRecorder()
	m.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("right secret: %d", w.Code)
	}
	h.wait(t)
	if c := h.cmds(); len(c) != 1 || c[0].Name != "status" {
		t.Errorf("commands %+v", c)
	}
	if s := secretFor(testToken); strings.Contains(s, testToken) || len(s) < 32 || s == secretFor("other") {
		t.Errorf("secret %q", s)
	}
}

func TestCommandFromGroupAndDialog(t *testing.T) {
	m, f := newFake(t)
	f.answer("GET /chats/-5", `{"chat_id":-5,"type":"chat","title":"Team"}`)
	h := newHandler()
	ctx := context.Background()
	m.consume(ctx, h, decode(t, groupCommand))
	m.consume(ctx, h, decode(t, `{"update_type":"message_created","message":{"sender":{"user_id":42},"recipient":{"chat_id":-5,"chat_type":"chat"},"body":{"mid":"x","text":"/status@other_bot"}}}`))
	m.consume(ctx, h, decode(t, `{"update_type":"message_created","message":{"sender":{"user_id":42},"recipient":{"chat_id":-5,"chat_type":"chat"},"body":{"mid":"x","text":"hello"}}}`))
	m.consume(ctx, h, decode(t, `{"update_type":"message_created","message":{"recipient":{"chat_id":-9,"chat_type":"channel"},"body":{"mid":"x","text":"/status"}}}`))
	m.consume(ctx, h, decode(t, `{"update_type":"message_created","message":{"sender":{"user_id":42},"recipient":{"chat_id":7,"chat_type":"dialog"},"body":{"mid":"mid.d","text":"/Help"}}}`))
	c := h.cmds()
	if len(c) != 2 {
		t.Fatalf("commands %+v", c)
	}
	g := c[0]
	if g.Messenger != Name || g.ChatID != "-5" || g.UserID != "42" || g.ChatTitle != "Team" || g.IsPrivate || len(g.Args) != 1 || g.Args[0] != "extra" {
		t.Errorf("group %+v", g)
	}
	if ref, ok := g.Ref.(*message); !ok || ref.Body.Mid != "mid.c" {
		t.Errorf("ref %+v", g.Ref)
	}
	d := c[1]
	if d.Name != "help" || d.ChatID != "7" || !d.IsPrivate {
		t.Errorf("dialog %+v", d)
	}
	if chat, _ := m.DirectChat(ctx, "42"); chat != "7" {
		t.Errorf("dialog not remembered: %q", chat)
	}
}

func TestBotStartedIsStart(t *testing.T) {
	m, _ := newFake(t)
	h := newHandler()
	m.consume(context.Background(), h, decode(t, `{"update_type":"bot_started","chat_id":7,"user":{"user_id":42},"user_locale":"ru"}`))
	c := h.cmds()
	if len(c) != 1 || c[0].Name != "start" || c[0].ChatID != "7" || c[0].UserID != "42" || !c[0].IsPrivate {
		t.Fatalf("%+v", c)
	}
	if chat, _ := m.DirectChat(context.Background(), "42"); chat != "7" {
		t.Errorf("dialog %q", chat)
	}
}

func TestPressCarriesOrigin(t *testing.T) {
	m, _ := newFake(t)
	h := newHandler()
	m.consume(context.Background(), h, decode(t, `{"update_type":"message_callback",
		"callback":{"callback_id":"cb.1","payload":"me:review","user":{"user_id":43}},
		"message":{"sender":{"user_id":1},"recipient":{"chat_id":-5,"chat_type":"chat"},"body":{"mid":"mid.r"},
			"link":{"type":"reply","sender":{"user_id":42},"chat_id":-5,"message":{"mid":"mid.c"}}}}`))
	if len(h.presses) != 1 {
		t.Fatal("no press")
	}
	p := h.presses[0]
	if p.ChatID != "-5" || p.UserID != "43" || p.Data != "me:review" || p.OriginUserID != "42" || p.IsPrivate {
		t.Errorf("press %+v", p)
	}
	if s, ok := p.Ref.(*pressState); !ok || s.callbackID != "cb.1" {
		t.Errorf("ref %+v", p.Ref)
	}
	m.consume(context.Background(), h, decode(t, `{"update_type":"message_callback","callback":{"callback_id":"cb.2","payload":"x","user":{"user_id":42}},
		"message":{"recipient":{"chat_id":7,"chat_type":"dialog"},"body":{"mid":"m"}}}`))
	if p := h.presses[1]; !p.IsPrivate || p.OriginUserID != "" {
		t.Errorf("dialog press %+v", p)
	}
}

func TestPollingAdvancesMarker(t *testing.T) {
	m, f := newFake(t)
	f.answer("GET /subscriptions", `{"subscriptions":[{"url":"https://old.example.com/max/updates"}]}`)
	var n int
	var mu sync.Mutex
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.mu.Lock()
	f.answers["GET /updates"] = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		k := n
		mu.Unlock()
		switch k {
		case 1:
			_, _ = w.Write([]byte(`{"updates":[{"update_type":"bot_started","chat_id":7,"user":{"user_id":42}}],"marker":11}`))
		default:
			cancel()
			_, _ = w.Write([]byte(`{"updates":[],"marker":11}`))
		}
	}
	f.mu.Unlock()
	h := newHandler()
	m.poll(ctx, h)
	var updates []call
	deleted := false
	for _, c := range f.all() {
		if c.Path == "/updates" {
			updates = append(updates, c)
		}
		if c.Method == "DELETE" && c.Path == "/subscriptions" && strings.Contains(c.Query, "old.example.com") {
			deleted = len(updates) == 0
		}
	}
	if !deleted {
		t.Error("old subscription not deleted before polling")
	}
	if len(updates) < 2 || strings.Contains(updates[0].Query, "marker") || !strings.Contains(updates[1].Query, "marker=11") {
		t.Errorf("updates %+v", updates)
	}
	if len(h.cmds()) != 1 {
		t.Errorf("commands %+v", h.cmds())
	}
}

func TestSubscribe(t *testing.T) {
	m, f := newFake(t)
	m.secret = "s3cret-value"
	if err := m.subscribe(context.Background(), "https://n.example.com/max/updates"); err != nil {
		t.Fatal(err)
	}
	c := f.last(t)
	if c.Method != "POST" || c.Path != "/subscriptions" || c.Body["url"] != "https://n.example.com/max/updates" || c.Body["secret"] != "s3cret-value" {
		t.Errorf("%+v", c)
	}
}

func TestLinkNeverLogged(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	m, _ := newFake(t)
	h := newHandler()
	for _, text := range []string{"/link github ghp_SECRET1", "/link ghp_SECRET2"} {
		m.consume(context.Background(), h, decode(t, `{"update_type":"message_created","message":{"sender":{"user_id":42},
			"recipient":{"chat_id":7,"chat_type":"dialog"},"body":{"mid":"m","text":"`+text+`"}}}`))
	}
	if strings.Contains(buf.String(), "SECRET") || len(h.cmds()) != 2 {
		t.Errorf("log:\n%s", buf.String())
	}
}

func TestCommandMenu(t *testing.T) {
	m, f := newFake(t)
	m.registerCommands(context.Background(), lister{})
	c := f.last(t)
	cmds, _ := c.Body["commands"].([]any)
	if c.Method != "PATCH" || c.Path != "/me/commands" || len(cmds) != 1 || cmds[0].(map[string]any)["name"] != "status" {
		t.Errorf("%+v", c)
	}
}

type lister struct{}

func (lister) Commands(i18n.Lang) []messenger.CommandInfo {
	return []messenger.CommandInfo{{Name: "status", Description: "Status"}}
}

func decode(t *testing.T, s string) update {
	t.Helper()
	var u update
	if err := json.Unmarshal([]byte(s), &u); err != nil {
		t.Fatal(err)
	}
	return u
}
