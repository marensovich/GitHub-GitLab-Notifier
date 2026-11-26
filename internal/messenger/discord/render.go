package discord

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/notify"
)

// Message is a Discord message body as the REST API takes it.
type Message struct {
	Content         string   `json:"content,omitempty"`
	Embeds          []Embed  `json:"embeds,omitempty"`
	Components      []Row    `json:"components,omitempty"`
	AllowedMentions Mentions `json:"allowed_mentions"`
	Flags           int      `json:"flags,omitempty"`
}

// Mentions says which mentions in a message ping anyone. Parse is always
// sent, so an empty list pings nobody.
type Mentions struct {
	Parse []string `json:"parse"`
}

// Embed is a Discord embed.
type Embed struct {
	Title       string       `json:"title,omitempty"`
	URL         string       `json:"url,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color"`
	Timestamp   string       `json:"timestamp,omitempty"`
	Author      *EmbedAuthor `json:"author,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
}

// EmbedAuthor is the line above an embed's title.
type EmbedAuthor struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// EmbedField is a name/value pair in an embed.
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// EmbedFooter is the line under an embed.
type EmbedFooter struct {
	Text string `json:"text"`
}

// Row is an action row (component type 1).
type Row struct {
	Type       int      `json:"type"`
	Components []Button `json:"components"`
}

// Button is a button (component type 2): a link (style 5) or one that sends
// an interaction (CustomID).
type Button struct {
	Type     int    `json:"type"`
	Style    int    `json:"style"`
	Label    string `json:"label"`
	URL      string `json:"url,omitempty"`
	CustomID string `json:"custom_id,omitempty"`
}

// Discord's limits.
const (
	maxTitle       = 256
	maxDescription = 4096
	maxFields      = 25
	maxFieldName   = 256
	maxFieldValue  = 1024
	maxFooter      = 2048
	maxAuthor      = 256
	maxEmbedTotal  = 6000
	maxContent     = 2000
	maxLabel       = 80
	maxRowButtons  = 5
	maxRows        = 5
	maxItemLen     = 300
	inlineField    = 40
)

// Embed colours.
const (
	colorGood    = 0x2EA043
	colorBad     = 0xCF222E
	colorWarn    = 0xD29922
	colorDefault = 0x5865F2
)

func runes(s string) int { return utf8.RuneCountInString(s) }

// cut shortens s to at most max runes, ending it with "…" when cut.
func cut(s string, max int) string {
	if runes(s) <= max {
		return s
	}
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}

var escaper = strings.NewReplacer(
	`\`, `\\`, `*`, `\*`, `_`, `\_`, `~`, `\~`, "`", "\\`", `|`, `\|`,
	`>`, `\>`, `[`, `\[`, `]`, `\]`, `(`, `\(`, `)`, `\)`,
)

// esc makes text show as typed in Discord markdown.
func esc(s string) string { return escaper.Replace(s) }

// lineStartRe finds markdown that only means something at the start of a
// line: headings, subtext and lists.
var lineStartRe = regexp.MustCompile(`^(\s*)(#{1,3}|-#|[-+]|[0-9]+\.)(\s)`)

// escLineStart escapes the markdown a line starts with, if any.
func escLineStart(l string) string {
	m := lineStartRe.FindStringSubmatchIndex(l)
	if m == nil {
		return l
	}
	marker := l[m[4]:m[5]]
	if strings.HasSuffix(marker, ".") {
		marker = strings.TrimSuffix(marker, ".") + `\.`
	} else {
		marker = `\` + marker
	}
	return l[:m[4]] + marker + l[m[5]:]
}

// safeURL returns u if it is an http(s) URL, otherwise "". Characters that
// would end a markdown link are percent-encoded.
func safeURL(u string) string {
	u = strings.TrimSpace(u)
	l := strings.ToLower(u)
	if !strings.HasPrefix(l, "https://") && !strings.HasPrefix(l, "http://") {
		return ""
	}
	return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29").Replace(u)
}

func part(p notify.Part) string {
	switch p.Kind {
	case notify.KindCode:
		if p.Text == "" {
			return ""
		}
		return "`" + strings.ReplaceAll(p.Text, "`", "ˋ") + "`"
	case notify.KindBold:
		return "**" + esc(p.Text) + "**"
	case notify.KindLink:
		if u := safeURL(p.URL); u != "" {
			return "[" + esc(p.Text) + "](" + u + ")"
		}
	}
	return esc(p.Text)
}

// cutParts renders ps in at most max runes. A part that does not fit is
// shortened (a link becomes plain text) and "…" ends the result.
func cutParts(ps []notify.Part, max int) string {
	var sb strings.Builder
	for _, p := range ps {
		s := part(p)
		if runes(sb.String())+runes(s) <= max {
			sb.WriteString(s)
			continue
		}
		room := max - 1 - runes(sb.String())
		if p.Kind == notify.KindLink {
			p.Kind = notify.KindText
		}
		r := []rune(p.Text)
		for n := min(len(r), room); n > 0; n-- {
			p.Text = string(r[:n])
			if s := part(p); runes(s) <= room {
				sb.WriteString(s)
				break
			}
		}
		sb.WriteString("…")
		break
	}
	return sb.String()
}

func color(n notify.Notification) int {
	h := strings.TrimSpace(n.Emoji + " " + n.Heading)
	for prefix, c := range map[string]int{
		"✅": colorGood, "🟢": colorGood, "🟣": colorGood,
		"❌": colorBad, "🔴": colorBad, "🚫": colorBad, "💔": colorBad,
		"⚠": colorWarn,
	} {
		if strings.HasPrefix(h, prefix) {
			return c
		}
	}
	return colorDefault
}

// budget is how much of a notification goes into the embed.
type budget struct {
	items    int // section items shown per section, -1 for all
	quote    int // runes of the quote, -1 for all
	notes    bool
	fieldMax int // runes of a field value
}

// Render renders n as a Discord message: one embed and a row of link
// buttons. It is shrunk to Discord's limits when needed.
func Render(lang i18n.Lang, n notify.Notification) Message {
	b := budget{items: -1, quote: -1, notes: true, fieldMax: maxFieldValue}
	m := build(lang, n, b)
	if CheckLimits(m) == nil {
		return m
	}
	most := 0
	for _, s := range n.Sections {
		most = max(most, len(s.Items))
	}
	for b.items = most; b.items > 0; {
		if b.items > 8 {
			b.items = b.items * 3 / 4
		} else {
			b.items--
		}
		if m = build(lang, n, b); CheckLimits(m) == nil {
			return m
		}
	}
	for b.quote = runes(n.Quote); b.quote > 0; {
		b.quote /= 2
		if m = build(lang, n, b); CheckLimits(m) == nil {
			return m
		}
	}
	b.notes = false
	if m = build(lang, n, b); CheckLimits(m) == nil {
		return m
	}
	for b.fieldMax > 20 {
		b.fieldMax = b.fieldMax * 3 / 4
		if m = build(lang, n, b); CheckLimits(m) == nil {
			return m
		}
	}
	m.Embeds[0].Description = cut(m.Embeds[0].Description, maxDescription/2)
	return m
}

func build(lang i18n.Lang, n notify.Notification, b budget) Message {
	e := Embed{Color: color(n), URL: safeURL(n.Item.URL)}

	head := strings.TrimSpace(n.Emoji + " " + n.Heading)
	e.Title = head
	if n.Item.Text != "" {
		sep := " · "
		if head == "" {
			sep = ""
		}
		e.Title = head + sep + cut(esc(n.Item.Text), maxTitle-runes(head)-runes(sep))
	}
	e.Title = cut(e.Title, maxTitle)

	if n.Repo.Text != "" {
		e.Author = &EmbedAuthor{Name: cut(n.Repo.Text, maxAuthor), URL: safeURL(n.Repo.URL)}
	}
	if n.Actor.Text != "" {
		e.Footer = &EmbedFooter{Text: cut(n.Actor.Text, maxFooter)}
	}
	if !n.Time.IsZero() {
		e.Timestamp = n.Time.UTC().Format(time.RFC3339)
	}

	for _, f := range n.Fields {
		if len(e.Fields) == maxFields {
			break
		}
		v := cutParts(f.Value, b.fieldMax)
		if v == "" {
			continue
		}
		name := cut(f.Label, maxFieldName)
		if name == "" {
			name = "​"
		}
		e.Fields = append(e.Fields, EmbedField{Name: name, Value: v, Inline: runes(v) <= inlineField})
	}

	var blocks []string
	if q := strings.TrimSpace(strings.ReplaceAll(n.Quote, "\r\n", "\n")); q != "" && b.quote != 0 {
		if b.quote > 0 {
			q = cut(q, b.quote)
		}
		lines := strings.Split(esc(q), "\n")
		for i, l := range lines {
			lines[i] = "> " + escLineStart(l)
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	for _, s := range n.Sections {
		items, more := s.Items, s.More
		if b.items >= 0 && len(items) > b.items {
			more += len(items) - b.items
			items = items[:b.items]
		}
		if len(items) == 0 && more == 0 {
			continue
		}
		lines := []string{"**" + esc(s.Summary) + "**"}
		for _, it := range items {
			lines = append(lines, "• "+cutParts(it, maxItemLen))
		}
		if more > 0 {
			lines = append(lines, "• *"+esc(i18n.Tf(lang, i18n.MsgSectionMore, more))+"*")
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	if b.notes && len(n.Notes) > 0 {
		var lines []string
		for _, note := range n.Notes {
			lines = append(lines, "*"+esc(note)+"*")
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	e.Description = strings.Join(blocks, "\n\n")

	m := Message{Embeds: []Embed{e}, AllowedMentions: Mentions{Parse: []string{}}}
	var row []Button
	for _, bt := range n.Buttons {
		if u := safeURL(bt.URL); u != "" && bt.Label != "" && len(row) < maxRowButtons {
			row = append(row, Button{Type: 2, Style: 5, Label: cut(bt.Label, maxLabel), URL: u})
		}
	}
	if len(row) > 0 {
		m.Components = []Row{{Type: 1, Components: row}}
	}
	return m
}

// CheckLimits reports the first Discord limit m breaks.
func CheckLimits(m Message) error {
	if runes(m.Content) > maxContent {
		return fmt.Errorf("content is %d characters, the limit is %d", runes(m.Content), maxContent)
	}
	if len(m.Embeds) > 10 {
		return fmt.Errorf("%d embeds, the limit is 10", len(m.Embeds))
	}
	total := 0
	for _, e := range m.Embeds {
		type text struct {
			what, s string
			limit   int
		}
		check := []text{{"title", e.Title, maxTitle}, {"description", e.Description, maxDescription}}
		if e.Author != nil {
			check = append(check, text{"author", e.Author.Name, maxAuthor})
		}
		if e.Footer != nil {
			check = append(check, text{"footer", e.Footer.Text, maxFooter})
		}
		if len(e.Fields) > maxFields {
			return fmt.Errorf("%d fields, the limit is %d", len(e.Fields), maxFields)
		}
		for _, f := range e.Fields {
			check = append(check, text{"field name", f.Name, maxFieldName}, text{"field value", f.Value, maxFieldValue})
		}
		for _, c := range check {
			n := runes(c.s)
			if n > c.limit {
				return fmt.Errorf("%s is %d characters, the limit is %d", c.what, n, c.limit)
			}
			total += n
		}
	}
	if total > maxEmbedTotal {
		return fmt.Errorf("embeds have %d characters, the limit is %d", total, maxEmbedTotal)
	}
	if len(m.Components) > maxRows {
		return fmt.Errorf("%d rows, the limit is %d", len(m.Components), maxRows)
	}
	for _, r := range m.Components {
		if len(r.Components) > maxRowButtons {
			return fmt.Errorf("%d buttons in a row, the limit is %d", len(r.Components), maxRowButtons)
		}
		for _, b := range r.Components {
			if runes(b.Label) > maxLabel {
				return fmt.Errorf("button label is %d characters, the limit is %d", runes(b.Label), maxLabel)
			}
		}
	}
	return nil
}
