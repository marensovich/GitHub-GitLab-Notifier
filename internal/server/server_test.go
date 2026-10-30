package server

import (
	"GitHub-GitLab-Notifier/internal/dispatcher"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/storage"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if err := i18n.Load("../../locales"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

type sentMessage struct {
	chatID  int64
	topicID int
	text    string
}

// fakeSender records messages and can fail sends to specific topics.
type fakeSender struct {
	sent         []sentMessage
	failTopicIDs map[int]bool
}

func (f *fakeSender) SendMessageToTopic(chatID int64, topicID int, text string) error {
	if f.failTopicIDs[topicID] {
		return errors.New("topic not found")
	}
	f.sent = append(f.sent, sentMessage{chatID, topicID, text})
	return nil
}

type env struct {
	store  *storage.Store
	sender *fakeSender
	srv    *Server
}

func newEnv(t *testing.T, cfg Config) *env {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	if cfg.DefaultLang == "" {
		cfg.DefaultLang = i18n.EN
	}
	sender := &fakeSender{failTopicIDs: map[int]bool{}}
	srv := New(cfg, dispatcher.NewDispatcher(cfg.DefaultLang, false), store, sender)
	return &env{store: store, sender: sender, srv: srv}
}

func (e *env) post(t *testing.T, headers map[string]string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(rec, req)
	return rec
}

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

const starPayload = `{"action":"created","repository":{"full_name":"octo/hello","html_url":"https://github.com/Octo/Hello"},"sender":{"login":"fan","html_url":"https://github.com/fan"}}`

func ghHeaders(event string) map[string]string {
	return map[string]string{"X-GitHub-Event": event, "Content-Type": "application/json"}
}

func TestRejectsWrongMethodAndUnknownPlatform(t *testing.T) {
	e := newEnv(t, Config{})

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	rec := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /webhook: got %d, want 405", rec.Code)
	}

	if rec := e.post(t, nil, "{}"); rec.Code != http.StatusBadRequest {
		t.Errorf("no platform header: got %d, want 400", rec.Code)
	}
}

func TestHealth(t *testing.T) {
	e := newEnv(t, Config{})
	for _, path := range []string{"/health", "/webhook/health"} {
		rec := httptest.NewRecorder()
		e.srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "pong" {
			t.Errorf("%s: got %d %q", path, rec.Code, rec.Body.String())
		}
	}
}

func TestFallsBackToDefaultChat(t *testing.T) {
	e := newEnv(t, Config{DefaultChatID: 100})

	rec := e.post(t, ghHeaders("star"), starPayload)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if len(e.sender.sent) != 1 || e.sender.sent[0].chatID != 100 || e.sender.sent[0].topicID != 0 {
		t.Fatalf("unexpected messages: %+v", e.sender.sent)
	}
}

func TestRoutesConnectedRepoToItsChatTopicAndLanguage(t *testing.T) {
	e := newEnv(t, Config{DefaultChatID: 100})

	must(t, e.store.SaveChat(storage.ChatConfig{ChatID: 200, Language: "ru", IsForum: true}))
	must(t, e.store.SaveChat(storage.ChatConfig{ChatID: 300, Language: "en", IsForum: true}))
	must(t, e.store.SaveTopic(storage.TopicInfo{TopicID: 55, ChatID: 200, Name: "⭐ Stars", EventType: "star"}))
	must(t, e.store.SetEventTopic(200, "star", 55))
	// Different spelling than in the payload: matching must be normalized.
	must(t, e.store.SaveRepo(storage.RepoInfo{ChatID: 200, RepoURL: "http://github.com/octo/hello.git/", Platform: "GitHub"}))

	e.post(t, ghHeaders("star"), starPayload)

	if len(e.sender.sent) != 1 {
		t.Fatalf("want exactly 1 message, got %+v", e.sender.sent)
	}
	msg := e.sender.sent[0]
	if msg.chatID != 200 || msg.topicID != 55 {
		t.Errorf("sent to chat %d topic %d, want chat 200 topic 55", msg.chatID, msg.topicID)
	}
	if !strings.Contains(msg.text, "поставил(а) звезду") {
		t.Errorf("message is not in Russian: %s", msg.text)
	}
}

