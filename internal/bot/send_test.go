package bot

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/notify"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"sync"
	"testing"

	"github.com/mymmrac/telego"
)

func TestMain(m *testing.M) {
	if err := i18n.Load("../../locales"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// apiCall is one request the fake Bot API received.
type apiCall struct {
	method string
	body   map[string]any
}

// fakeAPI is a Bot API server that answers each method from a queue of
// replies (the last reply repeats) and records every call.
type fakeAPI struct {
	mu      sync.Mutex
	calls   []apiCall
	replies map[string][]string
}

const okMessage = `{"ok":true,"result":{"message_id":1,"date":0,"chat":{"id":1,"type":"supergroup"}}}`

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	method := path.Base(r.URL.Path)
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	f.calls = append(f.calls, apiCall{method, body})

	reply := okMessage
	if q := f.replies[method]; len(q) > 0 {
		reply = q[0]
		if len(q) > 1 {
			f.replies[method] = q[1:]
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, reply)
}

func newSendBot(t *testing.T, rich bool, replies map[string][]string) (*Bot, *fakeAPI) {
	t.Helper()
	fake := &fakeAPI{replies: replies}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	api, err := telego.NewBot("123456:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		telego.WithAPIServer(srv.URL), telego.WithDiscardLogger())
	if err != nil {
		t.Fatal(err)
	}
	return &Bot{api: api, lang: i18n.EN, rich: rich}, fake
}

func apiError(code int, description string) string {
	b, _ := json.Marshal(map[string]any{"ok": false, "error_code": code, "description": description})
	return string(b)
}

func sample() notify.Notification {
	return notify.Notification{
		Heading: "🔀 New pull request",
		Item:    notify.Link{Text: "#12 Add dark mode", URL: "https://github.com/octo/hello/pull/12"},
		Repo:    notify.Link{Text: "octo/hello", URL: "https://github.com/octo/hello"},
		Buttons: []notify.Button{{Label: "Open PR", URL: "https://github.com/octo/hello/pull/12", Style: notify.StylePrimary}},
	}
}

func formats(as []notify.Attempt) []string {
	var out []string
	for _, a := range as {
		s := a.Format
		if a.Err != nil {
			s += ":err"
		}
		out = append(out, s)
	}
	return out
}

func methods(f *fakeAPI) []string {
	var out []string
	for _, c := range f.calls {
		out = append(out, c.method)
	}
	return out
}

func TestSendRichOK(t *testing.T) {
	b, fake := newSendBot(t, true, nil)
	attempts, err := b.SendNotification(-100, 9, notify.FormatRich, i18n.EN, sample())
	if err != nil {
		t.Fatal(err)
	}
	if got := methods(fake); len(got) != 1 || got[0] != "sendRichMessage" {
		t.Fatalf("calls = %v, want [sendRichMessage]", got)
	}
	body := fake.calls[0].body
	if body["message_thread_id"] != float64(9) {
		t.Errorf("message_thread_id = %v, want 9", body["message_thread_id"])
	}
	rm, _ := body["rich_message"].(map[string]any)
	if rm["skip_entity_detection"] != true || rm["html"] == "" {
		t.Errorf("rich_message = %v, want html and skip_entity_detection", rm)
	}
	if got := formats(attempts); len(got) != 1 || got[0] != "rich" {
		t.Errorf("attempts = %v", got)
	}
}

func TestSendRichRejectedFallsBack(t *testing.T) {
	b, fake := newSendBot(t, true, map[string][]string{
		"sendRichMessage": {apiError(400, "Bad Request: can't parse rich message")},
	})
	attempts, err := b.SendNotification(-100, 9, notify.FormatRich, i18n.EN, sample())
	if err != nil {
		t.Fatalf("fallback should succeed, got %v", err)
	}
	if got := methods(fake); len(got) != 2 || got[0] != "sendRichMessage" || got[1] != "sendMessage" {
		t.Fatalf("calls = %v", got)
	}
	classic := fake.calls[1].body
	if classic["message_thread_id"] != float64(9) || classic["parse_mode"] != "HTML" {
		t.Errorf("classic call = %v", classic)
	}
	markup, _ := classic["reply_markup"].(map[string]any)
	rows, _ := markup["inline_keyboard"].([]any)
	if len(rows) != 1 {
		t.Fatalf("want one keyboard row, got %v", classic["reply_markup"])
	}
	btn := rows[0].([]any)[0].(map[string]any)
	if btn["text"] != "Open PR" || btn["style"] != "primary" || btn["url"] == "" {
		t.Errorf("button = %v", btn)
	}
	if got := formats(attempts); len(got) != 2 || got[0] != "rich:err" || got[1] != "classic" {
		t.Errorf("attempts = %v", got)
	}
}

func TestSendTopicErrorNoFallback(t *testing.T) {
	b, fake := newSendBot(t, true, map[string][]string{
		"sendRichMessage": {apiError(400, "Bad Request: message thread not found")},
	})
	attempts, err := b.SendNotification(-100, 9, notify.FormatRich, i18n.EN, sample())
	if err == nil {
		t.Fatal("want the topic error returned")
	}
	if got := methods(fake); len(got) != 1 {
		t.Fatalf("calls = %v, want only the rich attempt", got)
	}
	if got := formats(attempts); len(got) != 1 || got[0] != "rich:err" {
		t.Errorf("attempts = %v", got)
	}
}

func TestSend429NoFallback(t *testing.T) {
	b, fake := newSendBot(t, true, map[string][]string{
		"sendRichMessage": {apiError(429, "Too Many Requests: retry after 31")},
	})
	if _, err := b.SendNotification(-100, 0, notify.FormatRich, i18n.EN, sample()); err == nil {
		t.Fatal("want the rate limit error returned")
	}
	if got := methods(fake); len(got) != 1 {
		t.Fatalf("calls = %v, want no fallback", got)
	}
}

func TestSendClassicFormat(t *testing.T) {
	b, fake := newSendBot(t, true, nil)
	attempts, err := b.SendNotification(-100, 0, notify.FormatClassic, i18n.EN, sample())
	if err != nil {
		t.Fatal(err)
	}
	if got := methods(fake); len(got) != 1 || got[0] != "sendMessage" {
		t.Fatalf("calls = %v, want [sendMessage]", got)
	}
	if _, ok := fake.calls[0].body["message_thread_id"]; ok {
		t.Error("topic 0 must not set message_thread_id")
	}
	if got := formats(attempts); len(got) != 1 || got[0] != "classic" {
		t.Errorf("attempts = %v", got)
	}
}

func TestSendRichDisabled(t *testing.T) {
	b, fake := newSendBot(t, false, nil)
	if _, err := b.SendNotification(-100, 0, notify.FormatRich, i18n.EN, sample()); err != nil {
		t.Fatal(err)
	}
	if got := methods(fake); len(got) != 1 || got[0] != "sendMessage" {
		t.Fatalf("calls = %v, want [sendMessage]", got)
	}
}

func TestIsTopicOrChatError(t *testing.T) {
	for desc, want := range map[string]bool{
		"Bad Request: message thread not found":  true,
		"Bad Request: TOPIC_CLOSED":              true,
		"Bad Request: TOPIC_DELETED":             true,
		"Bad Request: chat not found":            true,
		"Bad Request: not enough rights to send": true,
		"Bad Request: can't parse rich message":  false,
	} {
		if got := isTopicOrChatError(desc); got != want {
			t.Errorf("isTopicOrChatError(%q) = %v, want %v", desc, got, want)
		}
	}
}
