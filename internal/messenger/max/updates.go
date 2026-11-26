package max

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
)

// UpdatesPath is where MAX posts updates when the webhook is used.
const UpdatesPath = "/max/updates"

// updateTypes are the updates the bot asks for.
var updateTypes = []string{"message_created", "message_callback", "bot_started"}

const (
	pollTimeout = 30 // seconds MAX holds a long poll
	// handleTimeout bounds the work on one update: a MAX call that never
	// answers must not stop the polling loop or pile up goroutines.
	handleTimeout = 2 * time.Minute
	// setupTimeout bounds the calls made once on start.
	setupTimeout = 30 * time.Second
	maxCommands  = 32
)

// retryPause is the wait after a failed poll; a variable for tests.
var retryPause = 5 * time.Second

// update is a MAX update; only the fields the adapter reads.
type update struct {
	UpdateType string   `json:"update_type"`
	Message    *message `json:"message"`
	Callback   *struct {
		CallbackID string `json:"callback_id"`
		Payload    string `json:"payload"`
		User       *user  `json:"user"`
	} `json:"callback"`
	ChatID    int64 `json:"chat_id"`
	User      *user `json:"user"`
	Timestamp int64 `json:"timestamp"`
}

// key tells updates apart, so one MAX sends again is not handled twice;
// "" when nothing does.
func (u update) key() string {
	switch {
	case u.UpdateType == "message_created" && u.Message != nil && u.Message.Body.Mid != "":
		return "m:" + u.Message.Body.Mid
	case u.UpdateType == "message_callback" && u.Callback != nil && u.Callback.CallbackID != "":
		return "c:" + u.Callback.CallbackID
	case u.UpdateType == "bot_started" && u.Timestamp != 0:
		return "s:" + strconv.FormatInt(u.ChatID, 10) + ":" + strconv.FormatInt(u.Timestamp, 10)
	}
	return ""
}

// seenMax is how many update keys polling remembers.
const seenMax = 1000

// seen remembers the last seenMax update keys.
type seen struct {
	keys  map[string]bool
	order []string
}

// add reports whether k is new, and remembers it.
func (s *seen) add(k string) bool {
	if k == "" {
		return true
	}
	if s.keys[k] {
		return false
	}
	if s.keys == nil {
		s.keys = map[string]bool{}
	}
	s.keys[k] = true
	s.order = append(s.order, k)
	if len(s.order) > seenMax {
		delete(s.keys, s.order[0])
		s.order = s.order[1:]
	}
	return true
}

// webhookURL is where MAX should post updates, when public_url allows a
// webhook: MAX takes HTTPS on port 443 only.
func webhookURL(publicURL string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || (u.Port() != "" && u.Port() != "443") {
		return "", false
	}
	// Only the scheme, host and path are kept: MAX takes no port but 443,
	// and a query or fragment would end up before the path.
	u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = "", false, "", ""
	host := u.Hostname()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	u.Host = host
	return strings.TrimRight(u.String(), "/") + UpdatesPath, true
}

// secretFor derives the webhook secret from the token, so it needs no
// setting of its own and changes with the token.
func secretFor(token string) string {
	sum := sha256.Sum256([]byte("max-webhook:" + token))
	return hex.EncodeToString(sum[:])[:40]
}

func (m *Max) setHandler(ctx context.Context, h messenger.Handler) {
	m.mu.Lock()
	m.h, m.ctx = h, ctx
	m.mu.Unlock()
}

// Run sets the command menu, then takes updates by webhook when
// public_url allows one, otherwise by long polling, until ctx is done.
func (m *Max) Run(ctx context.Context, h messenger.Handler) error {
	m.setHandler(ctx, h)
	log.Printf(i18n.T(m.lang, i18n.MsgBotStarted), m.self)
	if l, ok := h.(messenger.CommandLister); ok {
		go m.registerCommands(ctx, l)
	}
	if hook, ok := webhookURL(m.publicURL); ok {
		if err := m.subscribe(ctx, hook); err != nil {
			log.Printf("❌ MAX: subscribing %s: %v; falling back to long polling", hook, err)
		} else {
			log.Printf("🔗 MAX: updates come to %s", hook)
			<-ctx.Done()
			return nil
		}
	}
	m.poll(ctx, h)
	return nil
}

