package server

import (
	"GitHub-GitLab-Notifier/internal/dispatcher"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/storage"
	"context"
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
	"time"
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

func lastDelivery(t *testing.T, e *env) storage.Delivery {
	t.Helper()
	list, err := e.store.RecentDeliveries(1)
	if err != nil || len(list) != 1 {
		t.Fatalf("no delivery logged: %v", err)
	}
	return list[0]
}

func TestRedeliveryIsSentOnce(t *testing.T) {
	e := newEnv(t, Config{DefaultChatID: 100})
	h := ghHeaders("star")
	h["X-GitHub-Delivery"] = "d-1"

	if rec := e.post(t, h, starPayload); rec.Body.String() != "OK" {
		t.Fatalf("first delivery: %d %q", rec.Code, rec.Body.String())
	}
	if d := lastDelivery(t, e); d.Status != storage.DeliveryDelivered || d.DeliveryID != "d-1" || d.Event != "star" || d.HTTPStatus != 200 {
		t.Errorf("first delivery logged as %+v", d)
	}

	rec := e.post(t, h, starPayload)
	if rec.Code != http.StatusOK || rec.Body.String() != "DUPLICATE" {
		t.Errorf("redelivery: %d %q, want 200 DUPLICATE", rec.Code, rec.Body.String())
	}
	if len(e.sender.sent) != 1 {
		t.Errorf("want 1 message, got %d", len(e.sender.sent))
	}
	if d := lastDelivery(t, e); d.Status != storage.DeliveryDuplicate {
		t.Errorf("redelivery logged as %q", d.Status)
	}

	// Another delivery ID, or none at all, is a new event.
	h["X-GitHub-Delivery"] = "d-2"
	e.post(t, h, starPayload)
	e.post(t, ghHeaders("star"), starPayload)
	e.post(t, ghHeaders("star"), starPayload)
	if len(e.sender.sent) != 4 {
		t.Errorf("want 4 messages, got %d", len(e.sender.sent))
	}
}

func TestGitLabRedeliveryUsesIdempotencyKey(t *testing.T) {
	e := newEnv(t, Config{DefaultChatID: 100})
	gl := `{"object_kind":"release","action":"create","tag":"v1","project":{"path_with_namespace":"g/a","web_url":"https://gitlab.com/g/a"}}`
	h := map[string]string{"X-Gitlab-Event": "Release Hook", "X-Gitlab-Event-UUID": "u-1"}

	e.post(t, h, gl)
	if rec := e.post(t, h, gl); rec.Body.String() != "DUPLICATE" {
		t.Errorf("same event UUID: %q, want DUPLICATE", rec.Body.String())
	}

	h["Idempotency-Key"] = "k-1"
	h["X-Gitlab-Event-UUID"] = "u-2"
	e.post(t, h, gl)
	h["X-Gitlab-Event-UUID"] = "u-3" // a retry keeps the key
	if rec := e.post(t, h, gl); rec.Body.String() != "DUPLICATE" {
		t.Errorf("same idempotency key: %q, want DUPLICATE", rec.Body.String())
	}
	if len(e.sender.sent) != 2 {
		t.Errorf("want 2 messages, got %d", len(e.sender.sent))
	}
}

func TestFailedDeliveryCanBeRetried(t *testing.T) {
	e := newEnv(t, Config{DefaultChatID: 100})
	e.sender.failTopicIDs[0] = true
	h := ghHeaders("star")
	h["X-GitHub-Delivery"] = "d-1"

	if rec := e.post(t, h, starPayload); rec.Code != http.StatusOK {
		t.Fatalf("failed send: got %d, want 200", rec.Code)
	}
	if d := lastDelivery(t, e); d.Status != storage.DeliveryFailed || d.Error == "" {
		t.Errorf("failed send logged as %+v", d)
	}

	e.sender.failTopicIDs[0] = false
	if rec := e.post(t, h, starPayload); rec.Body.String() != "OK" {
		t.Errorf("retry after failure: %q, want OK", rec.Body.String())
	}
	if len(e.sender.sent) != 1 {
		t.Errorf("want 1 message after retry, got %d", len(e.sender.sent))
	}
}

func TestRejectedAndIgnoredAreLogged(t *testing.T) {
	e := newEnv(t, Config{DefaultChatID: 100, WebhookSecret: "global"})

	e.post(t, ghHeaders("star"), starPayload)
	if d := lastDelivery(t, e); d.Status != storage.DeliveryRejected || d.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("unsigned webhook logged as %+v", d)
	}

	h := ghHeaders("check_suite")
	h["X-Hub-Signature-256"] = sign("global", starPayload)
	e.post(t, h, starPayload)
	d := lastDelivery(t, e)
	if d.Status != storage.DeliveryIgnored || d.RepoURL != "https://github.com/Octo/Hello" {
		t.Errorf("skipped event logged as %+v", d)
	}
}

func TestPruneDeliveriesStopsWithContext(t *testing.T) {
	e := newEnv(t, Config{DeliveriesRetention: time.Hour})
	must(t, e.store.RecordDelivery(storage.Delivery{Platform: "GitHub", ReceivedAt: time.Now().Add(-2 * time.Hour), Status: storage.DeliveryIgnored, HTTPStatus: 200}))
	must(t, e.store.RecordDelivery(storage.Delivery{Platform: "GitHub", ReceivedAt: time.Now(), Status: storage.DeliveryIgnored, HTTPStatus: 200}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.srv.PruneDeliveries(ctx) // prunes once, then returns because ctx is done

	if list, _ := e.store.RecentDeliveries(10); len(list) != 1 {
		t.Errorf("want 1 delivery left, got %d", len(list))
	}
}
