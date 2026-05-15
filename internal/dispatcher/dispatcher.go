package dispatcher

import (
	"GitHub-GitLab-Notifier/internal/handler"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"log"
)

// Dispatcher routes webhook events to the appropriate handler.
type Dispatcher struct {
	handlers map[platform.EventType]handler.EventHandler
	lang     i18n.Lang
}

// NewDispatcher creates a Dispatcher with all registered handlers.
func NewDispatcher(lang i18n.Lang) *Dispatcher {
	d := &Dispatcher{
		handlers: make(map[platform.EventType]handler.EventHandler),
		lang:     lang,
	}

	d.register(&handler.GitHubPushHandler{Lang: lang})
	d.register(&handler.GitHubStarHandler{Lang: lang})

	log.Printf(i18n.T(lang, i18n.MsgDispatcherInit), len(d.handlers))
	return d
}

// register adds a handler to the map.
func (d *Dispatcher) register(h handler.EventHandler) {
	d.handlers[h.GetEventType()] = h
}

// Dispatch routes an event and returns the formatted message.
func (d *Dispatcher) Dispatch(p platform.Platform, eventName string, json string) string {
	eventType := platform.FromEventName(p, eventName)

	if eventType == nil {
		log.Printf(i18n.T(d.lang, i18n.MsgUnknownEventLog), p, eventName)
		return i18n.Tf(d.lang, i18n.MsgUnknownEvent, eventName, p)
	}

	h, ok := d.handlers[*eventType]
	if !ok {
		log.Printf(i18n.T(d.lang, i18n.MsgNoHandlerLog), eventType)
		return i18n.Tf(d.lang, i18n.MsgUnknownEvent, eventName, p)
	}

	message, err := h.Handle(json)
	if err != nil {
		log.Printf(i18n.T(d.lang, i18n.MsgErrorEventLog), eventType, err)
		return i18n.Tf(d.lang, i18n.MsgErrorEvent, eventName)
	}

	return message
}
