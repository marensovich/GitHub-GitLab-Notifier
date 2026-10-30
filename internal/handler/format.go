package handler

import (
	"fmt"
	"html"
	"strings"
)

// esc escapes text for Telegram HTML parse mode.
func esc(s string) string {
	return html.EscapeString(s)
}

// link builds an HTML link with escaped URL and text.
// Falls back to plain escaped text when the URL is empty.
func link(url, text string) string {
	if url == "" {
		return esc(text)
	}
	return fmt.Sprintf(`<a href="%s">%s</a>`, esc(url), esc(text))
}

// truncate shortens s to at most max runes, appending "..." when cut.
// Works on runes so multi-byte (e.g. Cyrillic) text is never broken.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "..."
}

// firstLine returns the first line of a (commit) message.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// shortSHA returns the 7-character abbreviated commit hash.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// excerpt prepares a user-written body (issue, comment, review) for display:
// trims, limits the length and escapes HTML.
func excerpt(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	return esc(truncate(s, max))
}

// branchName strips refs/heads/ or refs/tags/ prefixes from a git ref.
func branchName(ref string) string {
	ref = strings.TrimPrefix(ref, "refs/heads/")
	return strings.TrimPrefix(ref, "refs/tags/")
}

// isZeroSHA reports whether sha is the all-zero hash used for created/deleted refs.
func isZeroSHA(sha string) bool {
	return sha != "" && strings.Trim(sha, "0") == ""
}
