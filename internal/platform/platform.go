package platform

// Platform represents a hosting platform.
//
// Платформа хостинга.
type Platform string

const (
	GitHub Platform = "GITHUB"
	GitLab Platform = "GITLAB"
)

// EventType represents a specific webhook event.
//
// Конкретное событие вебхука.
type EventType struct {
	Name     string
	Platform Platform
}

// Supported events.
var (
	// GitHub
	GitHubPush = EventType{"push", GitHub}
)

// FromEventName finds EventType by platform and event name string.
//
// Находит EventType по платформе и названию события.
func FromEventName(p Platform, name string) *EventType {
	switch p {
	case GitHub:
		switch name {
		case "push":
			return &GitHubPush
		}
	}
	return nil
}
