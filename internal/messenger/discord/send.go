package discord

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/notify"
)

// FormatEmbed is the only look a Discord message has.
const FormatEmbed = "embed"

// Discord error codes the adapter tells apart.
const (
	codeUnknownChannel   = 10003
	codeMissingAccess    = 50001
	codeCannotDM         = 50007
	codeCannotSendInText = 50008
	codeMissingPerms     = 50013
	codeInvalidForm      = 50035
	codeThreadArchived   = 50083
	codeNeedsThread      = 220001
)

// Permissions the bot needs to post in a forum's threads.
const (
	permSendMessages         = 1 << 11
	permSendMessagesInThread = 1 << 38
)

// staleAfter is how long without an answer from Discord Health asks again.
const staleAfter = 3 * time.Minute

// Send posts n to the thread, or to the channel when there is none.
func (d *Discord) Send(ctx context.Context, t messenger.Target, look string, lang i18n.Lang, n notify.Notification) ([]notify.Attempt, error) {
	channel := t.ChatID
	if t.ThreadID != "" {
		channel = t.ThreadID
	}
	err := sendError(d.c.do(ctx, http.MethodPost, "/channels/"+channel+"/messages", Render(lang, n), nil), t.ThreadID != "")
	return []notify.Attempt{{Format: FormatEmbed, Err: err}}, err
}

// sendError maps a Discord error to the messenger errors the outbox acts on.
func sendError(err error, inThread bool) error {
	var ae *apiError
	if !errors.As(err, &ae) {
		return err
	}
	gone := messenger.ErrChatGone
	if inThread {
		gone = messenger.ErrThreadNotFound
	}
	switch {
	case ae.Status == http.StatusTooManyRequests:
		return &messenger.RateLimited{RetryAfter: ae.RetryAfter, Err: err}
	case ae.Code == codeUnknownChannel, ae.Code == codeMissingAccess, ae.Code == codeMissingPerms, ae.Code == codeCannotDM:
		return fmt.Errorf("%w: %v", gone, err)
	case ae.Code == codeThreadArchived:
		return fmt.Errorf("%w: %v", messenger.ErrThreadNotFound, err)
	case !inThread && (ae.Code == codeNeedsThread || ae.Code == codeCannotSendInText):
		return fmt.Errorf("%w: %v", messenger.ErrNeedsThread, err)
	case ae.Code == codeInvalidForm, ae.Status == http.StatusBadRequest:
		return fmt.Errorf("%w: %v", messenger.ErrRejected, err)
	}
	return err
}

// CreateThread opens a post in a forum channel and returns its ID.
func (d *Discord) CreateThread(ctx context.Context, chatID, name string) (string, error) {
	body := map[string]any{
		"name":                  cut(name, 100),
		"auto_archive_duration": 10080,
		"message": map[string]any{
			"content":          esc(name),
			"allowed_mentions": Mentions{Parse: []string{}},
		},
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := d.c.do(ctx, http.MethodPost, "/channels/"+chatID+"/threads", body, &out); err != nil {
		return "", sendError(err, false)
	}
	return out.ID, nil
}

// CanManageThreads reports whether the bot may post in the channel's
// threads, as of the last interaction there.
func (d *Discord) CanManageThreads(ctx context.Context, chatID string) (bool, error) {
	d.mu.Lock()
	p, ok := d.appPerms[chatID]
	d.mu.Unlock()
	const need = permSendMessages | permSendMessagesInThread
	return ok && p&need == need, nil
}

// Health reports whether Discord answers. After staleAfter without an
// answer it asks once (GET /users/@me).
func (d *Discord) Health(ctx context.Context) error {
	if err := health(d.c.contact()); err == nil {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = d.c.do(callCtx, http.MethodGet, "/users/@me", nil, nil)
	return health(d.c.contact())
}

func health(lastOK time.Time, lastErr error) error {
	if time.Since(lastOK) < staleAfter {
		return nil
	}
	if lastErr != nil {
		return fmt.Errorf("no answer from Discord since %s: %w", lastOK.UTC().Format(time.RFC3339), lastErr)
	}
	return errors.New("no answer from Discord yet")
}

// shown is a request path fit for an error text: an interaction token is
// left out.
func shown(path string) string {
	if rest, ok := strings.CutPrefix(path, "/webhooks/"); ok {
		if app, _, ok := strings.Cut(rest, "/"); ok {
			return "/webhooks/" + app + "/…"
		}
	}
	return path
}
