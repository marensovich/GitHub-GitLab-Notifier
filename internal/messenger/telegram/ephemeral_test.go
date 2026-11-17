package telegram

import (
	"context"
	"testing"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"

	"github.com/mymmrac/telego"
)

type twoCommands struct{}

func (twoCommands) Commands(lang i18n.Lang) []messenger.CommandInfo {
	return []messenger.CommandInfo{{Name: "status", Description: "s"}, {Name: "help", Description: "h"}}
}

func TestCommandsRegisteredEphemeral(t *testing.T) {
	b, fake := newSendBot(t, false, nil)
	b.registerCommands(context.Background(), twoCommands{})
	n := 0
	for _, c := range fake.calls {
		if c.method != "setMyCommands" {
			continue
		}
		for _, raw := range c.body["commands"].([]any) {
			n++
			if cmd := raw.(map[string]any); cmd["is_ephemeral"] != true {
				t.Errorf("command %v is not ephemeral", cmd)
			}
		}
	}
	if n != 2*len(i18n.Supported) {
		t.Errorf("%d commands registered", n)
	}
}

func groupCommand(ephemeralID int) *telego.Message {
	return &telego.Message{
		MessageID:          7,
		Chat:               telego.Chat{ID: -100, Type: telego.ChatTypeSupergroup, Title: "team"},
		From:               &telego.User{ID: 5},
		Text:               "/status",
		EphemeralMessageID: ephemeralID,
	}
}

func lastCall(f *fakeAPI) apiCall { return f.calls[len(f.calls)-1] }

func TestEphemeralCommandGetsEphemeralReply(t *testing.T) {
	b, fake := newSendBot(t, false, nil)
	c, ok := b.command(groupCommand(9))
	if !ok {
		t.Fatal("not a command")
	}
	choices := [][]messenger.Choice{{{Label: "x", Data: "d"}}}
	if _, err := b.Reply(context.Background(), c, messenger.Reply{Text: "hi", Choices: choices}); err != nil {
		t.Fatal(err)
	}
	call := lastCall(fake)
	eph, _ := call.body["ephemeral_message_parameters"].(map[string]any)
	reply, _ := call.body["reply_parameters"].(map[string]any)
	if call.method != "sendMessage" || eph["receiver_user_id"] != float64(5) || reply["ephemeral_message_id"] != float64(9) || reply["message_id"] != nil {
		t.Errorf("call %s %v", call.method, call.body)
	}
}

func TestPlainCommandReplyUnchanged(t *testing.T) {
	b, fake := newSendBot(t, false, nil)
	c, _ := b.command(groupCommand(0))
	choices := [][]messenger.Choice{{{Label: "x", Data: "d"}}}
	if _, err := b.Reply(context.Background(), c, messenger.Reply{Text: "hi", Choices: choices}); err != nil {
		t.Fatal(err)
	}
	call := lastCall(fake)
	reply, _ := call.body["reply_parameters"].(map[string]any)
	if call.body["ephemeral_message_parameters"] != nil || reply["message_id"] != float64(7) {
		t.Errorf("call %v", call.body)
	}
}

func TestPressOnEphemeralEdits(t *testing.T) {
	b, fake := newSendBot(t, false, nil)
	chat := telego.Chat{ID: -100, Type: telego.ChatTypeSupergroup}
	q := &telego.CallbackQuery{ID: "q1", From: telego.User{ID: 5}, Data: "d", Message: &telego.Message{
		MessageID: 0, Chat: chat, EphemeralMessageID: 12, ReceiverUser: &telego.User{ID: 5},
	}}
	p := press(q)
	if p.OriginUserID != "5" {
		t.Errorf("origin %q", p.OriginUserID)
	}
	choices := [][]messenger.Choice{{{Label: "x", Data: "d"}}}
	if err := b.Edit(context.Background(), p, messenger.Reply{Text: "done", Choices: choices}); err != nil {
		t.Fatal(err)
	}
	call := lastCall(fake)
	if call.method != "editEphemeralMessageText" || call.body["chat_id"] != float64(-100) || call.body["receiver_user_id"] != float64(5) ||
		call.body["ephemeral_message_id"] != float64(12) || call.body["text"] != "done" || call.body["reply_markup"] == nil {
		t.Errorf("call %s %v", call.method, call.body)
	}

	q.Message = &telego.Message{MessageID: 3, Chat: chat}
	if err := b.Edit(context.Background(), press(q), messenger.Reply{Text: "done"}); err != nil {
		t.Fatal(err)
	}
	if m := lastCall(fake).method; m != "editMessageText" {
		t.Errorf("normal message edited with %s", m)
	}
}
