package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/messenger/fake"
	"GitHub-GitLab-Notifier/internal/metrics"
	"GitHub-GitLab-Notifier/internal/notify"
	"GitHub-GitLab-Notifier/storage"
)

type env struct {
	store   *storage.Store
	m       *fake.Messenger
	metrics *metrics.Metrics
	o       *Outbox
	now     time.Time
	a, b    storage.Destination
	seq     int
}

func newEnv(t *testing.T, caps messenger.Caps) *env {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "outbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	e := &env{store: store, m: fake.New("telegram", caps), metrics: metrics.New("test"),
		now: time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)}
	e.o = New(store, map[string]messenger.Messenger{"telegram": e.m}, e.metrics, func() time.Time { return e.now })
	e.a, _ = store.EnsureDestination("telegram", "-1", storage.Destination{Language: "en"})
	e.b, _ = store.EnsureDestination("telegram", "-2", storage.Destination{Language: "ru"})
	return e
}

var plainCaps = messenger.Caps{Threads: true, Looks: []string{"rich", "classic"}}

// enqueue queues one delivery whose notification has the given heading,
// with one row per destination.
func (e *env) enqueue(t *testing.T, heading, thread string, dests ...storage.Destination) string {
	t.Helper()
	e.seq++
	id := heading + "-" + string(rune('0'+e.seq))
	var items []storage.OutboxItem
	for _, d := range dests {
		payload, err := json.Marshal(Payload{Lang: i18n.Lang(d.Language), Look: "rich", N: notify.Notification{Heading: heading}})
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, storage.OutboxItem{DestinationID: d.ID, ThreadID: thread, EventType: "star",
			Payload: payload, NextAttemptAt: e.now})
	}
	if _, err := e.store.EnqueueDelivery(storage.Delivery{DeliveryID: id, Platform: "GitHub", Event: "star",
		ReceivedAt: e.now, HTTPStatus: 202}, items); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *env) drain() { e.o.Drain(context.Background()) }

