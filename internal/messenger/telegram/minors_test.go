package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/notify"
)

func TestUpgradedGroupMoves(t *testing.T) {
	moved := `{"ok":false,"error_code":400,"description":"Bad Request: group chat was upgraded to a supergroup chat","parameters":{"migrate_to_chat_id":-1001234}}`
	b, _ := newSendBot(t, true, map[string][]string{"sendRichMessage": {moved}, "sendMessage": {moved}})
	_, err := b.Send(context.Background(), messenger.Target{ChatID: "-55"}, notify.FormatRich, i18n.EN, sample())
	if messenger.Classify(err) != messenger.ClassChatMoved || messenger.MovedTo(err) != "-1001234" {
		t.Fatalf("%v: %v, moved to %q", err, messenger.Classify(err), messenger.MovedTo(err))
	}
}

func TestUnknownForbiddenRejected(t *testing.T) {
	reply := apiError(403, "Forbidden: something new")
	b, _ := newSendBot(t, false, map[string][]string{"sendMessage": {reply}})
	_, err := b.Send(context.Background(), messenger.Target{ChatID: "-55"}, notify.FormatClassic, i18n.EN, sample())
	if got := messenger.Classify(err); got != messenger.ClassRejected {
		t.Fatalf("Classify = %v", got)
	}
}

func TestClassicFitsHugeParts(t *testing.T) {
	n := sample()
	n.Heading = strings.Repeat("h", 6000)
	n.Item.Text = strings.Repeat("i", 6000)
	n.Fields = []notify.Field{{Label: strings.Repeat("L", 6000), Value: []notify.Part{notify.T("v")}}}
	text, kb := RenderClassic(i18n.EN, n)
	if VisibleLen(text) > classicMaxText || len(kb) == 0 || CheckClassicHTML(text) != nil {
		t.Fatalf("%d visible, %d rows, %v", VisibleLen(text), len(kb), CheckClassicHTML(text))
	}
}

func TestHealthAsksAtMostOncePerInterval(t *testing.T) {
	b, fake := newSendBot(t, false, map[string][]string{"getMe": {apiError(500, "down")}})
	b.lastContact = time.Time{} // nothing heard from Telegram for long
	for range 3 {
		if err := b.Health(context.Background()); err == nil {
			t.Fatal("healthy without an answer")
		}
	}
	n := 0
	for _, c := range fake.calls {
		if c.method == "getMe" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("getMe called %d times", n)
	}
}
