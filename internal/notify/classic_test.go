package notify

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"strings"
	"testing"
)

func TestClassicKeyboard(t *testing.T) {
	n := samplePR()
	n.Buttons = append(n.Buttons, Button{Label: "Third", URL: "https://x.io/3"}, Button{Label: "Fourth", URL: "https://x.io/4"})
	text, kb := Classic(i18n.EN, n)
	if err := CheckClassicHTML(text); err != nil {
		t.Fatalf("invalid classic html: %v\n%s", err, text)
	}
	if len(kb) != 1 || len(kb[0]) != 3 {
		t.Fatalf("want one row of 3 buttons, got %v", kb)
	}
	if kb[0][0].Label != "Open PR" || kb[0][0].Style != StylePrimary {
		t.Errorf("first button = %+v", kb[0][0])
	}
	for _, want := range []string{
		"🔀 New pull request",
		`<a href="https://github.com/octo/hello">octo/hello</a>`,
		`<b><a href="https://github.com/octo/hello/pull/12">#12 Add dark mode</a></b>`,
		"Branches: <code>feature</code> → <code>main</code>",
		"🔒 Confidential",
		"<blockquote expandable>This PR adds dark mode.\n\nCloses #10</blockquote>",
		`👤 <a href="https://github.com/dev">@dev</a>`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("classic text misses %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "<tg-") || strings.Contains(text, "<h4>") {
		t.Errorf("rich tags in classic text:\n%s", text)
	}
}

func TestClassicEscapesHostileText(t *testing.T) {
	text, _ := Classic(i18n.EN, hostileNotification())
	if err := CheckClassicHTML(text); err != nil {
		t.Fatalf("invalid classic html: %v\n%s", err, text)
	}
}

func TestClassicSectionLimit(t *testing.T) {
	n := samplePR()
	items := make([][]Part, 8)
	for i := range items {
		items[i] = []Part{T("c")}
	}
	n.Sections = []Section{{Summary: "Commits", Items: items, ClassicMax: 5, More: 2}}
	text, _ := Classic(i18n.EN, n)
	if got := strings.Count(text, "\n  c"); got != 5 {
		t.Errorf("want 5 listed items, got %d:\n%s", got, text)
	}
	if !strings.Contains(text, "…and 5 more") {
		t.Errorf("want …and 5 more (3 cut + 2 More):\n%s", text)
	}
}
