package max

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/notify"
)

// call is one request the fake MAX got.
type call struct {
	Method, Path, Query string
	Body                map[string]any
}

// fakeMAX answers "METHOD /path" from answers (default {}) and records
// every call.
type fakeMAX struct {
	mu      sync.Mutex
	calls   []call
	answers map[string]func(w http.ResponseWriter, r *http.Request)
}

func newFake(t *testing.T) (*Max, *fakeMAX) {
	t.Helper()
	f := &fakeMAX{answers: map[string]func(http.ResponseWriter, *http.Request){}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		c := call{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery}
		_ = json.Unmarshal(raw, &c.Body)
		f.mu.Lock()
		f.calls = append(f.calls, c)
		h := f.answers[r.Method+" "+r.URL.Path]
		f.mu.Unlock()
		if h == nil {
			_, _ = io.WriteString(w, `{}`)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	m := newWithClient(newClient(srv.URL, testToken), i18n.EN)
	m.self = "notifier_bot"
	return m, f
}

func (f *fakeMAX) answer(route, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers[route] = func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }
}

func (f *fakeMAX) status(route string, code int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers[route] = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}
}

func (f *fakeMAX) all() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

func (f *fakeMAX) last(t *testing.T) call {
	t.Helper()
	c := f.all()
	if len(c) == 0 {
		t.Fatal("no call")
	}
	return c[len(c)-1]
}

// rowsOf returns the keyboard rows of a message body.
func rowsOf(body map[string]any) []any {
	atts, _ := body["attachments"].([]any)
	for _, a := range atts {
		a := a.(map[string]any)
		if a["type"] == "inline_keyboard" {
			return a["payload"].(map[string]any)["buttons"].([]any)
		}
	}
	return nil
}

func TestSendHTMLWithButtons(t *testing.T) {
	m, f := newFake(t)
	f.answer("POST /messages", `{"message":{"body":{"mid":"mid.1"}}}`)
	n := notify.Notification{Emoji: "🚀", Heading: "Push", Repo: notify.Link{Text: "acme/app", URL: "https://github.com/acme/app"},
		Buttons: []notify.Button{{Label: "Compare", URL: "https://github.com/acme/app/compare"}}}
	attempts, err := m.Send(context.Background(), messenger.Target{ChatID: "-5"}, "", i18n.EN, n)
	if err != nil || len(attempts) != 1 || attempts[0].Err != nil {
		t.Fatalf("%v %+v", err, attempts)
	}
	c := f.last(t)
	if c.Method != "POST" || c.Path != "/messages" || !strings.Contains(c.Query, "chat_id=-5") || c.Body["format"] != "html" {
		t.Errorf("call %+v", c)
	}
	if !strings.Contains(c.Body["text"].(string), "acme/app") {
		t.Errorf("text %v", c.Body["text"])
	}
	rows := rowsOf(c.Body)
	if len(rows) != 1 {
		t.Fatalf("rows %v", rows)
	}
	b := rows[0].([]any)[0].(map[string]any)
	if b["type"] != "link" || b["text"] != "Compare" || b["url"] != "https://github.com/acme/app/compare" {
		t.Errorf("button %v", b)
	}
}

func TestSendErrorClassified(t *testing.T) {
	m, f := newFake(t)
	f.status("POST /messages", 403, `{"code":"chat.denied","message":"bot is not a member"}`)
	_, err := m.Send(context.Background(), messenger.Target{ChatID: "-5"}, "", i18n.EN, notify.Notification{Heading: "x"})
	if messenger.Classify(err) != messenger.ClassChatGone {
		t.Errorf("err %v", err)
	}
}

func TestReplyQuotesCommandWhenChoices(t *testing.T) {
	m, f := newFake(t)
	f.answer("POST /messages", `{"message":{"body":{"mid":"mid.2"}}}`)
	cmd := messenger.Command{ChatID: "-5", Ref: &message{Body: body{Mid: "mid.cmd"}}}
	if _, err := m.Reply(context.Background(), cmd, messenger.Reply{Text: "<b>hi</b>"}); err != nil {
		t.Fatal(err)
	}
	if c := f.last(t); c.Body["link"] != nil || rowsOf(c.Body) != nil || c.Body["text"] != "<b>hi</b>" || c.Body["format"] != "html" {
		t.Errorf("plain reply %+v", c.Body)
	}
	ref, err := m.Reply(context.Background(), cmd, messenger.Reply{Text: "pick", Choices: [][]messenger.Choice{{{Label: "Yes", Data: "y"}, {Label: "No", Data: "n"}}}})
	if err != nil {
		t.Fatal(err)
	}
	c := f.last(t)
	link, _ := c.Body["link"].(map[string]any)
	if link["type"] != "reply" || link["mid"] != "mid.cmd" {
		t.Errorf("link %v", c.Body["link"])
	}
	rows := rowsOf(c.Body)
	if len(rows) != 1 || len(rows[0].([]any)) != 2 {
		t.Fatalf("rows %v", rows)
	}
	b := rows[0].([]any)[1].(map[string]any)
	if b["type"] != "callback" || b["text"] != "No" || b["payload"] != "n" {
		t.Errorf("button %v", b)
	}
	if msg, ok := ref.(*message); !ok || msg.Body.Mid != "mid.2" {
		t.Errorf("ref %+v", ref)
	}
}

func TestEditAnswersCallback(t *testing.T) {
	m, f := newFake(t)
	p := messenger.Press{ChatID: "-5", Ref: &pressState{callbackID: "cb.1", msg: &message{Link: &linked{Type: "reply", Message: &body{Mid: "mid.cmd"}}}}}
	if err := m.Edit(context.Background(), p, messenger.Reply{Text: "done", Choices: [][]messenger.Choice{{{Label: "Again", Data: "a"}}}}); err != nil {
		t.Fatal(err)
	}
	c := f.last(t)
	msg, _ := c.Body["message"].(map[string]any)
	if c.Method != "POST" || c.Path != "/answers" || c.Query != "callback_id=cb.1" || msg["text"] != "done" || msg["format"] != "html" || len(rowsOf(msg)) != 1 {
		t.Fatalf("edit %+v", c)
	}
	if link, _ := msg["link"].(map[string]any); link["mid"] != "mid.cmd" {
		t.Errorf("edit lost the reply link: %v", msg["link"])
	}
	n := len(f.all())
	if err := m.Answer(context.Background(), p, "", false); err != nil || len(f.all()) != n {
		t.Errorf("empty answer after edit: %v, calls %d", err, len(f.all())-n)
	}
	if err := m.Answer(context.Background(), p, "Saved", true); err != nil {
		t.Fatal(err)
	}
	if c := f.last(t); c.Path != "/answers" || c.Body["notification"] != "Saved" {
		t.Errorf("answer %+v", c)
	}
	// An edit without buttons removes them.
	p2 := messenger.Press{Ref: &pressState{callbackID: "cb.2", msg: &message{}}}
	_ = m.Edit(context.Background(), p2, messenger.Reply{Text: "x"})
	msg, _ = f.last(t).Body["message"].(map[string]any)
	if atts, ok := msg["attachments"].([]any); !ok || len(atts) != 0 {
		t.Errorf("attachments %v", msg["attachments"])
	}
}
