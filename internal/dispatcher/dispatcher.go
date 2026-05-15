package dispatcher

import (
	"GitHub-GitLab-Notifier/internal/handler"
	"GitHub-GitLab-Notifier/internal/platform"
	"fmt"
	"log"
)

// Dispatcher routes webhook events to the appropriate handler.
//
// Распределяет вебхук-события по соответствующим обработчикам.
type Dispatcher struct {
	handlers map[platform.EventType]handler.EventHandler
}

// NewDispatcher creates a Dispatcher with all registered handlers.
//
// Создаёт Dispatcher со всеми зарегистрированными обработчиками.
func NewDispatcher() *Dispatcher {
	d := &Dispatcher{
		handlers: make(map[platform.EventType]handler.EventHandler),
	}

	d.register(&handler.GitHubPushHandler{})

	log.Printf("✅ WebhookDispatcher initialized with %d handlers", len(d.handlers))
	return d
}

// register adds a handler to the map.
func (d *Dispatcher) register(h handler.EventHandler) {
	d.handlers[h.GetEventType()] = h
}

// Dispatch routes an event and returns the formatted message.
//
// Направляет событие и возвращает отформатированное сообщение.
func (d *Dispatcher) Dispatch(p platform.Platform, eventName string, json string) string {
	eventType := platform.FromEventName(p, eventName)

	if eventType == nil {
		log.Printf("⚠️ Unknown event: %s / %s", p, eventName)
		return fmt.Sprintf("📢 Новое событие <code>%s</code> от %s", eventName, p)
	}

	h, ok := d.handlers[*eventType]
	if !ok {
		log.Printf("⚠️ No handler for: %v", eventType)
		return fmt.Sprintf("📢 Новое событие <code>%s</code> от %s", eventName, p)
	}

	message, err := h.Handle(json)
	if err != nil {
		log.Printf("❌ Error handling %v: %v", eventType, err)
		return fmt.Sprintf("❌ Ошибка обработки события <code>%s</code>", eventName)
	}

	return message
}
