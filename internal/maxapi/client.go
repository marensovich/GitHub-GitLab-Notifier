// Package maxapi is a minimal client for the MAX messenger Bot API
// (https://dev.max.ru/docs-api). It covers only what the notifier needs:
// bot info, sending messages, long polling for updates and bot commands.
package maxapi

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
)

// DefaultBaseURL is the Bot API endpoint recommended by MAX.
// It uses a certificate issued by the Russian Trusted Root CA (Минцифры),
// which must be trusted by the host (see MAX_CA_FILE).
const DefaultBaseURL = "https://platform-api2.max.ru"

// MaxTextLength is the maximum length of a message text in characters.
const MaxTextLength = 4000

// Message formats supported by POST /messages.
const (
	FormatHTML     = "html"
	FormatMarkdown = "markdown"
)

// Update types.
const (
	UpdateMessageCreated = "message_created"
)

// Client talks to the MAX Bot API.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New creates a client. An empty baseURL means DefaultBaseURL,
// a nil httpClient means http.DefaultClient.
func New(baseURL, token string, httpClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: httpClient}
}

// User is a MAX user or bot.
type User struct {
	UserID    int64  `json:"user_id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
	IsBot     bool   `json:"is_bot"`
}

// Recipient describes where a message was sent.
type Recipient struct {
	ChatID   *int64 `json:"chat_id"`
	ChatType string `json:"chat_type"` // chat, channel or dialog
	UserID   *int64 `json:"user_id"`
}

// MessageBody holds the message content.
type MessageBody struct {
	Mid  string `json:"mid"`
	Seq  int64  `json:"seq"`
	Text string `json:"text"`
}

// Message is a MAX message.
type Message struct {
	Sender    *User       `json:"sender,omitempty"`
	Recipient Recipient   `json:"recipient"`
	Timestamp int64       `json:"timestamp"`
	Body      MessageBody `json:"body"`
}

// Update is an event received via long polling.
type Update struct {
	UpdateType string   `json:"update_type"`
	Timestamp  int64    `json:"timestamp"`
	Message    *Message `json:"message,omitempty"`
}

// UpdateList is the response of GET /updates.
type UpdateList struct {
	Updates []Update `json:"updates"`
	Marker  *int64   `json:"marker"`
}

// BotCommand is a command shown in the bot menu.
type BotCommand struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SendOptions tune POST /messages.
type SendOptions struct {
	Format             string // FormatHTML, FormatMarkdown or "" for plain text
	DisableLinkPreview bool
}

// APIError is returned for non-2xx responses.
type APIError struct {
	StatusCode int
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("max api: HTTP %d %s: %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("max api: HTTP %d", e.StatusCode)
}

// IsRateLimited reports whether err is an HTTP 429 from the API.
func IsRateLimited(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests
}

// GetMe returns information about the bot (GET /me).
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var u User
	if err := c.do(ctx, http.MethodGet, "/me", nil, nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// SendMessage sends a text message to a group chat or dialog (POST /messages?chat_id=).
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, opts SendOptions) (*Message, error) {
	q := url.Values{}
	q.Set("chat_id", strconv.FormatInt(chatID, 10))
	if opts.DisableLinkPreview {
		q.Set("disable_link_preview", "true")
	}

	body := map[string]any{"text": text}
	if opts.Format != "" {
		body["format"] = opts.Format
	}

	var resp struct {
		Message Message `json:"message"`
	}
	if err := c.do(ctx, http.MethodPost, "/messages", q, body, &resp); err != nil {
		return nil, err
	}
	return &resp.Message, nil
}

// GetUpdates long-polls for new events (GET /updates).
// A nil marker returns only the latest updates; pass the marker from the
// previous response to continue. timeout is in seconds (0–90).
func (c *Client) GetUpdates(ctx context.Context, marker *int64, timeout int, types []string) (*UpdateList, error) {
	q := url.Values{}
	q.Set("timeout", strconv.Itoa(timeout))
	if marker != nil {
		q.Set("marker", strconv.FormatInt(*marker, 10))
	}
	if len(types) > 0 {
		q.Set("types", strings.Join(types, ","))
	}

	var list UpdateList
	if err := c.do(ctx, http.MethodGet, "/updates", q, nil, &list); err != nil {
		return nil, err
	}
	return &list, nil
}

// SetCommands replaces the bot command list (PATCH /me/commands).
func (c *Client) SetCommands(ctx context.Context, commands []BotCommand) error {
	return c.do(ctx, http.MethodPatch, "/me/commands", nil, map[string]any{"commands": commands}, nil)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reader io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return err
	}
	// MAX accepts the token only in the Authorization header, without a scheme.
	req.Header.Set("Authorization", c.token)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		apiErr := &APIError{StatusCode: resp.StatusCode}
		_ = json.Unmarshal(data, apiErr)
		return apiErr
	}

	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("max api: decode %s %s: %w", method, path, err)
	}
	return nil
}
