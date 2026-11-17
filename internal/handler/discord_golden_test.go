package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger/discord"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestGoldenDiscord renders every fixture as a Discord message in every
// language and compares it with <case>.discord.golden:
//
//	go test ./internal/handler -run TestGoldenDiscord -update
func TestGoldenDiscord(t *testing.T) {
	for _, h := range All() {
		fixtures, err := filepath.Glob(filepath.Join(eventDir(h.GetEventType()), "*.json"))
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(fixtures)
		for _, fixture := range fixtures {
			name := strings.TrimPrefix(strings.TrimSuffix(filepath.ToSlash(fixture), ".json"), "testdata/")
			t.Run(name, func(t *testing.T) {
				payload, err := os.ReadFile(fixture)
				if err != nil {
					t.Fatal(err)
				}
				got := renderDiscordGolden(t, h, payload)
				goldenPath := strings.TrimSuffix(fixture, ".json") + ".discord.golden"
				if *update {
					if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(goldenPath)
				if err != nil {
					t.Fatalf("%v (run: go test ./internal/handler -run TestGoldenDiscord -update)", err)
				}
				want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
				if !bytes.Equal(got, want) {
					t.Errorf("message differs from %s (run with -update if the change is intended)\n--- got ---\n%s\n--- want ---\n%s",
						goldenPath, got, want)
				}
			})
		}
	}
}

func renderDiscordGolden(t *testing.T, h EventHandler, payload []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	for i, lang := range i18n.Supported {
		n, err := h.Handle(lang, payload)
		if errors.Is(err, ErrSkip) {
			if i == 0 {
				b.WriteString("skip: true\n")
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", lang, err)
		}
		m := discord.Render(lang, n)
		if err := discord.CheckLimits(m); err != nil {
			t.Errorf("%s: %v", lang, err)
		}
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "--- %s ---\n", lang)
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(m); err != nil {
			t.Fatal(err)
		}
	}
	return b.Bytes()
}
