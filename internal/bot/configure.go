package bot

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"GitHub-GitLab-Notifier/storage"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoutil"
)

// configurePrefix versions the callback data of the setup buttons, so a
// later change of their meaning can tell old buttons apart.
const configurePrefix = "cfg1"

// Setup steps, as they appear in callback data ("cfg1:<step>:<value>").
const (
	stepTopics = "topics"
	stepFormat = "format"
)

// parseConfigureCallback splits "cfg1:<step>:<value>" and accepts only the
// steps and values the setup offers.
func parseConfigureCallback(data string) (step, value string, ok bool) {
	parts := strings.Split(data, ":")
	if len(parts) != 3 || parts[0] != configurePrefix {
		return "", "", false
	}
	step, value = parts[1], parts[2]
	switch {
	case step == stepTopics && (value == "yes" || value == "no"):
	case step == stepFormat && (value == storage.FormatRich || value == storage.FormatClassic):
	default:
		return "", "", false
	}
	return step, value, true
}

// configureChat starts the chat setup: one message with buttons, edited in
// place as the user answers. A forum chat is asked about topics first; a
// regular group goes straight to the message look and posts to the main
// chat. Running it again shows the current values and changes nothing until
// a button is pressed.
func (b *Bot) configureChat(msg *telego.Message) {
	chatID := msg.Chat.ID
	lang := b.chatLang(chatID)

	if err := b.ensureChat(msg.Chat); err != nil {
		log.Print(i18n.Tf(b.lang, i18n.MsgFailedToSaveChatConfig, err))
		b.reply(msg, i18n.T(lang, i18n.MsgCfgFailed))
		return
	}
	chat, err := b.store.GetChat(chatID)
	if err != nil {
		log.Print(i18n.Tf(b.lang, i18n.MsgFailedToSaveChatConfig, err))
		b.reply(msg, i18n.T(lang, i18n.MsgCfgFailed))
		return
	}

	var text string
	var keyboard *telego.InlineKeyboardMarkup
	if msg.Chat.IsForum {
		text, keyboard = topicsStep(lang, chat.UseTopics)
	} else {
		// Nothing is stored until a button is pressed; the format press
		// turns topic routing off for a chat that is not a forum.
		text, keyboard = formatStep(lang, chat.Format, "")
	}

	params := &telego.SendMessageParams{
		ChatID:          telegoutil.ID(chatID),
		Text:            text,
		ParseMode:       telego.ModeHTML,
		ReplyMarkup:     keyboard,
		ReplyParameters: &telego.ReplyParameters{MessageID: msg.MessageID, AllowSendingWithoutReply: true},
	}
	if msg.IsTopicMessage {
		params.MessageThreadID = msg.MessageThreadID
	}
	if _, err := b.api.SendMessage(context.Background(), params); err != nil {
		log.Printf(i18n.T(b.lang, i18n.MsgSendFailed), chatID, err)
	}
}

