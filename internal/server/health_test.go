package server

import (
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/messenger/fake"
	"GitHub-GitLab-Notifier/internal/metrics"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func getPath(e *env, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestHealthz(t *testing.T) {
	failing := errors.New("api down")
	telegramErr := error(nil)
	e := newEnv(t, Config{Version: "v9", Checks: map[string]Check{
		"db":       func(context.Context) error { return nil },
		"telegram": func(context.Context) error { return telegramErr },
	}})

	var body struct {
		Status  string
		Version string
		Checks  map[string]struct {
			OK    bool
			Error string
		}
	}
	rec := getPath(e, http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("healthy: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	must(t, json.Unmarshal(rec.Body.Bytes(), &body))
	if body.Status != "ok" || body.Version != "v9" || !body.Checks["db"].OK || !body.Checks["telegram"].OK {
		t.Errorf("healthy body: %+v", body)
	}

	telegramErr = failing
	rec = getPath(e, http.MethodGet, "/healthz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("failing check: got %d, want 503", rec.Code)
	}
	must(t, json.Unmarshal(rec.Body.Bytes(), &body))
	if body.Status != "fail" || body.Checks["telegram"].OK || body.Checks["telegram"].Error != "api down" || !body.Checks["db"].OK {
		t.Errorf("failing body: %+v", body)
	}

	if rec := getPath(e, http.MethodPost, "/healthz"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /healthz: %d", rec.Code)
	}
	// /health gives the same report, failures included.
	if rec := getPath(e, http.MethodGet, "/health"); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "api down") {
		t.Errorf("/health: %d %q", rec.Code, rec.Body.String())
	}
}

func TestMetricsEndpoint(t *testing.T) {
	m := metrics.New("v9")
	e := newEnv(t, Config{Metrics: m, ServeMetrics: true})
	e.hello(t)
	e.post(t, ghHeaders("star"), starPayload)
	e.post(t, ghHeaders("star"), starPayload)
	e.post(t, ghHeaders("check_suite"), starPayload)

	rec := getPath(e, http.MethodGet, "/metrics")
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics: %d", rec.Code)
	}
	out := rec.Body.String()
	for _, want := range []string{
		`notifier_webhooks_total{platform="GITHUB",event="star",status="queued"} 2`,
		`notifier_webhooks_total{platform="GITHUB",event="check_suite",status="ignored"} 1`,
		`notifier_webhook_duration_seconds_count 3`,
		`notifier_build_info{version="v9"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "octo/hello") {
		t.Error("repository URL leaked into metric labels")
	}

	off := newEnv(t, Config{})
	if rec := getPath(off, http.MethodGet, "/metrics"); rec.Code != http.StatusNotFound {
		t.Errorf("/metrics without ServeMetrics: %d, want 404", rec.Code)
	}
}

func TestHealthPerMessenger(t *testing.T) {
	tg := fake.New("telegram", messenger.Caps{})
	dc := fake.New("discord", messenger.Caps{})
	dc.HealthErr = errors.New("gateway closed")
	checks := Checks(func(context.Context) error { return nil }, map[string]messenger.Messenger{"telegram": tg, "discord": dc})

	e := newEnv(t, Config{Checks: checks})
	rec := getPath(e, http.MethodGet, "/healthz")
	var body struct {
		Checks map[string]struct {
			OK    bool
			Error string
		}
	}
	must(t, json.Unmarshal(rec.Body.Bytes(), &body))
	if rec.Code != http.StatusServiceUnavailable || len(body.Checks) != 3 {
		t.Fatalf("got %d %+v", rec.Code, body.Checks)
	}
	if !body.Checks["db"].OK || !body.Checks["telegram"].OK || body.Checks["discord"].Error != "gateway closed" {
		t.Errorf("checks = %+v", body.Checks)
	}
}
