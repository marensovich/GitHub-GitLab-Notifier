package telegram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"testing"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"

	"github.com/mymmrac/telego"
)

// menuHandler is a handler with a command menu.
type menuHandler struct{}

func (menuHandler) HandleCommand(context.Context, messenger.Command) {}
func (menuHandler) HandlePress(context.Context, messenger.Press)     {}
func (menuHandler) Commands(i18n.Lang) []messenger.CommandInfo {
	return []messenger.CommandInfo{{Name: "help", Description: "Help"}}
}

// TestRunDoesNotWaitForCommands checks that a slow setMyCommands (Telegram
// answering 504 after a long wait) does not hold up polling.
func TestRunDoesNotWaitForCommands(t *testing.T) {
	release := make(chan struct{})
	polled := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch path.Base(r.URL.Path) {
		case "setMyCommands":
			select {
			case <-release:
			case <-r.Context().Done():
			}
			_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
		case "getUpdates":
			select {
			case polled <- struct{}{}:
			default:
			}
			select {
			case <-time.After(50 * time.Millisecond):
			case <-r.Context().Done():
			}
			_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
		default:
			_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	api, err := telego.NewBot("123456:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		telego.WithAPIServer(srv.URL), telego.WithDiscardLogger())
	if err != nil {
		t.Fatal(err)
	}
	tg := newWithAPI(api, &telego.User{Username: "TestBot"}, i18n.EN, "test", true)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tg.Run(ctx, menuHandler{}) }()

	select {
	case <-polled:
	case <-time.After(2 * time.Second):
		t.Fatal("polling is still waiting for setMyCommands")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the context was cancelled")
	}
}
