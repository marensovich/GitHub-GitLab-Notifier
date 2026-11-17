package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNormalizeRepoURL(t *testing.T) {
	want := "github.com/octo/hello"
	for _, in := range []string{
		"https://github.com/octo/hello",
		"http://github.com/Octo/Hello/",
		"https://www.github.com/octo/hello.git",
		"  github.com/octo/hello.git/ ",
	} {
		if got := NormalizeRepoURL(in); got != want {
			t.Errorf("NormalizeRepoURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeliveriesLog(t *testing.T) {
	s := openTest(t)
	now := time.Now()

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.RecordDelivery(Delivery{DeliveryID: "a", Platform: "GitHub", Event: "push", ReceivedAt: now.Add(-48 * time.Hour), Status: DeliveryDelivered, HTTPStatus: 200}))
	must(s.RecordDelivery(Delivery{DeliveryID: "b", Platform: "GitHub", Event: "push", ReceivedAt: now, Status: DeliveryFailed, HTTPStatus: 200, Error: "boom", Duration: 1500 * time.Millisecond}))
	must(s.RecordDelivery(Delivery{Platform: "GitLab", Event: "Push Hook", ReceivedAt: now, Status: DeliveryDelivered, HTTPStatus: 200}))

	for _, c := range []struct {
		platform, id, want string
	}{
		{"GitHub", "a", DeliveryDelivered},
		{"GitLab", "a", ""}, // IDs are per platform
		{"GitHub", "b", DeliveryFailed},
		{"GitHub", "", ""},
		{"GitLab", "", ""}, // empty IDs never dedupe
	} {
		got, _, err := s.DeliveryState(c.platform, c.id)
		must(err)
		if got != c.want {
			t.Errorf("DeliveryState(%s, %q) = %q, want %q", c.platform, c.id, got, c.want)
		}
	}

	list, err := s.RecentDeliveries(10)
	must(err)
	if len(list) != 3 || list[2].DeliveryID != "a" {
		t.Fatalf("RecentDeliveries order: %+v", list)
	}
	if list[0].Duration != 1500*time.Millisecond && list[1].Duration != 1500*time.Millisecond {
		t.Errorf("duration not kept: %+v", list[:2])
	}

	n, err := s.PruneDeliveries(now.Add(-24 * time.Hour))
	must(err)
	if n != 1 {
		t.Errorf("pruned %d, want 1", n)
	}
	if list, _ := s.RecentDeliveries(10); len(list) != 2 {
		t.Errorf("after prune: %d deliveries, want 2", len(list))
	}

	// A logged duplicate is not a state of its own.
	must(s.RecordDelivery(Delivery{DeliveryID: "b", Platform: "GitHub", Event: "push", ReceivedAt: now, Status: DeliveryDuplicate, HTTPStatus: 200}))
	if got, _, _ := s.DeliveryState("GitHub", "b"); got != DeliveryFailed {
		t.Errorf("after a duplicate row: %q, want failed", got)
	}
}
