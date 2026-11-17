package telegram

import (
	"fmt"
	"regexp"
	"strings"
)

var tagRe = regexp.MustCompile(`<(/?)([a-z0-9-]+)((?:\s+[a-z-]+(?:="[^"<>]*")?)*)\s*(/?)>`)

var richTags = map[string]bool{
	"h4": true, "p": true, "b": true, "i": true, "code": true, "a": true, "table": true, "tr": true, "td": true,
	"blockquote": true, "details": true, "summary": true, "ul": true, "li": true, "footer": true,
	"tg-button": true, "tg-time": true, "br": true,
}

var classicTags = map[string]bool{"b": true, "i": true, "a": true, "code": true, "pre": true, "blockquote": true}

var voidTags = map[string]bool{"br": true}

// CheckRichHTML reports whether html only uses the rich tags this package
// produces, with balanced nesting and no unescaped "<" or ">" in text.
func CheckRichHTML(html string) error { return checkHTML(html, richTags) }

// CheckClassicHTML is CheckRichHTML for classic (parse mode HTML) messages.
func CheckClassicHTML(html string) error { return checkHTML(html, classicTags) }

func checkHTML(html string, allowed map[string]bool) error {
	var stack []string
	for _, m := range tagRe.FindAllStringSubmatch(html, -1) {
		closing, name, selfClosing := m[1] == "/", m[2], m[4] == "/"
		if !allowed[name] {
			return fmt.Errorf("unsupported tag <%s>", name)
		}
		if voidTags[name] || selfClosing {
			continue
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
	if strings.ContainsAny(tagRe.ReplaceAllString(html, ""), "<>") {
		return fmt.Errorf("unescaped < or > in text")
	}
	return nil
}
