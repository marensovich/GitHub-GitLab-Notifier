package outbox

import (
	"context"
	"fmt"
	"testing"

	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/storage"
)

func TestMovedChatFollowed(t *testing.T) {
	e := newEnv(t, plainCaps)
	e.m.Fail(func(_ int, to messenger.Target) error {
		if to.ChatID == "-1" {
			return &messenger.ChatMoved{To: "-1009", Err: fmt.Errorf("upgraded")}
		}
		return nil
	})
	id := e.enqueue(t, "x", "", e.a)
	e.drain()
	e.drain()
	sent := e.m.SentList()
	if e.status(t, id) != storage.DeliveryDelivered || len(sent) != 1 || sent[0].Target.ChatID != "-1009" {
		t.Fatalf("status %q, sent %+v", e.status(t, id), sent)
	}
	if d, _ := e.store.DestinationByID(e.a.ID); d == nil || d.ChatID != "-1009" || d.Inactive {
		t.Errorf("destination %+v", d)
	}
}

func TestMovedChatTakenGivesUp(t *testing.T) {
	e := newEnv(t, plainCaps)
	e.m.Fail(func(_ int, to messenger.Target) error {
		return &messenger.ChatMoved{To: "-2", Err: fmt.Errorf("upgraded")}
	})
	id := e.enqueue(t, "x", "", e.a)
	e.drain()
	if e.status(t, id) != storage.DeliveryFailed {
		t.Errorf("status %q", e.status(t, id))
	}
	if d, _ := e.store.DestinationByID(e.a.ID); d == nil || !d.Inactive {
		t.Errorf("destination %+v", d)
	}
}

func TestThreadMakerRefusalGivesUp(t *testing.T) {
	e := newEnv(t, plainCaps)
	e.m.Fail(func(int, messenger.Target) error { return messenger.ErrNeedsThread })
	e.o.SetThreadMaker(func(context.Context, int64, string, string) (string, error) {
		return "", fmt.Errorf("%w: a tag is required", messenger.ErrRejected)
	})
	id := e.enqueue(t, "x", "", e.a)
	e.drain()
	if e.status(t, id) != storage.DeliveryFailed {
		t.Errorf("status %q, want failed at once", e.status(t, id))
	}
	if d, _ := e.store.DestinationByID(e.a.ID); d == nil || d.Inactive {
		t.Errorf("destination %+v", d)
	}
}