// handleCallback applies a press on a setup button. Presses in chats that
// never ran /configure are ignored; presses by non-administrators are
// refused with an alert and change nothing.
func (b *Bot) handleCallback(q *telego.CallbackQuery) {
	step, value, ok := parseConfigureCallback(q.Data)
	if !ok || q.Message == nil {
		b.answer(q.ID, "", false)
		return
	}
	chatID := q.Message.GetChat().ID
	chat, err := b.store.GetChat(chatID)
	if err != nil {
		b.answer(q.ID, "", false)
		return
	}
	lang := b.chatLang(chatID)

	if refusal := b.canConfigure(chatID, q); refusal != "" {
		b.answer(q.ID, i18n.T(lang, refusal), true)
		return
	}

	messageID := q.Message.GetMessageID()
	switch step {
	case stepTopics:
		useTopics := value == "yes"
		intro := ""
		if useTopics {
			// Creating topics takes a few seconds: stop the button's spinner
			// first. Failures are shown in the message from here on.
			b.answer(q.ID, "", false)
			created, skipped, err := b.createTopics(chatID, lang)
			if err != nil {
				// Keep the buttons, so the choice can be made again once
				// the cause (usually the bot's rights) is fixed.
				question, keyboard := topicsStep(lang, chat.UseTopics)
				b.edit(chatID, messageID, err.Error()+"\n\n"+question, keyboard)
				return
			}
			intro = topicsResult(lang, created, skipped)
		}
		if err := b.store.SetChatSettings(chatID, chat.Format, useTopics); err != nil {
			log.Print(i18n.Tf(b.lang, i18n.MsgFailedToSaveChatConfig, err))
			if useTopics {
				question, keyboard := topicsStep(lang, chat.UseTopics)
				b.edit(chatID, messageID, i18n.T(lang, i18n.MsgCfgFailed)+"\n\n"+question, keyboard)
			} else {
				b.answer(q.ID, i18n.T(lang, i18n.MsgCfgFailed), true)
			}
			return
		}
		text, keyboard := formatStep(lang, chat.Format, intro)
		b.edit(chatID, messageID, text, keyboard)
		if useTopics {
			return
		}

	case stepFormat:
		// A chat that is not a forum has no topics to route to.
		useTopics := chat.UseTopics && q.Message.GetChat().IsForum
		if err := b.store.SetChatSettings(chatID, value, useTopics); err != nil {
			log.Print(i18n.Tf(b.lang, i18n.MsgFailedToSaveChatConfig, err))
			b.answer(q.ID, i18n.T(lang, i18n.MsgCfgFailed), true)
			return
		}
		b.edit(chatID, messageID, summary(lang, value, useTopics), nil)
	}
	b.answer(q.ID, "", false)
}

// canConfigure reports why the user who pressed a button may not change the
// chat settings, as an i18n key, or "" when they may: a chat administrator,
// the owner of a private chat, or, when the bot cannot read the member list,
// the user who sent /configure.
func (b *Bot) canConfigure(chatID int64, q *telego.CallbackQuery) string {
	if q.Message.GetChat().Type == telego.ChatTypePrivate {
		// Only the owner of a private chat can press its buttons, and
		// getChatMember reports them as a plain member.
		return ""
	}
	member, err := b.api.GetChatMember(context.Background(), &telego.GetChatMemberParams{
		ChatID: telegoutil.ID(chatID),
		UserID: q.From.ID,
	})
	if err != nil {
		log.Print(i18n.Tf(b.lang, i18n.MsgFailedToCheckBotAdminStatus, err))
		m := q.Message.Message()
		if m == nil || m.ReplyToMessage == nil {
			return i18n.MsgCfgNotAdmin
		}
		cmd := m.ReplyToMessage
		if cmd.SenderChat != nil && cmd.SenderChat.ID == chatID {
			// Sent by an anonymous administrator: nobody can be matched
			// to the command.
			return i18n.MsgCfgAnonymous
		}
		if cmd.From != nil && cmd.From.ID == q.From.ID {
			return ""
		}
		return i18n.MsgCfgNotAdmin
	}
	status := member.MemberStatus()
	if status == telego.MemberStatusCreator || status == telego.MemberStatusAdministrator {
		return ""
	}
	return i18n.MsgCfgNotAdmin
}

// topicsStep asks whether to route events into forum topics.
func topicsStep(lang i18n.Lang, useTopics bool) (string, *telego.InlineKeyboardMarkup) {
	return i18n.T(lang, i18n.MsgCfgTopicsQuestion), keyboard(
		choice(lang, i18n.MsgCfgTopicsYes, stepTopics, "yes", useTopics),
		choice(lang, i18n.MsgCfgTopicsNo, stepTopics, "no", !useTopics),
	)
}

// formatStep asks for the message look; intro (the topic creation result)
// goes above the question.
func formatStep(lang i18n.Lang, format, intro string) (string, *telego.InlineKeyboardMarkup) {
	text := i18n.T(lang, i18n.MsgCfgFormatQuestion)
	if intro != "" {
		text = intro + "\n\n" + text
	}
	return text, keyboard(
		choice(lang, i18n.MsgCfgFormatRich, stepFormat, storage.FormatRich, format != storage.FormatClassic),
		choice(lang, i18n.MsgCfgFormatClassic, stepFormat, storage.FormatClassic, format == storage.FormatClassic),
	)
}

