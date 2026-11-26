package web

import (
	"GitHub-GitLab-Notifier/storage"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeSource struct {
	dests    []storage.Destination
	repos    []storage.Repo
	subs     map[int64][]int64 // destination -> repo IDs
	threads  map[int64][]storage.Thread
	routes   map[int64]map[string]string
	topicErr error

	pending, deadCount int
	dead               []storage.OutboxItem
	unconnected        []storage.RepoCount
	links              []storage.Recipient
}

func (f *fakeSource) Links() ([]storage.Recipient, error) { return f.links, nil }

func (f *fakeSource) OutboxCounts() (int, int, error) { return f.pending, f.deadCount, nil }
func (f *fakeSource) DeadItems(limit int) ([]storage.OutboxItem, error) {
	if len(f.dead) > limit {
		return f.dead[:limit], nil
	}
	return f.dead, nil
}
func (f *fakeSource) UnconnectedRepos(time.Time) ([]storage.RepoCount, error) {
	return f.unconnected, nil
}

func (f *fakeSource) Destinations() ([]storage.Destination, error) { return f.dests, nil }
func (f *fakeSource) Repos() ([]storage.Repo, error)               { return f.repos, nil }
func (f *fakeSource) Threads(id int64) ([]storage.Thread, error) {
	return f.threads[id], f.topicErr
}
func (f *fakeSource) Routes(id int64) (map[string]string, error) { return f.routes[id], nil }
func (f *fakeSource) SubscriptionsOf(id int64) ([]storage.Repo, error) {
	var list []storage.Repo
	for _, rid := range f.subs[id] {
		for _, r := range f.repos {
			if r.ID == rid {
				list = append(list, r)
			}
		}
	}
	return list, nil
}

const secret = "s3cr3t-value-that-must-not-leak"

func newSource() *fakeSource {
	return &fakeSource{
		dests: []storage.Destination{
			{ID: 1, Messenger: "telegram", ChatID: "-1001", Language: "ru", Format: storage.FormatRich, UseThreads: true},
			{ID: 2, Messenger: "telegram", ChatID: "42", Language: "en", Format: storage.FormatRich, UseThreads: true},
		},
		repos: []storage.Repo{
			{ID: 10, URL: "https://github.com/acme/api", Platform: "GitHub", Secret: secret},
			{ID: 11, URL: "https://gitlab.com/acme/web", Platform: "GitLab"},
			{ID: 12, URL: "javascript:alert(1)", Platform: "GitHub"},
		},
		subs: map[int64][]int64{1: {10, 11}},
		threads: map[int64][]storage.Thread{
			1: {{DestinationID: 1, ThreadID: "5", Name: "CI/CD"}, {DestinationID: 1, ThreadID: "3", Name: "Pushes"}},
		},
		routes: map[int64]map[string]string{
			1: {"push": "3", "pipeline": "5", "workflow_run": "5"},
		},
	}
}

func newServer(t *testing.T, cfg Config, src Source) *Server {
	t.Helper()
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:8081"
	}
	s, err := New(cfg, src)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func get(t *testing.T, h http.Handler, method, target, host string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestNewRejectsNonLoopbackAddr(t *testing.T) {
	for _, addr := range []string{":8081", "0.0.0.0:8081", "[::]:8081", "192.168.1.10:8081", "example.com:8081", "bad"} {
		if _, err := New(Config{Addr: addr}, newSource()); !errors.Is(err, ErrRemoteAddr) {
			t.Errorf("addr %q: want ErrRemoteAddr, got %v", addr, err)
		}
	}
	for _, addr := range []string{"127.0.0.1:8081", "[::1]:8081", "localhost:8081", "127.0.0.2:8081"} {
		if _, err := New(Config{Addr: addr}, newSource()); err != nil {
			t.Errorf("addr %q: unexpected error %v", addr, err)
		}
	}
	if _, err := New(Config{Addr: "0.0.0.0:8081", AllowRemote: true}, newSource()); err != nil {
		t.Errorf("AllowRemote: unexpected error %v", err)
	}
}

func TestHostHeaderCheck(t *testing.T) {
	h := newServer(t, Config{}, newSource()).Handler()

	for _, host := range []string{"localhost:8081", "127.0.0.1:8081", "[::1]:8081", "LOCALHOST"} {
		if rec := get(t, h, http.MethodGet, "/", host); rec.Code != http.StatusOK {
			t.Errorf("host %q: want 200, got %d", host, rec.Code)
		}
	}
	for _, host := range []string{"evil.example:8081", "192.168.1.10:8081", ""} {
		if rec := get(t, h, http.MethodGet, "/", host); rec.Code != http.StatusForbidden {
			t.Errorf("host %q: want 403, got %d", host, rec.Code)
		}
	}

	remote := newServer(t, Config{Addr: "0.0.0.0:8081", AllowRemote: true}, newSource()).Handler()
	if rec := get(t, remote, http.MethodGet, "/", "panel.example:8081"); rec.Code != http.StatusOK {
		t.Errorf("AllowRemote: want 200, got %d", rec.Code)
	}
}

func TestOnlyReadMethods(t *testing.T) {
	h := newServer(t, Config{}, newSource()).Handler()
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := get(t, h, m, "/repos", "localhost")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", m, rec.Code)
		}
	}
	if rec := get(t, h, http.MethodHead, "/repos", "localhost"); rec.Code != http.StatusOK {
		t.Errorf("HEAD: want 200, got %d", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := newServer(t, Config{}, newSource()).Handler()
	rec := get(t, h, http.MethodGet, "/", "localhost")
	for k, want := range map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "no-store",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("CSP = %q", csp)
	}
}

func TestPagesRender(t *testing.T) {
	h := newServer(t, Config{Version: "v9.9.9"}, newSource()).Handler()

	overview := get(t, h, http.MethodGet, "/", "localhost").Body.String()
	for _, want := range []string{"v9.9.9", "GitHub 2", "GitLab 1", "without repositories", "no webhook secret", "not connected to any chat"} {
		if !strings.Contains(overview, want) {
			t.Errorf("overview: missing %q", want)
		}
	}

	repos := get(t, h, http.MethodGet, "/repos", "localhost").Body.String()
	for _, want := range []string{`href="https://github.com/acme/api"`, "https://gitlab.com/acme/web", "telegram -1001", "own", "none", "not connected"} {
		if !strings.Contains(repos, want) {
			t.Errorf("repos: missing %q", want)
		}
	}
	if strings.Contains(repos, `href="javascript:`) {
		t.Error("repos: non-http URL rendered as a link")
	}

	chats := get(t, h, http.MethodGet, "/chats", "localhost").Body.String()
	for _, want := range []string{"CI/CD", "Pushes", "workflow_run", "telegram", "notifications go to the main chat"} {
		if !strings.Contains(chats, want) {
			t.Errorf("chats: missing %q", want)
		}
	}
	if strings.Index(chats, "Pushes") > strings.Index(chats, "CI/CD") {
		t.Error("chats: topics not sorted by ID")
	}
}

func TestSecretsNeverRendered(t *testing.T) {
	src := newSource()
	// A secret in an error text of a dead message must not show either.
	src.deadCount = 1
	src.dead = []storage.OutboxItem{{ID: 1, DestinationID: 1, LastError: "401 for https://api.telegram.org/bot" + secret + "/send", State: storage.OutboxDead}}
	src.unconnected = []storage.RepoCount{{URL: "https://github.com/acme/x", Count: 1}}
	h := newServer(t, Config{Secrets: []string{secret}}, src).Handler()
	for _, path := range []string{"/", "/repos", "/chats"} {
		if body := get(t, h, http.MethodGet, path, "localhost").Body.String(); strings.Contains(body, secret) {
			t.Errorf("%s leaks the webhook secret", path)
		}
	}
}

func TestGlobalSecretCountsAsProtected(t *testing.T) {
	src := newSource()
	src.repos = src.repos[:2]
	h := newServer(t, Config{GlobalSecretSet: true}, src).Handler()

	if body := get(t, h, http.MethodGet, "/", "localhost").Body.String(); strings.Contains(body, "no webhook secret") {
		t.Error("overview warns about secrets although WEBHOOK_SECRET is set")
	}
	if body := get(t, h, http.MethodGet, "/repos", "localhost").Body.String(); !strings.Contains(body, ">global<") {
		t.Error("repos: global secret not shown")
	}
}

func TestTopicErrorShown(t *testing.T) {
	src := newSource()
	src.topicErr = errors.New("db locked")
	h := newServer(t, Config{}, src).Handler()
	if body := get(t, h, http.MethodGet, "/chats", "localhost").Body.String(); !strings.Contains(body, "db locked") {
		t.Error("chats: topic read error not shown")
	}
}

func TestEmptyStateAndNotFound(t *testing.T) {
	h := newServer(t, Config{}, &fakeSource{}).Handler()
	if body := get(t, h, http.MethodGet, "/repos", "localhost").Body.String(); !strings.Contains(body, "No repositories connected yet") {
		t.Error("repos: empty state missing")
	}
	if body := get(t, h, http.MethodGet, "/", "localhost").Body.String(); !strings.Contains(body, "Nothing needs attention") {
		t.Error("overview: empty state missing")
	}
	rec := get(t, h, http.MethodGet, "/nope", "localhost")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "Not found") {
		t.Errorf("unknown path: got %d", rec.Code)
	}
}

