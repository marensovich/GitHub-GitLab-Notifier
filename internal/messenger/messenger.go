// Package messenger is the boundary between the messenger-neutral core
// (commands, outbox) and the adapters for Telegram, Discord and MAX.
package messenger

import (
	"context"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/notify"
)

// Caps describes what an adapter supports and how fast it may send.
type Caps struct {
	// Threads is true when events can be routed to threads.
	Threads bool
	// Looks lists the message looks a chat can pick; empty or one item
	// means there is no look to choose.
	Looks []string
	// ButtonsPerRow is the most buttons a row of a reply can hold.
	ButtonsPerRow int
	// PerChatRate and GlobalRate are messages per second.
	PerChatRate, GlobalRate float64
	PerChatBurst            int
	// ForumOnlyThreads is true when a forum has no main chat: every
	// message there goes to a thread (Discord forum channels).
	ForumOnlyThreads bool
}

// Target is where a notification goes.
type Target struct {
	ChatID, ThreadID string
}

// Choice is a button of a reply. Data is at most 64 bytes.
type Choice struct {
	Label, Data string
}

// Reply is a command answer. Text is the limited HTML of the locales
// (b, i, code, a); each adapter converts it to its own markup.
type Reply struct {
	Text    string
	Choices [][]Choice
}

// Command is a command a user sent in a chat.
type Command struct {
	Messenger, ChatID, ThreadID, UserID, ChatTitle string
	IsPrivate, IsForum                             bool
	Name                                           string
	Args                                           []string
	// Ref is adapter-private: the message to reply to.
	Ref any
}

// Press is a press of a reply button.
type Press struct {
	Messenger, ChatID, UserID, Data string
	IsPrivate, IsForum              bool
	// OriginUserID is who sent the command the pressed message answers
	// ("" when unknown); OriginAnonymous is set when it was sent by an
	// anonymous administrator, so nobody can be matched to it.
	OriginUserID    string
	OriginAnonymous bool
	// Ref is adapter-private: the message the button belongs to.
	Ref any
}

// Handler receives what users do in a messenger.
type Handler interface {
	HandleCommand(ctx context.Context, c Command)
	HandlePress(ctx context.Context, p Press)
}

// CommandInfo is a command shown in a messenger's command menu.
type CommandInfo struct {
	Name, Description string
}

// CommandLister is implemented by a Handler that has a command menu; an
// adapter that can show one asks for it in each supported language.
type CommandLister interface {
	Commands(lang i18n.Lang) []CommandInfo
}

// Messenger is one messenger adapter.
type Messenger interface {
	Name() string
	Caps() Caps
	// Run receives updates and passes them to h until ctx is done.
	Run(ctx context.Context, h Handler) error
	Send(ctx context.Context, t Target, look string, lang i18n.Lang, n notify.Notification) ([]notify.Attempt, error)
	Reply(ctx context.Context, c Command, r Reply) (msgRef any, err error)
	Edit(ctx context.Context, p Press, r Reply) error
	Answer(ctx context.Context, p Press, text string, alert bool) error
	IsChatAdmin(ctx context.Context, chatID, userID string) (bool, error)
	CanManageThreads(ctx context.Context, chatID string) (bool, error)
	CreateThread(ctx context.Context, chatID, name string) (threadID string, err error)
	Health(ctx context.Context) error
}
