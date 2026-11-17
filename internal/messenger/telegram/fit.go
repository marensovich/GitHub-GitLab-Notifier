package telegram

import (
	"GitHub-GitLab-Notifier/internal/notify"
	"html"
	"unicode/utf16"
)

const (
	// classicMaxText is Telegram's limit for a message's text after the HTML
	// is parsed, in UTF-16 code units.
	classicMaxText = 4096
	// richMaxHTML is the rich message limit; it is checked on the HTML we
	// send, so escaping can never push a message over it.
	richMaxHTML = 32768
	// fitFieldParts is how many parts a field keeps when a message is cut.
	fitFieldParts = 20
)

// VisibleLen is the length of a classic HTML message as Telegram counts it:
// the text without tags and entities, in UTF-16 code units.
func VisibleLen(s string) int {
	text := html.UnescapeString(tagRe.ReplaceAllString(s, ""))
	return len(utf16.Encode([]rune(text)))
}

// shrinkSteps drop the least important content first. Each step works on a
// copy, so the caller's notification is never changed.
var shrinkSteps = []func(*notify.Notification){
	// Listed items become a count ("…and N more").
	func(n *notify.Notification) {
		sections := make([]notify.Section, len(n.Sections))
		for i, s := range n.Sections {
			s.More += len(s.Items)
			s.Items = nil
			sections[i] = s
		}
		n.Sections = sections
	},
	func(n *notify.Notification) { n.Quote = "" },
	func(n *notify.Notification) { n.Notes = nil },
	// Long field values keep their first parts.
	func(n *notify.Notification) {
		fields := make([]notify.Field, len(n.Fields))
		for i, f := range n.Fields {
			if len(f.Value) > fitFieldParts {
				f.Value = append(f.Value[:fitFieldParts:fitFieldParts], notify.T("…"))
			}
			fields[i] = f
		}
		n.Fields = fields
	},
}

// fit renders n and, while the result is over limit, renders it again with
// less content. The heading, the item, the repository and the buttons are
// never dropped.
func fit(n notify.Notification, render func(notify.Notification) string, size func(string) int, limit int) string {
	out := render(n)
	for _, step := range shrinkSteps {
		if size(out) <= limit {
			break
		}
		step(&n)
		out = render(n)
	}
	return out
}

func runeLen(s string) int { return len([]rune(s)) }
