package notify

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"strings"
	"testing"
)

// huge returns a notification far over every Telegram limit: a field with
// thousands of parts, a long quote, many notes and a long commit list.
func huge(text string) Notification {
	n := samplePR()
	var value []Part
	for i := 0; i < 3000; i++ {
		value = append(value, C(text), T(", "))
	}
	n.Fields = append(n.Fields, Field{Label: "Labels", Value: value})
	n.Quote = strings.Repeat(text, 1000)
	for i := 0; i < 200; i++ {
		n.Notes = append(n.Notes, strings.Repeat(text, 20))
	}
	items := make([][]Part, 500)
	for i := range items {
		items[i] = []Part{T(strings.Repeat(text, 50))}
	}
	n.Sections = []Section{{Summary: "Commits", Items: items, ClassicMax: 100}}
	return n
}

func TestClassicFitsTelegramLimit(t *testing.T) {
	text, _ := Classic(i18n.EN, huge("label-<&>"))
	if err := CheckClassicHTML(text); err != nil {
		t.Fatalf("invalid classic html: %v", err)
	}
	if l := VisibleLen(text); l > classicMaxText {
		t.Errorf("classic text is %d characters, limit %d", l, classicMaxText)
	}
	if !strings.Contains(text, "#12 Add dark mode") {
		t.Errorf("the item was dropped:\n%.500s", text)
	}
}

func TestRichFitsOnEscapedHTML(t *testing.T) {
	out := Rich(i18n.EN, huge("&<>&<>"))
	if err := CheckRichHTML(out); err != nil {
		t.Fatalf("invalid rich html: %v", err)
	}
	if l := len([]rune(out)); l > richMaxHTML {
		t.Errorf("rich html is %d characters, limit %d", l, richMaxHTML)
	}
	if !strings.Contains(out, "#12 Add dark mode") {
		t.Errorf("the item was dropped:\n%.500s", out)
	}
}

func TestVisibleLen(t *testing.T) {
	if got := VisibleLen(`<b>a &amp; b</b> <a href="https://x.io">😀</a>`); got != 8 {
		t.Errorf("VisibleLen = %d, want 8 (😀 is two UTF-16 units)", got)
	}
}
