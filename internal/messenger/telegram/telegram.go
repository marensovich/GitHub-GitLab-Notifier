// Package telegram is the Telegram adapter: long polling for commands and
// button presses, and sending notifications as rich or classic messages.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"GitHub-GitLab-Notifier/internal/config"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"

	"github.com/mymmrac/telego"
)

// Name is the messenger name stored with Telegram destinations.
const Name = config.MessengerTelegram

// Telegram is the Telegram adapter.
type Telegram struct {
	api *telego.Bot
	// token is hidden in every error the adapter returns or keeps.
	token   string
	self    *telego.User
	lang    i18n.Lang
	version string
	// rich enables rich messages for notifications.
	rich bool
	// richMissing is set when the Bot API server has no sendRichMessage
	// (an older self-hosted server); classic messages are sent from then on.
	richMissing atomic.Bool

	// Last successful call to the Telegram API and the last error, for Health.
	healthMu    sync.Mutex
	lastContact time.Time
	lastErr     error
	probe       messenger.Probe
}

var _ messenger.Messenger = (*Telegram)(nil)

// staleAfter is how long without a successful API call Health checks the
// API again before answering.
const staleAfter = 3 * time.Minute

// New connects to the Telegram Bot API and checks the token with getMe.
func New(cfg config.Telegram, defaultLang i18n.Lang, version string) (*Telegram, error) {
	if cfg.Token == "" {
		return nil, errors.New(i18n.T(defaultLang, i18n.MsgBotTokenIsNotSet))
	}
	client, err := newHTTPClient(defaultLang, cfg.Proxy)
	if err != nil {
		return nil, err
	}
	options := []telego.BotOption{telego.WithHTTPClient(client)}
	if apiURL := strings.TrimRight(cfg.APIURL, "/"); apiURL != "" {
		// Self-hosted Telegram Bot API server (https://github.com/tdlib/telegram-bot-api).
		options = append(options, telego.WithAPIServer(apiURL))
	}
	api, err := telego.NewBot(cfg.Token, options...)
	if err != nil {
		return nil, fmt.Errorf(i18n.T(defaultLang, i18n.MsgFailedToCreateBot), err)
	}
	self, err := api.GetMe(context.Background())
	if err != nil {
		err = (&Telegram{token: cfg.Token}).redact(err)
		return nil, fmt.Errorf(i18n.T(defaultLang, i18n.MsgFailedToGetBotInfo), err)
	}
	log.Printf(i18n.T(defaultLang, i18n.MsgBotRegistered), self.Username)
	return newWithAPI(api, self, defaultLang, version, cfg.Rich()), nil
}

func newWithAPI(api *telego.Bot, self *telego.User, lang i18n.Lang, version string, rich bool) *Telegram {
	t := &Telegram{api: api, token: api.Token(), self: self, lang: lang, version: version, rich: rich}
	t.contact(nil)
	return t
}

// newHTTPClient returns a client that goes through proxy ("" = direct).
// The proxy's credentials are never logged.
func newHTTPClient(lang i18n.Lang, proxy string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxy == "" {
		log.Println(i18n.T(lang, i18n.MsgDirectConnection))
		return &http.Client{Transport: transport}, nil
	}
	u, err := url.Parse(proxy)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("telegram.proxy: invalid proxy URL")
	}
	switch u.Scheme {
	case "http", "https", "socks5":
	default:
		return nil, fmt.Errorf("telegram.proxy: unsupported scheme %q (use http, https or socks5)", u.Scheme)
	}
	transport.Proxy = http.ProxyURL(u)
	log.Printf(i18n.T(lang, i18n.MsgProxyEnabled), u.Scheme, u.Hostname(), u.Port())
	return &http.Client{Transport: transport}, nil
}

func (t *Telegram) Name() string { return Name }

func (t *Telegram) Caps() messenger.Caps {
	return messenger.Caps{
		Threads:       true,
		Looks:         []string{"rich", "classic"},
		ButtonsPerRow: 3,
		PerChatRate:   1,
		GlobalRate:    30,
		PerChatBurst:  3,
	}
}

// Username returns the bot's username.
func (t *Telegram) Username() string { return t.self.Username }

