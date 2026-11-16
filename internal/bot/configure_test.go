package bot

import (
	"GitHub-GitLab-Notifier/internal/platform"
	"GitHub-GitLab-Notifier/storage"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
)

const (
	chatAdmin  = `{"ok":true,"result":{"status":"administrator","user":{"id":5,"is_bot":false,"first_name":"A"}}}`
	chatMember = `{"ok":true,"result":{"status":"member","user":{"id":6,"is_bot":false,"first_name":"M"}}}`
	answerOK   = `{"ok":true,"result":true}`
	topicOK    = `{"ok":true,"result":{"message_thread_id":77,"name":"t","icon_color":0}}`
)

// newConfigureBot is a bot with a real store and a fake Bot API.
func newConfigureBot(t *testing.T, replies map[string][]string) (*Bot, *fakeAPI) {
	t.Helper()
	if replies == nil {
		replies = map[string][]string{}
	}
	for method, reply := range map[string]string{
		"getChatMember":       chatAdmin,
		"answerCallbackQuery": answerOK,
		"createForumTopic":    topicOK,
	} {
		if _, ok := replies[method]; !ok {
			replies[method] = []string{reply}
		}
	}
	b, fake := newSendBot(t, true, replies)
	store, err := storage.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	b.store = store
	b.self = &telego.User{ID: 42, IsBot: true, Username: "notifier_bot"}
	return b, fake
}

func chat(forum bool) telego.Chat {
	return telego.Chat{ID: -100, Type: telego.ChatTypeSupergroup, IsForum: forum}
}

func runConfigure(b *Bot, c telego.Chat) {
	b.consume(telego.Update{Message: &telego.Message{
		MessageID: 1, Chat: c, From: &telego.User{ID: 5}, Text: "/configure",
	}})
}

func press(b *Bot, c telego.Chat, userID int64, data string) {
	b.consume(telego.Update{CallbackQuery: &telego.CallbackQuery{
		ID:      "q",
		From:    telego.User{ID: userID},
		Data:    data,
		Message: &telego.Message{MessageID: 10, Chat: c},
	}})
}

// lastKeyboard returns the callback buttons of the last setup message sent or edited.
func lastKeyboard(t *testing.T, f *fakeAPI) []map[string]any {
	t.Helper()
	for i := len(f.calls) - 1; i >= 0; i-- {
		c := f.calls[i]
		if c.method != "sendMessage" && c.method != "editMessageText" {
			continue
		}
		markup, _ := c.body["reply_markup"].(map[string]any)
		rows, _ := markup["inline_keyboard"].([]any)
		var out []map[string]any
		for _, row := range rows {
			for _, btn := range row.([]any) {
				out = append(out, btn.(map[string]any))
			}
		}
		return out
	}
	t.Fatal("no setup message was sent")
	return nil
}

func count(f *fakeAPI, method string) int {
	n := 0
	for _, c := range f.calls {
		if c.method == method {
			n++
		}
	}
	return n
}

func TestParseConfigureCallback(t *testing.T) {
	for data, want := range map[string][3]string{
		"cfg1:topics:yes":     {"topics", "yes", "true"},
		"cfg1:format:classic": {"format", "classic", "true"},
		"cfg0:topics:yes":     {"", "", "false"},
		"other:topics:yes":    {"", "", "false"},
		"cfg1:topics":         {"", "", "false"},
		"cfg1:topics:":        {"", "", "false"},
		"cfg1:size:big":       {"", "", "false"},
	} {
		step, value, ok := parseConfigureCallback(data)
		got := [3]string{step, value, map[bool]string{true: "true", false: "false"}[ok]}
		if got != want {
			t.Errorf("parseConfigureCallback(%q) = %v, want %v", data, got, want)
		}
	}
}

func TestConfigureNonForumSkipsTopics(t *testing.T) {
	b, fake := newConfigureBot(t, nil)
	runConfigure(b, chat(false))

	for _, btn := range lastKeyboard(t, fake) {
		if !strings.HasPrefix(btn["callback_data"].(string), "cfg1:format:") {
			t.Errorf("non-forum chat got button %v, want only the format step", btn)
		}
	}
	got, err := b.store.GetChat(-100)
	if err != nil {
		t.Fatal(err)
	}
	if got.UseTopics {
		t.Error("non-forum chat must store use_topics = false")
	}
}

func TestConfigureCallbackNonAdminRefused(t *testing.T) {
	b, fake := newConfigureBot(t, map[string][]string{"getChatMember": {chatMember}})
	runConfigure(b, chat(true))
	press(b, chat(true), 6, "cfg1:format:classic")

	got, _ := b.store.GetChat(-100)
	if got.Format != storage.FormatRich {
		t.Errorf("non-admin press stored format %q", got.Format)
	}
	var alert map[string]any
	for _, c := range fake.calls {
		if c.method == "answerCallbackQuery" {
			alert = c.body
		}
	}
	if alert == nil || alert["show_alert"] != true || alert["text"] == "" {
		t.Errorf("want an alert for the non-admin, got %v", alert)
	}
	if n := count(fake, "editMessageText"); n != 0 {
		t.Errorf("non-admin press edited the message %d times", n)
	}
}

