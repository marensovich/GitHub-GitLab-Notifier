package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// update rewrites the golden files instead of comparing against them:
//
//	go test ./internal/handler -run TestGolden -update
var update = flag.Bool("update", false, "rewrite golden files in testdata")

// eventDir is the testdata directory that holds the fixtures of an event:
// testdata/<platform>/<event>/, e.g. github/pull_request or gitlab/note_hook.
func eventDir(et platform.EventType) string {
	name := strings.ReplaceAll(strings.ToLower(et.Name), " ", "_")
	return filepath.Join("testdata", strings.ToLower(string(et.Platform)), name)
}

// TestGolden renders every fixture in testdata/<platform>/<event>/<case>.json
// with its handler in every supported language and compares the result with
// <case>.golden next to it. Any change to a message template, a translation
// or a payload parser therefore shows up as a diff of the golden file.
func TestGolden(t *testing.T) {
	covered := 0
	for _, h := range All() {
		et := h.GetEventType()
		dir := eventDir(et)
		fixtures, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(fixtures) == 0 {
			t.Errorf("%s %q has no fixtures in %s", et.Platform, et.Name, dir)
			continue
		}
		covered++
		sort.Strings(fixtures)
		for _, fixture := range fixtures {
			name := strings.TrimSuffix(filepath.ToSlash(fixture), ".json")
			name = strings.TrimPrefix(name, "testdata/")
			t.Run(name, func(t *testing.T) {
				payload, err := os.ReadFile(fixture)
				if err != nil {
					t.Fatal(err)
				}
				got := renderGolden(t, h, payload)
				goldenPath := strings.TrimSuffix(fixture, ".json") + ".golden"
				if *update {
					if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(goldenPath)
				if err != nil {
					t.Fatalf("%v (run: go test ./internal/handler -run TestGolden -update)", err)
				}
				want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
				if !bytes.Equal(got, want) {
					t.Errorf("message differs from %s (run with -update if the change is intended)\n--- got ---\n%s\n--- want ---\n%s",
						goldenPath, got, want)
				}
			})
		}
	}
	if covered != len(All()) {
		t.Errorf("%d of %d handlers have golden fixtures", covered, len(All()))
	}
}

// renderGolden produces the golden file content for one payload: the topic the
// event goes to (or "skip: true"), then the message in every language.
func renderGolden(t *testing.T, h EventHandler, payload []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	et := h.GetEventType()
	for i, lang := range i18n.Supported {
		msg, err := h.Handle(lang, payload)
		if errors.Is(err, ErrSkip) {
			if i == 0 {
				b.WriteString("skip: true\n")
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", lang, err)
		}
		assertTelegramHTML(t, msg)
		if i == 0 {
			topic := string(et.Topic)
			if topic == "" {
				topic = "general"
			}
			fmt.Fprintf(&b, "topic: %s\n", topic)
		}
		fmt.Fprintf(&b, "\n--- %s ---\n%s\n", lang, msg)
	}
	return b.Bytes()
}
