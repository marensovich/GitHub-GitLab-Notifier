package telegram

import (
	"context"
	"errors"
	"strconv"

	"GitHub-GitLab-Notifier/internal/messenger"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoutil"
)

var _ messenger.DirectMessenger = (*Telegram)(nil)

// IsMember reports whether a user is in a chat: left and kicked users are
// not, nor restricted users who are no longer members.
func (t *Telegram) IsMember(ctx context.Context, chatIDText, userID string) (bool, error) {
	chat, err := chatID(chatIDText)
	if err != nil {
		return false, err
	}
	user, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return false, err
	}
	member, err := t.api.GetChatMember(ctx, &telego.GetChatMemberParams{ChatID: telegoutil.ID(chat), UserID: user})
	t.contact(err)
	if err != nil {
		return false, classify(err)
	}
	return member.MemberIsMember(), nil
}

// DirectChat returns the private chat with a user: in Telegram its ID is
// the user's.
func (t *Telegram) DirectChat(_ context.Context, userID string) (string, error) {
	return userID, nil
}

// DeleteCommand deletes the message a command came in.
func (t *Telegram) DeleteCommand(ctx context.Context, c messenger.Command) error {
	msg, ok := c.Ref.(*telego.Message)
	if !ok || msg == nil {
		return errors.New("telegram: command has no message")
	}
	err := t.api.DeleteMessage(ctx, &telego.DeleteMessageParams{ChatID: telegoutil.ID(msg.Chat.ID), MessageID: msg.MessageID})
	t.contact(err)
	return classify(err)
}
