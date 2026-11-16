package notify

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if err := i18n.Load("../../locales"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func samplePR() Notification {
	return Notification{
		Emoji:   "🔀",
		Heading: "New pull request",
		Item:    Link{Text: "#12 Add dark mode", URL: "https://github.com/octo/hello/pull/12"},
		Repo:    Link{Text: "octo/hello", URL: "https://github.com/octo/hello"},
		Fields: []Field{
			{Label: "Branches", Value: []Part{C("feature"), T(" → "), C("main")}},
			{Label: "Changes", Value: []Part{B("+120"), T(" / "), B("−8")}},
			{Label: "Empty", Value: nil},
		},
		Quote: "This PR adds dark mode.\n\nCloses #10",
		Sections: []Section{{Summary: "Commits", Items: [][]Part{
			{L("59b20b8", "https://github.com/octo/hello/commit/59b20b8"), T(" fix")},
		}}},
		Notes: []string{"🔒 Confidential"},
		Buttons: []Button{
			{Label: "Open PR", URL: "https://github.com/octo/hello/pull/12", Style: StylePrimary},
			{Label: "Files", URL: "https://github.com/octo/hello/pull/12/files"},
		},
		Actor: Link{Text: "@dev", URL: "https://github.com/dev"},
		Time:  time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC),
	}
}

func TestRichLayout(t *testing.T) {
	out := Rich(i18n.EN, samplePR())
	if err := CheckRichHTML(out); err != nil {
		t.Fatalf("invalid rich html: %v\n%s", err, out)
	}
	order := []string{
		"<h4>🔀 New pull request</h4>",
		`<p><b><a href="https://github.com/octo/hello/pull/12">#12 Add dark mode</a></b></p>`,
		"<table compact>",
		`<tr><td>Repository</td><td><a href="https://github.com/octo/hello">octo/hello</a></td></tr>`,
		"<tr><td>Branches</td><td><code>feature</code> → <code>main</code></td></tr>",
		"<p>🔒 Confidential</p>",
		"<blockquote expandable>This PR adds dark mode.<br><br>Closes #10</blockquote>",
		"<details><summary>Commits (1)</summary><ul><li>",
		`<tg-button type="url" style="primary" url="https://github.com/octo/hello/pull/12">Open PR</tg-button>`,
		`<tg-button type="url" url="https://github.com/octo/hello/pull/12/files">Files</tg-button>`,
		`<footer><a href="https://github.com/dev">@dev</a> · <tg-time unix="1793354400" format="r">`,
	}
	pos := 0
	for _, want := range order {
		i := strings.Index(out[pos:], want)
		if i < 0 {
			t.Fatalf("missing or out of order %q in:\n%s", want, out)
		}
		pos += i + len(want)
	}
	if strings.Contains(out, "Empty") {
		t.Errorf("empty field rendered:\n%s", out)
	}
}

const hostile = `</table><tg-button type="url" url="x">&amp;<details>`

func hostileNotification() Notification {
	n := samplePR()
	n.Heading = hostile
	n.Item.Text = hostile
	n.Repo.Text = hostile
	n.Actor.Text = hostile
	n.Fields = []Field{{Label: hostile, Value: []Part{T(hostile), C(hostile), B(hostile), L(hostile, "https://x.io")}}}
	n.Quote = hostile
	n.Sections = []Section{{Summary: hostile, Items: [][]Part{{T(hostile)}}}}
	n.Notes = []string{hostile}
	n.Buttons = []Button{{Label: hostile, URL: "https://x.io"}}
	return n
}

func TestRichEscapesHostileText(t *testing.T) {
	out := Rich(i18n.EN, hostileNotification())
	if err := CheckRichHTML(out); err != nil {
		t.Fatalf("invalid rich html: %v\n%s", err, out)
	}
	if !strings.Contains(out, "&lt;/table&gt;") {
		t.Errorf("hostile text not escaped:\n%s", out)
	}
	if n := strings.Count(out, "<tg-button"); n != 1 {
		t.Errorf("want exactly 1 real button, got %d:\n%s", n, out)
	}
}

func TestUnsafeURLDropped(t *testing.T) {
	n := samplePR()
	n.Item.URL = "javascript:alert(1)"
	n.Buttons = []Button{{Label: "Bad", URL: "javascript:alert(1)"}, {Label: "Empty", URL: ""}}
	out := Rich(i18n.EN, n)
	if strings.Contains(out, "javascript:") {
		t.Errorf("unsafe url rendered:\n%s", out)
	}
	if strings.Contains(out, "<tg-button") {
		t.Errorf("button with unsafe url rendered:\n%s", out)
	}
	if !strings.Contains(out, "<p><b>#12 Add dark mode</b></p>") {
		t.Errorf("item text should stay without link:\n%s", out)
	}
	text, kb := Classic(i18n.EN, n)
	if strings.Contains(text, "javascript:") || len(kb) != 0 {
		t.Errorf("classic kept unsafe url: %q %v", text, kb)
	}
}

func TestRichLimits(t *testing.T) {
	n := samplePR()
	n.Quote = strings.Repeat("я", 100000)
	items := make([][]Part, 1000)
	for i := range items {
		items[i] = []Part{T(strings.Repeat("x", 300))}
	}
	n.Sections = []Section{{Summary: "Commits", Items: items}}
	out := Rich(i18n.EN, n)
	if l := len([]rune(out)); l >= 32768 {
		t.Errorf("rich message too long: %d", l)
	}
	if b := strings.Count(out, "<li>") + strings.Count(out, "<tr>") + strings.Count(out, "<p>"); b >= 500 {
		t.Errorf("too many blocks: %d", b)
	}
	if !strings.Contains(out, "…and 980 more") {
		t.Errorf("missing more line:\n%s", out[len(out)-400:])
	}
}

func TestEmptyOptionalParts(t *testing.T) {
	n := Notification{Emoji: "⭐", Heading: "New star", Repo: Link{Text: "octo/hello", URL: "https://github.com/octo/hello"}}
	out := Rich(i18n.EN, n)
	if err := CheckRichHTML(out); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"<blockquote", "<details", "<footer", "<tg-button", "<tg-time"} {
		if strings.Contains(out, bad) {
			t.Errorf("unexpected %s in:\n%s", bad, out)
		}
	}
	if !strings.Contains(out, `<p><b><a href="https://github.com/octo/hello">octo/hello</a></b></p>`) {
		t.Errorf("repo should be the item when Item is empty:\n%s", out)
	}
	if c := strings.Count(out, "octo/hello</a>"); c != 1 {
		t.Errorf("repo shown %d times, want once:\n%s", c, out)
	}
}