// RichAvailable reports whether rich messages are sent right now: they are
// switched on and the Bot API server supports them.
func (t *Telegram) RichAvailable() bool {
	return t.rich && !t.richMissing.Load()
}

// Run polls for updates and passes commands and button presses to h until
// ctx is done.
func (t *Telegram) Run(ctx context.Context, h messenger.Handler) error {
	log.Printf(i18n.T(t.lang, i18n.MsgBotStarted), t.self.Username)

	// setMyCommands can hang for minutes when Telegram answers 504; the menu
	// is cosmetic, so it must not delay polling.
	if l, ok := h.(messenger.CommandLister); ok {
		go t.registerCommands(ctx, l)
	}

	updates, err := t.api.UpdatesViaLongPolling(ctx, &telego.GetUpdatesParams{
		Timeout:        60,
		AllowedUpdates: []string{"message", "callback_query"},
	})
	if err != nil {
		return fmt.Errorf(i18n.T(t.lang, i18n.MsgBotStartFailed), err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case u, ok := <-updates:
			if !ok {
				return nil
			}
			t.consume(ctx, h, u)
		}
	}
}

// registerCommands publishes the command list shown in the "Menu" button.
func (t *Telegram) registerCommands(ctx context.Context, l messenger.CommandLister) {
	for _, lang := range i18n.Supported {
		var commands []telego.BotCommand
		for _, c := range l.Commands(lang) {
			// Ephemeral: the command and the answer are seen only by the
			// sender and the bot.
			commands = append(commands, telego.BotCommand{Command: c.Name, Description: c.Description, IsEphemeral: true})
		}
		params := &telego.SetMyCommandsParams{Commands: commands}
		if lang != i18n.EN {
			params.LanguageCode = string(lang)
		}
		if err := t.api.SetMyCommands(ctx, params); err != nil {
			log.Printf(i18n.T(t.lang, i18n.MsgSetCommandsFailed), err)
		}
	}
}

func (t *Telegram) consume(ctx context.Context, h messenger.Handler, u telego.Update) {
	if q := u.CallbackQuery; q != nil {
		if q.Message == nil {
			return
		}
		h.HandlePress(ctx, press(q))
		return
	}
	if u.Message == nil {
		return
	}
	if c, ok := t.command(u.Message); ok {
		userName := ""
		if u.Message.From != nil {
			// From is nil for channel posts.
			userName = u.Message.From.Username
		}
		log.Printf(i18n.T(t.lang, i18n.MsgNewMessage), userName, u.Message.Chat.ID, logged(c, u.Message.Text))
		h.HandleCommand(ctx, c)
	}
}

// logged is the text of a command as the log shows it: the token of /link
// never reaches the log.
func logged(c messenger.Command, text string) string {
	if c.Name != "link" || len(c.Args) == 0 {
		return text
	}
	if p := strings.ToLower(c.Args[0]); p == "github" || p == "gitlab" {
		return "/link " + p + " [hidden]"
	}
	return "/link [hidden]"
}

// press turns a button press into a Press, with the sender of the command
// the pressed message replies to.
func press(q *telego.CallbackQuery) messenger.Press {
	chat := q.Message.GetChat()
	p := messenger.Press{
		Messenger: Name,
		ChatID:    strconv.FormatInt(chat.ID, 10),
		UserID:    strconv.FormatInt(q.From.ID, 10),
		Data:      q.Data,
		IsPrivate: chat.Type == telego.ChatTypePrivate,
		IsForum:   chat.IsForum,
		Ref:       q,
	}
	m := q.Message.Message()
	if m != nil && m.EphemeralMessageID != 0 && m.ReceiverUser != nil {
		// Only its receiver sees an ephemeral answer.
		p.OriginUserID = strconv.FormatInt(m.ReceiverUser.ID, 10)
		return p
	}
	if m != nil && m.ReplyToMessage != nil {
		cmd := m.ReplyToMessage
		switch {
		case cmd.SenderChat != nil && cmd.SenderChat.ID == chat.ID:
			p.OriginAnonymous = true
		case cmd.From != nil:
			p.OriginUserID = strconv.FormatInt(cmd.From.ID, 10)
		}
	}
	return p
}

