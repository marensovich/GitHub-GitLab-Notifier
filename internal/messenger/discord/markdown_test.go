package discord

import (
	"strings"
	"testing"
)

func TestToMarkdown(t *testing.T) {
	for in, want := range map[string]string{
		"<b>x</b> and <strong>y</strong>": "**x** and **y**",
		"<i>x</i>":                        "*x*",
		"<code>a_b</code>":                "`a_b`",
		`<a href="https://e.com/a">t</a>`: "[t](https://e.com/a)",
		`<a href="javascript:x">t</a>`:    "t",
		"&lt;tag&gt; &amp; &quot;q&quot;": `<tag\> & "q"`,
		"<blink>x</blink><br>y":           "x\ny",
		"a_b *c*":                         `a\_b \*c\*`,
		"<code>&lt;x&gt;</code>":          "`<x>`",
		"<pre>line1\nline2</pre>":         "```\nline1\nline2\n```",
		"line1\nline2":                    "line1\nline2",
	} {
		if got := toMarkdown(in); got != want {
			t.Errorf("toMarkdown(%q) = %q, want %q", in, got, want)
		}
	}
	if got := toMarkdown(strings.Repeat("x", 2500)); runes(got) != 2000 || !strings.HasSuffix(got, "…") {
		t.Errorf("long text: %d runes", runes(got))
	}
}
