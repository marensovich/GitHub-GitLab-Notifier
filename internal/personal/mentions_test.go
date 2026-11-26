package personal

import (
	"slices"
	"testing"
)

func TestMentions(t *testing.T) {
	text := "hi @Alice and @bob-2, mail a@b.c, see @org/team, `@code`, x/@path\n```\n@block\n```\nthanks @alice!"
	if got := Mentions(text); !slices.Equal(got, []string{"alice", "bob-2"}) {
		t.Errorf("Mentions = %v", got)
	}
	if got := Mentions("(@carol) @dave.\n@eve_x"); !slices.Equal(got, []string{"carol", "dave", "eve_x"}) {
		t.Errorf("Mentions = %v", got)
	}
	if got := Mentions(""); len(got) != 0 {
		t.Errorf("empty: %v", got)
	}
}
