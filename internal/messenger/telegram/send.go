package telegram

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/notify"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	"github.com/mymmrac/telego/telegoutil"
)

// Send sends n to a chat (and topic, when t.ThreadID is set).
//
// A rich message is tried first unless the chat chose the classic look or
// rich messages are switched off. When Telegram rejects the rich message
// with a 400 that is not about the chat or the topic (for example content it
// cannot parse), the classic rendering is sent instead. Topic and chat
// errors, rate limits and network errors are returned as they are, wrapped
// in the messenger error classes.
//
// Every attempt is returned, in order, with its format and error.
func (t *Telegram) Send(ctx context.Context, to messenger.Target, look string, lang i18n.Lang, n notify.Notification) ([]notify.Attempt, error) {
	chat, err := chatID(to.ChatID)
	if err != nil {
		return nil, err
	}
	topic, err := threadID(to.ThreadID)
	if err != nil {
		return nil, err
	}
	attempts, err := t.send(ctx, chat, topic, look, lang, n)
	return attempts, t.redact(classify(err))
}

func (t *Telegram) send(ctx context.Context, chatID int64, topicID int, look string, lang i18n.Lang, n notify.Notification) ([]notify.Attempt, error) {
	var attempts []notify.Attempt

	if t.rich && look != notify.FormatClassic && !t.richMissing.Load() {
		err := t.sendRich(ctx, chatID, topicID, RenderRich(lang, n))
		attempts = append(attempts, notify.Attempt{Format: notify.FormatRich, Err: err})
		switch {
		case err == nil:
			t.contact(nil)
			return attempts, nil
		case richUnsupported(err):
			if !t.richMissing.Swap(true) {
				log.Printf("⚠️ The Bot API server has no sendRichMessage, sending classic messages from now on: %v", err)
			}
		case richRejected(err):
			logFallbackOnce(err)
		default:
			log.Printf(i18n.T(t.lang, i18n.MsgSendFailed), chatID, err)
			return attempts, err
		}
	}

	text, keyboard := RenderClassic(lang, n)
	err := t.sendClassic(ctx, chatID, topicID, text, urlKeyboard(keyboard))
	attempts = append(attempts, notify.Attempt{Format: notify.FormatClassic, Err: err})
	if err != nil && len(keyboard) > 0 && buttonURLRefused(err) {
		// Telegram refuses some button URLs (e.g. an internal GitLab host);
		// the links still work as text.
		err = t.sendClassic(ctx, chatID, topicID, text+buttonLinks(keyboard), nil)
		attempts = append(attempts, notify.Attempt{Format: notify.FormatClassic, Err: err})
	}
	if err != nil {
		log.Printf(i18n.T(t.lang, i18n.MsgSendFailed), chatID, err)
		return attempts, err
	}
	t.contact(nil)
	return attempts, nil
}

func (t *Telegram) sendRich(ctx context.Context, chatID int64, topicID int, html string) error {
	params := &telego.SendRichMessageParams{
		ChatID:      telegoutil.ID(chatID),
		RichMessage: telego.InputRichMessage{HTML: html, SkipEntityDetection: true},
	}
	if topicID > 0 {
		params.MessageThreadID = topicID
	}
	_, err := t.api.SendRichMessage(ctx, params)
	return err
}

func (t *Telegram) sendClassic(ctx context.Context, chatID int64, topicID int, text string, markup *telego.InlineKeyboardMarkup) error {
	_, err := t.api.SendMessage(ctx, classicParams(chatID, topicID, text, markup))
	return err
}

func classicParams(chatID int64, topicID int, text string, markup *telego.InlineKeyboardMarkup) *telego.SendMessageParams {
	params := &telego.SendMessageParams{
		ChatID:             telegoutil.ID(chatID),
		Text:               text,
		ParseMode:          telego.ModeHTML,
		LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
	}
	if topicID > 0 {
		params.MessageThreadID = topicID
	}
	if markup != nil {
		params.ReplyMarkup = markup
	}
	return params
}

// urlKeyboard is the inline keyboard of a classic notification.
func urlKeyboard(keyboard [][]notify.Button) *telego.InlineKeyboardMarkup {
	if len(keyboard) == 0 {
		return nil
	}
	markup := &telego.InlineKeyboardMarkup{}
	for _, row := range keyboard {
		var buttons []telego.InlineKeyboardButton
		for _, btn := range row {
			buttons = append(buttons, telego.InlineKeyboardButton{Text: btn.Label, URL: btn.URL, Style: string(btn.Style)})
		}
		markup.InlineKeyboard = append(markup.InlineKeyboard, buttons)
	}
	return markup
}

// classify wraps a Bot API error in the messenger error class it belongs to.
func classify(err error) error {
	var apiErr *telegoapi.Error
	if err == nil || !errors.As(err, &apiErr) {
		return err
	}
	if apiErr.ErrorCode == 429 {
		retry := 0
		if apiErr.Parameters != nil {
			retry = apiErr.Parameters.RetryAfter
		}
		return &messenger.RateLimited{RetryAfter: time.Duration(retry) * time.Second, Err: err}
	}
	d := strings.ToLower(apiErr.Description)
	for _, s := range threadErrors {
		if strings.Contains(d, s) {
			return fmt.Errorf("%w: %w", messenger.ErrThreadNotFound, err)
		}
	}
	for _, s := range chatErrors {
		if strings.Contains(d, s) {
			return fmt.Errorf("%w: %w", messenger.ErrChatGone, err)
		}
	}
	if apiErr.ErrorCode == 403 {
		return fmt.Errorf("%w: %w", messenger.ErrChatGone, err)
	}
	if apiErr.ErrorCode == 400 {
		// The classic rendering was refused too; the same bytes would be
		// refused again.
		return fmt.Errorf("%w: %w", messenger.ErrRejected, err)
	}
	return err
}

// threadErrors are parts of descriptions saying the topic is gone or closed.
var threadErrors = []string{"thread not found", "topic not found", "topic_closed", "topic_deleted"}

// chatErrors are parts of descriptions saying the bot cannot post to the chat.
var chatErrors = []string{
	"chat not found",
	"bot was kicked",
	"bot is not a member",
	"bot was blocked",
	"user is deactivated",
	"have no rights to send",
	"not enough rights",
	"group chat was upgraded",
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

// maxLoggedFallbacks bounds the rejection kinds remembered for logging.
const maxLoggedFallbacks = 64

// loggedFallbacks keeps the kinds of rich-message rejections already logged,
// so a recurring one is logged once per process instead of on every event.
var (
	loggedFallbacksMu sync.Mutex
	loggedFallbacks   = map[string]bool{}
	digitsRe          = regexp.MustCompile(`[0-9]+`)
)

func logFallbackOnce(err error) {
	// Byte offsets and positions differ between messages with the same cause.
	kind := digitsRe.ReplaceAllString(err.Error(), "N")
	loggedFallbacksMu.Lock()
	seen := loggedFallbacks[kind]
	if !seen && len(loggedFallbacks) < maxLoggedFallbacks {
		loggedFallbacks[kind] = true
	}
	loggedFallbacksMu.Unlock()
	if !seen {
		log.Printf("⚠️ Rich message rejected, sent the classic message instead: %v", err)
	}
}

func loggedFallbackCount() int {
	loggedFallbacksMu.Lock()
	defer loggedFallbacksMu.Unlock()
	return len(loggedFallbacks)
}
