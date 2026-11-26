package outbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/notify"
	"GitHub-GitLab-Notifier/storage"
)

// memberEnv has chat a subscribed to a repository and the DM of user 5.
type memberEnv struct {
	*env
	repo storage.Repo
	dm   storage.Destination
}

func newMemberEnv(t *testing.T) *memberEnv {
	t.Helper()
	e := newEnv(t, plainCaps)
	repo, err := e.store.CreateRepo("https://github.com/octo/private", "GitHub", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.Subscribe(repo.ID, e.a.ID); err != nil {
		t.Fatal(err)
	}
	dm, _ := e.store.EnsureDestination("telegram", "5", storage.Destination{Language: "en"})
	if _, err := e.store.Link("telegram", "5", dm.ID, storage.Account{Platform: "github", Host: "github.com", AccountID: "10", Login: "alice"}); err != nil {
		t.Fatal(err)
	}
	// The DM is subscribed too; it must not count as a team chat.
	if err := e.store.Subscribe(repo.ID, dm.ID); err != nil {
		t.Fatal(err)
	}
	return &memberEnv{env: e, repo: repo, dm: dm}
}

func (e *memberEnv) enqueueDM(t *testing.T, heading string) string {
	t.Helper()
	e.seq++
	id := heading + "-" + string(rune('0'+e.seq))
	payload, _ := json.Marshal(Payload{Lang: i18n.EN, Look: "rich", N: notify.Notification{Heading: heading},
		Member: &MemberCheck{RepoID: e.repo.ID, UserID: "5"}})
	if _, err := e.store.EnqueueDelivery(storage.Delivery{DeliveryID: id, Platform: "GitHub", Event: "issue_comment",
		ReceivedAt: e.now, HTTPStatus: 202}, []storage.OutboxItem{{DestinationID: e.dm.ID, Payload: payload, NextAttemptAt: e.now}}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMemberGetsDM(t *testing.T) {
	e := newMemberEnv(t)
	e.m.Members = map[string]bool{"-1/5": true}
	id := e.enqueueDM(t, "hi")
	e.drain()
	if got := headings(e.m.SentList()); len(got) != 1 || e.status(t, id) != storage.DeliveryDelivered {
		t.Errorf("sent %v, status %s", got, e.status(t, id))
	}
	if e.m.MemberCalls != 1 {
		t.Errorf("%d member checks", e.m.MemberCalls)
	}
}

func TestNonMemberGetsNothing(t *testing.T) {
	e := newMemberEnv(t)
	id := e.enqueueDM(t, "hi")
	e.drain()
	if got := e.m.SentList(); len(got) != 0 {
		t.Errorf("sent %v", got)
	}
	if s := e.status(t, id); s != storage.DeliveryDelivered {
		t.Errorf("status %s, want the row finished", s)
	}
}

func TestMemberCheckErrorRetries(t *testing.T) {
	e := newMemberEnv(t)
	e.m.MemberErr = errors.New("429")
	id := e.enqueueDM(t, "hi")
	e.drain()
	if got := e.m.SentList(); len(got) != 0 || e.status(t, id) != storage.DeliveryQueued {
		t.Errorf("sent %v, status %s", got, e.status(t, id))
	}
	e.m.MemberErr = nil
	e.m.Members = map[string]bool{"-1/5": true}
	e.now = e.now.Add(time.Minute)
	e.drain()
	if got := e.m.SentList(); len(got) != 1 {
		t.Errorf("after retry sent %v", got)
	}
}

func TestMemberAnswerIsCached(t *testing.T) {
	e := newMemberEnv(t)
	e.m.Members = map[string]bool{"-1/5": true}
	e.enqueueDM(t, "one")
	e.drain()
	e.enqueueDM(t, "two")
	e.drain()
	if e.m.MemberCalls != 1 || len(e.m.SentList()) != 2 {
		t.Errorf("%d checks, %d sent", e.m.MemberCalls, len(e.m.SentList()))
	}
	e.now = e.now.Add(11 * time.Minute)
	e.enqueueDM(t, "three")
	e.drain()
	if e.m.MemberCalls != 2 {
		t.Errorf("stale answer used: %d checks", e.m.MemberCalls)
	}
}

// plain hides IsMember of the fake: an adapter that cannot check members.
type plain struct{ messenger.Messenger }

func TestAdapterWithoutMemberCheckSends(t *testing.T) {
	e := newMemberEnv(t)
	e.o = New(e.store, map[string]messenger.Messenger{"telegram": plain{e.m}}, e.metrics, func() time.Time { return e.now })
	e.enqueueDM(t, "hi")
	e.drain()
	if len(e.m.SentList()) != 1 || e.m.MemberCalls != 0 {
		t.Errorf("sent %d, checks %d", len(e.m.SentList()), e.m.MemberCalls)
	}
}

// twoChats subscribes a second team chat -2 to the repository.
func (e *memberEnv) twoChats(t *testing.T) {
	t.Helper()
	b, _ := e.store.EnsureDestination("telegram", "-2", storage.Destination{Language: "en"})
	if err := e.store.Subscribe(e.repo.ID, b.ID); err != nil {
		t.Fatal(err)
	}
}

func TestBrokenChatDoesNotBlockMemberCheck(t *testing.T) {
	e := newMemberEnv(t)
	e.twoChats(t)
	e.m.MemberErrs = map[string]error{"-1": fmt.Errorf("%w: no such chat", messenger.ErrRejected)}
	e.m.Members = map[string]bool{"-2/5": true}
	id := e.enqueueDM(t, "hi")
	e.drain()
	if len(e.m.SentList()) != 1 || e.status(t, id) != storage.DeliveryDelivered {
		t.Errorf("sent %d, status %s", len(e.m.SentList()), e.status(t, id))
	}
}

func TestTemporaryCheckErrorRetriesWhenNoChatSaysYes(t *testing.T) {
	e := newMemberEnv(t)
	e.twoChats(t)
	e.m.MemberErrs = map[string]error{"-2": errors.New("timeout")}
	id := e.enqueueDM(t, "hi")
	e.drain()
	if len(e.m.SentList()) != 0 || e.status(t, id) != storage.DeliveryQueued {
		t.Errorf("sent %d, status %s", len(e.m.SentList()), e.status(t, id))
	}
}

func TestRateLimitedCheckSpendsNoAttempt(t *testing.T) {
	e := newMemberEnv(t)
	e.m.MemberErr = &messenger.RateLimited{RetryAfter: time.Second, Err: errors.New("429")}
	id := e.enqueueDM(t, "hi")
	for range 15 {
		e.drain()
		e.now = e.now.Add(time.Hour)
	}
	if s := e.status(t, id); s != storage.DeliveryQueued {
		t.Errorf("status %s after 15 rate-limited checks", s)
	}
}
