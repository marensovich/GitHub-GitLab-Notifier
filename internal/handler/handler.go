package handler

import "GitHub-GitLab-Notifier/internal/platform"

// EventHandler processes a specific webhook event.
//
// Обрабатывает конкретное событие вебхука.
type EventHandler interface {
	// GetEventType returns the event type this handler processes.
	GetEventType() platform.EventType

	// Handle parses JSON and returns formatted notification message.
	Handle(json string) (string, error)
}
