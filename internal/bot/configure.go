package bot

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"GitHub-GitLab-Notifier/storage"
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoutil"
)

// configureChat sets up a forum chat: creates one topic per event category
// (see platform.Topics) and maps the events to them.
// Topics that already exist (by name) are skipped, so the command is safe to
// run again, e.g. after an upgrade that adds new topics.
func (b *Bot) configureChat(msg *telego.Message) {
	chatID := msg.Chat.ID
	lang := b.chatLang(chatID)

	if !msg.Chat.IsForum {
		b.reply(msg, i18n.T(lang, i18n.MsgConfigureChatIsNotForum))
		return
	}

	if !b.isBotAdmin(chatID) {
		b.reply(msg, i18n.T(lang, i18n.MsgConfigureChatNotAdmin))
		return
	}

	if err := b.ensureChat(msg.Chat); err != nil {
		log.Print(i18n.Tf(b.lang, i18n.MsgFailedToSaveChatConfig, err))
	}

	existing, err := b.store.GetTopicsByChat(chatID)
	if err != nil {
		log.Print(i18n.Tf(b.lang, i18n.MsgFailedToGetExistingTopics, err))
		b.reply(msg, i18n.T(lang, i18n.MsgFailedToGetTopics))
		return
	}
	existingByName := make(map[string]storage.TopicInfo, len(existing))
	for _, t := range existing {
		existingByName[t.Name] = t
	}

	var created, skipped []string

	for _, def := range platform.Topics {
		eventType := string(def.Topic)

		if t, ok := existingByName[def.Name]; ok {
			// Make sure the mapping exists even for topics created by older versions.
			if err := b.store.SetEventTopic(chatID, eventType, t.TopicID); err != nil {
				log.Print(i18n.Tf(b.lang, i18n.MsgFailedToMapEventToStorage, eventType, t.TopicID, err))
			}
			skipped = append(skipped, def.Name)
			log.Print(i18n.Tf(b.lang, i18n.MsgTopicAlreadyExists, def.Name))
			continue
		}

		topic, err := b.api.CreateForumTopic(context.Background(), &telego.CreateForumTopicParams{
			ChatID: telegoutil.ID(chatID),
			Name:   def.Name,
		})
		if err != nil {
			log.Print(i18n.Tf(b.lang, i18n.MsgFailedTopicCreate, def.Name, err))
			b.reply(msg, i18n.Tf(lang, i18n.MsgTopicCreateFailed, def.Name, err.Error()))
			return
		}

		err = b.store.SaveTopic(storage.TopicInfo{
			TopicID:   topic.MessageThreadID,
			ChatID:    chatID,
			Name:      def.Name,
			EventType: eventType,
		})
		if err != nil {
			log.Print(i18n.Tf(b.lang, i18n.MsgFailedSaveTopicToStorage, def.Name, err))
		}

		if err := b.store.SetEventTopic(chatID, eventType, topic.MessageThreadID); err != nil {
			log.Print(i18n.Tf(b.lang, i18n.MsgFailedToMapEventToStorage, eventType, topic.MessageThreadID, err))
		}

		created = append(created, fmt.Sprintf("  • %s — ID: <code>%d</code>", def.Name, topic.MessageThreadID))
		log.Print(i18n.Tf(b.lang, i18n.MsgTopicCreated, def.Name, topic.MessageThreadID))
	}

	var sb strings.Builder
	if len(created) > 0 {
		sb.WriteString(i18n.T(lang, i18n.MsgConfigureChatSuccess))
		sb.WriteString("\n\n")
		sb.WriteString(strings.Join(created, "\n"))
		sb.WriteString("\n\n")
	}
	if len(skipped) > 0 {
		sb.WriteString(i18n.Tf(lang, i18n.MsgConfigureChatSkipped, strings.Join(skipped, ", ")))
		sb.WriteString("\n\n")
	}
	if len(created) == 0 && len(skipped) > 0 {
		sb.WriteString(i18n.T(lang, i18n.MsgConfigureChatAllExist))
		sb.WriteString("\n\n")
	}
	sb.WriteString(i18n.T(lang, i18n.MsgConfigureChatHowToUse))

	b.reply(msg, sb.String())
}