// summary is the last edit of the setup message.
func summary(lang i18n.Lang, format string, useTopics bool) string {
	topics := i18n.T(lang, i18n.MsgCfgTopicsOff)
	if useTopics {
		topics = i18n.T(lang, i18n.MsgCfgTopicsOn)
	}
	look := i18n.T(lang, i18n.MsgCfgFormatRichShort)
	if format == storage.FormatClassic {
		look = i18n.T(lang, i18n.MsgCfgFormatClassicShort)
	}
	return i18n.Tf(lang, i18n.MsgCfgSummary, topics, look, string(lang)) +
		"\n\n" + i18n.T(lang, i18n.MsgConfigureChatHowToUse)
}

// choice is one setup button, marked "✓" when it is the current value.
func choice(lang i18n.Lang, labelKey, step, value string, current bool) telego.InlineKeyboardButton {
	label := i18n.T(lang, labelKey)
	if current {
		label = "✓ " + label
	}
	return telego.InlineKeyboardButton{Text: label, CallbackData: configurePrefix + ":" + step + ":" + value}
}

// keyboard puts each button on its own row: the labels are long.
func keyboard(buttons ...telego.InlineKeyboardButton) *telego.InlineKeyboardMarkup {
	markup := &telego.InlineKeyboardMarkup{}
	for _, btn := range buttons {
		markup.InlineKeyboard = append(markup.InlineKeyboard, []telego.InlineKeyboardButton{btn})
	}
	return markup
}

// edit replaces the text and buttons of the setup message; a nil keyboard
// removes the buttons.
func (b *Bot) edit(chatID int64, messageID int, text string, keyboard *telego.InlineKeyboardMarkup) {
	_, err := b.api.EditMessageText(context.Background(), &telego.EditMessageTextParams{
		ChatID:      telegoutil.ID(chatID),
		MessageID:   messageID,
		Text:        text,
		ParseMode:   telego.ModeHTML,
		ReplyMarkup: keyboard,
	})
	if err != nil {
		log.Printf(i18n.T(b.lang, i18n.MsgSendFailed), chatID, err)
	}
}

// answer acknowledges a button press, optionally with an alert.
func (b *Bot) answer(queryID, text string, alert bool) {
	err := b.api.AnswerCallbackQuery(context.Background(), &telego.AnswerCallbackQueryParams{
		CallbackQueryID: queryID,
		Text:            text,
		ShowAlert:       alert,
	})
	if err != nil {
		log.Printf("❌ answerCallbackQuery: %v", err)
	}
}

// createTopics creates one forum topic per event category (see
// platform.Topics) and maps the events to them. Topics that already exist
// (by name) are skipped, so it is safe to run again, e.g. after an upgrade
// that adds new topics. The returned error is a message for the chat.
func (b *Bot) createTopics(chatID int64, lang i18n.Lang) (created, skipped []string, err error) {
	if !b.isBotAdmin(chatID) {
		return nil, nil, errors.New(i18n.T(lang, i18n.MsgConfigureChatNotAdmin))
	}

	existing, err := b.store.GetTopicsByChat(chatID)
	if err != nil {
		log.Print(i18n.Tf(b.lang, i18n.MsgFailedToGetExistingTopics, err))
		return nil, nil, errors.New(i18n.T(lang, i18n.MsgFailedToGetTopics))
	}
	existingByName := make(map[string]TopicInfo, len(existing))
	for _, t := range existing {
		existingByName[t.Name] = t
	}

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
			return created, skipped, errors.New(i18n.Tf(lang, i18n.MsgTopicCreateFailed, def.Name, err.Error()))
		}

		err = b.store.SaveTopic(TopicInfo{
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
	return created, skipped, nil
}

// topicsResult describes what createTopics did.
func topicsResult(lang i18n.Lang, created, skipped []string) string {
	var parts []string
	if len(created) > 0 {
		parts = append(parts, i18n.T(lang, i18n.MsgConfigureChatSuccess)+"\n\n"+strings.Join(created, "\n"))
	}
	if len(skipped) > 0 {
		parts = append(parts, i18n.Tf(lang, i18n.MsgConfigureChatSkipped, strings.Join(skipped, ", ")))
	}
	if len(created) == 0 && len(skipped) > 0 {
		parts = append(parts, i18n.T(lang, i18n.MsgConfigureChatAllExist))
	}
	return strings.Join(parts, "\n\n")
}
