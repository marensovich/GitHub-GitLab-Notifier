// Package discord is the Discord messenger: REST v10 for sending, and an
// interactions endpoint for slash commands and button presses.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// APIBase is Discord's REST API.
const APIBase = "https://discord.com/api/v10"

// client calls the Discord REST API. It has no timeout of its own: callers
// set deadlines through the context.
type client struct {
	base, token, userAgent string
	http                   *http.Client
}

func newClient(base, token, version string) *client {
	return &client{
		base:      base,
		token:     token,
		userAgent: "DiscordBot (https://github.com/marensovich/GitHub-GitLab-Notifier, " + version + ")",
		http:      &http.Client{},
	}
}

// apiError is a Discord error response.
type apiError struct {
	Status     int
	Code       int
	Message    string
	RetryAfter time.Duration
}

func (e *apiError) Error() string {
	s := fmt.Sprintf("discord: HTTP %d", e.Status)
	if e.Code != 0 {
		s += fmt.Sprintf(" code %d", e.Code)
	}
	if e.Message != "" {
		s += ": " + e.Message
	}
	return s
}

// do sends body as JSON (when not nil) and decodes the answer into out
// (when not nil).
func (c *client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+c.token)
	req.Header.Set("User-Agent", c.userAgent)
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
		return fmt.Errorf("discord: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("discord: %s %s: %w", method, path, err)
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Code       int     `json:"code"`
			Message    string  `json:"message"`
			RetryAfter float64 `json:"retry_after"`
		}
		_ = json.Unmarshal(raw, &e)
		return &apiError{
			Status:     resp.StatusCode,
			Code:       e.Code,
			Message:    e.Message,
			RetryAfter: time.Duration(e.RetryAfter * float64(time.Second)),
		}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}
