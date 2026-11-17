package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger/telegram"
	"GitHub-GitLab-Notifier/internal/platform"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if err := i18n.Load("../../locales"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// handlerCase describes one fixture-based handler test.
type handlerCase struct {
	fixture string
	event   platform.EventType
	// skip means the handler must return ErrSkip.
	skip bool
	// contains lists substrings that must appear in the English message.
	contains []string
	// absent lists substrings that must not appear in the English message.
	absent []string
}

// runHandlerCases runs fixture-based cases against the registered handlers.
func runHandlerCases(t *testing.T, cases []handlerCase) {
	t.Helper()

	handlers := make(map[platform.EventType]EventHandler)
	for _, h := range All() {
		handlers[h.GetEventType()] = h
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			h, ok := handlers[tc.event]
			if !ok {
				t.Fatalf("no handler registered for %+v", tc.event)
			}
			payload, err := os.ReadFile(filepath.Join("testdata", tc.fixture))
			if err != nil {
				t.Fatal(err)
			}

			for _, lang := range i18n.Supported {
				n, err := h.Handle(lang, payload)
				if tc.skip {
					if !errors.Is(err, ErrSkip) {
						t.Fatalf("%s: want ErrSkip, got %+v err=%v", lang, n, err)
					}
					continue
				}
				if err != nil {
					t.Fatalf("%s: unexpected error: %v", lang, err)
				}
				// The classic text plus its buttons line carries every piece of
				// content, so assertions run against it.
				msg := renderClassic(t, lang, n)
				if rich := telegram.RenderRich(lang, n); strings.Contains(rich+msg, "???") || strings.Contains(rich+msg, "%!") {
					t.Errorf("%s: message has missing translation or bad format verb:\n%s\n%s", lang, msg, rich)
				}
				if lang != i18n.EN {
					continue
				}
				for _, want := range tc.contains {
					if !strings.Contains(msg, want) {
						t.Errorf("message does not contain %q:\n%s", want, msg)
					}
				}
				for _, bad := range tc.absent {
					if strings.Contains(msg, bad) {
						t.Errorf("message must not contain %q:\n%s", bad, msg)
					}
				}
			}
		})
	}
}

func TestAllHandlersHaveUniqueEventTypes(t *testing.T) {
	seen := make(map[platform.EventType]bool)
	for _, h := range All() {
		et := h.GetEventType()
		if seen[et] {
			t.Errorf("duplicate handler for %+v", et)
		}
		seen[et] = true
		if platform.FromEventName(et.Platform, et.Name) == nil {
			t.Errorf("handler event %+v is not registered in platform", et)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := map[string]string{"5s": "5s", "2m5s": "2m 5s", "1h2m3s": "1h 2m 3s"}
	for in, want := range tests {
		d, _ := parseDuration(in)
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%s) = %q, want %q", in, got, want)
		}
	}
}
