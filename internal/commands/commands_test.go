package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"GitHub-GitLab-Notifier/internal/config"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/messenger/fake"
	"GitHub-GitLab-Notifier/internal/platform"
	"GitHub-GitLab-Notifier/storage"
)

func TestMain(m *testing.M) {
	if err := i18n.Load("../../locales"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

var telegramCaps = messenger.Caps{Threads: true, Looks: []string{"rich", "classic"}, ButtonsPerRow: 3}

type env struct {
	core  *Core
	m     *fake.Messenger
	store *storage.Store
	cfg   *config.Config
}

func newEnv(t *testing.T, caps messenger.Caps) *env {
	t.Helper()
	return newEnvNamed(t, "telegram", caps)
}

func newEnvNamed(t *testing.T, name string, caps messenger.Caps) *env {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := &config.Config{
		Server:   config.Server{PublicURL: "https://notify.example.com/"},
		Defaults: config.Defaults{Language: "en"},
		Admins:   []string{"telegram:900"},
	}
	m := fake.New(name, caps)
	m.Admins["-100/5"] = true
	return &env{core: New(store, cfg, "2.0.0-test", map[string]messenger.Messenger{name: m}), m: m, store: store, cfg: cfg}
}

// in is a command in the forum chat -100 sent by admin 5.
func in(text string) messenger.Command {
	fields := strings.Fields(strings.TrimPrefix(text, "/"))
	return messenger.Command{Messenger: "telegram", ChatID: "-100", UserID: "5", ChatTitle: "Team", IsForum: true,
		Name: fields[0], Args: fields[1:]}
}

func (e *env) run(c messenger.Command) string {
	e.core.HandleCommand(context.Background(), c)
	if len(e.m.Replies) == 0 {
		return ""
	}
	return e.m.Replies[len(e.m.Replies)-1].Reply.Text
}

func (e *env) press(data string, mods ...func(*messenger.Press)) {
	p := messenger.Press{Messenger: "telegram", ChatID: "-100", UserID: "5", Data: data, IsForum: true}
	for _, f := range mods {
		f(&p)
	}
	e.core.HandlePress(context.Background(), p)
}

func (e *env) dest(t *testing.T, chatID string) *storage.Destination {
	t.Helper()
	d, err := e.store.GetDestination("telegram", chatID)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// lastChoices returns the buttons of the last reply or edit.
func (e *env) lastChoices() []messenger.Choice {
	var rows [][]messenger.Choice
	switch {
	case len(e.m.Log) == 0:
	case e.lastOf("Reply", "Edit") == "Edit":
		rows = e.m.Edits[len(e.m.Edits)-1].Reply.Choices
	default:
		rows = e.m.Replies[len(e.m.Replies)-1].Reply.Choices
	}
	var out []messenger.Choice
	for _, r := range rows {
		out = append(out, r...)
	}
	return out
}

func (e *env) lastOf(names ...string) string {
	for i := len(e.m.Log) - 1; i >= 0; i-- {
		for _, n := range names {
			if e.m.Log[i] == n {
				return n
			}
		}
	}
	return ""
}

func (e *env) lastAnswer() fake.Answered {
	if len(e.m.Answers) == 0 {
		return fake.Answered{}
	}
	return e.m.Answers[len(e.m.Answers)-1]
}

func marked(choices []messenger.Choice) []string {
	var out []string
	for _, c := range choices {
		if strings.HasPrefix(c.Label, "✓") {
			out = append(out, c.Data)
		}
	}
	return out
}

// --- ported from internal/bot ---

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

func TestParseRepoURL(t *testing.T) {
	tests := []struct {
		in, url, platform string
		ok                bool
	}{
		{"https://github.com/octo/hello", "https://github.com/octo/hello", "GitHub", true},
		{"github.com/octo/hello.git", "https://github.com/octo/hello", "GitHub", true},
		{"https://gitlab.example.com/group/sub/proj/", "https://gitlab.example.com/group/sub/proj", "GitLab", true},
		{"https://github.com/octo", "", "", false},
		{"ftp://github.com/octo/hello", "", "", false},
	}
	for _, tt := range tests {
		url, platform, ok := parseRepoURL(tt.in)
		if url != tt.url || platform != tt.platform || ok != tt.ok {
			t.Errorf("parseRepoURL(%q) = %q %q %v, want %q %q %v", tt.in, url, platform, ok, tt.url, tt.platform, tt.ok)
		}
	}
}

func TestCommandsHaveDescriptions(t *testing.T) {
	e := newEnv(t, telegramCaps)
	for _, lang := range i18n.Supported {
		list := e.core.Commands(lang)
		if len(list) != len(commandNames) {
			t.Fatalf("%s: %d commands, want %d", lang, len(list), len(commandNames))
		}
		for _, c := range list {
			if c.Description == "???" || c.Description == "" {
				t.Errorf("%s: missing description for /%s", lang, c.Name)
			}
		}
	}
}

func TestConfigureNonForumSkipsTopics(t *testing.T) {
	e := newEnv(t, telegramCaps)
	c := in("/configure")
	c.IsForum = false
	e.run(c)
	for _, ch := range e.lastChoices() {
		if !strings.HasPrefix(ch.Data, "cfg1:format:") {
			t.Errorf("non-forum chat got button %v, want only the format step", ch)
		}
	}
	if d := e.dest(t, "-100"); d == nil || !d.UseThreads {
		t.Fatalf("/configure changed use_threads before any button was pressed: %+v", d)
	}
	e.press("cfg1:format:classic", func(p *messenger.Press) { p.IsForum = false })
	if d := e.dest(t, "-100"); d.UseThreads || d.Format != storage.FormatClassic {
		t.Errorf("after the press: %+v, want classic without threads", d)
	}
}

func TestConfigureAnonymousAdminGetsExplanation(t *testing.T) {
	e := newEnv(t, telegramCaps)
	e.m.AdminErr = errors.New("member list is inaccessible")
	e.run(in("/configure"))
	e.press("cfg1:format:classic", func(p *messenger.Press) { p.OriginAnonymous = true })
	a := e.lastAnswer()
	if !a.Alert || !strings.Contains(a.Text, "anonymous") {
		t.Errorf("want an alert explaining the anonymous /configure, got %+v", a)
	}
}

func TestConfigureSenderMayPressWhenAdminsUnknown(t *testing.T) {
	e := newEnv(t, telegramCaps)
	e.m.AdminErr = errors.New("member list is inaccessible")
	e.run(in("/configure"))
	e.press("cfg1:format:classic", func(p *messenger.Press) { p.UserID, p.OriginUserID = "7", "7" })
	if d := e.dest(t, "-100"); d.Format != storage.FormatClassic {
		t.Errorf("the sender of /configure could not choose: %+v", d)
	}
}

func TestConfigureTopicFailureKeepsButtons(t *testing.T) {
	e := newEnv(t, telegramCaps)
	e.m.ThreadErr = errors.New("not enough rights to create a topic")
	e.run(in("/configure"))
	e.press("cfg1:topics:yes")

	choices := e.lastChoices()
	if len(choices) == 0 || !strings.HasPrefix(choices[0].Data, "cfg1:topics:") {
		t.Errorf("after a failed topic creation want the topics buttons again, got %v", choices)
	}
	answered, created := -1, -1
	for i, call := range e.m.Log {
		if call == "Answer" && answered < 0 {
			answered = i
		}
		if call == "CreateThread" && created < 0 {
			created = i
		}
	}
	if answered < 0 || created < 0 || answered > created {
		t.Errorf("the press must be answered before topics are created (log %v)", e.m.Log)
	}
}

func TestConfigureWithoutThreadRights(t *testing.T) {
	e := newEnv(t, telegramCaps)
	e.m.NoThreadRights["-100"] = true
	e.run(in("/configure"))
	e.press("cfg1:topics:yes")
	last := e.m.Edits[len(e.m.Edits)-1].Reply.Text
	if !strings.Contains(last, i18n.T(i18n.EN, i18n.MsgConfigureChatNotAdmin)) || len(e.m.Threads) != 0 {
		t.Errorf("want the not-admin explanation and no threads, got %q (threads %v)", last, e.m.Threads)
	}
}

func TestConfigureCallbackNonAdminRefused(t *testing.T) {
	e := newEnv(t, telegramCaps)
	e.run(in("/configure"))
	e.press("cfg1:format:classic", func(p *messenger.Press) { p.UserID = "6" })
	if d := e.dest(t, "-100"); d.Format != storage.FormatRich {
		t.Errorf("non-admin press stored format %q", d.Format)
	}
	if a := e.lastAnswer(); !a.Alert || a.Text == "" {
		t.Errorf("want an alert for the non-admin, got %+v", a)
	}
	if len(e.m.Edits) != 0 {
		t.Errorf("non-admin press edited the message %d times", len(e.m.Edits))
	}
}

func TestConfigureStoresChoices(t *testing.T) {
	e := newEnv(t, telegramCaps)
	e.run(in("/configure"))
	e.press("cfg1:topics:yes")
	e.press("cfg1:format:classic")

	d := e.dest(t, "-100")
	if d.Format != storage.FormatClassic || !d.UseThreads {
		t.Errorf("stored %q, %v; want classic, true", d.Format, d.UseThreads)
	}
	if len(e.m.Threads) != len(platform.Topics) {
		t.Errorf("created %d threads, want one pass (%d)", len(e.m.Threads), len(platform.Topics))
	}
	routes, _ := e.store.Routes(d.ID)
	if len(routes) != len(platform.Topics) {
		t.Errorf("routes = %v", routes)
	}
	if ch := e.lastChoices(); len(ch) != 0 {
		t.Errorf("summary must have no buttons, got %v", ch)
	}

	// Running it again creates nothing new.
	e.press("cfg1:topics:yes")
	if len(e.m.Threads) != len(platform.Topics) {
		t.Errorf("second pass created threads again: %d", len(e.m.Threads))
	}
}

func TestConfigureCallbackFromUnknownChatIgnored(t *testing.T) {
	e := newEnv(t, telegramCaps)
	e.press("cfg1:format:classic")
	if d := e.dest(t, "-100"); d != nil {
		t.Error("a press in a chat that never ran /configure must not create it")
	}
	if len(e.m.Edits) != 0 {
		t.Errorf("edited %d times", len(e.m.Edits))
	}
}

func TestConfigureRerunMarksCurrent(t *testing.T) {
	e := newEnv(t, telegramCaps)
	e.run(in("/configure"))
	e.press("cfg1:topics:no")
	e.press("cfg1:format:classic")

	e.run(in("/configure"))
	if got := marked(e.lastChoices()); len(got) != 1 || got[0] != "cfg1:topics:no" {
		t.Errorf("topics step marks %v, want [cfg1:topics:no]", got)
	}
	if d := e.dest(t, "-100"); d.Format != storage.FormatClassic || d.UseThreads {
		t.Errorf("re-running /configure changed the settings: %q, %v", d.Format, d.UseThreads)
	}
	e.press("cfg1:topics:no")
	if got := marked(e.lastChoices()); len(got) != 1 || got[0] != "cfg1:format:classic" {
		t.Errorf("format step marks %v, want [cfg1:format:classic]", got)
	}
}

func TestConfigurePrivateChatOwnerMayPress(t *testing.T) {
	e := newEnv(t, telegramCaps)
	c := in("/configure")
	c.ChatID, c.UserID, c.IsForum, c.IsPrivate = "6", "6", false, true
	e.run(c)
	e.press("cfg1:format:classic", func(p *messenger.Press) { p.ChatID, p.UserID, p.IsForum, p.IsPrivate = "6", "6", false, true })
	if d := e.dest(t, "6"); d == nil || d.Format != storage.FormatClassic {
		t.Errorf("private chat owner could not choose classic: %+v", d)
	}
}

func TestStatusShowsChatSettings(t *testing.T) {
	e := newEnv(t, telegramCaps)
	d, _ := e.store.EnsureDestination("telegram", "-100", storage.Destination{Language: "en", Format: storage.FormatRich, UseThreads: true})
	if err := e.store.SetDestinationSettings(d.ID, storage.FormatClassic, false); err != nil {
		t.Fatal(err)
	}
	text := e.run(in("/status"))
	for _, want := range []string{"Look: classic", "Topics: off, everything goes to the main chat", "-100"} {
		if !strings.Contains(text, want) {
			t.Errorf("/status lacks %q:\n%s", want, text)
		}
	}
}

func TestStatusShowsRichSwitchedOff(t *testing.T) {
	e := newEnv(t, telegramCaps)
	e.m.RichOff = true
	e.store.EnsureDestination("telegram", "-100", storage.Destination{Language: "en", Format: storage.FormatRich, UseThreads: true})
	c := in("/status")
	c.IsForum = false
	text := e.run(c)
	if !strings.Contains(text, "Look: classic (the new look is off on this server)") {
		t.Errorf("/status should say the new look is off:\n%s", text)
	}
	if !strings.Contains(text, "Topics: off, everything goes to the main chat") {
		t.Errorf("a chat that is not a forum has no topic routing:\n%s", text)
	}
}

// --- new in 2.0 ---

const repo = "https://github.com/octo/hello"

func TestConnectNewRepo(t *testing.T) {
	e := newEnv(t, telegramCaps)
	text := e.run(in("/connect " + repo))
	r, _ := e.store.FindRepo(repo)
	if r == nil || len(r.Secret) != 40 {
		t.Fatalf("repo not stored with a fresh secret: %+v", r)
	}
	for _, want := range []string{r.Secret, "https://notify.example.com/webhook", "GitHub"} {
		if !strings.Contains(text, want) {
			t.Errorf("reply lacks %q:\n%s", want, text)
		}
	}
	d := e.dest(t, "-100")
	if subs, _ := e.store.SubscriptionsOf(d.ID); len(subs) != 1 || d.Title != "Team" {
		t.Errorf("subscriptions %v, title %q", subs, d.Title)
	}
}

func TestConnectAgainShowsSameSecret(t *testing.T) {
	e := newEnv(t, telegramCaps)
	e.run(in("/connect " + repo))
	r, _ := e.store.FindRepo(repo)
	text := e.run(in("/connect github.com/octo/hello.git"))
	if !strings.Contains(text, r.Secret) {
		t.Errorf("reconnecting must show the existing secret:\n%s", text)
	}
}

// connectOther connects repo in chat -200 first, as user 8.
func (e *env) connectOther(t *testing.T) *storage.Repo {
	t.Helper()
	c := in("/connect " + repo)
	c.ChatID, c.UserID = "-200", "8"
	e.run(c)
	r, _ := e.store.FindRepo(repo)
	if r == nil {
		t.Fatal("repo not created")
	}
	return r
}

func TestConnectSecondChatNeedsSecret(t *testing.T) {
	e := newEnv(t, telegramCaps)
	r := e.connectOther(t)
	text := e.run(in("/connect " + repo))
	if strings.Contains(text, r.Secret) {
		t.Fatal("the secret of another chat's repository was printed")
	}
	if text != i18n.T(i18n.EN, i18n.MsgConnectNeedsSecret) {
		t.Errorf("reply = %q", text)
	}
	if n, _ := e.store.SubscriberCount(r.ID); n != 1 {
		t.Errorf("subscribers = %d, want 1", n)
	}
	text = e.run(in("/connect " + repo + " wrong-secret"))
	if n, _ := e.store.SubscriberCount(r.ID); n != 1 || strings.Contains(text, r.Secret) {
		t.Errorf("a wrong secret subscribed the chat (%d) or leaked it", n)
	}
}

func TestConnectSecondChatWithSecret(t *testing.T) {
	e := newEnv(t, telegramCaps)
	r := e.connectOther(t)
	e.run(in("/connect " + repo + " " + r.Secret))
	if n, _ := e.store.SubscriberCount(r.ID); n != 2 {
		t.Errorf("subscribers = %d, want 2", n)
	}
}

func TestConnectSecondChatByAdmin(t *testing.T) {
	e := newEnv(t, telegramCaps)
	r := e.connectOther(t)
	c := in("/connect " + repo)
	c.UserID = "900"
	e.run(c)
	if n, _ := e.store.SubscriberCount(r.ID); n != 2 {
		t.Errorf("subscribers = %d, want 2", n)
	}
}

func TestConnectEmptySecretAdminOnly(t *testing.T) {
	e := newEnv(t, telegramCaps)
	r, err := e.store.CreateRepo(repo, "GitHub", "")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := e.store.EnsureDestination("telegram", "-200", storage.Destination{})
	e.store.Subscribe(r.ID, other.ID)

	text := e.run(in("/connect " + repo + " anything"))
	if text != i18n.T(i18n.EN, i18n.MsgConnectAdminOnly) {
		t.Errorf("reply = %q", text)
	}
	c := in("/connect " + repo)
	c.UserID = "900"
	e.run(c)
	if n, _ := e.store.SubscriberCount(r.ID); n != 2 {
		t.Errorf("admin could not connect: subscribers = %d", n)
	}
}

func TestDisconnectKeepsRepoForOtherChat(t *testing.T) {
	e := newEnv(t, telegramCaps)
	r := e.connectOther(t)
	e.run(in("/connect " + repo + " " + r.Secret))
	e.run(in("/disconnect " + repo))
	got, _ := e.store.FindRepo(repo)
	if got == nil || got.Secret != r.Secret {
		t.Fatalf("repository of the other chat was deleted: %+v", got)
	}
	if n, _ := e.store.SubscriberCount(r.ID); n != 1 {
		t.Errorf("subscribers = %d, want 1", n)
	}
}

func TestConfigureNoLookStepWithOneLook(t *testing.T) {
	e := newEnv(t, messenger.Caps{Threads: false, Looks: []string{"embed"}})
	text := e.run(in("/configure"))
	if len(e.lastChoices()) != 0 {
		t.Errorf("no step to ask, want no buttons, got %v", e.lastChoices())
	}
	if !strings.Contains(text, i18n.T(i18n.EN, i18n.MsgConfigureChatHowToUse)) {
		t.Errorf("want the summary straight away, got %q", text)
	}
}

func TestCommandInThreadActsOnChat(t *testing.T) {
	e := newEnv(t, telegramCaps)
	c := in("/connect " + repo)
	c.ThreadID = "12"
	e.run(c)
	if d := e.dest(t, "-100"); d == nil {
		t.Fatal("the parent chat was not stored")
	}
	if got := e.m.Replies[len(e.m.Replies)-1].Command.ThreadID; got != "12" {
		t.Errorf("reply went to thread %q, want 12", got)
	}
	d, _ := e.store.GetDestination("telegram", "-100")
	if subs, _ := e.store.SubscriptionsOf(d.ID); len(subs) != 1 {
		t.Errorf("subscriptions = %v", subs)
	}
}

func TestCommandReactivatesDestination(t *testing.T) {
	e := newEnv(t, telegramCaps)
	d, _ := e.store.EnsureDestination("telegram", "-100", storage.Destination{Language: "en"})
	e.store.SetDestinationInactive(d.ID, true)
	e.run(in("/help"))
	if got := e.dest(t, "-100"); got.Inactive {
		t.Error("a command must reactivate the destination")
	}
}

func TestLangChangesDestination(t *testing.T) {
	e := newEnv(t, telegramCaps)
	text := e.run(in("/lang ru"))
	if d := e.dest(t, "-100"); d == nil || d.Language != "ru" {
		t.Fatalf("language not stored: %+v", d)
	}
	if text != i18n.T(i18n.RU, i18n.MsgLangChanged) {
		t.Errorf("reply = %q", text)
	}
}

func TestUnknownMessengerIgnored(t *testing.T) {
	e := newEnv(t, telegramCaps)
	c := in("/help")
	c.Messenger = "max"
	e.run(c)
	if len(e.m.Replies) != 0 {
		t.Error("a command from a messenger without an adapter must be ignored")
	}
}

var discordCaps = messenger.Caps{Threads: true, ForumOnlyThreads: true, ButtonsPerRow: 5}

func discordIn(text string, forum bool) messenger.Command {
	c := in(text)
	c.Messenger, c.IsForum = "discord", forum
	return c
}

func TestThreadsOnlyInForums(t *testing.T) {
	e := newEnvNamed(t, "discord", messenger.Caps{Threads: true, ButtonsPerRow: 5})
	e.run(discordIn("/configure", false))
	if ch := e.lastChoices(); len(ch) != 0 {
		t.Errorf("text channel got buttons %v, want only the summary", ch)
	}
	e.run(discordIn("/configure", true))
	if ch := e.lastChoices(); len(ch) == 0 || !strings.HasPrefix(ch[0].Data, "cfg1:topics:") {
		t.Errorf("forum got %v, want the topics step", ch)
	}
}

func TestForumOnlyConfigureCreatesThreads(t *testing.T) {
	e := newEnvNamed(t, "discord", discordCaps)
	e.run(discordIn("/configure", true))
	if len(e.m.Threads) != len(platform.Topics) {
		t.Fatalf("created %d threads, want %d", len(e.m.Threads), len(platform.Topics))
	}
	if ch := e.lastChoices(); len(ch) != 0 {
		t.Errorf("forum-only chat got buttons %v", ch)
	}
	d, err := e.store.GetDestination("discord", "-100")
	if err != nil || d == nil || !d.UseThreads {
		t.Fatalf("destination %+v, %v; want use_threads", d, err)
	}
	if got := e.store.ThreadFor(d.ID, "push"); got == "" {
		t.Error("push is not routed to a thread")
	}
}

func TestMakeThread(t *testing.T) {
	e := newEnvNamed(t, "discord", discordCaps)
	d, err := e.store.EnsureDestination("discord", "-100", storage.Destination{UseThreads: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id, err := e.core.MakeThread(ctx, d.ID, "push", "")
	if err != nil || id == "" || e.m.Threads[0].Name != "🚀 Pushes" || e.store.ThreadFor(d.ID, "push") != id {
		t.Fatalf("MakeThread = %q, %v; threads %+v", id, err, e.m.Threads)
	}
	// Another queued message without a thread uses the stored one.
	if same, err := e.core.MakeThread(ctx, d.ID, "push", ""); err != nil || same != id || len(e.m.Threads) != 1 {
		t.Errorf("queued message: %q, %v; %d threads, want the stored one", same, err, len(e.m.Threads))
	}
	// The stored thread is gone: a new one replaces it.
	again, err := e.core.MakeThread(ctx, d.ID, "push", id)
	threads, _ := e.store.Threads(d.ID)
	if err != nil || again == id || e.store.ThreadFor(d.ID, "push") != again || len(threads) != 1 {
		t.Errorf("gone thread = %q, %v; stored %+v, want the old thread replaced", again, err, threads)
	}
	// Other messages that were queued for the gone thread follow the new one.
	if next, err := e.core.MakeThread(ctx, d.ID, "push", id); err != nil || next != again || len(e.m.Threads) != 2 {
		t.Errorf("second gone message: %q, %v; %d threads", next, err, len(e.m.Threads))
	}
	// Events without a category share one general thread.
	general, err := e.core.MakeThread(ctx, d.ID, "", "")
	if err != nil || e.m.Threads[2].Name != GeneralThreadName {
		t.Fatalf("general thread: %v, %+v", err, e.m.Threads)
	}
	if g, err := e.core.MakeThread(ctx, d.ID, "", ""); err != nil || g != general || len(e.m.Threads) != 3 {
		t.Errorf("second general event: %q, %v; %d threads", g, err, len(e.m.Threads))
	}
	if e.store.ThreadFor(d.ID, storage.GeneralRoute) != general {
		t.Errorf("general route %q", e.store.ThreadFor(d.ID, storage.GeneralRoute))
	}
}
