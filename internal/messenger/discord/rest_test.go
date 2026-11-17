package discord

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testToken = "t0k.SECRET.value"

func testClient(t *testing.T, h http.HandlerFunc) *client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return newClient(srv.URL, testToken, "test")
}

func TestDoSendsAuthAndJSON(t *testing.T) {
	var auth, agent, ctype string
	var body map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		auth, agent, ctype = r.Header.Get("Authorization"), r.Header.Get("User-Agent"), r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		if r.Method != http.MethodPost || r.URL.Path != "/channels/1/messages" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"99"}`)
	})
	var out struct{ ID string }
	if err := c.do(context.Background(), http.MethodPost, "/channels/1/messages", map[string]string{"content": "hi"}, &out); err != nil {
		t.Fatal(err)
	}
	if auth != "Bot "+testToken || !strings.HasPrefix(agent, "DiscordBot (") || !strings.HasPrefix(ctype, "application/json") {
		t.Errorf("headers: auth %q agent %q type %q", auth, agent, ctype)
	}
	if body["content"] != "hi" || out.ID != "99" {
		t.Errorf("body %v, out %+v", body, out)
	}
}

func TestDoErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   apiError
	}{
		{429, `{"message":"You are being rate limited.","retry_after":1.5,"global":false}`, apiError{Status: 429, RetryAfter: 1500 * time.Millisecond, Message: "You are being rate limited."}},
		{404, `{"message":"Unknown Channel","code":10003}`, apiError{Status: 404, Code: 10003, Message: "Unknown Channel"}},
		{500, `oops`, apiError{Status: 500}},
	}
	for _, tc := range cases {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		})
		err := c.do(context.Background(), http.MethodGet, "/users/@me", nil, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != tc.want.Status || ae.Code != tc.want.Code || ae.RetryAfter != tc.want.RetryAfter || ae.Message != tc.want.Message {
			t.Errorf("%d: got %#v (%v), want %+v", tc.status, ae, err, tc.want)
		}
	}
}

func TestDoNetworkErrorHidesToken(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	err := newClient(srv.URL, testToken, "test").do(context.Background(), http.MethodGet, "/users/@me", nil, nil)
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Errorf("err = %v", err)
	}
}
