package bot

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Bot handles Telegram messages and sends repository event notifications.
type Bot struct {
	api   *tgbotapi.BotAPI
	token string
	self  tgbotapi.User
	lang  i18n.Lang
}

// NewBot creates a Bot with optional proxy from environment variables.
func NewBot(lang i18n.Lang) (*Bot, error) {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("BOT_TOKEN is not set")
	}

	httpClient := newHTTPClient(lang)

	api, err := tgbotapi.NewBotAPIWithClient(token, tgbotapi.APIEndpoint, httpClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	bot := &Bot{
		api:   api,
		token: token,
		self:  api.Self,
		lang:  lang,
	}

	log.Printf(i18n.T(lang, i18n.MsgBotRegistered), bot.self.UserName)
	return bot, nil
}

// newHTTPClient creates an *http.Client with optional proxy.
func newHTTPClient(lang i18n.Lang) *http.Client {
	proxyEnabled, _ := strconv.ParseBool(os.Getenv("PROXY_ENABLED"))
	proxyHost := os.Getenv("PROXY_HOST")
	proxyPort := os.Getenv("PROXY_PORT")
	proxyType := os.Getenv("PROXY_TYPE")
	if proxyType == "" {
		proxyType = "HTTP"
	}

	transport := &http.Transport{}

	if proxyEnabled && proxyHost != "" && proxyPort != "" {
		proxyURL := fmt.Sprintf("%s://%s:%s", proxyType, proxyHost, proxyPort)
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			log.Printf("⚠️ Invalid proxy URL: %v", err)
		} else {
			transport.Proxy = http.ProxyURL(parsed)
			log.Printf(i18n.T(lang, i18n.MsgProxyEnabled), proxyType, proxyHost, proxyPort)
		}
	} else {
		log.Println(i18n.T(lang, i18n.MsgDirectConnection))
	}

	return &http.Client{Transport: transport}
}

// Token returns the bot token.
func (b *Bot) Token() string {
	return b.token
}

// Username returns the bot username.
func (b *Bot) Username() string {
	return b.self.UserName
}

// Lang returns the current language.
func (b *Bot) Lang() i18n.Lang {
	return b.lang
}

// Start begins Long Polling.
func (b *Bot) Start() {
	log.Printf(i18n.T(b.lang, i18n.MsgBotStarted), b.self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	go func() {
		for update := range updates {
			b.consume(update)
		}
	}()
}

// consume processes incoming messages.
func (b *Bot) consume(update tgbotapi.Update) {
	if update.Message == nil || update.Message.Text == "" {
		return
	}

	chatID := update.Message.Chat.ID
	userName := update.Message.From.UserName
	text := update.Message.Text

	log.Printf(i18n.T(b.lang, i18n.MsgNewMessage), userName, chatID, text)

	var response string

	switch {
	case text == "/start":
		response = i18n.T(b.lang, i18n.MsgWelcome)

	case text == "/help":
		response = i18n.T(b.lang, i18n.MsgHelp)

	case len(text) >= 8 && text[:8] == "/connect":
		response = i18n.T(b.lang, i18n.MsgConnectDev)

	default:
		response = i18n.T(b.lang, i18n.MsgUnknownCmd)
	}

	b.SendMessage(chatID, response)
}

// SendMessage sends a message with HTML parse mode.
func (b *Bot) SendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML

	if _, err := b.api.Send(msg); err != nil {
		log.Printf(i18n.T(b.lang, i18n.MsgSendFailed), chatID, err)
	}
}
