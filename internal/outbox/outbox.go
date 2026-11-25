// Package outbox sends queued notifications with retries. The webhook
// server stores one row per message and returns; a worker per messenger
// sends the rows in order for each chat, within the messenger's rate
// limits, and retries what failed.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/metrics"
	"GitHub-GitLab-Notifier/internal/notify"
	"GitHub-GitLab-Notifier/storage"
)

// maxAttempts is how many failed sends make a row dead.
const maxAttempts = 10

// schedule is the delay before each retry; the last one repeats.
var schedule = []time.Duration{
	5 * time.Second, 15 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour,
}

// Delay returns how long to wait after the given failed attempt (1-based).
func Delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(schedule) {
		return schedule[len(schedule)-1]
	}
	return schedule[attempt-1]
}

// Payload is what a row carries: the message and how to show it.
type Payload struct {
	Lang i18n.Lang
	Look string
	N    notify.Notification
	// Topic is the event category (platform.Topic); "" is general.
	Topic string
	// Member is set on a DM about a private repository: it is sent only
	// while the person is in a chat subscribed to the repository.
	Member *MemberCheck `json:",omitempty"`
}

// MemberCheck names the repository and the messenger user of a DM.
type MemberCheck struct {
	RepoID int64
	UserID string
}

// Outbox sends the queued rows.
type Outbox struct {
	store    *storage.Store
	ms       map[string]messenger.Messenger
	metrics  *metrics.Metrics
	clock    func() time.Time
	wake     map[string]chan struct{}
	limiters map[string]*limiter
	// sendTimeout bounds one send, so a hung request cannot stop the
	// messenger's worker.
	sendTimeout time.Duration
	makeThread  ThreadMaker

	mu      sync.Mutex
	members map[memberKey]memberAnswer
}

// memberTTL is how long an answer of IsMember is used.
const memberTTL = 10 * time.Minute

type memberKey struct{ messenger, chatID, userID string }

type memberAnswer struct {
	ok bool
	at time.Time
}

// ThreadMaker returns the thread for an event category (topic) of a
// destination, creating it unless a thread other than failed (the one the
// message could not reach, "" when it had none) is already stored.
type ThreadMaker func(ctx context.Context, destID int64, topic, failed string) (threadID string, err error)

// SetThreadMaker sets what creates a missing thread in a chat that takes
// messages only in threads; call it before Run.
func (o *Outbox) SetThreadMaker(f ThreadMaker) { o.makeThread = f }

// sendTimeout is how long one send may take before it counts as failed.
const sendTimeout = 30 * time.Second

// New returns an outbox for the given adapters, keyed by messenger name.
// The outbox gauges of m are read from the store.
func New(store *storage.Store, ms map[string]messenger.Messenger, m *metrics.Metrics, clock func() time.Time) *Outbox {
	if m == nil {
		m = metrics.New("")
	}
	m.SetOutboxCounts(store.OutboxCounts)
	o := &Outbox{store: store, ms: ms, metrics: m, clock: clock,
		wake: map[string]chan struct{}{}, limiters: map[string]*limiter{}, sendTimeout: sendTimeout,
		members: map[memberKey]memberAnswer{}}
	for name, msg := range ms {
		o.wake[name] = make(chan struct{}, 1)
		o.limiters[name] = newLimiter(msg.Caps())
	}
	return o
}

