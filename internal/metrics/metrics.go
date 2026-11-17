// Package metrics keeps the bot's counters and writes them in the
// Prometheus text exposition format. It has no dependencies: the set of
// metrics is small and fixed.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxWebhookSeries caps the number of distinct label sets of
// notifier_webhooks_total. Event names come from request headers, so
// without a cap unsigned junk requests could grow it without bound.
const maxWebhookSeries = 500

// otherEvent replaces event names that are malformed or over the cap.
const otherEvent = "other"

// durationBuckets are the upper bounds (seconds) of the webhook duration histogram.
var durationBuckets = []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// Metrics collects the bot's counters. The zero value is not usable; use New.
type Metrics struct {
	mu        sync.Mutex
	version   string
	started   time.Time
	webhooks  map[[3]string]uint64 // platform, event, status
	messages  map[[3]string]uint64 // messenger, format, result
	fallbacks uint64               // rich messages replaced by classic ones
	retries   map[string]uint64    // sends scheduled again, by messenger
	outbox    func() (pending, dead int, err error)
	durCounts []uint64 // per bucket, not cumulative; last = above all buckets
	durSum    float64
	durCount  uint64
}

// New creates an empty set of metrics.
func New(version string) *Metrics {
	return &Metrics{
		version:   version,
		started:   time.Now(),
		webhooks:  map[[3]string]uint64{},
		messages:  map[[3]string]uint64{},
		retries:   map[string]uint64{},
		durCounts: make([]uint64, len(durationBuckets)+1),
	}
}

// ObserveWebhook counts one handled webhook and how long it took.
func (m *Metrics) ObserveWebhook(platform, event, status string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := [3]string{platform, cleanEvent(event), status}
	if _, ok := m.webhooks[key]; !ok && len(m.webhooks) >= maxWebhookSeries {
		key[1] = otherEvent
	}
	m.webhooks[key]++

	sec := d.Seconds()
	m.durCounts[sort.SearchFloat64s(durationBuckets, sec)]++
	m.durSum += sec
	m.durCount++
}

// MessageSent counts one attempt to send a message through a messenger in
// a format ("rich" or "classic").
func (m *Metrics) MessageSent(messenger, format string, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	m.mu.Lock()
	m.messages[[3]string{messenger, format, result}]++
	m.mu.Unlock()
}

// MessageRetried counts one failed attempt that a later attempt replaced
// (a rich message sent again as classic, a keyboard sent again as links),
// so result="error" counts only messages that were not delivered.
func (m *Metrics) MessageRetried(messenger, format string) {
	m.mu.Lock()
	m.messages[[3]string{messenger, format, "retried"}]++
	m.mu.Unlock()
}

// RichFallback counts one rich message that Telegram rejected and that was
// sent in the classic format instead.
func (m *Metrics) RichFallback() {
	m.mu.Lock()
	m.fallbacks++
	m.mu.Unlock()
}

// SendRetried counts one message the outbox will try to send again.
func (m *Metrics) SendRetried(messenger string) {
	m.mu.Lock()
	m.retries[messenger]++
	m.mu.Unlock()
}

// SetOutboxCounts sets where the outbox gauges are read from at scrape time.
func (m *Metrics) SetOutboxCounts(f func() (pending, dead int, err error)) {
	m.mu.Lock()
	m.outbox = f
	m.mu.Unlock()
}

// cleanEvent keeps event names that look like real ones ("push",
// "Merge Request Hook") and maps the rest to "other".
func cleanEvent(event string) string {
	if event == "" || len(event) > 64 {
		return otherEvent
	}
	for _, r := range event {
		ok := r == '_' || r == ' ' || r == '-' || r == '.' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return otherEvent
		}
	}
	return event
}

