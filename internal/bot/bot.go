package bot

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/storage"
	"context"
	"crypto/rand"
	"encoding/hex"
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

// Bot handles Telegram messages and sends repository event notifications.
//
// Основной Telegram-бот для GitHub-GitLab Notifier.
type Bot struct {
	api   *telego.Bot
	token string
	self  *telego.User
	lang  i18n.Lang
	store *storage.Store
}

// NewBot creates a Bot with optional proxy from environment variables.
//
// Создаёт новый экземпляр Bot и проверяет токен.
func NewBot(lang i18n.Lang, store *storage.Store) (*Bot, error) {
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
		store: store,
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
	case strings.HasPrefix(text, "/start"):
		response = i18n.T(b.lang, i18n.MsgWelcome)

	case strings.HasPrefix(text, "/help"):
		response = i18n.T(b.lang, i18n.MsgHelp)

	case strings.HasPrefix(text, "/configure"):
		b.configureChat(update)
		return

	case strings.HasPrefix(text, "/connect"):
		b.handleConnect(update)
		return

	default:
		response = i18n.T(b.lang, i18n.MsgUnknownCmd)
	}

	b.SendMessage(chatID, response)
}

// handleConnect prepares and returns the webhook URL for the user.
// Detects external IP and port automatically.
//
// Обрабатывает команду /connect — подготавливает и возвращает URL вебхука.
// Автоматически определяет внешний IP и порт.
func (b *Bot) handleConnect(update telego.Update) {
	chatID := update.Message.Chat.ID
	text := update.Message.Text

	parts := strings.Fields(text)
	if len(parts) < 2 {
		b.SendMessage(chatID, i18n.T(b.lang, i18n.MsgConnectUsage))
		return
	}

	repoURL := parts[1]

	if !strings.Contains(repoURL, "github.com") && !strings.Contains(repoURL, "gitlab.com") {
		b.SendMessage(chatID, i18n.T(b.lang, i18n.MsgConnectInvalidURL))
		return
	}

	platform := "GitHub"
	if strings.Contains(repoURL, "gitlab.com") {
		platform = "GitLab"
	}

	domain := os.Getenv("DOMAIN")
	if domain == "" {
		b.SendMessage(chatID, i18n.T(b.lang, i18n.MsgConnectNoDomain))
		return
	}

	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimRight(domain, "/")

	if domain == "" {
		b.SendMessage(chatID, i18n.T(b.lang, i18n.MsgConnectNoDomain))
		return
	}

	protocol := "http"
	useHTTPS := os.Getenv("USE_HTTPS")
	if useHTTPS == "true" || useHTTPS == "1" || useHTTPS == "yes" {
		protocol = "https"
	}

	webhookURL := fmt.Sprintf("%s://%s/webhook", protocol, domain)
	secret := b.generateSecret()

	err := b.store.SaveRepo(storage.RepoInfo{
		ChatID:   chatID,
		RepoURL:  repoURL,
		Platform: platform,
		Secret:   secret,
	})
	if err != nil {
		log.Printf("❌ Failed to save repo: %v", err)
		b.SendMessage(chatID, i18n.T(b.lang, i18n.MsgConnectFailed))
		return
	}

	var response strings.Builder

	response.WriteString(i18n.Tf(b.lang, i18n.MsgConnectSuccess, platform, repoURL))
	response.WriteString("\n\n")
	response.WriteString(i18n.T(b.lang, i18n.MsgConnectURL))
	response.WriteString("\n")
	response.WriteString(fmt.Sprintf("<code>%s</code>", webhookURL))
	response.WriteString("\n\n")
	response.WriteString(i18n.T(b.lang, i18n.MsgConnectContentType))
	response.WriteString("\n")
	response.WriteString("<code>application/json</code>")

	if platform == "GitHub" {
		response.WriteString("\n\n")
		response.WriteString(i18n.T(b.lang, i18n.MsgConnectGitHub))
	} else {
		response.WriteString("\n\n")
		response.WriteString(i18n.T(b.lang, i18n.MsgConnectGitLab))
	}

	response.WriteString("\n\n")
	response.WriteString(i18n.Tf(b.lang, i18n.MsgConnectTest, webhookURL))

	b.SendMessage(chatID, response.String())

	log.Printf(i18n.Tf(b.lang, i18n.MsgConnectLogged, chatID, repoURL, webhookURL))
}

