package maxapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendMessage(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/messages" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "secret-token" {
			t.Errorf("Authorization = %q, want raw token", got)
		}
		if got := r.URL.Query().Get("chat_id"); got != "-70001" {
			t.Errorf("chat_id = %q", got)
		}
		if got := r.URL.Query().Get("disable_link_preview"); got != "true" {
			t.Errorf("disable_link_preview = %q", got)
		}
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &gotBody)
		_, _ = w.Write([]byte(`{"message":{"recipient":{"chat_id":-70001,"chat_type":"chat"},"body":{"mid":"m1","text":"hi"}}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token", srv.Client())
	msg, err := c.SendMessage(context.Background(), -70001, "<b>hi</b>", SendOptions{Format: FormatHTML, DisableLinkPreview: true})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Body.Mid != "m1" {
		t.Errorf("mid = %q", msg.Body.Mid)
	}
	if gotBody["text"] != "<b>hi</b>" || gotBody["format"] != "html" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestGetUpdatesAndMarker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("marker") != "42" || q.Get("timeout") != "5" || q.Get("types") != "message_created" {
			t.Errorf("query = %v", q)
		}
		_, _ = w.Write([]byte(`{"updates":[{"update_type":"message_created","timestamp":1,
			"message":{"sender":{"user_id":7,"first_name":"Amir","username":"amir"},
			"recipient":{"chat_id":-5,"chat_type":"chat"},"body":{"mid":"x","text":"/status"}}}],"marker":43}`))
	}))
	defer srv.Close()

	marker := int64(42)
	list, err := New(srv.URL, "t", srv.Client()).GetUpdates(context.Background(), &marker, 5, []string{UpdateMessageCreated})
	if err != nil {
		t.Fatal(err)
	}
	if list.Marker == nil || *list.Marker != 43 {
		t.Errorf("marker = %v", list.Marker)
	}
	if len(list.Updates) != 1 || list.Updates[0].Message.Body.Text != "/status" || *list.Updates[0].Message.Recipient.ChatID != -5 {
		t.Errorf("updates = %+v", list.Updates)
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":"too.many.requests","message":"slow down"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "t", srv.Client()).GetMe(context.Background())
	if !IsRateLimited(err) {
		t.Fatalf("want rate limit error, got %v", err)
	}
	if apiErr := err.(*APIError); apiErr.Code != "too.many.requests" || apiErr.Message != "slow down" {
		t.Errorf("error = %+v", apiErr)
	}
}

func TestDefaultBaseURL(t *testing.T) {
	if c := New("", "t", nil); c.baseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q", c.baseURL)
	}
	if c := New("https://example.org/", "t", nil); c.baseURL != "https://example.org" {
		t.Errorf("trailing slash not trimmed: %q", c.baseURL)
	}
}
