// Package bot implements the Telegram side of the notifier: command
// handling (long polling) and message delivery.
package bot

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/storage"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoutil"
)

// Bot handles Telegram commands and sends repository event notifications.
type Bot struct {
	api     *telego.Bot
	token   string
	self    *telego.User
	lang    i18n.Lang
	store   *storage.Store
	version string
}

// NewBot creates a Bot with an optional proxy configured from environment variables.
func NewBot(lang i18n.Lang, store *storage.Store, version string) (*Bot, error) {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		return nil, errors.New(i18n.T(lang, i18n.MsgBotTokenIsNotSet))
	}

	api, err := telego.NewBot(token, telego.WithHTTPClient(newHTTPClient(lang)))
	if err != nil {
		return nil, fmt.Errorf(i18n.T(lang, i18n.MsgFailedToCreateBot), err)
	}

	self, err := api.GetMe(context.Background())
	if err != nil {
		return nil, fmt.Errorf(i18n.T(lang, i18n.MsgFailedToGetBotInfo), err)
	}

	bot := &Bot{
		api:     api,
		token:   token,
		self:    self,
		lang:    lang,
		store:   store,
		version: version,
	}

	log.Printf(i18n.T(lang, i18n.MsgBotRegistered), bot.self.Username)
	return bot, nil
}

// newHTTPClient creates an *http.Client with an optional HTTP or SOCKS5 proxy.
func newHTTPClient(lang i18n.Lang) *http.Client {
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

	transport := http.DefaultTransport.(*http.Transport).Clone()

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

	return &http.Client{Transport: transport}
}

// Username returns the bot username.
func (b *Bot) Username() string {
	return b.self.Username
}

// Start begins long polling for incoming updates until ctx is cancelled.
func (b *Bot) Start(ctx context.Context) {
	log.Printf(i18n.T(b.lang, i18n.MsgBotStarted), b.self.Username)

	b.registerCommands(ctx)

	updates, err := b.api.UpdatesViaLongPolling(ctx, &telego.GetUpdatesParams{
		Timeout: 60,
	})
	if err != nil {
		log.Fatalf(i18n.T(b.lang, i18n.MsgBotStartFailed), err)
	}

	go func() {
		for update := range updates {
			b.consume(update)
		}
	}()
}

// registerCommands publishes the command list shown in the Telegram "Menu" button.
func (b *Bot) registerCommands(ctx context.Context) {
	for _, lang := range i18n.Supported {
		var commands []telego.BotCommand
		for _, name := range commandNames {
			commands = append(commands, telego.BotCommand{
				Command:     name,
				Description: i18n.T(lang, "cmd_"+name),
			})
		}

		params := &telego.SetMyCommandsParams{Commands: commands}
		if lang != i18n.EN {
			params.LanguageCode = string(lang)
		}
		if err := b.api.SetMyCommands(ctx, params); err != nil {
			log.Printf(i18n.T(b.lang, i18n.MsgSetCommandsFailed), err)
		}
	}
}

// consume processes a single Telegram update.
func (b *Bot) consume(update telego.Update) {
	msg := update.Message
	if msg == nil || msg.Text == "" || !strings.HasPrefix(msg.Text, "/") {
		return
	}

	command, args, ok := parseCommand(msg.Text, b.self.Username)
	if !ok {
		return
	}

	userName := ""
	if msg.From != nil {
		// From is nil for anonymous group admins and channel posts.
		userName = msg.From.Username
	}
	log.Printf(i18n.T(b.lang, i18n.MsgNewMessage), userName, msg.Chat.ID, msg.Text)

	b.handleCommand(msg, command, args)
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

// ensureChat makes sure the chat exists in the database (repos and topics
// reference it), keeping its stored language if it already exists.
func (b *Bot) ensureChat(chat telego.Chat) error {
	return b.store.SaveChat(storage.ChatConfig{
		ChatID:   chat.ID,
		Language: string(b.chatLang(chat.ID)),
		IsForum:  chat.IsForum,
	})
}

// reply sends a message to the chat (and forum topic) the command came from.
func (b *Bot) reply(msg *telego.Message, text string) {
	topicID := 0
	if msg.IsTopicMessage {
		topicID = msg.MessageThreadID
	}
	_ = b.SendMessageToTopic(msg.Chat.ID, topicID, text)
}

// SendMessageToTopic sends an HTML message to a forum topic.
// If topicID is 0, the message goes to the main (General) chat.
func (b *Bot) SendMessageToTopic(chatID int64, topicID int, text string) error {
	params := &telego.SendMessageParams{
		ChatID:    telegoutil.ID(chatID),
		Text:      text,
		ParseMode: telego.ModeHTML,
		LinkPreviewOptions: &telego.LinkPreviewOptions{
			IsDisabled: true,
		},
	}
	if topicID > 0 {
		params.MessageThreadID = topicID
	}

	_, err := b.api.SendMessage(context.Background(), params)
	if err != nil {
		log.Printf(i18n.T(b.lang, i18n.MsgSendFailed), chatID, err)
	}
	return err
}

// SendMessage sends an HTML message to the main chat.
func (b *Bot) SendMessage(chatID int64, text string) error {
	return b.SendMessageToTopic(chatID, 0, text)
}

// isBotAdmin checks if the bot is an administrator in the given chat.
func (b *Bot) isBotAdmin(chatID int64) bool {
	member, err := b.api.GetChatMember(context.Background(), &telego.GetChatMemberParams{
		ChatID: telegoutil.ID(chatID),
		UserID: b.self.ID,
	})
	if err != nil {
		log.Print(i18n.Tf(b.lang, i18n.MsgFailedToCheckBotAdminStatus, err))
		return false
	}

	status := member.MemberStatus()
	return status == telego.MemberStatusCreator || status == telego.MemberStatusAdministrator
}
