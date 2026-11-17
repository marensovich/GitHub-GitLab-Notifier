package telegram

import (
	"html"
	"strings"

	"GitHub-GitLab-Notifier/internal/notify"
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

func link(l notify.Link, max int) string {
	text := esc(truncate(l.Text, max))
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
			sb.WriteString(link(notify.Link{Text: p.Text, URL: p.URL}, maxTitle))
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
func buttons(bs []notify.Button) []notify.Button {
	var out []notify.Button
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

func nonEmpty(fs []notify.Field) []notify.Field {
	var out []notify.Field
	for _, f := range fs {
		if len(f.Value) > 0 {
			out = append(out, f)
		}
	}
	return out
}
