package bot

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/storage"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/mymmrac/telego"
)

// commandNames lists the commands shown in the Telegram menu, in order.
// Each has a "cmd_<name>" translation used as its description.
var commandNames = []string{"start", "help", "configure", "connect", "disconnect", "status", "lang", "version"}

// handleCommand executes a parsed bot command.
func (b *Bot) handleCommand(msg *telego.Message, command string, args []string) {
	lang := b.chatLang(msg.Chat.ID)

	switch command {
	case "start":
		b.reply(msg, i18n.T(lang, i18n.MsgWelcome))
	case "help":
		b.reply(msg, i18n.T(lang, i18n.MsgHelp))
	case "configure":
		b.configureChat(msg)
	case "connect":
		b.handleConnect(msg, args)
	case "disconnect":
		b.handleDisconnect(msg, args)
	case "status":
		b.handleStatus(msg)
	case "lang":
		b.handleLang(msg, args)
	case "version":
		b.reply(msg, i18n.Tf(lang, i18n.MsgVersion, html.EscapeString(b.version)))
	default:
		b.reply(msg, i18n.T(lang, i18n.MsgUnknownCmd))
	}
}

// parseRepoURL validates a repository URL and detects its platform.
// Any https URL with an owner/name path is accepted, so self-hosted GitLab
// instances work too; URLs whose host contains "github" are GitHub, others GitLab.
func parseRepoURL(raw string) (repoURL, platformName string, ok bool) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", "", false
	}

	path := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	if strings.Count(path, "/") < 1 {
		return "", "", false
	}

	platformName = "GitLab"
	if strings.Contains(strings.ToLower(u.Host), "github") {
		platformName = "GitHub"
	}
	return u.Scheme + "://" + u.Host + "/" + path, platformName, true
}

// webhookURL builds the public webhook URL from DOMAIN and USE_HTTPS.
func webhookURL() (string, bool) {
	domain := strings.TrimSpace(os.Getenv("DOMAIN"))
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimRight(domain, "/")
	if domain == "" {
		return "", false
	}

	protocol := "http"
	switch strings.ToLower(os.Getenv("USE_HTTPS")) {
	case "", "true", "1", "yes":
		protocol = "https"
	}
	return fmt.Sprintf("%s://%s/webhook", protocol, domain), true
}

// handleConnect registers a repository for the chat and explains how to
// create the webhook on GitHub/GitLab.
func (b *Bot) handleConnect(msg *telego.Message, args []string) {
	chatID := msg.Chat.ID
	lang := b.chatLang(chatID)

	if len(args) < 1 {
		b.reply(msg, i18n.T(lang, i18n.MsgConnectUsage))
		return
	}

	repoURL, platformName, ok := parseRepoURL(args[0])
	if !ok {
		b.reply(msg, i18n.T(lang, i18n.MsgConnectInvalidURL))
		return
	}

	hookURL, ok := webhookURL()
	if !ok {
		b.reply(msg, i18n.T(lang, i18n.MsgConnectNoDomain))
		return
	}

	if err := b.ensureChat(msg.Chat); err != nil {
		log.Print(i18n.Tf(b.lang, i18n.MsgFailedToSaveChatConfig, err))
		b.reply(msg, i18n.T(lang, i18n.MsgConnectFailed))
		return
	}

	// Keep the secret when re-connecting, otherwise the already configured
	// webhook would stop passing signature verification.
	secret := generateSecret()
	if existing := b.store.FindRepo(repoURL); existing != nil && existing.Secret != "" {
		secret = existing.Secret
		repoURL = existing.RepoURL
	}

	err := b.store.SaveRepo(storage.RepoInfo{
		ChatID:   chatID,
		RepoURL:  repoURL,
		Platform: platformName,
		Secret:   secret,
	})
	if err != nil {
		log.Printf(i18n.T(b.lang, i18n.MsgFailedToSaveRepo), err)
		b.reply(msg, i18n.T(lang, i18n.MsgConnectFailed))
		return
	}

	var sb strings.Builder
	sb.WriteString(i18n.Tf(lang, i18n.MsgConnectSuccess, platformName, html.EscapeString(repoURL)))
	sb.WriteString("\n\n")
	sb.WriteString(i18n.T(lang, i18n.MsgConnectURL))
	sb.WriteString(fmt.Sprintf("\n<code>%s</code>\n\n", html.EscapeString(hookURL)))
	sb.WriteString(i18n.T(lang, i18n.MsgConnectContentType))
	sb.WriteString("\n<code>application/json</code>\n\n")
	sb.WriteString(i18n.Tf(lang, i18n.MsgConnectSecret, secret))
	sb.WriteString("\n\n")
	if platformName == "GitHub" {
		sb.WriteString(i18n.T(lang, i18n.MsgConnectGitHub))
	} else {
		sb.WriteString(i18n.T(lang, i18n.MsgConnectGitLab))
	}
	sb.WriteString("\n\n")
	sb.WriteString(i18n.Tf(lang, i18n.MsgConnectTest, html.EscapeString(hookURL)))

	b.reply(msg, sb.String())
	log.Print(i18n.Tf(b.lang, i18n.MsgConnectLogged, chatID, repoURL, hookURL))
}

