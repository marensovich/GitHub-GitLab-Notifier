package telegram

import (
	"context"
	"errors"

	"GitHub-GitLab-Notifier/internal/messenger"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoutil"
)

// Reply sends r to the chat and topic the command came from. A reply with
// buttons quotes the command, so a later press can be matched to whoever
// sent it.
func (t *Telegram) Reply(ctx context.Context, c messenger.Command, r messenger.Reply) (any, error) {
	chat, err := chatID(c.ChatID)
	if err != nil {
		return nil, err
	}
	topic, err := threadID(c.ThreadID)
	if err != nil {
		return nil, err
	}
	params := classicParams(chat, topic, r.Text, choiceKeyboard(r.Choices))
	if msg, ok := c.Ref.(*telego.Message); ok && len(r.Choices) > 0 {
		params.ReplyParameters = &telego.ReplyParameters{MessageID: msg.MessageID, AllowSendingWithoutReply: true}
	}
	sent, err := t.api.SendMessage(ctx, params)
	t.contact(err)
	if err != nil {
		return nil, classify(err)
	}
	return sent, nil
}

// Edit replaces the message a pressed button belongs to.
func (t *Telegram) Edit(ctx context.Context, p messenger.Press, r messenger.Reply) error {
	q, ok := p.Ref.(*telego.CallbackQuery)
	if !ok || q.Message == nil {
		return errors.New("telegram: the press has no message to edit")
	}
	params := &telego.EditMessageTextParams{
		ChatID:             telegoutil.ID(q.Message.GetChat().ID),
		MessageID:          q.Message.GetMessageID(),
		Text:               r.Text,
		ParseMode:          telego.ModeHTML,
		LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true},
	}
	if kb := choiceKeyboard(r.Choices); kb != nil {
		params.ReplyMarkup = kb
	}
	_, err := t.api.EditMessageText(ctx, params)
	t.contact(err)
	return classify(err)
}

// Answer stops the button's loading indicator, showing text as a toast or,
// with alert, as a dialog.
func (t *Telegram) Answer(ctx context.Context, p messenger.Press, text string, alert bool) error {
	q, ok := p.Ref.(*telego.CallbackQuery)
	if !ok {
		return errors.New("telegram: the press has no callback query")
	}
	err := t.api.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
		CallbackQueryID: q.ID,
		Text:            text,
		ShowAlert:       alert,
	})
	t.contact(err)
	return classify(err)
}

// choiceKeyboard turns reply choices into callback buttons.
func choiceKeyboard(choices [][]messenger.Choice) *telego.InlineKeyboardMarkup {
	if len(choices) == 0 {
		return nil
	}
	markup := &telego.InlineKeyboardMarkup{}
	for _, row := range choices {
		var buttons []telego.InlineKeyboardButton
		for _, c := range row {
			buttons = append(buttons, telego.InlineKeyboardButton{Text: c.Label, CallbackData: c.Data})
		}
		markup.InlineKeyboard = append(markup.InlineKeyboard, buttons)
	}
	return markup
}
