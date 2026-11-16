package bot

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"testing"
	"time"

	"github.com/mymmrac/telego"
)

// TestStartDoesNotWaitForCommands checks that a slow setMyCommands (Telegram
// answering 504 after a long wait) does not hold up Start, and with it the
// webhook server that main starts afterwards.
func TestStartDoesNotWaitForCommands(t *testing.T) {
	release := make(chan struct{})
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
	b := &Bot{api: api, lang: i18n.EN, self: &telego.User{Username: "TestBot"}}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan struct{})
	go func() {
		b.Start(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start is still waiting for setMyCommands")
	}
}
