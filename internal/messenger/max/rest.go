// Package max is the MAX messenger: the MAX Bot API over plain HTTP for
// sending, and a webhook or long polling for commands and button presses.
package max

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"GitHub-GitLab-Notifier/internal/messenger"
)

// APIBase is the MAX Bot API. The docs name platform-api2.max.ru, whose
// certificate comes from the Russian Trusted CA that Go does not trust by
// default; this host serves the same API with a public CA.
const APIBase = "https://platform-api.max.ru"

// client calls the MAX Bot API. It has no timeout of its own: callers set
// deadlines through the context.
type client struct {
	base, token string
	http        *http.Client

	mu      sync.Mutex
	lastOK  time.Time // last answer from MAX
	lastErr error     // why the last call got no answer, if it did not
}

func newClient(base, token string) *client {
	return &client{base: strings.TrimRight(base, "/"), token: token, http: &http.Client{}}
}

func (c *client) markContact(at time.Time, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.lastErr = err
		return
	}
	c.lastOK, c.lastErr = at, nil
}

func (c *client) contact() (time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastOK, c.lastErr
}

// apiError is a MAX error answer: an HTTP error, or 200 with
// "success": false.
type apiError struct {
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e *apiError) Error() string {
	s := fmt.Sprintf("max: HTTP %d", e.Status)
	if e.Code != "" {
		s += " " + e.Code
	}
	if e.Message != "" {
		s += ": " + e.Message
	}
	return s
}

// do sends body as JSON (when not nil) and decodes the answer into out
// (when not nil). The token goes in the Authorization header only.
func (c *client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// A url.Error repeats the URL; keep only the cause.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		err = fmt.Errorf("max: %s %s: %w", method, path, err)
		c.markContact(time.Time{}, err)
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("max: %s %s: %w", method, path, err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode >= 500 {
		c.markContact(time.Time{}, fmt.Errorf("max: HTTP %d", resp.StatusCode))
	} else {
		c.markContact(time.Now(), nil)
	}
	var e struct {
		Success *bool  `json:"success"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)
	if resp.StatusCode >= 300 {
		ae := &apiError{Status: resp.StatusCode, Code: e.Code, Message: e.Message}
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
			ae.RetryAfter = time.Duration(s) * time.Second
		}
		return ae
	}
	if e.Success != nil && !*e.Success {
		return &apiError{Status: resp.StatusCode, Code: e.Code, Message: e.Message}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// classify maps a MAX error to the messenger errors the outbox acts on.
func classify(err error) error {
	var ae *apiError
	if !errors.As(err, &ae) {
		return err
	}
	switch {
	case ae.Status == http.StatusTooManyRequests:
		return &messenger.RateLimited{RetryAfter: ae.RetryAfter, Err: err}
	case ae.Status == http.StatusUnauthorized:
		return err
	case ae.Status == http.StatusNotFound, ae.Status == http.StatusForbidden,
		ae.Code == "not.found", ae.Code == "chat.not.found", ae.Code == "chat.denied", ae.Code == "access.denied":
		return fmt.Errorf("%w: %v", messenger.ErrChatGone, err)
	case ae.Status == http.StatusBadRequest, strings.HasPrefix(ae.Code, "proto."):
		return fmt.Errorf("%w: %v", messenger.ErrRejected, err)
	}
	return err
}
