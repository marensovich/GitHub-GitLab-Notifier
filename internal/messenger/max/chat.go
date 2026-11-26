package max

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"GitHub-GitLab-Notifier/internal/messenger"
)

// Chat types MAX reports.
const (
	chatDialog  = "dialog"
	chatGroup   = "chat"
	chatChannel = "channel"
)

// chatInfo is what is kept of a chat.
type chatInfo struct {
	Type           string `json:"type"`
	Title          string `json:"title"`
	DialogWithUser *user  `json:"dialog_with_user"`

	at time.Time // when it was fetched
}

// chatTTL is how long a chat's title is kept before it is fetched again.
const chatTTL = 10 * time.Minute

// chat returns a chat's type and title, fetched again after chatTTL.
func (m *Max) chat(ctx context.Context, id string) (chatInfo, error) {
	m.mu.Lock()
	ch, ok := m.chats[id]
	m.mu.Unlock()
	if ok && time.Since(ch.at) < chatTTL {
		return ch, nil
	}
	ch = chatInfo{}
	if err := m.c.do(ctx, http.MethodGet, "/chats/"+url.PathEscape(id), nil, nil, &ch); err != nil {
		return ch, classify(err)
	}
	ch.at = time.Now()
	m.mu.Lock()
	m.chats[id] = ch
	m.mu.Unlock()
	return ch, nil
}

type member struct {
	UserID  int64 `json:"user_id"`
	IsAdmin bool  `json:"is_admin"`
	IsOwner bool  `json:"is_owner"`
}

// member returns the user's membership of a group, nil when not a member.
// MAX answers it only to a bot that administers the chat.
func (m *Max) member(ctx context.Context, chatID, userID string) (*member, error) {
	var out struct {
		Members []member `json:"members"`
	}
	path := "/chats/" + url.PathEscape(chatID) + "/members"
	if err := m.c.do(ctx, http.MethodGet, path, url.Values{"user_ids": {userID}}, nil, &out); err != nil {
		return nil, classify(err)
	}
	for _, mb := range out.Members {
		if fmt.Sprint(mb.UserID) == userID {
			return &mb, nil
		}
	}
	return nil, nil
}

// IsChatAdmin implements messenger.Messenger. The user of a dialog
// manages it; in a group they must be its owner or an administrator.
func (m *Max) IsChatAdmin(ctx context.Context, chatID, userID string) (bool, error) {
	if userID == "" {
		return false, errors.New("max: the command has no sender")
	}
	ch, err := m.chat(ctx, chatID)
	if err != nil {
		return false, err
	}
	if ch.Type == chatDialog {
		return ch.DialogWithUser == nil || ch.DialogWithUser.id() == userID, nil
	}
	mb, err := m.member(ctx, chatID, userID)
	if err != nil {
		return false, fmt.Errorf("max: the bot must be an administrator of chat %s to check its members: %w", chatID, err)
	}
	return mb != nil && (mb.IsAdmin || mb.IsOwner), nil
}

// IsMember implements messenger.MemberChecker.
func (m *Max) IsMember(ctx context.Context, chatID, userID string) (bool, error) {
	ch, err := m.chat(ctx, chatID)
	if err != nil {
		return false, err
	}
	if ch.Type == chatDialog {
		return ch.DialogWithUser != nil && ch.DialogWithUser.id() == userID, nil
	}
	mb, err := m.member(ctx, chatID, userID)
	return mb != nil, err
}

// sawDialog remembers the dialog chat of a user.
func (m *Max) sawDialog(userID, chatID string) {
	if userID == "" || chatID == "" {
		return
	}
	m.mu.Lock()
	m.dialogs[userID] = chatID
	m.mu.Unlock()
}

// DirectChat implements messenger.DirectMessenger: the dialog the user has
// written to the bot in. /link is answered only there, so it is known.
func (m *Max) DirectChat(_ context.Context, userID string) (string, error) {
	m.mu.Lock()
	chat, ok := m.dialogs[userID]
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("max: user %s has not written to the bot privately yet", userID)
	}
	return chat, nil
}

// DeleteCommand implements messenger.DirectMessenger. MAX lets a bot
// delete only its own messages in a dialog, so this usually fails there;
// the caller tells the user to delete the message themself.
func (m *Max) DeleteCommand(ctx context.Context, c messenger.Command) error {
	msg, ok := c.Ref.(*message)
	if !ok || msg.Body.Mid == "" {
		return errors.New("max: the command has no message")
	}
	return classify(m.c.do(ctx, http.MethodDelete, "/messages", url.Values{"message_id": {msg.Body.Mid}}, nil, nil))
}
