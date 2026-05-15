package bot

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoutil"
)

// Bot handles Telegram messages and sends repository event notifications.
//
// Основной Telegram-бот для GitHub-GitLab Notifier.
type Bot struct {
	api   *telego.Bot
	token string
	self  *telego.User
	lang  i18n.Lang
}

// NewBot creates a Bot with optional proxy from environment variables.
//
// Создаёт новый экземпляр Bot и проверяет токен.
func NewBot(lang i18n.Lang) (*Bot, error) {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf(i18n.T(lang, i18n.MsgBotTokenIsNotSet))
	}

	httpClient := newHTTPClient(lang)

	api, err := telego.NewBot(token, telego.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf(i18n.T(lang, i18n.MsgFailedToCreateBot), err)
	}

	self, err := api.GetMe(context.Background())
	if err != nil {
		return nil, fmt.Errorf(i18n.T(lang, i18n.MsgFailedToGetBotInfo), err)
	}

	bot := &Bot{
		api:   api,
		token: token,
		self:  self,
		lang:  lang,
	}

	log.Printf(i18n.T(lang, i18n.MsgBotRegistered), bot.self.Username)
	return bot, nil
}

// newHTTPClient creates an *http.Client with optional proxy.
//
// Создаёт *http.Client с опциональным прокси.
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

// Token returns the bot token.
//
// Возвращает токен бота.
func (b *Bot) Token() string {
	return b.token
}

// Username returns the bot username.
//
// Возвращает имя пользователя бота.
func (b *Bot) Username() string {
	return b.self.Username
}

// Lang returns the current language.
//
// Возвращает текущий язык.
func (b *Bot) Lang() i18n.Lang {
	return b.lang
}

// Start begins Long Polling for incoming updates.
//
// Запускает Long Polling для входящих обновлений.
func (b *Bot) Start() {
	log.Printf(i18n.T(b.lang, i18n.MsgBotStarted), b.self.Username)

	ctx := context.Background()

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

// consume processes a single Telegram update.
//
// Обрабатывает одно обновление Telegram.
func (b *Bot) consume(update telego.Update) {
	if update.Message == nil || update.Message.Text == "" {
		return
	}

	chatID := update.Message.Chat.ID
	userName := update.Message.From.Username
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

// SendMessage sends a text message with HTML parse mode to the specified chat.
//
// Отправляет текстовое сообщение с HTML-разметкой в указанный чат.
func (b *Bot) SendMessage(chatID int64, text string) {
	_, err := b.api.SendMessage(context.Background(), &telego.SendMessageParams{
		ChatID:    telegoutil.ID(chatID),
		Text:      text,
		ParseMode: telego.ModeHTML,
	})
	if err != nil {
		log.Printf(i18n.T(b.lang, i18n.MsgSendFailed), chatID, err)
	}
}