func TestStaticCSS(t *testing.T) {
	h := newServer(t, Config{}, newSource()).Handler()
	rec := get(t, h, http.MethodGet, "/static/style.css", "localhost")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/css") {
		t.Errorf("style.css: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestStoreSatisfiesSource(t *testing.T) {
	var _ Source = (*storage.Store)(nil)
}

func TestChatsPageShowsSettings(t *testing.T) {
	src := newSource()
	src.dests = []storage.Destination{
		{ID: 1, Messenger: "telegram", ChatID: "-1001", Language: "ru", Format: storage.FormatClassic, UseThreads: false},
		{ID: 2, Messenger: "telegram", ChatID: "42", Language: "en", Format: storage.FormatRich, UseThreads: false},
	}
	h := newServer(t, Config{}, src).Handler()
	body := get(t, h, http.MethodGet, "/chats", "localhost").Body.String()
	for _, want := range []string{
		`<span class="badge">look: classic</span>`,
		`<span class="badge">look: rich</span>`,
		`<span class="badge warn">topics off</span>`,
		"Topic routing is off",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("chats: missing %q", want)
		}
	}
	if n := strings.Count(body, "topics off"); n != 1 {
		t.Errorf("only the forum chat has topic routing to turn off, got %d badges", n)
	}
	if strings.Contains(body, "Rich messages are off") {
		t.Error("the rich-off notice is shown while rich messages are on")
	}
}

