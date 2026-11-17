package telegram

import (
	"context"
	"testing"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/notify"

	"github.com/mymmrac/telego"
)

func TestSendClassifiesErrors(t *testing.T) {
	rateLimited := `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 7","parameters":{"retry_after":7}}`
	cases := []struct {
		name  string
		reply string
		want  messenger.Class
	}{
		{"rate limited", rateLimited, messenger.ClassRateLimited},
		{"thread", apiError(400, "Bad Request: message thread not found"), messenger.ClassThreadNotFound},
		{"topic closed", apiError(400, "Bad Request: TOPIC_CLOSED"), messenger.ClassThreadNotFound},
		{"kicked", apiError(403, "Forbidden: bot was kicked from the group chat"), messenger.ClassChatGone},
		{"chat not found", apiError(400, "Bad Request: chat not found"), messenger.ClassChatGone},
		{"server error", apiError(500, "Internal Server Error"), messenger.ClassTemporary},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, _ := newSendBot(t, true, map[string][]string{"sendRichMessage": {c.reply}, "sendMessage": {c.reply}})
			_, err := b.Send(context.Background(), messenger.Target{ChatID: "-100", ThreadID: "9"}, notify.FormatRich, i18n.EN, sample())
			if got := messenger.Classify(err); got != c.want {
				t.Fatalf("Classify(%v) = %v, want %v", err, got, c.want)
			}
			if c.want == messenger.ClassRateLimited && messenger.RetryAfter(err) != 7*time.Second {
				t.Errorf("RetryAfter = %v, want 7s", messenger.RetryAfter(err))
			}
		})
	}
}

func TestSendRejectsBadTarget(t *testing.T) {
	b, fake := newSendBot(t, true, nil)
	_, err := b.Send(context.Background(), messenger.Target{ChatID: "general"}, notify.FormatRich, i18n.EN, sample())
	if messenger.Classify(err) != messenger.ClassRejected || len(fake.calls) != 0 {
		t.Fatalf("err = %v, calls = %v", err, methods(fake))
	}
}

func TestUpdateToCommand(t *testing.T) {
	b, _ := newSendBot(t, true, nil)
	forum := telego.Chat{ID: -100, Type: telego.ChatTypeSupergroup, Title: "Team", IsForum: true}

	c, ok := b.command(&telego.Message{
		Chat: forum, From: &telego.User{ID: 5}, Text: "/connect@notifier_bot https://github.com/a/b",
		IsTopicMessage: true, MessageThreadID: 12,
	})
	if !ok {
		t.Fatal("topic command not parsed")
	}
	if c.Messenger != "telegram" || c.ChatID != "-100" || c.ThreadID != "12" || c.UserID != "5" ||
		c.Name != "connect" || len(c.Args) != 1 || !c.IsForum || c.IsPrivate || c.ChatTitle != "Team" {
		t.Errorf("command = %+v", c)
	}

	if _, ok := b.command(&telego.Message{Chat: forum, From: &telego.User{ID: 5}, Text: "/connect@OtherBot x"}); ok {
		t.Error("a command for another bot must be ignored")
	}
	if _, ok := b.command(&telego.Message{Chat: forum, From: &telego.User{ID: 5}, Text: "hello"}); ok {
		t.Error("plain text is not a command")
	}

	c, ok = b.command(&telego.Message{
		Chat: forum, From: &telego.User{ID: 1087968824}, SenderChat: &telego.Chat{ID: -100}, Text: "/configure",
	})
	if !ok || c.UserID != "" {
		t.Errorf("anonymous admin: ok=%v command=%+v, want an empty UserID", ok, c)
	}
	if c.ThreadID != "" {
		t.Errorf("a message outside a topic has no thread, got %q", c.ThreadID)
	}

	private := telego.Chat{ID: 7, Type: telego.ChatTypePrivate}
	if c, _ := b.command(&telego.Message{Chat: private, From: &telego.User{ID: 7}, Text: "/start"}); !c.IsPrivate {
		t.Error("private chat not marked")
	}
}

func TestIsChatAdminAnonymousCannotTell(t *testing.T) {
	b, fake := newSendBot(t, true, nil)
	if ok, err := b.IsChatAdmin(context.Background(), "-100", ""); ok || err == nil {
		t.Fatalf("anonymous: ok=%v err=%v, want false and an error", ok, err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("no API call expected, got %v", methods(fake))
	}
}

func TestReplyKeyboard(t *testing.T) {
	b, fake := newSendBot(t, true, nil)
	_, err := b.Reply(context.Background(), messenger.Command{ChatID: "-100", ThreadID: "12"}, messenger.Reply{
		Text:    "<b>Pick</b>",
		Choices: [][]messenger.Choice{{{Label: "Rich", Data: "cfg1:f:rich"}, {Label: "Classic", Data: "cfg1:f:classic"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := fake.calls[0].body
	if fake.calls[0].method != "sendMessage" || body["parse_mode"] != "HTML" || body["message_thread_id"] != float64(12) {
		t.Fatalf("call = %s %v", fake.calls[0].method, body)
	}
	rows := body["reply_markup"].(map[string]any)["inline_keyboard"].([]any)
	btn := rows[0].([]any)[1].(map[string]any)
	if btn["text"] != "Classic" || btn["callback_data"] != "cfg1:f:classic" {
		t.Errorf("button = %v", btn)
	}
}

func TestPressCarriesOrigin(t *testing.T) {
	forum := telego.Chat{ID: -100, Type: telego.ChatTypeSupergroup, IsForum: true}
	p := press(&telego.CallbackQuery{ID: "q", From: telego.User{ID: 6}, Data: "cfg1:format:rich",
		Message: &telego.Message{MessageID: 10, Chat: forum, ReplyToMessage: &telego.Message{
			Chat: forum, From: &telego.User{ID: 5}, Text: "/configure"}}})
	if p.ChatID != "-100" || p.UserID != "6" || p.OriginUserID != "5" || p.OriginAnonymous || !p.IsForum || p.IsPrivate {
		t.Errorf("press = %+v", p)
	}
	p = press(&telego.CallbackQuery{ID: "q", From: telego.User{ID: 6},
		Message: &telego.Message{MessageID: 10, Chat: forum, ReplyToMessage: &telego.Message{
			Chat: forum, From: &telego.User{ID: 1087968824}, SenderChat: &forum, Text: "/configure"}}})
	if !p.OriginAnonymous || p.OriginUserID != "" {
		t.Errorf("anonymous origin not detected: %+v", p)
	}
}