// UsesWebhook reports whether Run will take updates on UpdatesPath.
func (m *Max) UsesWebhook() bool {
	_, ok := webhookURL(m.publicURL)
	return ok
}

// subscribe asks MAX to post updates to hook with the secret.
func (m *Max) subscribe(ctx context.Context, hook string) error {
	body := map[string]any{"url": hook, "update_types": updateTypes, "secret": m.secret}
	ctx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()
	return m.c.do(ctx, http.MethodPost, "/subscriptions", nil, body, nil)
}

// unsubscribe removes every webhook of the bot: MAX does not serve long
// polling while one is set. It reports whether all of them are gone.
func (m *Max) unsubscribe(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()
	var out struct {
		Subscriptions []struct {
			URL string `json:"url"`
		} `json:"subscriptions"`
	}
	if err := m.c.do(ctx, http.MethodGet, "/subscriptions", nil, nil, &out); err != nil {
		log.Printf("⚠️ MAX: listing webhooks: %v", err)
		return false
	}
	ok := true
	for _, s := range out.Subscriptions {
		if err := m.c.do(ctx, http.MethodDelete, "/subscriptions", url.Values{"url": {s.URL}}, nil, nil); err != nil {
			log.Printf("⚠️ MAX: removing the webhook %s: %v", s.URL, err)
			ok = false
		}
	}
	return ok
}

// poll takes updates by long polling until ctx is done. Webhooks are
// removed first, and again after a failed poll until that works.
func (m *Max) poll(ctx context.Context, h messenger.Handler) {
	var marker *int64
	var done seen
	cleared := false
	for ctx.Err() == nil {
		if !cleared {
			cleared = m.unsubscribe(ctx)
		}
		q := url.Values{"timeout": {strconv.Itoa(pollTimeout)}, "types": {strings.Join(updateTypes, ",")}}
		if marker != nil {
			q.Set("marker", strconv.FormatInt(*marker, 10))
		}
		var out struct {
			Updates []update `json:"updates"`
			Marker  *int64   `json:"marker"`
		}
		callCtx, cancel := context.WithTimeout(ctx, (pollTimeout+15)*time.Second)
		err := m.c.do(callCtx, http.MethodGet, "/updates", q, nil, &out)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("❌ MAX: getting updates: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryPause):
			}
			continue
		}
		cleared = true
		if out.Marker != nil {
			marker = out.Marker
		}
		for _, u := range out.Updates {
			if done.add(u.key()) {
				m.consume(ctx, h, u)
			}
		}
	}
}

// ServeHTTP takes a webhook update. A request without the right secret is
// refused; the update is handled after the answer, since MAX waits for it.
func (m *Max) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	got := r.Header.Get("X-Max-Bot-Api-Secret")
	if m.secret == "" || subtle.ConstantTimeCompare([]byte(got), []byte(m.secret)) != 1 {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var u update
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || json.Unmarshal(raw, &u) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	h, ctx := m.h, m.ctx
	m.mu.Unlock()
	if h == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	go m.consume(ctx, h, u)
}