// handleDisconnect removes a repository from the chat.
func (b *Bot) handleDisconnect(msg *telego.Message, args []string) {
	lang := b.chatLang(msg.Chat.ID)

	if len(args) < 1 {
		b.reply(msg, i18n.T(lang, i18n.MsgDisconnectUsage))
		return
	}

	deleted, err := b.store.DeleteRepo(msg.Chat.ID, args[0])
	switch {
	case err != nil:
		log.Printf(i18n.T(b.lang, i18n.MsgFailedToSaveRepo), err)
		b.reply(msg, i18n.T(lang, i18n.MsgConnectFailed))
	case !deleted:
		b.reply(msg, i18n.Tf(lang, i18n.MsgDisconnectNotFound, html.EscapeString(args[0])))
	default:
		b.reply(msg, i18n.Tf(lang, i18n.MsgDisconnectSuccess, html.EscapeString(args[0])))
	}
}

// handleStatus shows the chat configuration: language, topics and repositories.
func (b *Bot) handleStatus(msg *telego.Message) {
	chatID := msg.Chat.ID
	lang := b.chatLang(chatID)

	forum := i18n.T(lang, i18n.MsgStatusNo)
	if msg.Chat.IsForum {
		forum = i18n.T(lang, i18n.MsgStatusYes)
	}

	var sb strings.Builder
	sb.WriteString(i18n.Tf(lang, i18n.MsgStatusHeader, chatID, lang, forum))

	topics, err := b.store.GetTopicsByChat(chatID)
	if err == nil && len(topics) > 0 {
		sb.WriteString(i18n.T(lang, i18n.MsgStatusTopicsHeader))
		for _, t := range topics {
			sb.WriteString(i18n.Tf(lang, i18n.MsgStatusTopicLine,
				html.EscapeString(t.Name), html.EscapeString(t.EventType), t.TopicID))
		}
	} else {
		sb.WriteString(i18n.T(lang, i18n.MsgStatusNoTopics))
	}

	repos, err := b.store.GetReposByChat(chatID)
	if err == nil && len(repos) > 0 {
		sb.WriteString(i18n.Tf(lang, i18n.MsgStatusReposHeader, len(repos)))
		for _, r := range repos {
			sb.WriteString(i18n.Tf(lang, i18n.MsgStatusRepoLine,
				html.EscapeString(r.RepoURL), html.EscapeString(r.Platform)))
		}
	} else {
		sb.WriteString(i18n.T(lang, i18n.MsgStatusNoRepos))
	}

	sb.WriteString(i18n.Tf(lang, i18n.MsgStatusVersion, html.EscapeString(b.version)))
	b.reply(msg, sb.String())
}

// handleLang shows or changes the notification language of the chat.
func (b *Bot) handleLang(msg *telego.Message, args []string) {
	chatID := msg.Chat.ID
	current := b.chatLang(chatID)

	if len(args) < 1 {
		b.reply(msg, i18n.Tf(current, i18n.MsgLangUsage, current))
		return
	}

	newLang := i18n.Lang(strings.ToLower(args[0]))
	if !i18n.Valid(newLang) {
		b.reply(msg, i18n.Tf(current, i18n.MsgLangInvalid, html.EscapeString(args[0])))
		return
	}

	if err := b.store.SetChatLanguage(chatID, string(newLang), msg.Chat.IsForum); err != nil {
		log.Print(i18n.Tf(b.lang, i18n.MsgFailedToSaveChatConfig, err))
		b.reply(msg, i18n.T(current, i18n.MsgConnectFailed))
		return
	}
	b.reply(msg, i18n.T(newLang, i18n.MsgLangChanged))
}

// generateSecret creates a random secret for webhook verification.
func generateSecret() string {
	bytes := make([]byte, 20)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
