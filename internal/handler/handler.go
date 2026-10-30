package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"errors"
)

// ErrSkip is returned by a handler when the event is valid but should not
// produce a notification (e.g. noisy PR actions or in-progress pipelines).
var ErrSkip = errors.New("event skipped")

// EventHandler processes a specific webhook event.
type EventHandler interface {
	// GetEventType returns the event type this handler processes.
	GetEventType() platform.EventType

	// Handle parses the JSON payload and returns a formatted notification
	// message (Telegram HTML) in the requested language.
	Handle(lang i18n.Lang, payload []byte) (string, error)
}

// All returns every built-in event handler.
func All() []EventHandler {
	return []EventHandler{
		// GitHub
		&GitHubPingHandler{},
		&GitHubPushHandler{},
		&GitHubStarHandler{},
		&GitHubForkHandler{},
		&GitHubPullRequestHandler{},
		&GitHubPullRequestReviewHandler{},
		&GitHubIssuesHandler{},
		&GitHubIssueCommentHandler{},
		&GitHubReleaseHandler{},
		&GitHubWorkflowRunHandler{},
	}
}
