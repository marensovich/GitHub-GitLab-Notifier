package dispatcher

import (
	"GitHub-GitLab-Notifier/internal/handler"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"errors"
	"log"
)

// Result is the outcome of dispatching a webhook event.
type Result struct {
	// Message is the formatted Telegram HTML notification.
	Message string
	// Topic is the forum topic the notification belongs to.
	Topic platform.Topic
	// Skip is true when no notification should be sent.
	Skip bool
}

// Dispatcher routes webhook events to the appropriate handler.
type Dispatcher struct {
	handlers map[platform.EventType]handler.EventHandler
	// lang is used for log messages only; notifications use the per-call language.
	lang i18n.Lang
	// notifyUnknown forwards unsupported events as a generic notification
	// instead of silently ignoring them.
	notifyUnknown bool
}

// NewDispatcher creates a Dispatcher with all built-in handlers registered.
func NewDispatcher(lang i18n.Lang, notifyUnknown bool) *Dispatcher {
	d := &Dispatcher{
		handlers:      make(map[platform.EventType]handler.EventHandler),
		lang:          lang,
		notifyUnknown: notifyUnknown,
	}

	for _, h := range handler.All() {
		d.register(h)
	}

	log.Printf(i18n.T(lang, i18n.MsgDispatcherInit), len(d.handlers))
	return d
}

// register adds a handler to the map.
func (d *Dispatcher) register(h handler.EventHandler) {
	d.handlers[h.GetEventType()] = h
}

// HandlerCount returns the number of registered handlers.
func (d *Dispatcher) HandlerCount() int {
	return len(d.handlers)
}

// Dispatch routes an event to its handler and returns the formatted
// notification in the given language.
func (d *Dispatcher) Dispatch(lang i18n.Lang, p platform.Platform, eventName string, payload []byte) Result {
	eventType := platform.FromEventName(p, eventName)

	if eventType == nil {
		log.Printf(i18n.T(d.lang, i18n.MsgUnknownEventLog), p, eventName)
		return d.unknown(lang, p, eventName)
	}

	h, ok := d.handlers[*eventType]
	if !ok {
		log.Printf(i18n.T(d.lang, i18n.MsgNoHandlerLog), *eventType)
		return d.unknown(lang, p, eventName)
	}

	message, err := h.Handle(lang, payload)
	if errors.Is(err, handler.ErrSkip) {
		return Result{Topic: eventType.Topic, Skip: true}
	}
	if err != nil {
		log.Printf(i18n.T(d.lang, i18n.MsgErrorEventLog), *eventType, err)
		return Result{Message: i18n.Tf(lang, i18n.MsgErrorEvent, eventName), Topic: eventType.Topic}
	}

	return Result{Message: message, Topic: eventType.Topic}
}

func (d *Dispatcher) unknown(lang i18n.Lang, p platform.Platform, eventName string) Result {
	if !d.notifyUnknown {
		return Result{Skip: true}
	}
	return Result{Message: i18n.Tf(lang, i18n.MsgUnknownEvent, eventName, p)}
}
