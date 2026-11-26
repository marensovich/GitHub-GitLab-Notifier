package personal

import (
	"regexp"
	"strings"
)

var (
	fenced = regexp.MustCompile("(?s)```.*?(```|$)")
	inline = regexp.MustCompile("`[^`\n]*`")
	// A mention is @name not glued to a word, an e-mail or a path; a name
	// followed by "/" is a team (@org/team) and does not count.
	mention = regexp.MustCompile(`(?:^|[^\w@/.\-])@([A-Za-z0-9](?:[\w.\-]*[A-Za-z0-9_])?)(/?)`)
)

// Mentions returns the logins mentioned in a Markdown text, lower-case,
// each once, in order. Code is ignored.
func Mentions(text string) []string {
	text = fenced.ReplaceAllString(text, " ")
	text = inline.ReplaceAllString(text, " ")
	var out []string
	seen := map[string]bool{}
	for _, m := range mention.FindAllStringSubmatch(text, -1) {
		if m[2] == "/" {
			continue
		}
		login := strings.ToLower(m[1])
		if !seen[login] {
			seen[login] = true
			out = append(out, login)
		}
	}
	return out
}
