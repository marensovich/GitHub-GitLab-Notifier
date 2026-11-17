package storage

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func outboxStore(t *testing.T) (*Store, Destination, Destination) {
	t.Helper()
	s := openTest(t)
	a, err := s.EnsureDestination("telegram", "-1", Destination{Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.EnsureDestination("telegram", "-2", Destination{Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	return s, a, b
}

func enqueue(t *testing.T, s *Store, id string, dests ...Destination) int64 {
	t.Helper()
	var items []OutboxItem
	for _, d := range dests {
		items = append(items, OutboxItem{DestinationID: d.ID, EventType: "star", Payload: []byte(`{}`), NextAttemptAt: t0})
	}
	row, err := s.EnqueueDelivery(Delivery{DeliveryID: id, Platform: "GitHub", Event: "star", ReceivedAt: t0, HTTPStatus: 202}, items)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func claim(t *testing.T, s *Store, busy map[int64]bool) *OutboxItem {
	t.Helper()
	it, err := s.ClaimNext("telegram", t0, busy)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func TestEnqueueIsAtomic(t *testing.T) {
	s, a, _ := outboxStore(t)
	items := []OutboxItem{
		{DestinationID: a.ID, Payload: []byte(`{}`), NextAttemptAt: t0},
		{DestinationID: 9999, Payload: []byte(`{}`), NextAttemptAt: t0}, // no such destination
	}
	if _, err := s.EnqueueDelivery(Delivery{DeliveryID: "d", Platform: "GitHub", ReceivedAt: t0}, items); err == nil {
		t.Fatal("want an error for an unknown destination")
	}
	if list, _ := s.RecentDeliveries(10); len(list) != 0 {
		t.Errorf("delivery row left behind: %+v", list)
	}
	if p, _, _ := s.OutboxCounts(); p != 0 {
		t.Errorf("pending = %d, want 0", p)
	}
}

func TestClaimKeepsDestinationOrder(t *testing.T) {
	s, a, b := outboxStore(t)
	enqueue(t, s, "1", a)
	enqueue(t, s, "2", a, b)

	first := claim(t, s, nil)
	if first == nil || first.DestinationID != a.ID || first.State != "sending" {
		t.Fatalf("first claim = %+v", first)
	}
	// a has a row in flight: only b may go.
	second := claim(t, s, nil)
	if second == nil || second.DestinationID != b.ID {
		t.Fatalf("second claim = %+v, want destination b", second)
	}
	if third := claim(t, s, nil); third != nil {
		t.Fatalf("third claim = %+v, want nothing while both are sending", third)
	}

	// A retry of a's first row holds back its second one.
	if err := s.MarkRetry(first.ID, 1, t0.Add(time.Minute), "boom"); err != nil {
		t.Fatal(err)
	}
	if it := claim(t, s, map[int64]bool{b.ID: true}); it != nil {
		t.Fatalf("claimed %+v before a's older row is due", it)
	}
	it, err := s.ClaimNext("telegram", t0.Add(time.Minute), nil)
	if err != nil || it == nil || it.ID != first.ID || it.Attempts != 1 || it.LastError != "boom" {
		t.Fatalf("after the delay got %+v, %v; want the retried row", it, err)
	}
}

func TestClaimSkipsBusyDestination(t *testing.T) {
	s, a, b := outboxStore(t)
	enqueue(t, s, "1", a)
	enqueue(t, s, "2", b)
	it := claim(t, s, map[int64]bool{a.ID: true})
	if it == nil || it.DestinationID != b.ID {
		t.Fatalf("claim = %+v, want destination b", it)
	}
	if _, err := s.ClaimNext("discord", t0, nil); err != nil {
		t.Fatal(err)
	}
	if it, _ := s.ClaimNext("discord", t0, nil); it != nil {
		t.Errorf("a discord worker claimed a telegram row: %+v", it)
	}
}

func TestResetSending(t *testing.T) {
	s, a, _ := outboxStore(t)
	enqueue(t, s, "1", a)
	if claim(t, s, nil) == nil {
		t.Fatal("nothing claimed")
	}
	n, err := s.ResetSending()
	if err != nil || n != 1 {
		t.Fatalf("ResetSending = %d, %v", n, err)
	}
	if claim(t, s, nil) == nil {
		t.Error("the reset row cannot be claimed again")
	}
}

func deliveryStatus(t *testing.T, s *Store, id string) string {
	t.Helper()
	status, _, err := s.DeliveryState("GitHub", id)
	if err != nil {
		t.Fatal(err)
	}
	return status
}

func TestDeliveryStatusFollowsRows(t *testing.T) {
	s, a, b := outboxStore(t)
	enqueue(t, s, "ok", a, b)
	if got := deliveryStatus(t, s, "ok"); got != DeliveryQueued {
		t.Fatalf("status = %q, want queued", got)
	}
	x, y := claim(t, s, nil), claim(t, s, nil)
	must(t, s.MarkDone(x.ID))
	if got := deliveryStatus(t, s, "ok"); got != DeliveryQueued {
		t.Errorf("one of two done: status = %q, want queued", got)
	}
	must(t, s.MarkDone(y.ID))
	if got := deliveryStatus(t, s, "ok"); got != DeliveryDelivered {
		t.Errorf("all done: status = %q, want delivered", got)
	}

	enqueue(t, s, "bad", a)
	must(t, s.MarkDead(claim(t, s, nil).ID, "chat gone"))
	if got := deliveryStatus(t, s, "bad"); got != DeliveryFailed {
		t.Errorf("dead: status = %q, want failed", got)
	}
	list, _ := s.RecentDeliveries(1)
	if list[0].Error != "chat gone" {
		t.Errorf("failed delivery error = %q", list[0].Error)
	}
	if got := deliveryStatus(t, s, "none"); got != "" {
		t.Errorf("unknown delivery: status = %q", got)
	}
}

func TestRequeueDead(t *testing.T) {
	s, a, _ := outboxStore(t)
	row := enqueue(t, s, "d", a)
	it := claim(t, s, nil)
	must(t, s.MarkRetry(it.ID, 9, t0, "temporary"))
	must(t, s.MarkDead(claim(t, s, nil).ID, "temporary"))
	if _, dead, _ := s.OutboxCounts(); dead != 1 {
		t.Fatalf("dead = %d", dead)
	}

	n, err := s.RequeueDead(row)
	if err != nil || n != 1 {
		t.Fatalf("RequeueDead = %d, %v", n, err)
	}
	if got := deliveryStatus(t, s, "d"); got != DeliveryQueued {
		t.Errorf("status = %q, want queued", got)
	}
	again, err := s.ClaimNext("telegram", time.Now(), nil)
	if err != nil || again == nil || again.Attempts != 0 {
		t.Fatalf("requeued row = %+v, %v; want it claimable with no attempts", again, err)
	}
}

func TestPruneOutbox(t *testing.T) {
	s, a, b := outboxStore(t)
	enqueue(t, s, "1", a, b)
	x, y := claim(t, s, nil), claim(t, s, nil)
	must(t, s.MarkDone(x.ID))
	must(t, s.MarkDead(y.ID, "gone"))
	enqueue(t, s, "2", a) // pending rows are never pruned

	now := time.Now()
	if n, _ := s.PruneOutbox(now.Add(6 * 24 * time.Hour)); n != 0 {
		t.Errorf("pruned %d rows after 6 days", n)
	}
	if n, _ := s.PruneOutbox(now.Add(8 * 24 * time.Hour)); n != 1 {
		t.Errorf("pruned %d rows after 8 days, want the done one", n)
	}
	if n, _ := s.PruneOutbox(now.Add(31 * 24 * time.Hour)); n != 1 {
		t.Errorf("pruned %d rows after 31 days, want the dead one", n)
	}
	if p, _, _ := s.OutboxCounts(); p != 1 {
		t.Errorf("pending = %d, want 1", p)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
