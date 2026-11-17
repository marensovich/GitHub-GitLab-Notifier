package storage

import (
	"testing"
	"time"
)

func TestDeadItemsNewestFirst(t *testing.T) {
	s, a, b := outboxStore(t)
	enqueue(t, s, "d1", a)
	enqueue(t, s, "d2", b)
	first := claim(t, s, nil)
	must(t, s.MarkDead(first.ID, "chat gone"))
	second := claim(t, s, nil)
	must(t, s.MarkDead(second.ID, "too many attempts"))

	items, err := s.DeadItems(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != second.ID || items[0].LastError != "too many attempts" || items[1].State != OutboxDead {
		t.Errorf("DeadItems = %+v", items)
	}
	if items, _ := s.DeadItems(1); len(items) != 1 {
		t.Errorf("limit not applied: %d items", len(items))
	}
}

func TestUnconnectedRepos(t *testing.T) {
	s := openTest(t)
	now := time.Now()
	log := func(url, status, errText string, at time.Time) {
		must(t, s.RecordDelivery(Delivery{Platform: "GitHub", Event: "push", RepoURL: url, ReceivedAt: at, Status: status, HTTPStatus: 202, Error: errText}))
	}
	log("https://github.com/a/one", DeliveryIgnored, "not connected", now)
	log("https://github.com/a/one", DeliveryIgnored, "not connected", now.Add(-time.Hour))
	log("https://github.com/a/two", DeliveryIgnored, "not connected", now)
	log("https://github.com/a/old", DeliveryIgnored, "not connected", now.Add(-8*24*time.Hour))
	log("https://github.com/a/skip", DeliveryIgnored, "", now) // skipped event, not "not connected"

	got, err := s.UnconnectedRepos(now.Add(-7 * 24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	want := []RepoCount{{"https://github.com/a/one", 2}, {"https://github.com/a/two", 1}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("UnconnectedRepos = %+v, want %+v", got, want)
	}
}
