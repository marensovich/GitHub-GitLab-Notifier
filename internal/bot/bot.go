// Package bot implements the MAX messenger side of the notifier: command
// handling (long polling) and message delivery.
package bot

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/maxapi"
	"GitHub-GitLab-Notifier/storage"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// pollTimeout is the long-polling timeout passed to GET /updates (seconds).
	pollTimeout = 30
	// minSendInterval keeps us under the MAX limit of 2 messages per second per chat.
	minSendInterval = 550 * time.Millisecond
	// sendTimeout bounds a single POST /messages call.
	sendTimeout = 20 * time.Second
)

// incoming is a command message received from MAX.
type incoming struct {
	ChatID   int64
	ChatType string
	Text     string
	From     string
}

// Bot handles MAX commands and sends repository event notifications.
type Bot struct {
	api     *maxapi.Client
	self    *maxapi.User
	lang    i18n.Lang
	store   *storage.Store
	version string

	mu       sync.Mutex
	nextSend map[int64]time.Time // per-chat send slots (rate limiting)
}

// NewBot creates a Bot with an optional proxy and extra CA configured from
// environment variables.
func NewBot(lang i18n.Lang, store *storage.Store, version string) (*Bot, error) {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		return nil, errors.New(i18n.T(lang, i18n.MsgBotTokenIsNotSet))
	}

	httpClient, err := newHTTPClient(lang)
	if err != nil {
		return nil, fmt.Errorf(i18n.T(lang, i18n.MsgFailedToCreateBot), err)
	}
	api := maxapi.New(os.Getenv("MAX_API_URL"), token, httpClient)

	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	self, err := api.GetMe(ctx)
	if err != nil {
		return nil, fmt.Errorf(i18n.T(lang, i18n.MsgFailedToGetBotInfo), err)
	}

	bot := &Bot{
		api:      api,
		self:     self,
		lang:     lang,
		store:    store,
		version:  version,
		nextSend: make(map[int64]time.Time),
	}

	log.Printf(i18n.T(lang, i18n.MsgBotRegistered), bot.Username())
	return bot, nil
}

// newHTTPClient creates an *http.Client with an optional HTTP or SOCKS5 proxy
// and optional extra trusted CA certificates (MAX_CA_FILE).
func newHTTPClient(lang i18n.Lang) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	// platform-api2.max.ru uses a certificate from the Russian Trusted Root CA,
	// which is missing from many system trust stores.
	if caFile := strings.TrimSpace(os.Getenv("MAX_CA_FILE")); caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read MAX_CA_FILE: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("MAX_CA_FILE %s: no PEM certificates found", caFile)
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}

	proxyEnabled, _ := strconv.ParseBool(os.Getenv("PROXY_ENABLED"))
	proxyHost := os.Getenv("PROXY_HOST")
	proxyPort := os.Getenv("PROXY_PORT")
	proxyType := strings.ToLower(os.Getenv("PROXY_TYPE"))
	switch proxyType {
	case "", "http":
		proxyType = "http"
	case "socks", "socks5":
		proxyType = "socks5"
	}

	if proxyEnabled && proxyHost != "" && proxyPort != "" {
		proxyURL := fmt.Sprintf("%s://%s:%s", proxyType, proxyHost, proxyPort)
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			log.Printf(i18n.T(lang, i18n.MsgInvalidProxyUrl), err)
		} else {
			transport.Proxy = http.ProxyURL(parsed)
			log.Printf(i18n.T(lang, i18n.MsgProxyEnabled), proxyType, proxyHost, proxyPort)
		}
	} else {
		log.Println(i18n.T(lang, i18n.MsgDirectConnection))
	}

	// No client-wide timeout: long polling holds requests open; every call
	// gets its own context deadline instead.
	return &http.Client{Transport: transport}, nil
}

// Username returns the bot username (or its display name if it has none).
func (b *Bot) Username() string {
	if b.self.Username != "" {
		return b.self.Username
	}
	return b.self.FirstName
}

// Start registers the command menu and begins long polling for incoming
// updates until ctx is cancelled.
func (b *Bot) Start(ctx context.Context) {
	log.Printf(i18n.T(b.lang, i18n.MsgBotStarted), b.Username())
	b.registerCommands(ctx)
	go b.poll(ctx)
}

