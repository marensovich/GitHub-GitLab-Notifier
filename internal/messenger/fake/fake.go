// Package fake is an in-memory messenger for tests of the core.
package fake

import (
	"context"
	"strconv"
	"sync"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/notify"
)

// Sent is a recorded notification.
type Sent struct {
	Target messenger.Target
	Look   string
	Lang   i18n.Lang
	N      notify.Notification
}

// Replied is a recorded command reply.
type Replied struct {
	Command messenger.Command
	Reply   messenger.Reply
}

// Edited is a recorded edit after a button press.
type Edited struct {
	Press messenger.Press
	Reply messenger.Reply
}

// Answered is a recorded answer to a button press.
type Answered struct {
	Press messenger.Press
	Text  string
	Alert bool
}

// Thread is a thread created through the fake.
type Thread struct {
	ChatID, ThreadID, Name string
}

// Messenger records every call. Its exported fields may be read and
// changed by the test between calls.
type Messenger struct {
	mu   sync.Mutex
	name string
	caps messenger.Caps
	fail func(call int, t messenger.Target) error
	call int

	Sent    []Sent
	Replies []Replied
	Edits   []Edited
	Answers []Answered
	Threads []Thread
	// Admins holds chatID+"/"+userID of chat admins.
	Admins map[string]bool
	// NoThreadRights lists chats where the bot cannot manage threads.
	NoThreadRights map[string]bool
	// AdminErr, when set, is returned by IsChatAdmin (the member list
	// cannot be read).
	AdminErr error
	// ThreadErr, when set, fails CreateThread.
	ThreadErr error
	// RichOff makes RichAvailable report false.
	RichOff bool
	// HealthErr is returned by Health.
	HealthErr error
	// Log names every call in order ("Reply", "Answer", "CreateThread", ...).
	Log []string
}

// RichAvailable reports whether the rich look is available (see RichOff).
func (m *Messenger) RichAvailable() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.RichOff
}

var _ messenger.Messenger = (*Messenger)(nil)

// New returns a fake messenger with the given name and caps.
func New(name string, caps messenger.Caps) *Messenger {
	return &Messenger{name: name, caps: caps, Admins: map[string]bool{}, NoThreadRights: map[string]bool{}}
}

// Fail scripts send errors: f gets the 1-based number of the Send call
// and its target; a non-nil result fails that call and nothing is recorded.
func (m *Messenger) Fail(f func(call int, t messenger.Target) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = f
}

func (m *Messenger) Name() string         { return m.name }
func (m *Messenger) Caps() messenger.Caps { return m.caps }

func (m *Messenger) Run(ctx context.Context, _ messenger.Handler) error {
	<-ctx.Done()
	return nil
}

func (m *Messenger) Send(_ context.Context, t messenger.Target, look string, lang i18n.Lang, n notify.Notification) ([]notify.Attempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.call++
	if m.fail != nil {
		if err := m.fail(m.call, t); err != nil {
			return nil, err
		}
	}
	m.Sent = append(m.Sent, Sent{Target: t, Look: look, Lang: lang, N: n})
	return nil, nil
}

func (m *Messenger) Reply(_ context.Context, c messenger.Command, r messenger.Reply) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Log = append(m.Log, "Reply")
	m.Replies = append(m.Replies, Replied{Command: c, Reply: r})
	return len(m.Replies), nil
}

func (m *Messenger) Edit(_ context.Context, p messenger.Press, r messenger.Reply) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Log = append(m.Log, "Edit")
	m.Edits = append(m.Edits, Edited{Press: p, Reply: r})
	return nil
}

func (m *Messenger) Answer(_ context.Context, p messenger.Press, text string, alert bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Log = append(m.Log, "Answer")
	m.Answers = append(m.Answers, Answered{Press: p, Text: text, Alert: alert})
	return nil
}

func (m *Messenger) IsChatAdmin(_ context.Context, chatID, userID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Log = append(m.Log, "IsChatAdmin")
	if m.AdminErr != nil {
		return false, m.AdminErr
	}
	return m.Admins[chatID+"/"+userID], nil
}

func (m *Messenger) CanManageThreads(_ context.Context, chatID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.caps.Threads && !m.NoThreadRights[chatID], nil
}

func (m *Messenger) CreateThread(_ context.Context, chatID, name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Log = append(m.Log, "CreateThread")
	if m.ThreadErr != nil {
		return "", m.ThreadErr
	}
	id := strconv.Itoa(1000 + len(m.Threads))
	m.Threads = append(m.Threads, Thread{ChatID: chatID, ThreadID: id, Name: name})
	return id, nil
}

func (m *Messenger) Health(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.HealthErr
}
