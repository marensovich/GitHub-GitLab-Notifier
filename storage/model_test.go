package storage

import "testing"

func dest(t *testing.T, s *Store, messenger, chatID string) Destination {
	t.Helper()
	d, err := s.EnsureDestination(messenger, chatID, Destination{Language: "en", Format: FormatRich, UseThreads: true})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestEnsureDestinationKeepsSettings(t *testing.T) {
	s := openTest(t)
	d := dest(t, s, "telegram", "-100")
	if err := s.SetDestinationSettings(d.ID, FormatClassic, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDestinationLanguage(d.ID, "ru"); err != nil {
		t.Fatal(err)
	}
	again, err := s.EnsureDestination("telegram", "-100", Destination{Language: "en", Format: FormatRich, UseThreads: true, Title: "Team"})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != d.ID || again.Format != FormatClassic || again.UseThreads || again.Language != "ru" || again.Title != "Team" {
		t.Errorf("EnsureDestination = %+v, want stored settings with the new title", again)
	}
	other := dest(t, s, "discord", "-100")
	if other.ID == d.ID {
		t.Error("the same chat ID in another messenger is the same destination")
	}
	if err := s.SetDestinationSettings(d.ID, "fancy", true); err == nil {
		t.Error("unknown format accepted")
	}
}

func TestSetDestinationInactive(t *testing.T) {
	s := openTest(t)
	d := dest(t, s, "telegram", "1")
	if err := s.SetDestinationInactive(d.ID, true); err != nil {
		t.Fatal(err)
	}
	got, _ := s.DestinationByID(d.ID)
	if got == nil || !got.Inactive {
		t.Fatalf("destination = %+v, want inactive", got)
	}
}

func TestSubscribeIdempotent(t *testing.T) {
	s := openTest(t)
	d := dest(t, s, "telegram", "1")
	r, err := s.CreateRepo("https://github.com/acme/api", "GitHub", "x")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Subscribe(r.ID, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := s.SubscriberCount(r.ID); n != 1 {
		t.Errorf("subscribers = %d, want 1", n)
	}
	repos, err := s.SubscriptionsOf(d.ID)
	if err != nil || len(repos) != 1 || repos[0].URL != "https://github.com/acme/api" {
		t.Errorf("SubscriptionsOf = %+v, %v", repos, err)
	}
	if _, err := s.CreateRepo("HTTPS://github.com/acme/api/", "GitHub", "y"); err == nil {
		t.Error("the same repository was created twice")
	}
}

func TestUnsubscribeDeletesLastRepo(t *testing.T) {
	s := openTest(t)
	a, b := dest(t, s, "telegram", "1"), dest(t, s, "telegram", "2")
	r, _ := s.CreateRepo("https://github.com/acme/api", "GitHub", "x")
	s.Subscribe(r.ID, a.ID)
	s.Subscribe(r.ID, b.ID)

	ok, err := s.Unsubscribe(a.ID, "github.com/acme/api")
	if err != nil || !ok {
		t.Fatalf("Unsubscribe = %v, %v", ok, err)
	}
	if got, _ := s.FindRepo("github.com/acme/api"); got == nil {
		t.Fatal("repository deleted while another chat is subscribed")
	}
	if ok, _ := s.Unsubscribe(a.ID, "github.com/acme/api"); ok {
		t.Error("second Unsubscribe reported a removal")
	}
	if ok, _ := s.Unsubscribe(b.ID, "https://github.com/acme/api"); !ok {
		t.Error("last Unsubscribe failed")
	}
	if got, _ := s.FindRepo("github.com/acme/api"); got != nil {
		t.Errorf("repository kept after its last subscription: %+v", got)
	}
}

func TestSubscribersSkipInactive(t *testing.T) {
	s := openTest(t)
	a, b := dest(t, s, "telegram", "1"), dest(t, s, "telegram", "2")
	r, _ := s.CreateRepo("https://github.com/acme/api", "GitHub", "x")
	s.Subscribe(r.ID, a.ID)
	s.Subscribe(r.ID, b.ID)
	s.SetDestinationInactive(b.ID, true)

	subs, err := s.Subscribers(r.ID)
	if err != nil || len(subs) != 1 || subs[0].ID != a.ID {
		t.Errorf("Subscribers = %+v, %v; want only the active one", subs, err)
	}
}

func TestThreadsAndRoutes(t *testing.T) {
	s := openTest(t)
	d := dest(t, s, "telegram", "-100")
	if got := s.ThreadFor(d.ID, "push"); got != "" {
		t.Errorf("ThreadFor without a route = %q, want empty", got)
	}
	if err := s.SaveThread(Thread{DestinationID: d.ID, ThreadID: "9", Name: "Pushes"}, "push"); err != nil {
		t.Fatal(err)
	}
	if got := s.ThreadFor(d.ID, "push"); got != "9" {
		t.Errorf("ThreadFor = %q, want 9", got)
	}
	routes, _ := s.Routes(d.ID)
	if routes["push"] != "9" {
		t.Errorf("Routes = %v", routes)
	}
	if err := s.DeleteThread(d.ID, "9"); err != nil {
		t.Fatal(err)
	}
	if got := s.ThreadFor(d.ID, "push"); got != "" {
		t.Errorf("route kept after its thread was deleted: %q", got)
	}
}