func (e *env) status(t *testing.T, id string) string {
	t.Helper()
	s, _, err := e.store.DeliveryState("GitHub", id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func headings(sent []fake.Sent) []string {
	var out []string
	for _, s := range sent {
		out = append(out, s.Target.ChatID+":"+s.N.Heading)
	}
	return out
}

func (e *env) metricsText() string {
	var b strings.Builder
	e.metrics.WriteTo(&b)
	return b.String()
}

func TestDelaySchedule(t *testing.T) {
	want := []time.Duration{5 * time.Second, 15 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute,
		time.Hour, time.Hour, time.Hour}
	for i, w := range want {
		if got := Delay(i + 1); got != w {
			t.Errorf("Delay(%d) = %v, want %v", i+1, got, w)
		}
	}
}

func TestOutageThenRecovery(t *testing.T) {
	e := newEnv(t, plainCaps)
	e.m.Fail(func(call int, _ messenger.Target) error {
		if call <= 3 {
			return errors.New("connection refused")
		}
		return nil
	})
	id := e.enqueue(t, "x", "", e.a)

	for _, wait := range []time.Duration{0, 5 * time.Second, 15 * time.Second} {
		e.now = e.now.Add(wait)
		e.drain()
		e.drain() // a second pass before the delay changes nothing
		if len(e.m.SentList()) != 0 {
			t.Fatalf("sent during the outage")
		}
	}
	e.now = e.now.Add(59 * time.Second)
	e.drain()
	if len(e.m.SentList()) != 0 {
		t.Fatal("retried before the third delay (1m) passed")
	}
	e.now = e.now.Add(time.Second)
	e.drain()
	if len(e.m.SentList()) != 1 || e.status(t, id) != storage.DeliveryDelivered {
		t.Fatalf("after recovery: sent %d, status %q", len(e.m.SentList()), e.status(t, id))
	}
	if out := e.metricsText(); !strings.Contains(out, `notifier_send_retries_total{messenger="telegram"} 3`) {
		t.Errorf("retries not counted:\n%s", out)
	}
}

func TestRateLimitedOnlyDelaysThatChat(t *testing.T) {
	e := newEnv(t, plainCaps)
	limited := true
	e.m.Fail(func(_ int, to messenger.Target) error {
		if to.ChatID == "-1" && limited {
			limited = false
			return &messenger.RateLimited{RetryAfter: 30 * time.Second}
		}
		return nil
	})
	e.enqueue(t, "a1", "", e.a)
	e.enqueue(t, "b1", "", e.b)
	e.enqueue(t, "a2", "", e.a)

	e.drain()
	if got := headings(e.m.SentList()); len(got) != 1 || got[0] != "-2:b1" {
		t.Fatalf("sent %v, want only the healthy chat", got)
	}
	e.now = e.now.Add(29 * time.Second)
	e.drain()
	if len(e.m.SentList()) != 1 {
		t.Fatal("the limited chat was retried before retry_after")
	}
	e.now = e.now.Add(time.Second)
	e.drain()
	if got := headings(e.m.SentList()); len(got) != 3 || got[1] != "-1:a1" || got[2] != "-1:a2" {
		t.Fatalf("sent %v, want a1 then a2 after the wait", got)
	}
}

func TestDeadAfterTenAttempts(t *testing.T) {
	e := newEnv(t, plainCaps)
	e.m.Fail(func(int, messenger.Target) error { return errors.New("bad gateway") })
	id := e.enqueue(t, "x", "", e.a)
	for i := 1; i <= 10; i++ {
		e.drain()
		e.now = e.now.Add(2 * time.Hour)
		if i < 10 && e.status(t, id) != storage.DeliveryQueued {
			t.Fatalf("attempt %d: status %q, want queued", i, e.status(t, id))
		}
	}
	if e.status(t, id) != storage.DeliveryFailed {
		t.Fatalf("status %q, want failed after 10 attempts", e.status(t, id))
	}
	if p, dead, _ := e.store.OutboxCounts(); p != 0 || dead != 1 {
		t.Errorf("pending %d dead %d", p, dead)
	}
}

func TestThreadNotFoundFallsBack(t *testing.T) {
	e := newEnv(t, plainCaps)
	e.m.Fail(func(_ int, to messenger.Target) error {
		if to.ThreadID != "" {
			return messenger.ErrThreadNotFound
		}
		return nil
	})
	id := e.enqueue(t, "x", "55", e.a)
	e.drain()
	sent := e.m.SentList()
	if len(sent) != 1 || sent[0].Target.ThreadID != "" || e.status(t, id) != storage.DeliveryDelivered {
		t.Fatalf("sent %+v, status %q; want one message in the main chat", sent, e.status(t, id))
	}
}

func TestChatGoneMarksInactive(t *testing.T) {
	e := newEnv(t, plainCaps)
	e.m.Fail(func(int, messenger.Target) error { return messenger.ErrChatGone })
	id := e.enqueue(t, "x", "", e.a)
	e.drain()
	if e.status(t, id) != storage.DeliveryFailed {
		t.Errorf("status %q, want failed", e.status(t, id))
	}
	if d, _ := e.store.DestinationByID(e.a.ID); d == nil || !d.Inactive {
		t.Errorf("destination not marked inactive: %+v", d)
	}
}

func TestRestartResumesPending(t *testing.T) {
	e := newEnv(t, plainCaps)
	id := e.enqueue(t, "x", "", e.a)
	// A worker claimed the row and the process died before it was sent.
	if it, err := e.store.ClaimNext("telegram", e.now, nil); err != nil || it == nil {
		t.Fatalf("claim: %v %v", it, err)
	}

	o := New(e.store, map[string]messenger.Messenger{"telegram": e.m}, e.metrics, func() time.Time { return e.now })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { o.Run(ctx); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	for len(e.m.SentList()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if len(e.m.SentList()) != 1 || e.status(t, id) != storage.DeliveryDelivered {
		t.Fatalf("after restart: sent %d, status %q", len(e.m.SentList()), e.status(t, id))
	}
}

func TestOrderPerDestination(t *testing.T) {
	e := newEnv(t, plainCaps)
	e.enqueue(t, "first", "", e.a)
	e.enqueue(t, "second", "", e.a)
	e.drain()
	if got := headings(e.m.SentList()); len(got) != 2 || got[0] != "-1:first" || got[1] != "-1:second" {
		t.Fatalf("sent %v, want first then second", got)
	}
}

func TestPerChatRateLimit(t *testing.T) {
	e := newEnv(t, messenger.Caps{Looks: []string{"rich"}, PerChatRate: 1, PerChatBurst: 1})
	e.enqueue(t, "a1", "", e.a)
	e.enqueue(t, "a2", "", e.a)
	e.enqueue(t, "b1", "", e.b)
	e.drain()
	if got := headings(e.m.SentList()); len(got) != 2 || got[0] != "-1:a1" || got[1] != "-2:b1" {
		t.Fatalf("sent %v, want a1 and b1 while a waits for a token", got)
	}
	e.now = e.now.Add(time.Second)
	e.drain()
	if got := headings(e.m.SentList()); len(got) != 3 || got[2] != "-1:a2" {
		t.Fatalf("sent %v, want a2 a second later", got)
	}
}

func TestSuccessReactivatesDestination(t *testing.T) {
	e := newEnv(t, plainCaps)
	e.enqueue(t, "x", "", e.a)
	if err := e.store.SetDestinationInactive(e.a.ID, true); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if d, _ := e.store.DestinationByID(e.a.ID); d.Inactive {
		t.Error("a successful send must mark the destination active")
	}
}

func TestMetricsCountAttemptsByFormat(t *testing.T) {
	e := newEnv(t, plainCaps)
	e.m.Attempts = []notify.Attempt{
		{Format: notify.FormatRich, Err: errors.New("can't parse")},
		{Format: notify.FormatClassic},
	}
	e.enqueue(t, "x", "", e.a)
	e.drain()
	out := e.metricsText()
	for _, want := range []string{
		`notifier_messages_sent_total{messenger="telegram",format="rich",result="retried"} 1`,
		`notifier_messages_sent_total{messenger="telegram",format="classic",result="ok"} 1`,
		`notifier_rich_fallbacks_total 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestMetricsCountFinalFailureOnly checks that "error" counts messages that
// were not delivered, not attempts that a later attempt replaced.
func TestMetricsCountFinalFailureOnly(t *testing.T) {
	e := newEnv(t, plainCaps)
	e.m.Attempts = []notify.Attempt{
		{Format: notify.FormatRich, Err: errors.New("can't parse")},
		{Format: notify.FormatClassic, Err: errors.New("button url")},
		{Format: notify.FormatClassic, Err: errors.New("chat not found")},
	}
	e.enqueue(t, "x", "", e.a)
	e.drain()
	out := e.metricsText()
	for _, want := range []string{
		`notifier_messages_sent_total{messenger="telegram",format="rich",result="retried"} 1`,
		`notifier_messages_sent_total{messenger="telegram",format="classic",result="retried"} 1`,
		`notifier_messages_sent_total{messenger="telegram",format="classic",result="error"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestOutboxGauges(t *testing.T) {
	e := newEnv(t, plainCaps)
	e.m.Fail(func(int, messenger.Target) error { return messenger.ErrChatGone })
	e.enqueue(t, "x", "", e.a)
	e.drain()
	e.m.Fail(nil)
	e.enqueue(t, "y", "", e.b)
	out := e.metricsText()
	for _, want := range []string{"notifier_outbox_pending 1", "notifier_outbox_dead 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRejectedGivesUpAtOnce(t *testing.T) {
	e := newEnv(t, plainCaps)
	e.m.Fail(func(int, messenger.Target) error { return messenger.ErrRejected })
	id := e.enqueue(t, "x", "", e.a)
	e.drain()
	if e.status(t, id) != storage.DeliveryFailed {
		t.Fatalf("status %q, want failed after one rejected attempt", e.status(t, id))
	}
}

func TestHungSendTimesOut(t *testing.T) {
	e := newEnv(t, plainCaps)
	e.o.sendTimeout = 50 * time.Millisecond
	e.m.Hang = true
	id := e.enqueue(t, "x", "", e.a)
	done := make(chan struct{})
	go func() { e.drain(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a hung send blocked the worker")
	}
	if e.status(t, id) != storage.DeliveryQueued {
		t.Errorf("status %q, want queued for a retry", e.status(t, id))
	}
}