// generateSecret creates a random secret for webhook verification.
//
// Создаёт случайный секрет для проверки вебхуков.
func (b *Bot) generateSecret() string {
	bytes := make([]byte, 32)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// getExistingTopics returns a set of existing topic names for the given chat.
//
// Возвращает множество существующих названий тем для указанного чата.
func (b *Bot) getExistingTopicNames(chatID int64) (map[string]bool, error) {
	existingTopics, err := b.store.GetTopicsByChat(chatID)
	if err != nil {
		return nil, fmt.Errorf(i18n.Tf(b.lang, i18n.MsgFailedToGetExistingTopics, err))
	}

	names := make(map[string]bool, len(existingTopics))
	for _, t := range existingTopics {
		names[t.Name] = true
	}
	return names, nil
}

// configureChat sets up a forum chat for repository notifications.
// Skips topics that already exist.
//
// Настраивает чат-форум для уведомлений о репозиториях.
// Пропускает уже существующие темы.
// configureChat sets up a forum chat for repository notifications.
func (b *Bot) configureChat(update telego.Update) {
	chatID := update.Message.Chat.ID

	if !update.Message.Chat.IsForum {
		b.SendMessage(chatID, i18n.T(b.lang, i18n.MsgConfigureChatIsNotForum))
		return
	}

	if !b.isBotAdmin(chatID) {
		b.SendMessage(chatID, i18n.T(b.lang, i18n.MsgConfigureChatNotAdmin))
		return
	}

	err := b.store.SaveChat(storage.ChatConfig{
		ChatID:   chatID,
		Language: string(b.lang),
		IsForum:  true,
	})
	if err != nil {
		log.Printf(i18n.Tf(b.lang, i18n.MsgFailedToSaveChatConfig, err))
	}

	existingNames, err := b.getExistingTopicNames(chatID)
	if err != nil {
		log.Printf(i18n.Tf(b.lang, i18n.MsgFailedToGetExistingTopics, err))
		b.SendMessage(chatID, i18n.T(b.lang, i18n.MsgFailedToGetTopics))
		return
	}

	type topicDef struct {
		name      string
		eventType string
	}

	topics := []topicDef{
		{"⭐ Stars", "star"},
		{"🚀 Pushes", "push"},
	}

	var createdTopics []string
	var skippedTopics []string

	for _, t := range topics {
		if existingNames[t.name] {
			skippedTopics = append(skippedTopics, t.name)
			log.Printf(i18n.Tf(b.lang, i18n.MsgTopicAlreadyExists, t.name))
			continue
		}

		topic, err := b.api.CreateForumTopic(context.Background(), &telego.CreateForumTopicParams{
			ChatID: telegoutil.ID(chatID),
			Name:   t.name,
		})
		if err != nil {
			log.Printf(i18n.Tf(b.lang, i18n.MsgFailedTopicCreate, t.name, err))
			b.SendMessage(chatID, i18n.Tf(b.lang, i18n.MsgTopicCreateFailed, t.name, err.Error()))
			return
		}

		err = b.store.SaveTopic(storage.TopicInfo{
			TopicID:   topic.MessageThreadID,
			ChatID:    chatID,
			Name:      t.name,
			EventType: t.eventType,
		})
		if err != nil {
			log.Printf(i18n.Tf(b.lang, i18n.MsgFailedSaveTopicToStorage, t.name, err))
		}

		err = b.store.SetEventTopic(chatID, t.eventType, topic.MessageThreadID)
		if err != nil {
			log.Printf(i18n.Tf(b.lang, i18n.MsgFailedToMapEventToStorage, t.eventType, topic.MessageThreadID, err))
		}

		createdTopics = append(createdTopics,
			fmt.Sprintf("  • %s — ID: <code>%d</code>", t.name, topic.MessageThreadID))

		log.Printf(i18n.Tf(b.lang, i18n.MsgTopicCreated, t.name, topic.MessageThreadID))
	}

	var response strings.Builder

	if len(createdTopics) > 0 {
		response.WriteString(i18n.T(b.lang, i18n.MsgConfigureChatSuccess))
		response.WriteString("\n\n")
		response.WriteString(strings.Join(createdTopics, "\n"))
		response.WriteString("\n\n")
	}

	if len(skippedTopics) > 0 {
		response.WriteString(i18n.Tf(b.lang, i18n.MsgConfigureChatSkipped, strings.Join(skippedTopics, ", ")))
		response.WriteString("\n\n")
	}

	if len(createdTopics) == 0 && len(skippedTopics) > 0 {
		response.WriteString(i18n.T(b.lang, i18n.MsgConfigureChatAllExist))
		response.WriteString("\n\n")
	}

	response.WriteString(i18n.T(b.lang, i18n.MsgConfigureChatHowToUse))

	b.SendMessageToTopic(chatID, update.Message.MessageThreadID, response.String())
}

// isBotAdmin checks if the bot is an administrator in the given chat.
//
// Проверяет, является ли бот администратором в указанном чате.
func (b *Bot) isBotAdmin(chatID int64) bool {
	member, err := b.api.GetChatMember(context.Background(), &telego.GetChatMemberParams{
		ChatID: telegoutil.ID(chatID),
		UserID: b.self.ID,
	})
	if err != nil {
		log.Printf(i18n.Tf(b.lang, i18n.MsgFailedToCheckBotAdminStatus, err))
		return false
	}

	status := member.MemberStatus()
	return status == "creator" || status == "administrator"
}

// getTopicIDForEvent returns the topic ID for a given event type.
// Returns 0 if no topic is configured (sends to general).
//
// Возвращает ID темы для указанного типа события.
// Возвращает 0, если тема не настроена (отправка в общий чат).
func (b *Bot) getTopicIDForEvent(chatID int64, eventType string) int {
	return b.store.GetTopicByEvent(chatID, eventType)
}

// SendMessageToTopic sends a message to a specific forum topic.
// If topicID is 0, sends to the main chat.
//
// Отправляет сообщение в определённую тему форума.
// Если topicID равен 0, отправляет в общий чат.
func (b *Bot) SendMessageToTopic(chatID int64, topicID int, text string) {
	params := &telego.SendMessageParams{
		ChatID:    telegoutil.ID(chatID),
		Text:      text,
		ParseMode: telego.ModeHTML,
	}

	if topicID > 0 {
		params.MessageThreadID = topicID
	}

	_, err := b.api.SendMessage(context.Background(), params)
	if err != nil {
		log.Printf(i18n.T(b.lang, i18n.MsgSendFailed), chatID, err)
	}
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