// command turns a message into a command, or reports false when it is not
// a command for this bot.
func (t *Telegram) command(msg *telego.Message) (messenger.Command, bool) {
	name, args, ok := parseCommand(msg.Text, t.self.Username)
	if !ok {
		return messenger.Command{}, false
	}
	c := messenger.Command{
		Messenger: Name,
		ChatID:    strconv.FormatInt(msg.Chat.ID, 10),
		ChatTitle: msg.Chat.Title,
		IsPrivate: msg.Chat.Type == telego.ChatTypePrivate,
		IsForum:   msg.Chat.IsForum,
		Name:      name,
		Args:      args,
		Ref:       msg,
	}
	if msg.IsTopicMessage && msg.MessageThreadID != 0 {
		c.ThreadID = strconv.Itoa(msg.MessageThreadID)
	}
	// An anonymous administrator posts as the chat itself; nobody can be
	// matched to the command, so UserID stays empty.
	anonymous := msg.SenderChat != nil && msg.SenderChat.ID == msg.Chat.ID
	if msg.From != nil && !anonymous {
		c.UserID = strconv.FormatInt(msg.From.ID, 10)
	}
	return c, true
}

// parseCommand splits "/cmd@BotName arg1 arg2" into "cmd" and its arguments.
// Returns ok=false for commands addressed to another bot.
func parseCommand(text, botUsername string) (command string, args []string, ok bool) {
	fields := strings.Fields(text)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return "", nil, false
	}
	command = strings.TrimPrefix(fields[0], "/")
	if name, target, found := strings.Cut(command, "@"); found {
		if !strings.EqualFold(target, botUsername) {
			return "", nil, false
		}
		command = name
	}
	return strings.ToLower(command), fields[1:], true
}

// contact records the result of a Telegram API call.
func (t *Telegram) contact(err error) {
	t.healthMu.Lock()
	defer t.healthMu.Unlock()
	if err != nil {
		t.lastErr = t.redact(err)
		return
	}
	t.lastContact, t.lastErr = time.Now(), nil
}

// redacted is an error whose text has the bot token cut out; errors.Is and
// errors.As still see the original.
type redacted struct {
	text string
	err  error
}

func (r *redacted) Error() string { return r.text }
func (r *redacted) Unwrap() error { return r.err }

// redact hides the bot token in err's text: net/http errors quote the
// request URL, which carries it (https://api.telegram.org/bot<token>/...).
func (t *Telegram) redact(err error) error {
	if err == nil || t.token == "" || !strings.Contains(err.Error(), t.token) {
		return err
	}
	return &redacted{text: strings.ReplaceAll(err.Error(), t.token, "<token>"), err: err}
}

// Health reports whether the Telegram API answered recently. When the last
// successful call is older than staleAfter it calls getMe once to find out.
func (t *Telegram) Health(ctx context.Context) error {
	t.healthMu.Lock()
	err := telegramHealth(t.lastContact, t.lastErr, time.Now())
	t.healthMu.Unlock()
	if err == nil || !t.probe.Due() {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, callErr := t.api.GetMe(callCtx)
	t.contact(callErr)
	t.healthMu.Lock()
	defer t.healthMu.Unlock()
	return telegramHealth(t.lastContact, t.lastErr, time.Now())
}

func telegramHealth(lastContact time.Time, lastErr error, now time.Time) error {
	if now.Sub(lastContact) < staleAfter {
		return nil
	}
	ago := now.Sub(lastContact).Round(time.Second)
	if lastErr != nil {
		return fmt.Errorf("no successful Telegram API call for %s: %w", ago, lastErr)
	}
	return fmt.Errorf("no successful Telegram API call for %s", ago)
}

// chatID parses a stored chat ID.
func chatID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid Telegram chat ID %q", messenger.ErrRejected, s)
	}
	return id, nil
}

// threadID parses a stored thread ID ("" = the main chat).
func threadID(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	id, err := strconv.Atoi(s)
	if err != nil || id < 0 {
		return 0, fmt.Errorf("%w: invalid Telegram topic ID %q", messenger.ErrThreadNotFound, s)
	}
	return id, nil
}
