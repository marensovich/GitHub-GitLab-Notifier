package max

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"GitHub-GitLab-Notifier/internal/config"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
)

// Name is the messenger name used in the database and the config.
const Name = config.MessengerMax

// FormatHTML is the only look a MAX message has.
const FormatHTML = "html"

// staleAfter is how long without an answer from MAX Health asks again.
const staleAfter = 3 * time.Minute

// Max is the MAX adapter. It sends through the Bot API and receives
// updates by webhook (it is the http.Handler of UpdatesPath) or by long
// polling.
type Max struct {
	c         *client
	lang      i18n.Lang
	self      string // the bot's username
	secret    string // the webhook secret
	publicURL string

	mu      sync.Mutex
	h       messenger.Handler
	ctx     context.Context
	chats   map[string]chatInfo
	dialogs map[string]string // user ID -> chat ID of the dialog with the bot
}

var (
	_ messenger.Messenger       = (*Max)(nil)
	_ messenger.MemberChecker   = (*Max)(nil)
	_ messenger.DirectMessenger = (*Max)(nil)
	_ http.Handler              = (*Max)(nil)
)

// user is a MAX user as updates carry it.
type user struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

func (u *user) id() string {
	if u == nil || u.UserID == 0 {
		return ""
	}
	return strconv.FormatInt(u.UserID, 10)
}

type recipient struct {
	ChatID   int64  `json:"chat_id"`
	ChatType string `json:"chat_type"`
	UserID   int64  `json:"user_id"`
}

type body struct {
	Mid  string `json:"mid"`
	Text string `json:"text"`
}

// linked is the message a message replies to or forwards.
type linked struct {
	Type    string `json:"type"`
	Sender  *user  `json:"sender"`
	ChatID  int64  `json:"chat_id"`
	Message *body  `json:"message"`
}

// message is a MAX message; it is the Ref of a Command.
type message struct {
	Sender    *user     `json:"sender"`
	Recipient recipient `json:"recipient"`
	Body      body      `json:"body"`
	Link      *linked   `json:"link"`
}

// pressState is the Ref of a Press: the callback and the message its
// button belongs to.
type pressState struct {
	callbackID string
	msg        *message

	mu       sync.Mutex
	answered bool
}

// New connects to MAX with cfg and checks the token. publicURL is
// server.public_url: with HTTPS on port 443 updates come by webhook.
func New(cfg config.Max, lang i18n.Lang, publicURL string) (*Max, error) {
	base := cfg.APIURL
	if base == "" {
		base = APIBase
	}
	m := newWithClient(newClient(base, cfg.Token), lang)
	m.secret, m.publicURL = secretFor(cfg.Token), publicURL
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var me user
	if err := m.c.do(ctx, http.MethodGet, "/me", nil, nil, &me); err != nil {
		return nil, fmt.Errorf("max: checking the token: %w", err)
	}
	m.self = me.Username
	return m, nil
}

func newWithClient(c *client, lang i18n.Lang) *Max {
	return &Max{c: c, lang: lang, chats: map[string]chatInfo{}, dialogs: map[string]string{}}
}

// Name implements messenger.Messenger.
func (m *Max) Name() string { return Name }

// Caps implements messenger.Messenger.
func (m *Max) Caps() messenger.Caps {
	return messenger.Caps{
		ButtonsPerRow: maxButtons,
		PerChatRate:   2,
		PerChatBurst:  2,
		GlobalRate:    25,
	}
}

// CanManageThreads implements messenger.Messenger: MAX has no threads.
func (m *Max) CanManageThreads(context.Context, string) (bool, error) { return false, nil }

// CreateThread implements messenger.Messenger: MAX has no threads.
func (m *Max) CreateThread(context.Context, string, string) (string, error) {
	return "", errors.New("max: chats have no threads")
}

// Health reports whether MAX answers. After staleAfter without an answer
// it asks once (GET /me).
func (m *Max) Health(ctx context.Context) error {
	if err := health(m.c.contact()); err == nil {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = m.c.do(callCtx, http.MethodGet, "/me", nil, nil, nil)
	return health(m.c.contact())
}

func health(lastOK time.Time, lastErr error) error {
	if time.Since(lastOK) < staleAfter {
		return nil
	}
	if lastErr != nil {
		return fmt.Errorf("no answer from MAX since %s: %w", lastOK.UTC().Format(time.RFC3339), lastErr)
	}
	return errors.New("no answer from MAX yet")
}
