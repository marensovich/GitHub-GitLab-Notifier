package bot

import (
	"context"
	"log"

	"GitHub-GitLab-Notifier/internal/i18n"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoutil"
)

// SendMessageToTopic sends an HTML message to a forum topic, or to the main
// chat when topicID is 0. Notifications go through the Telegram adapter;
// this only answers commands until they move to the core.
func (b *Bot) SendMessageToTopic(chatID int64, topicID int, text string) error {
	params := &telego.SendMessageParams{
		ChatID:             telegoutil.ID(chatID),
		Text:               text,
		ParseMode:          telego.ModeHTML,
		LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
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
