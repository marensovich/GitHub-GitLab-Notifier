package bot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"sync"
	"testing"

	"GitHub-GitLab-Notifier/internal/i18n"

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
	return &Bot{api: api, lang: i18n.EN, richAvailable: func() bool { return rich }}, fake
}

func methods(f *fakeAPI) []string {
	var out []string
	for _, c := range f.calls {
		out = append(out, c.method)
	}
	return out
}

func apiError(code int, description string) string {
	b, _ := json.Marshal(map[string]any{"ok": false, "error_code": code, "description": description})
	return string(b)
}
