package i18n

import (
	"regexp"
	"slices"
	"testing"
)

var verbRe = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)

func load(t *testing.T) {
	t.Helper()
	if err := Load("../../locales"); err != nil {
		t.Fatalf("load locales: %v", err)
	}
}

// TestLocalesHaveSameKeys ensures every language defines the same set of keys.
func TestLocalesHaveSameKeys(t *testing.T) {
	load(t)

	en := Keys(EN)
	slices.Sort(en)
	for _, lang := range Supported {
		keys := Keys(lang)
		slices.Sort(keys)
		for _, k := range en {
			if _, found := slices.BinarySearch(keys, k); !found {
				t.Errorf("%s: missing key %q", lang, k)
			}
		}
		for _, k := range keys {
			if _, found := slices.BinarySearch(en, k); !found {
				t.Errorf("%s: extra key %q not present in en", lang, k)
			}
		}
	}
}

// TestLocalesHaveSameFormatVerbs ensures translations keep the same printf
// verbs in the same order, otherwise Tf would produce broken output.
func TestLocalesHaveSameFormatVerbs(t *testing.T) {
	load(t)

	for _, key := range Keys(EN) {
		want := verbRe.FindAllString(T(EN, key), -1)
		for _, lang := range Supported {
			got := verbRe.FindAllString(T(lang, key), -1)
			if !slices.Equal(got, want) {
				t.Errorf("%s/%s: format verbs %v, want %v", lang, key, got, want)
			}
		}
	}
}

func TestFallbackToEnglish(t *testing.T) {
	load(t)

	if got := T(Lang("de"), MsgWelcome); got != T(EN, MsgWelcome) {
		t.Errorf("unknown language should fall back to English, got %q", got)
	}
	if got := T(EN, "no_such_key"); got != "???" {
		t.Errorf("missing key: got %q, want ???", got)
	}
}
