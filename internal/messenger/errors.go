package messenger

import (
	"errors"
	"fmt"
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
)

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
