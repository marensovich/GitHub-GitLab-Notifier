package discord

import (
	"html"
	"regexp"
	"strings"
)

var (
	tagRe  = regexp.MustCompile(`<(/?)([a-zA-Z]+)([^>]*)>`)
	hrefRe = regexp.MustCompile(`href\s*=\s*"([^"]*)"`)
)

// toMarkdown converts the limited HTML of the locales (b, i, code, pre, a)
// to Discord markdown. Other tags are dropped; the result is cut to
// Discord's message length.
func toMarkdown(htmlText string) string {
	var sb strings.Builder
	var links []string // open <a> tags: their URL, "" when unsafe
	code := 0          // inside <code> or <pre>

	text := func(s string) {
		s = html.UnescapeString(s)
		if code > 0 {
			sb.WriteString(strings.ReplaceAll(s, "`", "ˋ"))
			return
		}
		sb.WriteString(esc(s))
	}
	rest := htmlText
	for {
		loc := tagRe.FindStringSubmatchIndex(rest)
		if loc == nil {
			text(rest)
			break
		}
		text(rest[:loc[0]])
		closing := loc[3] > loc[2]
		name := strings.ToLower(rest[loc[4]:loc[5]])
		attrs := rest[loc[6]:loc[7]]
		rest = rest[loc[1]:]

		switch name {
		case "b", "strong":
			sb.WriteString("**")
		case "i", "em":
			sb.WriteString("*")
		case "u":
			sb.WriteString("__")
		case "s":
			sb.WriteString("~~")
		case "code":
			sb.WriteString("`")
			code = adjust(code, closing)
		case "pre":
			if closing {
				sb.WriteString("\n```")
			} else {
				sb.WriteString("```\n")
			}
			code = adjust(code, closing)
		case "br":
			sb.WriteString("\n")
		case "a":
			if !closing {
				u := ""
				if m := hrefRe.FindStringSubmatch(attrs); m != nil {
					u = safeURL(html.UnescapeString(m[1]))
				}
				links = append(links, u)
				if u != "" {
					sb.WriteString("[")
				}
			} else if len(links) > 0 {
				u := links[len(links)-1]
				links = links[:len(links)-1]
				if u != "" {
					sb.WriteString("](" + u + ")")
				}
			}
		}
	}
	return cut(sb.String(), maxContent)
}

func adjust(depth int, closing bool) int {
	if closing {
		return max(depth-1, 0)
	}
	return depth + 1
}
