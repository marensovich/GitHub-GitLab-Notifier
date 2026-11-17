package telegram

import (
	"context"
	"errors"
	"strconv"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoutil"
)

// errAnonymous is returned for a command sent by an anonymous administrator:
// nobody can be matched to it.
var errAnonymous = errors.New("telegram: sent by an anonymous administrator, cannot tell who")

// IsChatAdmin reports whether a user is the creator or an administrator of
// a chat. An empty userID (an anonymous administrator) cannot be checked.
func (t *Telegram) IsChatAdmin(ctx context.Context, chatIDText, userID string) (bool, error) {
	if userID == "" {
		return false, errAnonymous
	}
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
	status := member.MemberStatus()
	return status == telego.MemberStatusCreator || status == telego.MemberStatusAdministrator, nil
}

// CanManageThreads reports whether the chat is a forum and the bot may
// create topics in it.
func (t *Telegram) CanManageThreads(ctx context.Context, chatIDText string) (bool, error) {
	chat, err := chatID(chatIDText)
	if err != nil {
		return false, err
	}
	info, err := t.api.GetChat(ctx, &telego.GetChatParams{ChatID: telegoutil.ID(chat)})
	t.contact(err)
	if err != nil {
		return false, classify(err)
	}
	if !info.IsForum {
		return false, nil
	}
	member, err := t.api.GetChatMember(ctx, &telego.GetChatMemberParams{ChatID: telegoutil.ID(chat), UserID: t.self.ID})
	t.contact(err)
	if err != nil {
		return false, classify(err)
	}
	switch m := member.(type) {
	case *telego.ChatMemberOwner:
		return true, nil
	case *telego.ChatMemberAdministrator:
		return m.CanManageTopics, nil
	}
	return false, nil
}

// CreateThread creates a forum topic and returns its ID.
func (t *Telegram) CreateThread(ctx context.Context, chatIDText, name string) (string, error) {
	chat, err := chatID(chatIDText)
	if err != nil {
		return "", err
	}
	topic, err := t.api.CreateForumTopic(ctx, &telego.CreateForumTopicParams{ChatID: telegoutil.ID(chat), Name: name})
	t.contact(err)
	if err != nil {
		return "", classify(err)
	}
	return strconv.Itoa(topic.MessageThreadID), nil
}
