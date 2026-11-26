package max

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"GitHub-GitLab-Notifier/internal/config"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
)

func TestWebhookURLIPv6AndQuery(t *testing.T) {
	for in, want := range map[string]string{
		"https://[2001:db8::1]:443":    "https://[2001:db8::1]/max/updates",
		"https://[2001:db8::1]":        "https://[2001:db8::1]/max/updates",
		"https://n.example.com/?a=1#x": "https://n.example.com/max/updates",
		"https://n.example.com/p/?a=1": "https://n.example.com/p/max/updates",
	} {
		if got, ok := webhookURL(in); got != want || !ok {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
}

func TestAPIURLMustBeHTTPS(t *testing.T) {
	_, err := New(config.Max{Token: "x", APIURL: "http://api.example.com"}, i18n.EN, "")
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("err %v", err)
	}
}

func TestBotStartedWithoutChatDropped(t *testing.T) {
	m, _ := newFake(t)
	h := newHandler()
	m.consume(context.Background(), h, decode(t, `{"update_type":"bot_started","user":{"user_id":42}}`))
	if len(h.cmds()) != 0 {
		t.Errorf("commands %+v", h.cmds())
	}
	if _, err := m.DirectChat(context.Background(), "42"); err == nil {
		t.Error("dialog 0 remembered")
	}
}

func TestChatInfoExpires(t *testing.T) {
	m, f := newFake(t)
	f.answer("GET /chats/-5", `{"chat_id":-5,"type":"chat","title":"Team"}`)
	if _, err := m.chat(context.Background(), "-5"); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	c := m.chats["-5"]
	c.at = time.Now().Add(-chatTTL - time.Second)
	m.chats["-5"] = c
	m.mu.Unlock()
	f.answer("GET /chats/-5", `{"chat_id":-5,"type":"chat","title":"Renamed"}`)
	if ch, err := m.chat(context.Background(), "-5"); err != nil || ch.Title != "Renamed" {
		t.Fatalf("%+v %v", ch, err)
	}
}

func longHTML() string {
	return strings.Repeat(`<b>status</b> <a href="https://example.com/x">repo &amp; co</a>`+"\n", 120)
}

func TestLongReplyFits(t *testing.T) {
	m, f := newFake(t)
	if _, err := m.Reply(context.Background(), messenger.Command{ChatID: "-5"}, messenger.Reply{Text: longHTML()}); err != nil {
		t.Fatal(err)
	}
	text, _ := f.last(t).Body["text"].(string)
	if runes(text) > maxText || CheckHTML(text) != nil || !strings.Contains(text, "…") {
		t.Fatalf("%d runes, %v, ends %q", runes(text), CheckHTML(text), text[len(text)-20:])
	}
	p := messenger.Press{Ref: &pressState{callbackID: "cb", msg: &message{}}}
	_ = m.Edit(context.Background(), p, messenger.Reply{Text: longHTML()})
	msg, _ := f.last(t).Body["message"].(map[string]any)
	if text, _ := msg["text"].(string); runes(text) > maxText || CheckHTML(text) != nil {
		t.Fatalf("edit %d runes, %v", runes(text), CheckHTML(text))
	}
}

func TestFitHTMLClosesTags(t *testing.T) {
	s := "<blockquote>" + strings.Repeat("a &amp; b ", 600) + "</blockquote>"
	got := fitHTML(s)
	if runes(got) > maxText || CheckHTML(got) != nil || !strings.HasSuffix(got, "…</blockquote>") {
		t.Fatalf("%d runes, %v, ends %q", runes(got), CheckHTML(got), got[len(got)-30:])
	}
	if fitHTML("<b>x</b>") != "<b>x</b>" {
		t.Error("short text changed")
	}
}

func TestEditFallsBackToMessageEdit(t *testing.T) {
	m, f := newFake(t)
	f.status("POST /answers", http.StatusBadRequest, `{"code":"proto.payload","message":"callback expired"}`)
	p := messenger.Press{Ref: &pressState{callbackID: "cb", msg: &message{Body: body{Mid: "mid.bot"}}}}
	if err := m.Edit(context.Background(), p, messenger.Reply{Text: "done"}); err != nil {
		t.Fatal(err)
	}
	c := f.last(t)
	msg := c.Body
	if c.Method != "PUT" || c.Path != "/messages" || c.Query != "message_id=mid.bot" || msg["text"] != "done" {
		t.Fatalf("fallback %+v", c)
	}
	n := len(f.all())
	if err := m.Answer(context.Background(), p, "", false); err != nil || len(f.all()) != n {
		t.Errorf("empty answer after fallback edit: %v", err)
	}
}

func TestPollingWithoutMarkerSkipsSeen(t *testing.T) {
	m, f := newFake(t)
	f.answer("GET /chats/-5", `{"chat_id":-5,"type":"chat","title":"Team"}`)
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
		if k >= 3 {
			cancel()
			_, _ = w.Write([]byte(`{"updates":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"updates":[` + groupCommand + `]}`))
	}
	f.mu.Unlock()
	h := newHandler()
	m.poll(ctx, h)
	if got := len(h.cmds()); got != 1 {
		t.Errorf("%d commands from the same update", got)
	}
}

func TestPollingRetriesUnsubscribe(t *testing.T) {
	old := retryPause
	retryPause = 10 * time.Millisecond
	defer func() { retryPause = old }()
	m, f := newFake(t)
	var lists, polls int
	var mu sync.Mutex
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.mu.Lock()
	f.answers["GET /subscriptions"] = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		lists++
		k := lists
		mu.Unlock()
		if k == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"subscriptions":[{"url":"https://old.example.com/max/updates"}]}`))
	}
	f.answers["GET /updates"] = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		polls++
		k := polls
		mu.Unlock()
		if k == 1 {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"conflict","message":"webhook is set"}`))
			return
		}
		cancel()
		_, _ = w.Write([]byte(`{"updates":[],"marker":1}`))
	}
	f.mu.Unlock()
	m.poll(ctx, newHandler())
	deleted := false
	for _, c := range f.all() {
		if c.Method == "DELETE" && c.Path == "/subscriptions" {
			deleted = true
		}
	}
	if !deleted || lists != 2 {
		t.Errorf("deleted %v, listed %d times", deleted, lists)
	}
}
