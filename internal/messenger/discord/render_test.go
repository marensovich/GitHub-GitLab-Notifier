package discord

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/notify"
)

func TestMain(m *testing.M) {
	if err := i18n.Load("../../../locales"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func sample() notify.Notification {
	return notify.Notification{
		Emoji:   "🚀",
		Heading: "New push",
		Item:    notify.Link{Text: "main", URL: "https://example.com/tree/main"},
		Repo:    notify.Link{Text: "acme/app", URL: "https://example.com/acme/app"},
		Fields: []notify.Field{
			{Label: "Commits", Value: []notify.Part{notify.T("3")}},
			{Label: "Long", Value: []notify.Part{notify.T(strings.Repeat("x", 41))}},
			{Label: "Empty"},
		},
		Quote: "first line\nsecond line",
		Sections: []notify.Section{{
			Summary: "Commits",
			Items:   [][]notify.Part{{notify.C("abc123"), notify.T(" fix")}, {notify.L("pr", "https://example.com/pr/1")}},
			More:    2,
		}},
		Notes:   []string{"forced"},
		Buttons: []notify.Button{{Label: "Open", URL: "https://example.com/c"}, {Label: "bad", URL: "javascript:x"}},
		Actor:   notify.Link{Text: "octocat", URL: "https://example.com/octocat"},
		Time:    time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC),
	}
}

func TestRenderShape(t *testing.T) {
	m := Render(i18n.EN, sample())
	if len(m.Embeds) != 1 {
		t.Fatalf("embeds: %d", len(m.Embeds))
	}
	e := m.Embeds[0]
	if e.Title != "🚀 New push · main" || e.URL != "https://example.com/tree/main" {
		t.Errorf("title %q url %q", e.Title, e.URL)
	}
	if e.Author == nil || e.Author.Name != "acme/app" || e.Author.URL != "https://example.com/acme/app" {
		t.Errorf("author %+v", e.Author)
	}
	if len(e.Fields) != 2 || e.Fields[0].Name != "Commits" || e.Fields[0].Value != "3" || !e.Fields[0].Inline || e.Fields[1].Inline {
		t.Errorf("fields %+v", e.Fields)
	}
	for _, want := range []string{"> first line\n> second line", "**Commits**\n• `abc123` fix\n• [pr](https://example.com/pr/1)\n• *…and 2 more*", "*forced*"} {
		if !strings.Contains(e.Description, want) {
			t.Errorf("description lacks %q:\n%s", want, e.Description)
		}
	}
	if e.Footer == nil || e.Footer.Text != "octocat" || e.Timestamp != "2026-11-17T10:00:00Z" {
		t.Errorf("footer %+v timestamp %q", e.Footer, e.Timestamp)
	}
	if len(m.Components) != 1 || len(m.Components[0].Components) != 1 {
		t.Fatalf("components %+v", m.Components)
	}
	b := m.Components[0].Components[0]
	if m.Components[0].Type != 1 || b.Type != 2 || b.Style != 5 || b.Label != "Open" || b.URL != "https://example.com/c" {
		t.Errorf("button %+v", b)
	}
}

func TestRenderManyButtons(t *testing.T) {
	n := sample()
	n.Buttons = nil
	for range 7 {
		n.Buttons = append(n.Buttons, notify.Button{Label: "b", URL: "https://example.com"})
	}
	if m := Render(i18n.EN, n); len(m.Components) != 1 || len(m.Components[0].Components) != 5 {
		t.Errorf("components %+v", m.Components)
	}
	n.Buttons = nil
	if m := Render(i18n.EN, n); m.Components != nil {
		t.Errorf("components %+v", m.Components)
	}
}

func TestRenderColor(t *testing.T) {
	for emoji, want := range map[string]int{
		"✅": 0x2EA043, "🟢": 0x2EA043, "🟣": 0x2EA043,
		"❌": 0xCF222E, "🔴": 0xCF222E, "🚫": 0xCF222E, "💔": 0xCF222E,
		"⚠️": 0xD29922,
		"🚀":  0x5865F2, "": 0x5865F2,
	} {
		n := sample()
		n.Emoji = emoji
		if got := Render(i18n.EN, n).Embeds[0].Color; got != want {
			t.Errorf("%q: color %#x, want %#x", emoji, got, want)
		}
	}
}

func TestRenderEscapes(t *testing.T) {
	n := sample()
	n.Fields = []notify.Field{{Label: "Title", Value: []notify.Part{notify.T(`a*b_c~d` + "`" + `e|f>g[h](i)\j @everyone <@123>`)}}}
	m := Render(i18n.EN, n)
	want := `a\*b\_c\~d\` + "`" + `e\|f\>g\[h\]\(i\)\\j @everyone <@123\>`
	if got := m.Embeds[0].Fields[0].Value; got != want {
		t.Errorf("value %q, want %q", got, want)
	}
	raw, _ := json.Marshal(m)
	if !strings.Contains(string(raw), `"allowed_mentions":{"parse":[]}`) {
		t.Errorf("json %s", raw)
	}
}

func TestRenderFitsLimits(t *testing.T) {
	n := sample()
	n.Fields = nil
	for i := range 300 {
		n.Fields = append(n.Fields, notify.Field{Label: "F", Value: []notify.Part{notify.T(strings.Repeat("v", i%50))}})
	}
	n.Fields[3].Value = []notify.Part{notify.T(strings.Repeat("w", 2000))}
	n.Quote = strings.Repeat("q", 10000)
	n.Sections[0].Items = nil
	for range 500 {
		n.Sections[0].Items = append(n.Sections[0].Items, []notify.Part{notify.T(strings.Repeat("i", 60))})
	}
	n.Item.Text = strings.Repeat("t", 400)
	n.Buttons = []notify.Button{{Label: strings.Repeat("l", 100), URL: "https://example.com"}}
	m := Render(i18n.EN, n)
	if err := CheckLimits(m); err != nil {
		t.Fatal(err)
	}
	e := m.Embeds[0]
	if !strings.HasPrefix(e.Title, "🚀 New push · ttt") || e.Author.Name != "acme/app" {
		t.Errorf("title %q author %q", e.Title, e.Author.Name)
	}
	if !strings.Contains(e.Description, "more*") {
		t.Errorf("description lost the count: %q", e.Description[len(e.Description)-60:])
	}
}

func TestCheckLimits(t *testing.T) {
	ok := Message{Embeds: []Embed{{Title: "t"}}}
	if err := CheckLimits(ok); err != nil {
		t.Fatal(err)
	}
	for name, m := range map[string]Message{
		"title":       {Embeds: []Embed{{Title: strings.Repeat("t", 257)}}},
		"description": {Embeds: []Embed{{Description: strings.Repeat("d", 4097)}}},
		"fields":      {Embeds: []Embed{{Fields: make([]EmbedField, 26)}}},
		"value":       {Embeds: []Embed{{Fields: []EmbedField{{Name: "n", Value: strings.Repeat("v", 1025)}}}}},
		"total":       {Embeds: []Embed{{Description: strings.Repeat("d", 4000), Fields: []EmbedField{{Name: "n", Value: strings.Repeat("v", 1000)}, {Name: "n", Value: strings.Repeat("v", 1000)}}}}},
		"label":       {Components: []Row{{Type: 1, Components: []Button{{Type: 2, Style: 5, Label: strings.Repeat("l", 81)}}}}},
	} {
		if CheckLimits(m) == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
