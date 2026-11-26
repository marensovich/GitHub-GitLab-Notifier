package max

import (
	"strings"
	"testing"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/notify"
)

func TestRenderEscapes(t *testing.T) {
	n := notify.Notification{
		Emoji: "🚀", Heading: "Push <script>",
		Repo:    notify.Link{Text: "a&b", URL: "https://example.com/a?x=1&y=2"},
		Item:    notify.Link{Text: "evil", URL: "javascript:alert(1)"},
		Quote:   "<b>not bold</b>",
		Buttons: []notify.Button{{Label: "Open", URL: "https://example.com"}, {Label: "Bad", URL: "javascript:x"}},
	}
	m := Render(i18n.EN, n)
	for _, bad := range []string{"<script>", "javascript:", "<b>not bold"} {
		if strings.Contains(m.Text, bad) {
			t.Errorf("text has %q:\n%s", bad, m.Text)
		}
	}
	if !strings.Contains(m.Text, `<a href="https://example.com/a?x=1&amp;y=2">a&amp;b</a>`) {
		t.Errorf("repo link:\n%s", m.Text)
	}
	if err := CheckHTML(m.Text); err != nil {
		t.Error(err)
	}
	if len(m.Buttons) != 1 || m.Buttons[0].Label != "Open" {
		t.Errorf("buttons %+v", m.Buttons)
	}
}

func TestRenderFits(t *testing.T) {
	var commits [][]notify.Part
	for range 300 {
		commits = append(commits, []notify.Part{notify.C("abcdef1"), notify.T(" " + strings.Repeat("long message ", 10))})
	}
	n := notify.Notification{
		Emoji: "🚀", Heading: "Push",
		Repo:     notify.Link{Text: "acme/app", URL: "https://github.com/acme/app"},
		Quote:    strings.Repeat("q", 3000),
		Sections: []notify.Section{{Summary: "Commits", Items: commits}},
		Time:     time.Unix(0, 0),
	}
	m := Render(i18n.EN, n)
	if got := len([]rune(m.Text)); got > maxText {
		t.Errorf("%d runes", got)
	}
	if !strings.Contains(m.Text, "Push") || !strings.Contains(m.Text, "acme/app") {
		t.Errorf("lost the heading or the repo:\n%s", m.Text)
	}
	if err := CheckHTML(m.Text); err != nil {
		t.Error(err)
	}
}

func TestCheckHTML(t *testing.T) {
	for s, ok := range map[string]bool{
		"<b>x</b> <i>y</i> <code>z</code>":      true,
		`<a href="https://x">x</a>`:             true,
		"<blockquote>q</blockquote>":            true,
		"<b>open":                               false,
		"<span>x</span>":                        false,
		"<blockquote expandable>q</blockquote>": false,
	} {
		if err := CheckHTML(s); (err == nil) != ok {
			t.Errorf("%q: %v", s, err)
		}
	}
}
