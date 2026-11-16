package metrics

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func render(m *Metrics) string {
	var b strings.Builder
	m.WriteTo(&b)
	return b.String()
}

func TestExposition(t *testing.T) {
	m := New(`v1.2.3"x`)
	m.ObserveWebhook("GitHub", "push", "delivered", 5*time.Millisecond)
	m.ObserveWebhook("GitHub", "push", "delivered", 30*time.Millisecond)
	m.ObserveWebhook("GitLab", "Merge Request Hook", "ignored", 2*time.Second)
	m.MessageSent("telegram", "rich", nil)
	m.MessageSent("telegram", "rich", errors.New("boom"))
	m.MessageSent("telegram", "rich", nil)
	m.MessageSent("telegram", "classic", nil)
	m.RichFallback()

	out := render(m)
	for _, want := range []string{
		`notifier_build_info{version="v1.2.3\"x"} 1`,
		`notifier_webhooks_total{platform="GitHub",event="push",status="delivered"} 2`,
		`notifier_webhooks_total{platform="GitLab",event="Merge Request Hook",status="ignored"} 1`,
		`notifier_webhook_duration_seconds_bucket{le="0.01"} 1`,
		`notifier_webhook_duration_seconds_bucket{le="0.05"} 2`,
		`notifier_webhook_duration_seconds_bucket{le="1"} 2`,
		`notifier_webhook_duration_seconds_bucket{le="2.5"} 3`,
		`notifier_webhook_duration_seconds_bucket{le="+Inf"} 3`,
		`notifier_webhook_duration_seconds_count 3`,
		`notifier_messages_sent_total{messenger="telegram",format="rich",result="ok"} 2`,
		`notifier_messages_sent_total{messenger="telegram",format="rich",result="error"} 1`,
		`notifier_messages_sent_total{messenger="telegram",format="classic",result="ok"} 1`,
		`notifier_rich_fallbacks_total 1`,
		"# TYPE notifier_webhook_duration_seconds histogram",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestEventLabelIsBounded(t *testing.T) {
	m := New("dev")
	m.ObserveWebhook("GitHub", "<script>", "ignored", 0)
	m.ObserveWebhook("GitHub", "", "rejected", 0)
	for i := 0; i < maxWebhookSeries+50; i++ {
		m.ObserveWebhook("GitHub", fmt.Sprintf("e%d", i), "ignored", 0)
	}
	if n := len(m.webhooks); n > maxWebhookSeries+1 {
		t.Errorf("%d series, want at most %d", n, maxWebhookSeries+1)
	}
	out := render(m)
	if strings.Contains(out, "<script>") {
		t.Error("malformed event name exported")
	}
	if !strings.Contains(out, `event="other"`) {
		t.Error(`overflow not folded into event="other"`)
	}
}

func TestHandler(t *testing.T) {
	h := New("dev").Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Errorf("GET: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", rec.Code)
	}
}
