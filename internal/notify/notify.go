// Package notify describes a notification independently of the messenger.
//
// Handlers fill a Notification with plain text; each messenger adapter
// renders it, and so does all the escaping.
package notify

import "time"

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
