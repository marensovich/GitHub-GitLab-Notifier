package max

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/notify"
)

const (
	// maxText is MAX's text limit. It is checked on the HTML sent: the docs
	// do not say whether tags count, so they are counted.
	maxText    = 4000
	maxButtons = 3 // a row with a link button holds at most 3
	maxTitle   = 200
	// compactTitle is the length of the fixed parts when nothing else
	// is left to drop: even escaped 5 times, four of them fit.
	compactTitle = 150
	// maxURL is the longest link kept; a longer one leaves its text only.
	maxURL   = 1000
	maxQuote = 500
	fitParts = 20
)

// Message is a notification as MAX gets it: HTML text and link buttons in
// one row.
type Message struct {
	Text    string          `json:"text"`
	Buttons []notify.Button `json:"buttons,omitempty"`
}

// Render renders n as MAX HTML, cut down to the text limit when needed.
// The heading, the repository, the item and the buttons are never dropped.
func Render(lang i18n.Lang, n notify.Notification) Message {
	text := render(lang, n)
	for _, step := range shrinkSteps {
		if runes(text) <= maxText {
			break
		}
		step(&n)
		text = render(lang, n)
	}
	if runes(text) > maxText {
		// Only the fixed parts are left, as short text without links:
		// cut the text, never the markup.
		n.Fields, n.Sections, n.Notes, n.Quote = nil, nil, nil, ""
		for _, l := range []*notify.Link{&n.Repo, &n.Item, &n.Actor} {
			*l = notify.Link{Text: truncate(l.Text, compactTitle)}
		}
		n.Heading = truncate(n.Heading, compactTitle)
		text = render(lang, n)
	}
	return Message{Text: text, Buttons: buttons(n.Buttons)}
}

func render(lang i18n.Lang, n notify.Notification) string {
	var sb strings.Builder
	sb.WriteString(esc(strings.TrimSpace(n.Emoji+" "+truncate(n.Heading, maxTitle))) + "\n")
	if n.Repo.Text != "" {
		sb.WriteString("<b>" + link(n.Repo) + "</b>\n")
	}
	sb.WriteString("\n")
	if n.Item.Text != "" {
		sb.WriteString("<b>" + link(n.Item) + "</b>\n")
	}
	for _, f := range n.Fields {
		if len(f.Value) > 0 {
			sb.WriteString(esc(f.Label) + ": " + parts(f.Value) + "\n")
		}
	}
	for _, note := range n.Notes {
		sb.WriteString(esc(note) + "\n")
	}
	if n.Actor.Text != "" {
		sb.WriteString("👤 " + link(n.Actor) + "\n")
	}
	if q := strings.TrimSpace(strings.ReplaceAll(n.Quote, "\r\n", "\n")); q != "" {
		sb.WriteString("\n<blockquote>" + esc(truncate(q, maxQuote)) + "</blockquote>\n")
	}
	for _, s := range n.Sections {
		if len(s.Items) == 0 && s.More == 0 {
			continue
		}
		items, more := s.Items, s.More
		if s.ClassicMax > 0 && len(items) > s.ClassicMax {
			more += len(items) - s.ClassicMax
			items = items[:s.ClassicMax]
		}
		sb.WriteString("\n<b>" + esc(s.Summary) + ":</b>\n")
		for _, it := range items {
			sb.WriteString("  " + parts(it) + "\n")
		}
		if more > 0 {
			sb.WriteString("  <i>" + esc(i18n.Tf(lang, i18n.MsgSectionMore, more)) + "</i>\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// shrinkSteps drop the least important content first; each works on a
// copy of the slices it changes.
var shrinkSteps = []func(*notify.Notification){
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
	func(n *notify.Notification) {
		fields := make([]notify.Field, len(n.Fields))
		for i, f := range n.Fields {
			if len(f.Value) > fitParts {
				f.Value = append(f.Value[:fitParts:fitParts], notify.T("…"))
			}
			fields[i] = f
		}
		n.Fields = fields
	},
}

func runes(s string) int { return len([]rune(s)) }

func esc(s string) string { return html.EscapeString(s) }

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// safeURL returns u if it is an http(s) URL, otherwise "".
func safeURL(u string) string {
	u = strings.TrimSpace(u)
	l := strings.ToLower(u)
	if len(u) <= maxURL && (strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://")) {
		return u
	}
	return ""
}

func link(l notify.Link) string {
	text := esc(truncate(l.Text, maxTitle))
	if u := safeURL(l.URL); u != "" {
		return `<a href="` + esc(u) + `">` + text + `</a>`
	}
	return text
}

func parts(ps []notify.Part) string {
	var sb strings.Builder
	for _, p := range ps {
		switch p.Kind {
		case notify.KindCode:
			sb.WriteString("<code>" + esc(truncate(p.Text, maxTitle)) + "</code>")
		case notify.KindBold:
			sb.WriteString("<b>" + esc(truncate(p.Text, maxTitle)) + "</b>")
		case notify.KindLink:
			sb.WriteString(link(notify.Link{Text: p.Text, URL: p.URL}))
		default:
			sb.WriteString(esc(truncate(p.Text, maxTitle)))
		}
	}
	return sb.String()
}

// buttons keeps at most maxButtons buttons with a label and a safe URL.
func buttons(bs []notify.Button) []notify.Button {
	var out []notify.Button
	for _, b := range bs {
		if u := safeURL(b.URL); u != "" && b.Label != "" {
			out = append(out, notify.Button{Label: truncate(b.Label, 128), URL: u})
		}
		if len(out) == maxButtons {
			break
		}
	}
	return out
}

var tagRe = regexp.MustCompile(`<(/?)([a-z0-9-]+)((?:\s+[a-z-]+(?:="[^"<>]*")?)*)\s*>`)

var allowedTags = map[string]bool{"b": true, "i": true, "a": true, "code": true, "pre": true, "blockquote": true}

// CheckHTML reports whether s only uses the tags this package produces,
// without attributes other than a link's href, balanced, with no "<" or ">"
// left in the text.
func CheckHTML(s string) error {
	var stack []string
	for _, m := range tagRe.FindAllStringSubmatch(s, -1) {
		closing, name, attrs := m[1] == "/", m[2], strings.TrimSpace(m[3])
		if !allowedTags[name] {
			return fmt.Errorf("unsupported tag <%s>", name)
		}
		if attrs != "" && !(name == "a" && strings.HasPrefix(attrs, "href=")) {
			return fmt.Errorf("unsupported attributes in <%s %s>", name, attrs)
		}
		if !closing {
			stack = append(stack, name)
			continue
		}
		if len(stack) == 0 || stack[len(stack)-1] != name {
			return fmt.Errorf("unbalanced </%s>", name)
		}
		stack = stack[:len(stack)-1]
	}
	if len(stack) > 0 {
		return fmt.Errorf("unclosed tags %v", stack)
	}
	if strings.ContainsAny(tagRe.ReplaceAllString(s, ""), "<>") {
		return fmt.Errorf("unescaped < or > in text")
	}
	return nil
}