func TestBroadcastsUnconnectedRepoToAllChats(t *testing.T) {
	e := newEnv(t, Config{DefaultChatID: 100})
	must(t, e.store.SaveChat(storage.ChatConfig{ChatID: 200, Language: "ru"}))
	must(t, e.store.SaveChat(storage.ChatConfig{ChatID: 300, Language: "en"}))

	e.post(t, ghHeaders("star"), starPayload)

	if len(e.sender.sent) != 2 {
		t.Fatalf("want 2 messages, got %+v", e.sender.sent)
	}
}

func TestFallsBackToGeneralWhenTopicSendFails(t *testing.T) {
	e := newEnv(t, Config{})
	must(t, e.store.SaveChat(storage.ChatConfig{ChatID: 200, Language: "en", IsForum: true}))
	must(t, e.store.SaveTopic(storage.TopicInfo{TopicID: 55, ChatID: 200, Name: "⭐ Stars", EventType: "star"}))
	must(t, e.store.SetEventTopic(200, "star", 55))
	e.sender.failTopicIDs[55] = true

	e.post(t, ghHeaders("star"), starPayload)

	if len(e.sender.sent) != 1 || e.sender.sent[0].topicID != 0 {
		t.Fatalf("want fallback to general chat, got %+v", e.sender.sent)
	}
}

func TestSkippedEventsSendNothing(t *testing.T) {
	e := newEnv(t, Config{DefaultChatID: 100})

	for _, event := range []string{"check_suite", "watch"} {
		rec := e.post(t, ghHeaders(event), starPayload)
		if rec.Code != http.StatusOK || rec.Body.String() != "SKIPPED" {
			t.Errorf("%s: got %d %q, want 200 SKIPPED", event, rec.Code, rec.Body.String())
		}
	}
	if len(e.sender.sent) != 0 {
		t.Fatalf("want no messages, got %+v", e.sender.sent)
	}
}

func TestGitHubSignatureWithRepoSecret(t *testing.T) {
	e := newEnv(t, Config{DefaultChatID: 100})
	must(t, e.store.SaveChat(storage.ChatConfig{ChatID: 200, Language: "en"}))
	must(t, e.store.SaveRepo(storage.RepoInfo{ChatID: 200, RepoURL: "https://github.com/octo/hello", Secret: "s3cret"}))

	h := ghHeaders("star")
	h["X-Hub-Signature-256"] = sign("s3cret", starPayload)
	if rec := e.post(t, h, starPayload); rec.Code != http.StatusOK {
		t.Errorf("valid signature: got %d", rec.Code)
	}

	h["X-Hub-Signature-256"] = sign("wrong", starPayload)
	if rec := e.post(t, h, starPayload); rec.Code != http.StatusUnauthorized {
		t.Errorf("invalid signature: got %d, want 401", rec.Code)
	}

	// Webhooks created before secrets were supported send no signature.
	if rec := e.post(t, ghHeaders("star"), starPayload); rec.Code != http.StatusOK {
		t.Errorf("no signature without global secret: got %d, want 200", rec.Code)
	}
}

func TestGlobalWebhookSecretIsRequired(t *testing.T) {
	e := newEnv(t, Config{DefaultChatID: 100, WebhookSecret: "global"})

	if rec := e.post(t, ghHeaders("star"), starPayload); rec.Code != http.StatusUnauthorized {
		t.Errorf("missing signature: got %d, want 401", rec.Code)
	}

	h := ghHeaders("star")
	h["X-Hub-Signature-256"] = sign("global", starPayload)
	if rec := e.post(t, h, starPayload); rec.Code != http.StatusOK {
		t.Errorf("valid global signature: got %d", rec.Code)
	}

	gl := `{"object_kind":"release","action":"create","tag":"v1","project":{"path_with_namespace":"g/a","web_url":"https://gitlab.com/g/a"}}`
	if rec := e.post(t, map[string]string{"X-Gitlab-Event": "Release Hook", "X-Gitlab-Token": "nope"}, gl); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong GitLab token: got %d, want 401", rec.Code)
	}
	if rec := e.post(t, map[string]string{"X-Gitlab-Event": "Release Hook", "X-Gitlab-Token": "global"}, gl); rec.Code != http.StatusOK {
		t.Errorf("valid GitLab token: got %d", rec.Code)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
