package max

import (
	"context"
	"strings"
	"testing"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/notify"
)

// deadlineHandler records whether the core got a context with a deadline.
type deadlineHandler struct{ has chan bool }

func (h deadlineHandler) HandleCommand(ctx context.Context, _ messenger.Command) {
	_, ok := ctx.Deadline()
	h.has <- ok
}
func (h deadlineHandler) HandlePress(ctx context.Context, _ messenger.Press) {
	_, ok := ctx.Deadline()
	h.has <- ok
}

func TestUpdateHandlingHasDeadline(t *testing.T) {
	m, _ := newFake(t)
	h := deadlineHandler{has: make(chan bool, 1)}
	m.consume(context.Background(), h, decode(t, `{"update_type":"bot_started","chat_id":7,"user":{"user_id":42}}`))
	select {
	case ok := <-h.has:
		if !ok {
			t.Error("no deadline")
		}
	case <-time.After(time.Second):
		t.Fatal("not handled")
	}
}

func TestRenderFitsHugeLinks(t *testing.T) {
	long := "https://example.com/" + strings.Repeat("a&b", 1000)
	text := strings.Repeat(`"`, 300)
	n := notify.Notification{
		Emoji: "🚀", Heading: strings.Repeat("&", 300),
		Repo:  notify.Link{Text: "acme/app" + text, URL: long},
		Item:  notify.Link{Text: text, URL: long},
		Actor: notify.Link{Text: text, URL: long},
	}
	m := Render(i18n.EN, n)
	if got := runes(m.Text); got > maxText {
		t.Errorf("%d runes", got)
	}
	if !strings.Contains(m.Text, "acme/app") {
		t.Errorf("lost the repo:\n%.300s", m.Text)
	}
	if err := CheckHTML(m.Text); err != nil {
		t.Error(err)
	}
}

func TestEmptyAnswerWithoutEditAnswers(t *testing.T) {
	m, f := newFake(t)
	p := messenger.Press{Ref: &pressState{callbackID: "cb.9", msg: &message{}}}
	if err := m.Answer(context.Background(), p, "", false); err != nil {
		t.Fatal(err)
	}
	if c := f.last(t); c.Path != "/answers" || c.Query != "callback_id=cb.9" || c.Body["notification"] == nil {
		t.Errorf("%+v", c)
	}
}

func TestBotMessagesIgnored(t *testing.T) {
	m, _ := newFake(t)
	h := newHandler()
	m.consume(context.Background(), h, decode(t, `{"update_type":"message_created","message":{"sender":{"user_id":1,"is_bot":true},
		"recipient":{"chat_id":7,"chat_type":"dialog"},"body":{"mid":"m","text":"/help"}}}`))
	if len(h.cmds()) != 0 {
		t.Errorf("%+v", h.cmds())
	}
}
