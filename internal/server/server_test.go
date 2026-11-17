package server

import (
	"GitHub-GitLab-Notifier/internal/dispatcher"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/messenger/fake"
	"GitHub-GitLab-Notifier/internal/messenger/telegram"
	"GitHub-GitLab-Notifier/internal/outbox"
	"GitHub-GitLab-Notifier/storage"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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

type env struct {
	store *storage.Store
	m     *fake.Messenger
	o     *outbox.Outbox
	srv   *Server
	calls int // sends tried since failThread
}

// failThread makes sends to a thread fail as deleted and counts every send.
func (e *env) failThread(id string) {
	e.m.Fail(func(_ int, to messenger.Target) error {
		e.calls++
		if to.ThreadID == id {
			return messenger.ErrThreadNotFound
		}
		return nil
	})
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
	m := fake.New("telegram", messenger.Caps{Threads: true, Looks: []string{"rich", "classic"}})
	o := outbox.New(store, map[string]messenger.Messenger{"telegram": m}, cfg.Metrics, time.Now)
	srv := New(cfg, dispatcher.NewDispatcher(cfg.DefaultLang, false), store, o)
	return &env{store: store, m: m, o: o, srv: srv}
}

// postOnly sends a webhook and leaves its messages in the outbox.
func (e *env) postOnly(t *testing.T, headers map[string]string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(rec, req)
	return rec
}

// post sends a webhook and then delivers what it queued.
func (e *env) post(t *testing.T, headers map[string]string, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := e.postOnly(t, headers, body)
	e.o.Drain(context.Background())
	return rec
}

// sentMessage is a delivered message in the shape the tests check.
type sentMessage struct {
	chatID  int64
	topicID int
	format  string
	lang    i18n.Lang
	text    string
}

func (e *env) sent() []sentMessage {
	var out []sentMessage
	for _, s := range e.m.SentList() {
		chatID, _ := strconv.ParseInt(s.Target.ChatID, 10, 64)
		topicID, _ := strconv.Atoi(s.Target.ThreadID)
		text, _ := telegram.RenderClassic(s.Lang, s.N)
		out = append(out, sentMessage{chatID, topicID, s.Look, s.Lang, text})
	}
	return out
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
	e := newEnv(t, Config{Version: "v9"})
	for _, path := range []string{"/health", "/healthz", "/webhook/health"} {
		rec := httptest.NewRecorder()
		e.srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"version":"v9"`) {
			t.Errorf("%s: got %d %q", path, rec.Code, rec.Body.String())
		}
	}
}

func TestUnconnectedRepoIsDropped(t *testing.T) {
	e := newEnv(t, Config{})
	// Chats exist, but none is subscribed to the repository.
	e.chat(t, 200, "ru")
	e.connect(t, e.chat(t, 300, "en"), "https://github.com/octo/other", "")

	rec := e.post(t, ghHeaders("star"), starPayload)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("got %d: %s, want 202", rec.Code, rec.Body.String())
	}
	if len(e.sent()) != 0 {
		t.Fatalf("want no messages, got %+v", e.sent())
	}
	if d := lastDelivery(t, e); d.Status != storage.DeliveryIgnored || d.Error != "not connected" {
		t.Errorf("delivery logged as %+v", d)
	}
}

func TestRoutesConnectedRepoToItsChatTopicAndLanguage(t *testing.T) {
	e := newEnv(t, Config{})

	ru := e.chat(t, 200, "ru")
	e.chat(t, 300, "en")
	e.topic(t, ru, 55, "star")
	// Different spelling than in the payload: matching must be normalized.
	e.connect(t, ru, "http://github.com/octo/hello.git/", "")

	e.post(t, ghHeaders("star"), starPayload)

	if len(e.sent()) != 1 {
		t.Fatalf("want exactly 1 message, got %+v", e.sent())
	}
	msg := e.sent()[0]
	if msg.chatID != 200 || msg.topicID != 55 {
		t.Errorf("sent to chat %d topic %d, want chat 200 topic 55", msg.chatID, msg.topicID)
	}
	if !strings.Contains(msg.text, "Новая звезда") {
		t.Errorf("message is not in Russian: %s", msg.text)
	}
}

func TestFallsBackToGeneralWhenTopicSendFails(t *testing.T) {
	e := newEnv(t, Config{})
	d := e.chat(t, 200, "en")
	e.topic(t, d, 55, "star")
	e.connect(t, d, "https://github.com/octo/hello", "")
	e.failThread("55")

	e.post(t, ghHeaders("star"), starPayload)

	if len(e.sent()) != 1 || e.sent()[0].topicID != 0 {
		t.Fatalf("want fallback to general chat, got %+v", e.sent())
	}
}

func TestSkippedEventsSendNothing(t *testing.T) {
	e := newEnv(t, Config{})

	for _, event := range []string{"check_suite", "watch"} {
		rec := e.post(t, ghHeaders(event), starPayload)
		if rec.Code != http.StatusOK || rec.Body.String() != "SKIPPED" {
			t.Errorf("%s: got %d %q, want 200 SKIPPED", event, rec.Code, rec.Body.String())
		}
	}
	if len(e.sent()) != 0 {
		t.Fatalf("want no messages, got %+v", e.sent())
	}
}

func TestGitHubSignatureWithRepoSecret(t *testing.T) {
	e := newEnv(t, Config{})
	e.connect(t, e.chat(t, 200, "en"), "https://github.com/octo/hello", "s3cret")

	h := ghHeaders("star")
	h["X-Hub-Signature-256"] = sign("s3cret", starPayload)
	if rec := e.post(t, h, starPayload); rec.Code != http.StatusAccepted {
		t.Errorf("valid signature: got %d", rec.Code)
	}

	h["X-Hub-Signature-256"] = sign("wrong", starPayload)
	if rec := e.post(t, h, starPayload); rec.Code != http.StatusUnauthorized {
		t.Errorf("invalid signature: got %d, want 401", rec.Code)
	}

	// Webhooks created before secrets were supported send no signature.
	if rec := e.post(t, ghHeaders("star"), starPayload); rec.Code != http.StatusAccepted {
		t.Errorf("no signature without global secret: got %d, want 202", rec.Code)
	}
}

func TestGlobalWebhookSecretIsRequired(t *testing.T) {
	e := newEnv(t, Config{WebhookSecret: "global"})
	e.hello(t)
	e.connect(t, e.chat(t, 100, "en"), "https://gitlab.com/g/a", "")

	if rec := e.post(t, ghHeaders("star"), starPayload); rec.Code != http.StatusUnauthorized {
		t.Errorf("missing signature: got %d, want 401", rec.Code)
	}

	h := ghHeaders("star")
	h["X-Hub-Signature-256"] = sign("global", starPayload)
	if rec := e.post(t, h, starPayload); rec.Code != http.StatusAccepted {
		t.Errorf("valid global signature: got %d", rec.Code)
	}

	gl := `{"object_kind":"release","action":"create","tag":"v1","project":{"path_with_namespace":"g/a","web_url":"https://gitlab.com/g/a"}}`
	if rec := e.post(t, map[string]string{"X-Gitlab-Event": "Release Hook", "X-Gitlab-Token": "nope"}, gl); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong GitLab token: got %d, want 401", rec.Code)
	}
	if rec := e.post(t, map[string]string{"X-Gitlab-Event": "Release Hook", "X-Gitlab-Token": "global"}, gl); rec.Code != http.StatusAccepted {
		t.Errorf("valid GitLab token: got %d", rec.Code)
	}
}

// chat stores a Telegram chat as a destination.
func (e *env) chat(t *testing.T, chatID int64, lang string) storage.Destination {
	t.Helper()
	d, err := e.store.EnsureDestination("telegram", strconv.FormatInt(chatID, 10),
		storage.Destination{Language: lang, Format: storage.FormatRich, UseThreads: true})
	must(t, err)
	return d
}

// connect subscribes a destination to a repository, creating it if needed.
func (e *env) connect(t *testing.T, d storage.Destination, url, secret string) {
	t.Helper()
	repo, err := e.store.FindRepo(url)
	must(t, err)
	if repo == nil {
		created, err := e.store.CreateRepo(url, "GitHub", secret)
		must(t, err)
		repo = &created
	}
	must(t, e.store.Subscribe(repo.ID, d.ID))
}

// topic routes an event type to a forum topic of a destination.
func (e *env) topic(t *testing.T, d storage.Destination, topicID int, event string) {
	t.Helper()
	must(t, e.store.SaveThread(storage.Thread{DestinationID: d.ID, ThreadID: strconv.Itoa(topicID), Name: event}, event))
}

// hello subscribes chat 100 to the repository of starPayload.
func (e *env) hello(t *testing.T) {
	t.Helper()
	e.connect(t, e.chat(t, 100, "en"), "https://github.com/octo/hello", "")
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
	e := newEnv(t, Config{})
	e.hello(t)
	h := ghHeaders("star")
	h["X-GitHub-Delivery"] = "d-1"

	if rec := e.post(t, h, starPayload); rec.Code != http.StatusAccepted || rec.Body.String() != "QUEUED" {
		t.Fatalf("first delivery: %d %q", rec.Code, rec.Body.String())
	}
	if d := lastDelivery(t, e); d.Status != storage.DeliveryDelivered || d.DeliveryID != "d-1" || d.Event != "star" || d.HTTPStatus != 202 {
		t.Errorf("first delivery logged as %+v", d)
	}

	rec := e.post(t, h, starPayload)
	if rec.Code != http.StatusOK || rec.Body.String() != "DUPLICATE" {
		t.Errorf("redelivery: %d %q, want 200 DUPLICATE", rec.Code, rec.Body.String())
	}
	if len(e.sent()) != 1 {
		t.Errorf("want 1 message, got %d", len(e.sent()))
	}
	if d := lastDelivery(t, e); d.Status != storage.DeliveryDuplicate {
		t.Errorf("redelivery logged as %q", d.Status)
	}

	// Another delivery ID, or none at all, is a new event.
	h["X-GitHub-Delivery"] = "d-2"
	e.post(t, h, starPayload)
	e.post(t, ghHeaders("star"), starPayload)
	e.post(t, ghHeaders("star"), starPayload)
	if len(e.sent()) != 4 {
		t.Errorf("want 4 messages, got %d", len(e.sent()))
	}
}

func TestGitLabRedeliveryUsesIdempotencyKey(t *testing.T) {
	e := newEnv(t, Config{})
	e.connect(t, e.chat(t, 100, "en"), "https://gitlab.com/g/a", "")
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
	if len(e.sent()) != 2 {
		t.Errorf("want 2 messages, got %d", len(e.sent()))
	}
}

func TestWebhookAnswers202BeforeSending(t *testing.T) {
	e := newEnv(t, Config{})
	e.hello(t)
	rec := e.postOnly(t, ghHeaders("star"), starPayload)
	if rec.Code != http.StatusAccepted || rec.Body.String() != "QUEUED" {
		t.Fatalf("got %d %q, want 202 QUEUED", rec.Code, rec.Body.String())
	}
	if len(e.sent()) != 0 {
		t.Fatal("the handler sent the message itself")
	}
	if d := lastDelivery(t, e); d.Status != storage.DeliveryQueued || d.HTTPStatus != 202 {
		t.Errorf("delivery logged as %+v", d)
	}
	if pending, _, _ := e.store.OutboxCounts(); pending != 1 {
		t.Errorf("pending = %d, want 1", pending)
	}
}

func TestRedeliveryOfQueuedIsDuplicate(t *testing.T) {
	e := newEnv(t, Config{})
	e.hello(t)
	h := ghHeaders("star")
	h["X-GitHub-Delivery"] = "d-1"
	e.postOnly(t, h, starPayload)
	if rec := e.postOnly(t, h, starPayload); rec.Code != http.StatusOK || rec.Body.String() != "DUPLICATE" {
		t.Errorf("redelivery while queued: %d %q, want 200 DUPLICATE", rec.Code, rec.Body.String())
	}
	if pending, _, _ := e.store.OutboxCounts(); pending != 1 {
		t.Errorf("pending = %d, want 1", pending)
	}
}

func TestRedeliveryOfFailedRequeues(t *testing.T) {
	e := newEnv(t, Config{})
	e.hello(t)
	e.m.Fail(func(int, messenger.Target) error { return messenger.ErrChatGone })
	h := ghHeaders("star")
	h["X-GitHub-Delivery"] = "d-1"

	e.post(t, h, starPayload)
	if d := lastDelivery(t, e); d.Status != storage.DeliveryFailed || d.Error == "" {
		t.Fatalf("delivery logged as %+v, want failed with an error", d)
	}

	e.m.Fail(nil)
	rec := e.post(t, h, starPayload)
	if rec.Code != http.StatusAccepted || rec.Body.String() != "REQUEUED" {
		t.Errorf("redelivery of a failed one: %d %q, want 202 REQUEUED", rec.Code, rec.Body.String())
	}
	if len(e.sent()) != 1 {
		t.Errorf("want 1 message after the requeue, got %d", len(e.sent()))
	}
	if status, _, _ := e.store.DeliveryState(lastDelivery(t, e).Platform, "d-1"); status != storage.DeliveryDelivered {
		t.Errorf("status = %q, want delivered", status)
	}
}

func TestFanOutToSubscribers(t *testing.T) {
	e := newEnv(t, Config{})
	e.connect(t, e.chat(t, 200, "ru"), "https://github.com/octo/hello", "")
	e.connect(t, e.chat(t, 300, "en"), "https://github.com/octo/hello", "")

	e.post(t, ghHeaders("star"), starPayload)

	langs := map[int64]i18n.Lang{}
	for _, m := range e.sent() {
		langs[m.chatID] = m.lang
	}
	if len(e.sent()) != 2 || langs[200] != i18n.RU || langs[300] != i18n.EN {
		t.Fatalf("sent %+v, want one message per chat in its language", e.sent())
	}
}

func TestRejectedAndIgnoredAreLogged(t *testing.T) {
	e := newEnv(t, Config{WebhookSecret: "global"})

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

func TestUseTopicsOffRoutesToMain(t *testing.T) {
	e := newEnv(t, Config{})
	d := e.chat(t, 200, "en")
	e.topic(t, d, 55, "star")
	must(t, e.store.SetDestinationSettings(d.ID, storage.FormatClassic, false))
	e.connect(t, d, "https://github.com/octo/hello", "")

	if rec := e.post(t, ghHeaders("star"), starPayload); rec.Code != http.StatusAccepted {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if len(e.sent()) != 1 {
		t.Fatalf("want 1 message, got %+v", e.sent())
	}
	if msg := e.sent()[0]; msg.topicID != 0 || msg.format != storage.FormatClassic {
		t.Errorf("got topic %d format %q, want the general chat in classic", msg.topicID, msg.format)
	}
}

func TestDeletedTopicFallsBackToMain(t *testing.T) {
	e := newEnv(t, Config{})
	d := e.chat(t, 200, "en")
	e.topic(t, d, 55, "star")
	e.connect(t, d, "https://github.com/octo/hello", "")
	e.failThread("55")

	e.post(t, ghHeaders("star"), starPayload)
	if e.calls != 2 {
		t.Errorf("want 2 sends (topic, then general), got %d", e.calls)
	}
	if len(e.sent()) != 1 || e.sent()[0].topicID != 0 || e.sent()[0].format != storage.FormatRich {
		t.Fatalf("want one rich message in the general chat, got %+v", e.sent())
	}
}
