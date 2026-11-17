package outbox

import (
	"math"
	"time"

	"GitHub-GitLab-Notifier/internal/messenger"
)

// bucket is a token bucket.
type bucket struct {
	tokens float64
	last   time.Time
}

// refill adds the tokens earned since the last call, up to burst.
func (b *bucket) refill(rate, burst float64, now time.Time) {
	if b.last.IsZero() {
		b.tokens, b.last = burst, now
		return
	}
	if dt := now.Sub(b.last).Seconds(); dt > 0 {
		b.tokens = math.Min(burst, b.tokens+dt*rate)
		b.last = now
	}
}

// limiter keeps a messenger within its per-chat and global send rates
// (Caps); a rate of 0 means no limit. It is used by one worker only.
type limiter struct {
	chatRate, chatBurst     float64
	globalRate, globalBurst float64
	chats                   map[int64]*bucket
	global                  bucket
}

func newLimiter(c messenger.Caps) *limiter {
	l := &limiter{chatRate: c.PerChatRate, globalRate: c.GlobalRate, chats: map[int64]*bucket{}}
	l.chatBurst = math.Max(1, float64(c.PerChatBurst))
	l.globalBurst = math.Max(1, c.GlobalRate)
	return l
}

// globalWait returns how long until the global bucket has a token.
func (l *limiter) globalWait(now time.Time) time.Duration {
	if l.globalRate <= 0 {
		return 0
	}
	l.global.refill(l.globalRate, l.globalBurst, now)
	if l.global.tokens >= 1 {
		return 0
	}
	return time.Duration((1 - l.global.tokens) / l.globalRate * float64(time.Second))
}

// throttled returns the destinations that have no token now. Full buckets
// are forgotten, so the map only holds chats that sent recently.
func (l *limiter) throttled(now time.Time) map[int64]bool {
	if l.chatRate <= 0 {
		return nil
	}
	busy := map[int64]bool{}
	for id, b := range l.chats {
		b.refill(l.chatRate, l.chatBurst, now)
		switch {
		case b.tokens >= l.chatBurst:
			delete(l.chats, id)
		case b.tokens < 1:
			busy[id] = true
		}
	}
	return busy
}

// take spends a token of the destination and of the messenger.
func (l *limiter) take(dest int64, now time.Time) {
	if l.globalRate > 0 {
		l.global.refill(l.globalRate, l.globalBurst, now)
		l.global.tokens--
	}
	if l.chatRate > 0 {
		b := l.chats[dest]
		if b == nil {
			b = &bucket{}
			l.chats[dest] = b
		}
		b.refill(l.chatRate, l.chatBurst, now)
		b.tokens--
	}
}