func TestConfigureStoresChoices(t *testing.T) {
	b, fake := newConfigureBot(t, nil)
	runConfigure(b, chat(true))
	press(b, chat(true), 5, "cfg1:topics:yes")
	press(b, chat(true), 5, "cfg1:format:classic")

	got, _ := b.store.GetChat(-100)
	if got.Format != storage.FormatClassic || !got.UseTopics {
		t.Errorf("stored %q, %v; want classic, true", got.Format, got.UseTopics)
	}
	if n := count(fake, "createForumTopic"); n != len(platform.Topics) {
		t.Errorf("createForumTopic called %d times, want one pass (%d)", n, len(platform.Topics))
	}
	if kb := lastKeyboard(t, fake); len(kb) != 0 {
		t.Errorf("summary must have no buttons, got %v", kb)
	}
}

func TestConfigureCallbackFromUnknownChatIgnored(t *testing.T) {
	b, fake := newConfigureBot(t, nil)
	press(b, chat(true), 5, "cfg1:format:classic")
	if _, err := b.store.GetChat(-100); err == nil {
		t.Error("a press in a chat that never ran /configure must not create it")
	}
	if n := count(fake, "editMessageText"); n != 0 {
		t.Errorf("edited %d times", n)
	}
}

func TestConfigureRerunMarksCurrent(t *testing.T) {
	b, fake := newConfigureBot(t, nil)
	runConfigure(b, chat(true))
	press(b, chat(true), 5, "cfg1:topics:no")
	press(b, chat(true), 5, "cfg1:format:classic")

	runConfigure(b, chat(true))
	marked := func() []string {
		var out []string
		for _, btn := range lastKeyboard(t, fake) {
			if strings.HasPrefix(btn["text"].(string), "✓") {
				out = append(out, btn["callback_data"].(string))
			}
		}
		return out
	}
	if got := marked(); len(got) != 1 || got[0] != "cfg1:topics:no" {
		t.Errorf("topics step marks %v, want [cfg1:topics:no]", got)
	}
	got, _ := b.store.GetChat(-100)
	if got.Format != storage.FormatClassic || got.UseTopics {
		t.Errorf("re-running /configure changed the settings: %q, %v", got.Format, got.UseTopics)
	}

	press(b, chat(true), 5, "cfg1:topics:no")
	if got := marked(); len(got) != 1 || got[0] != "cfg1:format:classic" {
		t.Errorf("format step marks %v, want [cfg1:format:classic]", got)
	}
}

func TestConfigurePrivateChatOwnerMayPress(t *testing.T) {
	b, _ := newConfigureBot(t, map[string][]string{"getChatMember": {chatMember}})
	private := telego.Chat{ID: 6, Type: telego.ChatTypePrivate}
	runConfigure(b, private)
	press(b, private, 6, "cfg1:format:classic")
	if got, _ := b.store.GetChat(6); got == nil || got.Format != storage.FormatClassic {
		t.Errorf("private chat owner could not choose classic: %+v", got)
	}
}

// statusText runs /status in c and returns the text of the reply.
func statusText(t *testing.T, b *Bot, f *fakeAPI, c telego.Chat) string {
	t.Helper()
	b.consume(telego.Update{Message: &telego.Message{
		MessageID: 1, Chat: c, From: &telego.User{ID: 5}, Text: "/status",
	}})
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].method == "sendMessage" {
			text, _ := f.calls[i].body["text"].(string)
			return text
		}
	}
	t.Fatal("no reply to /status")
	return ""
}

func TestStatusShowsChatSettings(t *testing.T) {
	b, fake := newConfigureBot(t, nil)
	c := chat(true)
	if err := b.store.SaveChat(storage.ChatConfig{ChatID: c.ID, Language: "en", IsForum: true}); err != nil {
		t.Fatal(err)
	}
	if err := b.store.SetChatSettings(c.ID, storage.FormatClassic, false); err != nil {
		t.Fatal(err)
	}

	text := statusText(t, b, fake, c)
	for _, want := range []string{"Look: classic", "Topics: off, everything goes to the main chat"} {
		if !strings.Contains(text, want) {
			t.Errorf("/status lacks %q:\n%s", want, text)
		}
	}
}

func TestStatusShowsRichSwitchedOff(t *testing.T) {
	b, fake := newConfigureBot(t, nil)
	b.rich = false
	c := chat(false)
	if err := b.store.SaveChat(storage.ChatConfig{ChatID: c.ID, Language: "en"}); err != nil {
		t.Fatal(err)
	}

	text := statusText(t, b, fake, c)
	if !strings.Contains(text, "Look: classic (the new look is off on this server)") {
		t.Errorf("/status should say the new look is off:\n%s", text)
	}
}
