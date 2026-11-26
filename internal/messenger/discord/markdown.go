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
	return cutMarkdown(sb.String(), maxContent)
}

// cutMarkdown cuts markdown to max runes like cut, but never leaves a link
// half done or a style open.
func cutMarkdown(s string, max int) string {
	if runes(s) <= max {
		return s
	}
	r := []rune(s)
	for n := max - 1; n > 0; n-- {
		out := string(r[:n])
		// An unfinished link would show its markup: drop it.
		if i := strings.LastIndex(out, "["); i >= 0 && (i == 0 || out[i-1] != '\\') && !strings.Contains(out[i:], ")") {
			out = out[:i]
			n = runes(out)
		}
		out += "…" + closers(out)
		if runes(out) <= max {
			return out
		}
	}
	return "…"
}

// mdMarkers are the markdown markers that come in pairs, longest first.
var mdMarkers = []string{"```", "**", "__", "~~", "`", "*"}

// closers returns what closes the markers left open in s, inner first.
func closers(s string) string {
	var open []string
	code := ""
	for i := 0; i < len(s); {
		if s[i] == '\\' && code == "" {
			i += 2
			continue
		}
		matched := ""
		for _, m := range mdMarkers {
			if strings.HasPrefix(s[i:], m) {
				matched = m
				break
			}
		}
		switch {
		case matched == "":
			i++
			continue
		case code != "":
			// Inside code only its own marker counts.
			if matched == code {
				code = ""
				open = open[:len(open)-1]
			}
		case matched == "`" || matched == "```":
			code = matched
			open = append(open, matched)
		case len(open) > 0 && open[len(open)-1] == matched:
			open = open[:len(open)-1]
		default:
			open = append(open, matched)
		}
		i += len(matched)
	}
	var sb strings.Builder
	for i := len(open) - 1; i >= 0; i-- {
		if open[i] == "```" {
			sb.WriteString("\n")
		}
		sb.WriteString(open[i])
	}
	return sb.String()
}

func adjust(depth int, closing bool) int {
	if closing {
		return max(depth-1, 0)
	}
	return depth + 1
}
