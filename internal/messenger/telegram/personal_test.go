package telegram

import (
	"bytes"
	"context"
	"log"
	"os"
	"strings"
	"testing"

	"GitHub-GitLab-Notifier/internal/messenger"

	"github.com/mymmrac/telego"
)

var _ messenger.MemberChecker = (*Telegram)(nil)
var _ messenger.DirectMessenger = (*Telegram)(nil)

func TestIsMember(t *testing.T) {
	member := func(status, extra string) string {
		return `{"ok":true,"result":{"status":"` + status + `","user":{"id":5,"is_bot":false,"first_name":"a"}` + extra + `}}`
	}
	b, fake := newSendBot(t, false, map[string][]string{"getChatMember": {
		member("member", ""), member("left", ""), member("kicked", `,"until_date":0`),
		member("restricted", `,"is_member":false`), member("administrator", ""),
	}})
	for i, want := range []bool{true, false, false, false, true} {
		got, err := b.IsMember(context.Background(), "-100", "5")
		if err != nil || got != want {
			t.Errorf("answer %d: %v %v, want %v", i, got, err, want)
		}
	}
	if c := lastCall(fake); c.body["chat_id"] != float64(-100) || c.body["user_id"] != float64(5) {
		t.Errorf("call %+v", c)
	}
}

func TestDirectChatIsTheUser(t *testing.T) {
	b, fake := newSendBot(t, false, nil)
	before := len(fake.calls)
	if id, err := b.DirectChat(context.Background(), "5"); id != "5" || err != nil || len(fake.calls) != before {
		t.Errorf("%q %v, %d calls", id, err, len(fake.calls)-before)
	}
}

func TestDeleteCommand(t *testing.T) {
	b, fake := newSendBot(t, false, map[string][]string{"deleteMessage": {`{"ok":true,"result":true}`}})
	c, _ := b.command(&telego.Message{MessageID: 7, Chat: telego.Chat{ID: 5, Type: telego.ChatTypePrivate},
		From: &telego.User{ID: 5}, Text: "/link github x"})
	if err := b.DeleteCommand(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if call := lastCall(fake); call.method != "deleteMessage" || call.body["chat_id"] != float64(5) || call.body["message_id"] != float64(7) {
		t.Errorf("call %s %v", call.method, call.body)
	}
}

type nopHandler struct{ got []messenger.Command }

func (h *nopHandler) HandleCommand(_ context.Context, c messenger.Command) { h.got = append(h.got, c) }
func (h *nopHandler) HandlePress(context.Context, messenger.Press)         {}

func TestLinkTokenNotLogged(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	b, _ := newSendBot(t, false, nil)
	h := &nopHandler{}
	b.consume(context.Background(), h, telego.Update{Message: &telego.Message{MessageID: 7,
		Chat: telego.Chat{ID: 5, Type: telego.ChatTypePrivate}, From: &telego.User{ID: 5, Username: "al"},
		Text: "/link github ghp_SECRET"}})
	if len(h.got) != 1 || h.got[0].Args[1] != "ghp_SECRET" {
		t.Fatalf("command %+v", h.got)
	}
	if strings.Contains(buf.String(), "ghp_SECRET") || !strings.Contains(buf.String(), "/link github") {
		t.Errorf("log %q", buf.String())
	}
}

func TestLinkArgsNeverLogged(t *testing.T) {
	for _, text := range []string{"/link ghp_SECRET", "/link gitlab x.y ghp_SECRET", "/link ghp_SECRET a b"} {
		c := messenger.Command{Name: "link", Args: strings.Fields(text)[1:]}
		if got := logged(c, text); strings.Contains(got, "ghp_SECRET") {
			t.Errorf("%q logged as %q", text, got)
		}
	}
}