// poll runs the GET /updates loop.
func (b *Bot) poll(ctx context.Context) {
	var marker *int64
	backoff := time.Second

	for ctx.Err() == nil {
		reqCtx, cancel := context.WithTimeout(ctx, (pollTimeout+15)*time.Second)
		list, err := b.api.GetUpdates(reqCtx, marker, pollTimeout, []string{maxapi.UpdateMessageCreated})
		cancel()

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf(i18n.T(b.lang, i18n.MsgPollFailed), err)
			if maxapi.IsRateLimited(err) && backoff < 10*time.Second {
				backoff = 10 * time.Second
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second

		if list.Marker != nil {
			marker = list.Marker
		}
		for _, u := range list.Updates {
			b.consume(u)
		}
	}
}

// registerCommands publishes the command list shown in the MAX bot menu.
// MAX has a single command list per bot, so the default language is used.
func (b *Bot) registerCommands(ctx context.Context) {
	commands := make([]maxapi.BotCommand, 0, len(commandNames))
	for _, name := range commandNames {
		commands = append(commands, maxapi.BotCommand{
			Name:        name,
			Description: i18n.T(b.lang, "cmd_"+name),
		})
	}

	reqCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	if err := b.api.SetCommands(reqCtx, commands); err != nil {
		log.Printf(i18n.T(b.lang, i18n.MsgSetCommandsFailed), err)
	}
}

// consume processes a single MAX update.
func (b *Bot) consume(update maxapi.Update) {
	in, ok := toIncoming(update)
	if !ok || !strings.HasPrefix(in.Text, "/") {
		return
	}

	command, args, ok := parseCommand(in.Text, b.self.Username)
	if !ok {
		return
	}

	log.Printf(i18n.T(b.lang, i18n.MsgNewMessage), in.From, in.ChatID, in.Text)
	b.handleCommand(in, command, args)
}

// toIncoming extracts a text message from an update.
func toIncoming(update maxapi.Update) (incoming, bool) {
	msg := update.Message
	if update.UpdateType != maxapi.UpdateMessageCreated || msg == nil || msg.Recipient.ChatID == nil {
		return incoming{}, false
	}
	text := strings.TrimSpace(msg.Body.Text)
	if text == "" {
		return incoming{}, false
	}

	from := ""
	if msg.Sender != nil {
		// Sender is absent for channel posts.
		from = msg.Sender.Username
		if from == "" {
			from = msg.Sender.FirstName
		}
		if msg.Sender.IsBot {
			return incoming{}, false
		}
	}

	return incoming{
		ChatID:   *msg.Recipient.ChatID,
		ChatType: msg.Recipient.ChatType,
		Text:     text,
		From:     from,
	}, true
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

// chatLang returns the language stored for a chat, or the default language.
func (b *Bot) chatLang(chatID int64) i18n.Lang {
	chat, err := b.store.GetChat(chatID)
	if err == nil {
		if l := i18n.Lang(chat.Language); i18n.Valid(l) {
			return l
		}
	}
	return b.lang
}

// ensureChat makes sure the chat exists in the database (repos reference it),
// keeping its stored language if it already exists. MAX has no forum topics.
func (b *Bot) ensureChat(chatID int64) error {
	return b.store.SaveChat(storage.ChatConfig{
		ChatID:   chatID,
		Language: string(b.chatLang(chatID)),
		IsForum:  false,
	})
}

// reply sends a message to the chat the command came from.
func (b *Bot) reply(in incoming, text string) {
	_ = b.SendMessage(in.ChatID, text)
}

// SendMessageToTopic satisfies server.Sender. MAX has no forum topics, so
// topicID is ignored and the message always goes to the chat itself.
func (b *Bot) SendMessageToTopic(chatID int64, _ int, text string) error {
	return b.SendMessage(chatID, text)
}

// SendMessage sends an HTML message to the chat, respecting the per-chat
// rate limit and retrying once on HTTP 429.
func (b *Bot) SendMessage(chatID int64, text string) error {
	text = truncateRunes(text, maxapi.MaxTextLength)
	opts := maxapi.SendOptions{Format: maxapi.FormatHTML, DisableLinkPreview: true}

	var err error
	for attempt := 0; attempt < 2; attempt++ {
		b.waitSendSlot(chatID)

		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		_, err = b.api.SendMessage(ctx, chatID, text, opts)
		cancel()

		if err == nil || !maxapi.IsRateLimited(err) {
			break
		}
		time.Sleep(time.Second)
	}

	if err != nil {
		log.Printf(i18n.T(b.lang, i18n.MsgSendFailed), chatID, err)
	}
	return err
}

// waitSendSlot blocks until the chat may receive the next message.
func (b *Bot) waitSendSlot(chatID int64) {
	b.mu.Lock()
	now := time.Now()
	slot := b.nextSend[chatID]
	if slot.Before(now) {
		slot = now
	}
	b.nextSend[chatID] = slot.Add(minSendInterval)
	b.mu.Unlock()

	if d := time.Until(slot); d > 0 {
		time.Sleep(d)
	}
}

// truncateRunes cuts s to at most max runes, appending "..." when cut.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-3]) + "..."
}
