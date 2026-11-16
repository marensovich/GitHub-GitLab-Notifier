// Package notify describes a notification independently of the messenger and
// renders it for Telegram, either as a rich message (Bot API 10.x) or as a
// classic HTML message with an inline keyboard.
//
// Handlers fill a Notification with plain text; every piece of HTML, and so
// every bit of escaping, is produced here.
package notify

import (
	"html"
	"strings"
	"time"
)

// Style is the colour of a button.
type Style string

const (
	StyleDefault Style = ""
	StylePrimary Style = "primary"
	StyleSuccess Style = "success"
	StyleDanger  Style = "danger"
)

// PartKind is the formatting of a Part.
type PartKind int

const (
	KindText PartKind = iota
	KindCode
	KindLink
	KindBold
)

// Part is a piece of a field value or a list item.
type Part struct {
	Kind PartKind
	Text string
	URL  string // KindLink only
}

// T is plain text, C inline code, L a link and B bold text.
func T(s string) Part         { return Part{Kind: KindText, Text: s} }
func C(s string) Part         { return Part{Kind: KindCode, Text: s} }
func L(text, url string) Part { return Part{Kind: KindLink, Text: text, URL: url} }
func B(s string) Part         { return Part{Kind: KindBold, Text: s} }

// Link is a text with an optional URL.
type Link struct {
	Text string
	URL  string
}

// Field is one row of metadata: a label and its value.
type Field struct {
	Label string
	Value []Part
}

// Section is a list shown collapsed in rich messages (commits, files).
type Section struct {
	Summary string
	Items   [][]Part
	// More is the number of items that exist but are not in Items.
	More int
	// ClassicMax limits the items listed in classic messages (0 = no limit).
	ClassicMax int
}

// Button is a URL button.
type Button struct {
	Label string
	URL   string
	Style Style
}

// Notification is one event notification.
type Notification struct {
	Emoji, Heading string
	// Item is the thing the event is about ("#12 Add dark mode"); may be empty.
	Item     Link
	Repo     Link
	Fields   []Field
	Quote    string
	Sections []Section
	Notes    []string
	Buttons  []Button
	Actor    Link
	// Time is when the event happened; zero means unknown.
	Time time.Time
}

// Attempt is one send attempt of a notification.
type Attempt struct {
	Format string // FormatRich or FormatClassic
	Err    error
}

// Message formats.
const (
	FormatRich    = "rich"
	FormatClassic = "classic"
)

const (
	maxButtons     = 3
	maxTitle       = 200
	maxQuote       = 500
	maxRichItems   = 20
	timeFallbackFm = "2006-01-02 15:04 UTC"
)

// safeURL returns u if it is an http(s) URL, otherwise "".
func safeURL(u string) string {
	l := strings.ToLower(strings.TrimSpace(u))
	if strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://") {
		return strings.TrimSpace(u)
	}
	return ""
}

func esc(s string) string { return html.EscapeString(s) }

// truncate shortens s to at most max runes, appending "…" when cut.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func link(l Link, max int) string {
	text := esc(truncate(l.Text, max))
	if u := safeURL(l.URL); u != "" {
		return `<a href="` + esc(u) + `">` + text + `</a>`
	}
	return text
}

func parts(ps []Part) string {
	var sb strings.Builder
	for _, p := range ps {
		switch p.Kind {
		case KindCode:
			sb.WriteString("<code>" + esc(truncate(p.Text, maxTitle)) + "</code>")
		case KindBold:
			sb.WriteString("<b>" + esc(truncate(p.Text, maxTitle)) + "</b>")
		case KindLink:
			sb.WriteString(link(Link{Text: p.Text, URL: p.URL}, maxTitle))
		default:
			sb.WriteString(esc(truncate(p.Text, maxTitle)))
		}
	}
	return sb.String()
}

func quote(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	return esc(truncate(s, maxQuote))
}

// buttons returns at most maxButtons buttons with safe URLs.
func buttons(bs []Button) []Button {
	var out []Button
	for _, b := range bs {
		if u := safeURL(b.URL); u != "" && b.Label != "" {
			b.URL = u
			out = append(out, b)
		}
		if len(out) == maxButtons {
			break
		}
	}
	return out
}

func nonEmpty(fs []Field) []Field {
	var out []Field
	for _, f := range fs {
		if len(f.Value) > 0 {
			out = append(out, f)
		}
	}
	return out
}