// WriteTo writes all metrics in the Prometheus text format.
func (m *Metrics) WriteTo(w io.Writer) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var b strings.Builder

	b.WriteString("# HELP notifier_build_info Version of the running bot.\n# TYPE notifier_build_info gauge\n")
	fmt.Fprintf(&b, "notifier_build_info{version=%s} 1\n", quote(m.version))

	b.WriteString("# HELP notifier_start_time_seconds Unix time the bot started.\n# TYPE notifier_start_time_seconds gauge\n")
	fmt.Fprintf(&b, "notifier_start_time_seconds %d\n", m.started.Unix())

	b.WriteString("# HELP notifier_webhooks_total Webhooks received, by result.\n# TYPE notifier_webhooks_total counter\n")
	webhookKeys := make([][3]string, 0, len(m.webhooks))
	for k := range m.webhooks {
		webhookKeys = append(webhookKeys, k)
	}
	sort.Slice(webhookKeys, func(i, j int) bool { return joinKey(webhookKeys[i][:]) < joinKey(webhookKeys[j][:]) })
	for _, k := range webhookKeys {
		fmt.Fprintf(&b, "notifier_webhooks_total{platform=%s,event=%s,status=%s} %d\n",
			quote(k[0]), quote(k[1]), quote(k[2]), m.webhooks[k])
	}

	b.WriteString("# HELP notifier_webhook_duration_seconds Time to handle a webhook, including sending.\n# TYPE notifier_webhook_duration_seconds histogram\n")
	var cum uint64
	for i, le := range durationBuckets {
		cum += m.durCounts[i]
		fmt.Fprintf(&b, "notifier_webhook_duration_seconds_bucket{le=\"%s\"} %d\n", strconv.FormatFloat(le, 'g', -1, 64), cum)
	}
	fmt.Fprintf(&b, "notifier_webhook_duration_seconds_bucket{le=\"+Inf\"} %d\n", m.durCount)
	fmt.Fprintf(&b, "notifier_webhook_duration_seconds_sum %s\n", strconv.FormatFloat(m.durSum, 'g', -1, 64))
	fmt.Fprintf(&b, "notifier_webhook_duration_seconds_count %d\n", m.durCount)

	b.WriteString("# HELP notifier_messages_sent_total Send attempts to messengers, by format and result.\n# TYPE notifier_messages_sent_total counter\n")
	messageKeys := make([][3]string, 0, len(m.messages))
	for k := range m.messages {
		messageKeys = append(messageKeys, k)
	}
	sort.Slice(messageKeys, func(i, j int) bool { return joinKey(messageKeys[i][:]) < joinKey(messageKeys[j][:]) })
	for _, k := range messageKeys {
		fmt.Fprintf(&b, "notifier_messages_sent_total{messenger=%s,format=%s,result=%s} %d\n",
			quote(k[0]), quote(k[1]), quote(k[2]), m.messages[k])
	}

	b.WriteString("# HELP notifier_rich_fallbacks_total Rich messages rejected by Telegram and sent as classic ones.\n# TYPE notifier_rich_fallbacks_total counter\n")
	fmt.Fprintf(&b, "notifier_rich_fallbacks_total %d\n", m.fallbacks)

	b.WriteString("# HELP notifier_send_retries_total Messages scheduled to be sent again, by messenger.\n# TYPE notifier_send_retries_total counter\n")
	retryKeys := make([]string, 0, len(m.retries))
	for k := range m.retries {
		retryKeys = append(retryKeys, k)
	}
	sort.Strings(retryKeys)
	for _, k := range retryKeys {
		fmt.Fprintf(&b, "notifier_send_retries_total{messenger=%s} %d\n", quote(k), m.retries[k])
	}

	if m.outbox != nil {
		if pending, dead, err := m.outbox(); err == nil {
			b.WriteString("# HELP notifier_outbox_pending Messages waiting to be sent.\n# TYPE notifier_outbox_pending gauge\n")
			fmt.Fprintf(&b, "notifier_outbox_pending %d\n", pending)
			b.WriteString("# HELP notifier_outbox_dead Messages given up on.\n# TYPE notifier_outbox_dead gauge\n")
			fmt.Fprintf(&b, "notifier_outbox_dead %d\n", dead)
		}
	}

	n, err := io.WriteString(w, b.String())
	return int64(n), err
}

// Handler serves the metrics over HTTP (GET/HEAD only).
func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		m.WriteTo(w)
	})
}

func joinKey(parts []string) string { return strings.Join(parts, "\x00") }

// quote escapes a label value: backslash, double quote and newline.
func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}