// consume passes a command or a button press to the core.
func (m *Max) consume(ctx context.Context, h messenger.Handler, u update) {
	ctx, cancel := context.WithTimeout(ctx, handleTimeout)
	defer cancel()
	switch u.UpdateType {
	case "bot_started":
		if u.ChatID == 0 || u.User.id() == "" {
			return
		}
		chat := strconv.FormatInt(u.ChatID, 10)
		m.sawDialog(u.User.id(), chat)
		h.HandleCommand(ctx, messenger.Command{Messenger: Name, ChatID: chat, UserID: u.User.id(), IsPrivate: true, Name: "start"})
	case "message_created":
		if c, ok := m.command(ctx, u.Message); ok {
			log.Printf(i18n.T(m.lang, i18n.MsgNewMessage), u.Message.Sender.Username, c.ChatID, logged(c, u.Message.Body.Text))
			h.HandleCommand(ctx, c)
		}
	case "message_callback":
		if u.Callback == nil || u.Message == nil || u.Callback.User.id() == "" {
			return
		}
		msg := u.Message
		chat := strconv.FormatInt(msg.Recipient.ChatID, 10)
		dialog := msg.Recipient.ChatType == chatDialog
		if dialog {
			m.sawDialog(u.Callback.User.id(), chat)
		}
		p := messenger.Press{
			Messenger: Name,
			ChatID:    chat,
			UserID:    u.Callback.User.id(),
			Data:      u.Callback.Payload,
			IsPrivate: dialog,
			Ref:       &pressState{callbackID: u.Callback.CallbackID, msg: msg},
		}
		if msg.Link != nil && msg.Link.Type == "reply" {
			p.OriginUserID = msg.Link.Sender.id()
		}
		h.HandlePress(ctx, p)
	}
}

// command turns a message into a command, or reports false when it is not
// a command for this bot. Channel posts are not commands.
func (m *Max) command(ctx context.Context, msg *message) (messenger.Command, bool) {
	if msg == nil || msg.Sender.id() == "" || msg.Sender.IsBot || msg.Recipient.ChatType == chatChannel {
		return messenger.Command{}, false
	}
	name, args, ok := parseCommand(msg.Body.Text, m.self)
	if !ok {
		return messenger.Command{}, false
	}
	chat := strconv.FormatInt(msg.Recipient.ChatID, 10)
	c := messenger.Command{
		Messenger: Name,
		ChatID:    chat,
		UserID:    msg.Sender.id(),
		IsPrivate: msg.Recipient.ChatType == chatDialog,
		Name:      name,
		Args:      args,
		Ref:       msg,
	}
	if c.IsPrivate {
		m.sawDialog(c.UserID, chat)
	} else if ch, err := m.chat(ctx, chat); err == nil {
		c.ChatTitle = ch.Title
	}
	return c, true
}

// parseCommand splits "/cmd@bot arg1 arg2" into "cmd" and its arguments;
// ok is false for text that is not a command or is meant for another bot.
func parseCommand(text, username string) (string, []string, bool) {
	fields := strings.Fields(text)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") || len(fields[0]) == 1 {
		return "", nil, false
	}
	name := strings.TrimPrefix(fields[0], "/")
	if n, target, found := strings.Cut(name, "@"); found {
		if !strings.EqualFold(target, username) {
			return "", nil, false
		}
		name = n
	}
	return strings.ToLower(name), fields[1:], true
}

// logged is the text of a command as the log shows it: the arguments of
// /link never reach the log.
func logged(c messenger.Command, text string) string {
	if c.Name != "link" || len(c.Args) == 0 {
		return text
	}
	if p := strings.ToLower(c.Args[0]); p == "github" || p == "gitlab" {
		return "/link " + p + " [hidden]"
	}
	return "/link [hidden]"
}

// registerCommands sets the command menu, in the default language: MAX
// has one menu for everyone.
func (m *Max) registerCommands(ctx context.Context, l messenger.CommandLister) {
	type command struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}
	var cmds []command
	for _, c := range l.Commands(m.lang) {
		if len(cmds) == maxCommands {
			break
		}
		cmds = append(cmds, command{Name: c.Name, Description: truncate(c.Description, 128)})
	}
	ctx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()
	if err := m.c.do(ctx, http.MethodPatch, "/me/commands", nil, map[string]any{"commands": cmds}, nil); err != nil {
		log.Printf(i18n.T(m.lang, i18n.MsgSetCommandsFailed), err)
	}
}
