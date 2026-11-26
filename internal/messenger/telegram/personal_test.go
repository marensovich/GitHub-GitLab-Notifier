package telegram

import (
	"context"
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
