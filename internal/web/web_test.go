package web

import (
	"GitHub-GitLab-Notifier/storage"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeSource struct {
	chats    []storage.ChatConfig
	repos    []storage.RepoInfo
	topics   map[int64][]storage.TopicInfo
	mappings map[int64]map[string]int
	topicErr error
}

func (f *fakeSource) GetAllChats() []storage.ChatConfig { return f.chats }
func (f *fakeSource) GetAllRepos() []storage.RepoInfo   { return f.repos }
func (f *fakeSource) GetTopicsByChat(id int64) ([]storage.TopicInfo, error) {
	return f.topics[id], f.topicErr
}
func (f *fakeSource) GetEventMappings(id int64) (map[string]int, error) {
	return f.mappings[id], nil
}

const secret = "s3cr3t-value-that-must-not-leak"

func newSource() *fakeSource {
	return &fakeSource{
		chats: []storage.ChatConfig{
			{ChatID: -1001, Language: "ru", IsForum: true},
			{ChatID: 42, Language: "en"},
		},
		repos: []storage.RepoInfo{
			{ChatID: -1001, RepoURL: "https://github.com/acme/api", Platform: "GitHub", Secret: secret},
			{ChatID: -1001, RepoURL: "https://gitlab.com/acme/web", Platform: "GitLab"},
			{ChatID: 7, RepoURL: "javascript:alert(1)", Platform: "GitHub"},
		},
		topics: map[int64][]storage.TopicInfo{
			-1001: {{TopicID: 5, ChatID: -1001, Name: "CI/CD"}, {TopicID: 3, ChatID: -1001, Name: "Pushes"}},
		},
		mappings: map[int64]map[string]int{
			-1001: {"push": 3, "pipeline": 5, "workflow_run": 5},
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
	for _, want := range []string{"v9.9.9", "GitHub 2", "GitLab 1", "without repositories", "no webhook secret", "chat the bot has no record of"} {
		if !strings.Contains(overview, want) {
			t.Errorf("overview: missing %q", want)
		}
	}

	repos := get(t, h, http.MethodGet, "/repos", "localhost").Body.String()
	for _, want := range []string{`href="https://github.com/acme/api"`, "https://gitlab.com/acme/web", "-1001", "own", "none"} {
		if !strings.Contains(repos, want) {
			t.Errorf("repos: missing %q", want)
		}
	}
	if strings.Contains(repos, `href="javascript:`) {
		t.Error("repos: non-http URL rendered as a link")
	}

	chats := get(t, h, http.MethodGet, "/chats", "localhost").Body.String()
	for _, want := range []string{"CI/CD", "Pushes", "workflow_run", "forum", "notifications go to the main chat"} {
		if !strings.Contains(chats, want) {
			t.Errorf("chats: missing %q", want)
		}
	}
	if strings.Index(chats, "Pushes") > strings.Index(chats, "CI/CD") {
		t.Error("chats: topics not sorted by ID")
	}
}

func TestSecretsNeverRendered(t *testing.T) {
	h := newServer(t, Config{}, newSource()).Handler()
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
	src.chats = []storage.ChatConfig{
		{ChatID: -1001, Language: "ru", IsForum: true, Format: storage.FormatClassic, UseTopics: false},
		{ChatID: 42, Language: "en", Format: storage.FormatRich, UseTopics: false},
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
}