func TestChatsPageShowsRichOff(t *testing.T) {
	src := newSource()
	src.dests = []storage.Destination{
		{ID: 2, Messenger: "telegram", ChatID: "42", Language: "en", Format: storage.FormatRich, UseThreads: true},
	}
	cfg := Config{RichAvailable: func() bool { return false }}
	body := get(t, newServer(t, cfg, src).Handler(), http.MethodGet, "/chats", "localhost").Body.String()
	for _, want := range []string{
		"Rich messages are off",
		`<span class="badge warn">look: rich → classic</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("chats: missing %q", want)
		}
	}
}

func TestChatsGroupedByMessenger(t *testing.T) {
	src := newSource()
	src.dests = []storage.Destination{
		{ID: 3, Messenger: "max", ChatID: "777", Title: "Max chat"},
		{ID: 1, Messenger: "telegram", ChatID: "-1001", Title: "Team <b>", Format: storage.FormatRich},
		{ID: 2, Messenger: "discord", ChatID: "998877", Title: "#deploys"},
	}
	body := get(t, newServer(t, Config{}, src).Handler(), http.MethodGet, "/chats", "localhost").Body.String()
	order := []string{`<h2 class="group">discord</h2>`, "#deploys", `<h2 class="group">max</h2>`, "Max chat", `<h2 class="group">telegram</h2>`, "Team &lt;b&gt;", "-1001"}
	last := -1
	for _, want := range order {
		i := strings.Index(body, want)
		if i < 0 {
			t.Fatalf("chats: missing %q", want)
		}
		if i < last {
			t.Errorf("chats: %q is out of order", want)
		}
		last = i
	}
	if strings.Contains(body, "Team <b>") {
		t.Error("chat title not escaped")
	}
}

func TestInactiveDestinationMarked(t *testing.T) {
	src := newSource()
	src.dests[1].Inactive = true
	body := get(t, newServer(t, Config{}, src).Handler(), http.MethodGet, "/chats", "localhost").Body.String()
	if n := strings.Count(body, `<span class="badge warn">inactive</span>`); n != 1 {
		t.Errorf("want 1 inactive badge, got %d", n)
	}
}

func TestOverviewShowsOutbox(t *testing.T) {
	src := newSource()
	src.pending, src.deadCount = 3, 1
	src.dead = []storage.OutboxItem{{ID: 7, DestinationID: 1, EventType: "push", Attempts: 10, LastError: "chat not found", State: storage.OutboxDead}}
	body := get(t, newServer(t, Config{}, src).Handler(), http.MethodGet, "/", "localhost").Body.String()
	for _, want := range []string{"Send queue", ">3<", "waiting", "1 given up", "telegram -1001", "push", "chat not found"} {
		if !strings.Contains(body, want) {
			t.Errorf("overview: missing %q", want)
		}
	}

	quiet := newSource()
	body = get(t, newServer(t, Config{}, quiet).Handler(), http.MethodGet, "/", "localhost").Body.String()
	if strings.Contains(body, "Messages given up on") {
		t.Error("overview lists dead messages although there are none")
	}
}

func TestOverviewWarnsUnconnectedRepo(t *testing.T) {
	src := &fakeSource{unconnected: []storage.RepoCount{{URL: "https://github.com/acme/forgotten", Count: 12}}}
	body := get(t, newServer(t, Config{}, src).Handler(), http.MethodGet, "/", "localhost").Body.String()
	for _, want := range []string{"https://github.com/acme/forgotten", "12 webhooks", "last 7 days"} {
		if !strings.Contains(body, want) {
			t.Errorf("overview: missing %q", want)
		}
	}
	if strings.Contains(body, "Nothing needs attention") {
		t.Error("overview says nothing needs attention")
	}
}

func TestPeopleTable(t *testing.T) {
	src := newSource()
	src.links = []storage.Recipient{
		{Person: storage.Person{Messenger: "discord", UserID: "77", Muted: []string{"mention"}},
			Account: storage.Account{Platform: "gitlab", Host: "git.example.com", AccountID: "7", Login: "bob"}},
		{Person: storage.Person{Messenger: "telegram", UserID: "5"},
			Account: storage.Account{Platform: "github", Host: "github.com", AccountID: "10", Login: "alice"}},
	}
	h := newServer(t, Config{}, src).Handler()
	page := get(t, h, http.MethodGet, "/people", "localhost").Body.String()
	for _, want := range []string{"<th>Messenger</th><th>User</th><th>Platform</th><th>Host</th><th>Login</th><th>Muted</th></tr>",
		"discord", "77", "git.example.com", "bob", "mention", "telegram", "alice", `href="/people" aria-current="page"`} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Index(page, "bob") > strings.Index(page, "alice") {
		t.Error("rows not in the order of the store")
	}
	src.links = nil
	if page := get(t, h, http.MethodGet, "/people", "localhost").Body.String(); !strings.Contains(page, "/link") {
		t.Error("empty state does not mention /link")
	}
}
