package messenger

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Adapters wrap their send errors in these so the outbox can decide what to
// do. Anything else is treated as temporary and retried.
var (
	// ErrThreadNotFound: the thread was deleted; the chat itself is fine.
	ErrThreadNotFound = errors.New("thread not found")
	// ErrChatGone: the bot was removed from the chat or the chat is gone.
	ErrChatGone = errors.New("chat gone")
	// ErrRejected: the messenger refused this message; retrying won't help.
	ErrRejected = errors.New("message rejected")
	// ErrNeedsThread: the chat takes messages only in threads and none was
	// given (a Discord forum channel).
	ErrNeedsThread = errors.New("chat needs a thread")
)

// RateLimited is returned when the messenger asks to slow down.
type RateLimited struct {
	RetryAfter time.Duration
	Err        error
}

func (e *RateLimited) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("rate limited, retry after %s: %v", e.RetryAfter, e.Err)
	}
	return fmt.Sprintf("rate limited, retry after %s", e.RetryAfter)
}

func (e *RateLimited) Unwrap() error { return e.Err }

// Class is what a send error means for delivery.
type Class int

const (
	ClassOK Class = iota
	ClassRateLimited
	ClassThreadNotFound
	ClassChatGone
	ClassRejected
	ClassNeedsThread
	ClassTemporary
	ClassChatMoved
)

// ChatMoved: the chat has a new ID (a Telegram group that became a
// supergroup); its messages go to To from now on.
type ChatMoved struct {
	To  string
	Err error
}

func (e *ChatMoved) Error() string { return fmt.Sprintf("chat moved to %s: %v", e.To, e.Err) }
func (e *ChatMoved) Unwrap() error { return e.Err }

// MovedTo returns the new chat ID of a ChatMoved error, or "".
func MovedTo(err error) string {
	var cm *ChatMoved
	if errors.As(err, &cm) {
		return cm.To
	}
	return ""
}

// ProbeEvery is how often Health may ask a messenger's API at most.
const ProbeEvery = 30 * time.Second

// Probe limits how often Health asks a messenger's API.
type Probe struct {
	mu   sync.Mutex
	last time.Time
}

// Due reports whether a check may run now, and if so counts it as run.
func (p *Probe) Due() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.last.IsZero() && time.Since(p.last) < ProbeEvery {
		return false
	}
	p.last = time.Now()
	return true
}

func (c Class) String() string {
	switch c {
	case ClassOK:
		return "ok"
	case ClassRateLimited:
		return "rate limited"
	case ClassThreadNotFound:
		return "thread not found"
	case ClassChatGone:
		return "chat gone"
	case ClassRejected:
		return "rejected"
	case ClassNeedsThread:
		return "needs thread"
	case ClassChatMoved:
		return "chat moved"
	}
	return "temporary"
}

// Classify sorts a send error.
func Classify(err error) Class {
	var rl *RateLimited
	switch {
	case err == nil:
		return ClassOK
	case errors.As(err, &rl):
		return ClassRateLimited
	case MovedTo(err) != "":
		return ClassChatMoved
	case errors.Is(err, ErrThreadNotFound):
		return ClassThreadNotFound
	case errors.Is(err, ErrChatGone):
		return ClassChatGone
	case errors.Is(err, ErrRejected):
		return ClassRejected
	case errors.Is(err, ErrNeedsThread):
		return ClassNeedsThread
	}
	return ClassTemporary
}

// RetryAfter returns how long a rate-limited send asked to wait, or 0.
func RetryAfter(err error) time.Duration {
	var rl *RateLimited
	if errors.As(err, &rl) {
		return rl.RetryAfter
	}
	return 0
}
