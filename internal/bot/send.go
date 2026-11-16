package bot

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/notify"
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"strings"
	"sync"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	"github.com/mymmrac/telego/telegoutil"
)

// SendNotification sends n to a chat (and topic, when topicID > 0).
//
// A rich message is tried first unless the chat chose the classic format or
// rich messages are switched off. When Telegram rejects the rich message
// with a 400 that is not about the chat or the topic (for example content it
// cannot parse), the classic rendering is sent instead. Topic and chat
// errors, rate limits and network errors are returned as they are, so the
// caller can fall back to the general chat or count the failure.
//
// Every attempt is returned, in order, with its format and error.
func (b *Bot) SendNotification(chatID int64, topicID int, format string, lang i18n.Lang, n notify.Notification) ([]notify.Attempt, error) {
	var attempts []notify.Attempt

	if b.rich && format != notify.FormatClassic && !b.richMissing.Load() {
		err := b.sendRich(chatID, topicID, notify.Rich(lang, n))
		attempts = append(attempts, notify.Attempt{Format: notify.FormatRich, Err: err})
		switch {
		case err == nil:
			b.contact(nil)
			return attempts, nil
		case richUnsupported(err):
			if !b.richMissing.Swap(true) {
				log.Printf("⚠️ The Bot API server has no sendRichMessage, sending classic messages from now on: %v", err)
			}
		case richRejected(err):
			logFallbackOnce(err)
		default:
			log.Printf(i18n.T(b.lang, i18n.MsgSendFailed), chatID, err)
			return attempts, err
		}
	}

	text, keyboard := notify.Classic(lang, n)
	err := b.sendClassic(chatID, topicID, text, keyboard)
	attempts = append(attempts, notify.Attempt{Format: notify.FormatClassic, Err: err})
	if err != nil && len(keyboard) > 0 && buttonURLRefused(err) {
		// Telegram refuses some button URLs (e.g. an internal GitLab host);
		// the links still work as text.
		err = b.sendClassic(chatID, topicID, text+buttonLinks(keyboard), nil)
		attempts = append(attempts, notify.Attempt{Format: notify.FormatClassic, Err: err})
	}
	if err != nil {
		log.Printf(i18n.T(b.lang, i18n.MsgSendFailed), chatID, err)
		return attempts, err
	}
	b.contact(nil)
	return attempts, nil
}

func (b *Bot) sendRich(chatID int64, topicID int, html string) error {
	params := &telego.SendRichMessageParams{
		ChatID:      telegoutil.ID(chatID),
		RichMessage: telego.InputRichMessage{HTML: html, SkipEntityDetection: true},
	}
	if topicID > 0 {
		params.MessageThreadID = topicID
	}
	_, err := b.api.SendRichMessage(context.Background(), params)
	return err
}

func (b *Bot) sendClassic(chatID int64, topicID int, text string, keyboard [][]notify.Button) error {
	params := &telego.SendMessageParams{
		ChatID:             telegoutil.ID(chatID),
		Text:               text,
		ParseMode:          telego.ModeHTML,
		LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
	}
	if topicID > 0 {
		params.MessageThreadID = topicID
	}
	if len(keyboard) > 0 {
		markup := &telego.InlineKeyboardMarkup{}
		for _, row := range keyboard {
			var buttons []telego.InlineKeyboardButton
			for _, btn := range row {
				buttons = append(buttons, telego.InlineKeyboardButton{Text: btn.Label, URL: btn.URL, Style: string(btn.Style)})
			}
			markup.InlineKeyboard = append(markup.InlineKeyboard, buttons)
		}
		params.ReplyMarkup = markup
	}
	_, err := b.api.SendMessage(context.Background(), params)
	return err
}

// richRejected reports whether err is a 400 about the message itself, after
// which the classic rendering is worth a try.
func richRejected(err error) bool {
	var apiErr *telegoapi.Error
	if !errors.As(err, &apiErr) || apiErr.ErrorCode != 400 {
		return false
	}
	return !isTopicOrChatError(apiErr.Description)
}

// richUnsupported reports whether the Bot API server does not know
// sendRichMessage (a self-hosted server older than Bot API 10).
func richUnsupported(err error) bool {
	var apiErr *telegoapi.Error
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.ErrorCode == 404 || strings.Contains(strings.ToLower(apiErr.Description), "method not found")
}

// buttonURLRefused reports whether Telegram refused a URL of an inline button.
func buttonURLRefused(err error) bool {
	var apiErr *telegoapi.Error
	if !errors.As(err, &apiErr) || apiErr.ErrorCode != 400 {
		return false
	}
	d := strings.ToLower(apiErr.Description)
	return strings.Contains(d, "button") && strings.Contains(d, "url")
}

// buttonLinks renders the buttons as text links, for a message sent
// without its keyboard.
func buttonLinks(keyboard [][]notify.Button) string {
	var links []string
	for _, row := range keyboard {
		for _, btn := range row {
			links = append(links, fmt.Sprintf(`<a href="%s">%s</a>`, html.EscapeString(btn.URL), html.EscapeString(btn.Label)))
		}
	}
	return "\n\n" + strings.Join(links, " · ")
}

// topicOrChatErrors are parts of 400 descriptions about where the message
// goes rather than what it contains; sending the classic rendering to the
// same place would fail the same way.
var topicOrChatErrors = []string{
	"thread not found",
	"topic not found",
	"topic_closed",
	"topic_deleted",
	"chat not found",
	"not enough rights",
	"have no rights",
}

func isTopicOrChatError(description string) bool {
	d := strings.ToLower(description)
	for _, s := range topicOrChatErrors {
		if strings.Contains(d, s) {
			return true
		}
	}
	return false
}

// loggedFallbacks keeps the rich-message rejections already logged, so a
// recurring one is logged once per process instead of on every event.
var loggedFallbacks sync.Map

func logFallbackOnce(err error) {
	if _, seen := loggedFallbacks.LoadOrStore(err.Error(), true); !seen {
		log.Printf("⚠️ Rich message rejected, sent the classic message instead: %v", err)
	}
}

// SendMessageToTopic sends an HTML message to a specific forum topic.
// If topicID is 0, the message goes to the main chat.
func (b *Bot) SendMessageToTopic(chatID int64, topicID int, text string) error {
	err := b.sendClassic(chatID, topicID, text, nil)
	if err == nil {
		b.contact(nil)
	} else {
		log.Printf(i18n.T(b.lang, i18n.MsgSendFailed), chatID, err)
	}
	return err
}

// SendMessage sends an HTML message to the main chat.
func (b *Bot) SendMessage(chatID int64, text string) error {
	return b.SendMessageToTopic(chatID, 0, text)
}
