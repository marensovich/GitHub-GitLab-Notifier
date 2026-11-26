package max

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"GitHub-GitLab-Notifier/internal/messenger"
)

const testToken = "t0k-SECRET-value"

func testClient(t *testing.T, h http.HandlerFunc) *client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return newClient(srv.URL, testToken)
}

func TestDoSendsTokenHeader(t *testing.T) {
	var auth, query string
	var body map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		auth, query = r.Header.Get("Authorization"), r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		if r.Method != http.MethodPost || r.URL.Path != "/messages" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"message":{"body":{"mid":"m1"}}}`)
	})
	var out struct {
		Message struct {
			Body struct{ Mid string }
		}
	}
	err := c.do(context.Background(), http.MethodPost, "/messages", url.Values{"chat_id": {"-5"}}, map[string]string{"text": "hi"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if auth != testToken || query != "chat_id=-5" || strings.Contains(query, testToken) {
		t.Errorf("auth %q query %q", auth, query)
	}
	if body["text"] != "hi" || out.Message.Body.Mid != "m1" {
		t.Errorf("body %v out %+v", body, out)
	}
}

func TestSuccessFalseIsError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"success":false,"code":"chat.denied","message":"no"}`)
	})
	err := c.do(context.Background(), http.MethodDelete, "/messages", nil, nil, nil)
	if messenger.Classify(classify(err)) != messenger.ClassChatGone {
		t.Errorf("err %v", err)
	}
	ok := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"success":true}`)
	})
	if err := ok.do(context.Background(), http.MethodDelete, "/messages", nil, nil, nil); err != nil {
		t.Errorf("success: %v", err)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		status int
		header string
		body   string
		class  messenger.Class
		wait   time.Duration
	}{
		{404, "", `{"code":"not.found","message":"Chat 5 not found"}`, messenger.ClassChatGone, 0},
		{403, "", `{"code":"chat.denied","message":"denied"}`, messenger.ClassChatGone, 0},
		{400, "", `{"code":"proto.payload","message":"bad"}`, messenger.ClassRejected, 0},
		{429, "3", `{"code":"too.many.requests","message":"slow"}`, messenger.ClassRateLimited, 3 * time.Second},
		{429, "", ``, messenger.ClassRateLimited, 0},
		{503, "", ``, messenger.ClassTemporary, 0},
		{401, "", `{"code":"verify.token","message":"Invalid access_token"}`, messenger.ClassTemporary, 0},
	}
	for _, tc := range cases {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			if tc.header != "" {
				w.Header().Set("Retry-After", tc.header)
			}
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		})
		err := classify(c.do(context.Background(), http.MethodGet, "/me", nil, nil, nil))
		if got := messenger.Classify(err); got != tc.class || messenger.RetryAfter(err) != tc.wait {
			t.Errorf("%d %s: class %v wait %v (%v)", tc.status, tc.body, got, messenger.RetryAfter(err), err)
		}
	}
	if err := classify(nil); err != nil {
		t.Errorf("nil: %v", err)
	}
}

func TestErrorHidesToken(t *testing.T) {
	c := newClient("http://127.0.0.1:1", testToken)
	err := c.do(context.Background(), http.MethodGet, "/me", url.Values{"x": {"1"}}, nil, nil)
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Errorf("err %v", err)
	}
	if _, last := c.contact(); last == nil {
		t.Error("no contact error recorded")
	}
	var ae *apiError
	if errors.As(err, &ae) {
		t.Errorf("network error is an apiError: %v", err)
	}
}