// Notify wakes the workers: new rows were queued.
func (o *Outbox) Notify() {
	for _, ch := range o.wake {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Run sends rows until ctx is done: rows left sending by a stopped process
// are sent again, then one worker per messenger wakes on Notify or every
// second. Finished rows are pruned every hour.
func (o *Outbox) Run(ctx context.Context) {
	if n, err := o.store.ResetSending(); err != nil {
		log.Printf("❌ outbox: %v", err)
	} else if n > 0 {
		log.Printf("🔁 outbox: %d unfinished messages will be sent again", n)
	}
	done := make(chan struct{}, len(o.ms))
	for name := range o.ms {
		go func() {
			o.worker(ctx, name)
			done <- struct{}{}
		}()
	}
	go o.prune(ctx)
	for range o.ms {
		<-done
	}
}

func (o *Outbox) worker(ctx context.Context, name string) {
	for {
		wait := o.drain(ctx, name)
		if wait <= 0 || wait > time.Second {
			wait = time.Second
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-o.wake[name]:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (o *Outbox) prune(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if _, err := o.store.PruneOutbox(o.clock()); err != nil {
			log.Printf("❌ outbox: prune: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Drain sends every row that is due now, messenger by messenger.
func (o *Outbox) Drain(ctx context.Context) {
	names := make([]string, 0, len(o.ms))
	for name := range o.ms {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		o.drain(ctx, name)
	}
}

// drain sends the due rows of one messenger. It returns how long to wait
// when the messenger's global rate stops it, else 0.
func (o *Outbox) drain(ctx context.Context, name string) time.Duration {
	m, lim := o.ms[name], o.limiters[name]
	for ctx.Err() == nil {
		now := o.clock()
		if wait := lim.globalWait(now); wait > 0 {
			return wait
		}
		it, err := o.store.ClaimNext(name, now, lim.throttled(now))
		if err != nil {
			log.Printf("❌ outbox: %s: %v", name, err)
			return 0
		}
		if it == nil {
			return 0
		}
		lim.take(it.DestinationID, now)
		o.send(ctx, name, m, it)
	}
	return 0
}

// send sends one row and records what happened.
func (o *Outbox) send(ctx context.Context, name string, m messenger.Messenger, it *storage.OutboxItem) {
	d, err := o.store.DestinationByID(it.DestinationID)
	if err != nil {
		o.retry(name, it, err)
		return
	}
	if d == nil {
		o.dead(it, "destination deleted")
		return
	}
	var p Payload
	if err := json.Unmarshal(it.Payload, &p); err != nil {
		o.dead(it, "bad payload: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(ctx, o.sendTimeout)
	defer cancel()
	if mc, ok := m.(messenger.MemberChecker); ok && p.Member != nil {
		member, err := o.isMember(ctx, name, mc, p.Member)
		if err != nil {
			o.retry(name, it, fmt.Errorf("checking membership: %w", err))
			return
		}
		if !member {
			log.Printf("🔒 %s: user %s is in no chat of repository %d, DM not sent", name, p.Member.UserID, p.Member.RepoID)
			if err := o.store.MarkDone(it.ID); err != nil {
				log.Printf("❌ outbox: %v", err)
			}
			return
		}
	}
	to := messenger.Target{ChatID: d.ChatID, ThreadID: it.ThreadID}
	attempts, err := m.Send(ctx, to, p.Look, p.Lang, p.N)
	o.count(name, attempts)
	if to.ThreadID != "" && messenger.Classify(err) == messenger.ClassThreadNotFound {
		// The thread was deleted or closed: use the main chat from now on.
		log.Printf("⚠️ %s: thread %s of chat %s is gone, sending to the main chat: %v", name, to.ThreadID, d.ChatID, err)
		if err := o.store.ClearOutboxThread(it.ID); err != nil {
			log.Printf("❌ outbox: %v", err)
		}
		to.ThreadID = ""
		attempts, err = m.Send(ctx, to, p.Look, p.Lang, p.N)
		o.count(name, attempts)
	}

	if messenger.Classify(err) == messenger.ClassNeedsThread && o.makeThread != nil {
		id, makeErr := o.makeThread(ctx, d.ID, p.Topic, it.ThreadID)
		if makeErr != nil {
			err = fmt.Errorf("%w (creating a thread: %v)", err, makeErr)
		} else {
			if err := o.store.SetOutboxThread(it.ID, id); err != nil {
				log.Printf("❌ outbox: %v", err)
			}
			to.ThreadID = id
			attempts, err = m.Send(ctx, to, p.Look, p.Lang, p.N)
			o.count(name, attempts)
		}
	}

	switch messenger.Classify(err) {
	case messenger.ClassOK:
		if err := o.store.MarkDone(it.ID); err != nil {
			log.Printf("❌ outbox: %v", err)
		}
		if d.Inactive {
			// The chat accepts messages again (e.g. a requeued delivery).
			o.store.SetDestinationInactive(d.ID, false)
		}
	case messenger.ClassRateLimited:
		// The messenger said when to come back; this is not a failed attempt.
		wait := messenger.RetryAfter(err)
		if wait <= 0 {
			wait = Delay(1)
		}
		o.metrics.SendRetried(name)
		if err := o.store.MarkRetry(it.ID, it.Attempts, o.clock().Add(wait), err.Error()); err != nil {
			log.Printf("❌ outbox: %v", err)
		}
	case messenger.ClassChatGone:
		log.Printf("⚠️ %s: chat %s is gone, no more messages go there: %v", name, d.ChatID, err)
		o.dead(it, err.Error())
		if err := o.store.SetDestinationInactive(d.ID, true); err != nil {
			log.Printf("❌ outbox: %v", err)
		}
	case messenger.ClassRejected:
		// The same message would be refused again.
		log.Printf("❌ %s: chat %s refused a message, giving up on it: %v", name, d.ChatID, err)
		o.dead(it, err.Error())
	default:
		o.retry(name, it, err)
	}
}

// isMember reports whether the user is a member of a chat of the
// messenger subscribed to the repository. Answers are kept for memberTTL.
func (o *Outbox) isMember(ctx context.Context, name string, mc messenger.MemberChecker, c *MemberCheck) (bool, error) {
	chats, err := o.store.SubscribersIn(c.RepoID, name)
	if err != nil {
		return false, err
	}
	now := o.clock()
	for _, d := range chats {
		key := memberKey{name, d.ChatID, c.UserID}
		o.mu.Lock()
		a, ok := o.members[key]
		o.mu.Unlock()
		if !ok || now.Sub(a.at) >= memberTTL {
			member, err := mc.IsMember(ctx, d.ChatID, c.UserID)
			if err != nil {
				return false, err
			}
			a = memberAnswer{ok: member, at: now}
			o.mu.Lock()
			o.members[key] = a
			o.mu.Unlock()
		}
		if a.ok {
			return true, nil
		}
	}
	return false, nil
}

// retry schedules a failed row again, or gives up after maxAttempts.
func (o *Outbox) retry(name string, it *storage.OutboxItem, err error) {
	n := it.Attempts + 1
	if n >= maxAttempts {
		log.Printf("❌ %s: giving up on a message after %d attempts: %v", name, n, err)
		o.dead(it, err.Error())
		return
	}
	o.metrics.SendRetried(name)
	if err := o.store.MarkRetry(it.ID, n, o.clock().Add(Delay(n)), err.Error()); err != nil {
		log.Printf("❌ outbox: %v", err)
	}
}

func (o *Outbox) dead(it *storage.OutboxItem, reason string) {
	if err := o.store.MarkDead(it.ID, reason); err != nil {
		log.Printf("❌ outbox: %v", err)
	}
}

// count records every attempt of one send by format: an attempt that a
// later one replaced counts as "retried", so "error" counts only messages
// that were not delivered.
func (o *Outbox) count(name string, attempts []notify.Attempt) {
	for i, a := range attempts {
		if a.Err != nil && i < len(attempts)-1 {
			o.metrics.MessageRetried(name, a.Format)
		} else {
			o.metrics.MessageSent(name, a.Format, a.Err)
		}
		if i > 0 && a.Format == notify.FormatClassic && attempts[i-1].Format == notify.FormatRich {
			o.metrics.RichFallback()
		}
	}
}
