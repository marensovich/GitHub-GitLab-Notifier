package bot

import (
	"fmt"
	"log"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Bot Core Telegram bot for the GitHub-GitLab Notifier.
//
// Handles user commands via Long Polling and sends
// repository event notifications.
//
// Основной Telegram-бот для GitHub-GitLab Notifier.
//
// Обрабатывает команды пользователей через Long Polling
// и отправляет уведомления о событиях в репозиториях.
//
// Author: marensovich
// Version: v.0.0.2
// Since: v.0.0.2
type Bot struct {
	api   *tgbotapi.BotAPI
	token string
	self  tgbotapi.User
}

// NewBot creates a new Bot instance and validates the token.
//
// Создаёт новый экземпляр Bot и проверяет токен.
func NewBot() (*Bot, error) {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("BOT_TOKEN is not set")
	}

	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	api.Debug = false // включай true для отладки

	bot := &Bot{
		api:   api,
		token: token,
		self:  api.Self,
	}

	log.Printf("✅ Bot @%s registered. Session running: true", bot.self.UserName)
	return bot, nil
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
	return b.self.UserName
}

// Start begins Long Polling for incoming updates.
// Runs in a separate goroutine and handles commands.
//
// Запускает Long Polling для входящих обновлений.
// Работает в отдельной горутине и обрабатывает команды.
func (b *Bot) Start() {
	log.Printf("🚀 Bot @%s started, listening for commands...", b.self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	go func() {
		for update := range updates {
			b.consume(update)
		}
	}()
}

// consume processes a single Telegram update.
//
// Обрабатывает одно обновление Telegram.
func (b *Bot) consume(update tgbotapi.Update) {
	if update.Message == nil || update.Message.Text == "" {
		return
	}

	chatID := update.Message.Chat.ID
	userName := update.Message.From.UserName
	text := update.Message.Text

	log.Printf("📩 New message from @%s (chat %d): %s", userName, chatID, text)

	var response string

	switch {
	case text == "/start":
		response = "👋 Привет! Я бот для уведомлений из GitLab/GitHub.\n" +
			"Используй /connect <URL> для подключения репозитория.\n" +
			"/help — список команд."

	case text == "/help":
		response = "📚 Доступные команды:\n" +
			"/start — начало работы\n" +
			"/connect <URL> — подключить репозиторий\n" +
			"/status — статус подключённых репозиториев\n" +
			"/help — эта справка"

	case len(text) >= 8 && text[:8] == "/connect":
		response = "🔗 Функция подключения репозитория в разработке.\n" +
			"Скоро здесь будет интеграция с GitHub/GitLab!"

	default:
		response = "Извини, я пока понимаю только команды. Напиши /help"
	}

	b.SendMessage(chatID, response)
}

// SendMessage sends a text message with HTML parse mode to the specified chat.
//
// Отправляет текстовое сообщение с HTML-разметкой в указанный чат.
func (b *Bot) SendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML

	if _, err := b.api.Send(msg); err != nil {
		log.Printf("❌ Failed to send message to chat %d: %v", chatID, err)
	}
}
