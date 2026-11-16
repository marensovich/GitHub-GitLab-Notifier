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

func TestRepoLifecycle(t *testing.T) {
	s := openTest(t)

	if err := s.SaveChat(ChatConfig{ChatID: 1, Language: "en"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRepo(RepoInfo{ChatID: 1, RepoURL: "https://github.com/octo/hello", Platform: "GitHub", Secret: "x"}); err != nil {
		t.Fatal(err)
	}

	repo := s.FindRepo("http://github.com/OCTO/hello/")
	if repo == nil || repo.Secret != "x" {
		t.Fatalf("FindRepo: got %+v", repo)
	}

	repos, err := s.GetReposByChat(1)
	if err != nil || len(repos) != 1 {
		t.Fatalf("GetReposByChat: %v %+v", err, repos)
	}

	if ok, _ := s.DeleteRepo(2, "https://github.com/octo/hello"); ok {
		t.Error("DeleteRepo must not delete a repo of another chat")
	}
	if ok, err := s.DeleteRepo(1, "github.com/octo/hello"); !ok || err != nil {
		t.Errorf("DeleteRepo: ok=%v err=%v", ok, err)
	}
	if s.FindRepo("https://github.com/octo/hello") != nil {
		t.Error("repo still present after delete")
	}
}

func TestSetChatLanguage(t *testing.T) {
	s := openTest(t)

	if err := s.SetChatLanguage(7, "ru", true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChatLanguage(7, "en", false); err != nil {
		t.Fatal(err)
	}
	chat, err := s.GetChat(7)
	if err != nil {
		t.Fatal(err)
	}
	if chat.Language != "en" || !chat.IsForum {
		t.Errorf("got %+v, want language=en and is_forum preserved", chat)
	}
}

func TestEventMapping(t *testing.T) {
	s := openTest(t)
	if err := s.SaveChat(ChatConfig{ChatID: 1, Language: "en", IsForum: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTopic(TopicInfo{TopicID: 10, ChatID: 1, Name: "🚀 Pushes", EventType: "push"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEventTopic(1, "push", 10); err != nil {
		t.Fatal(err)
	}
	if got := s.GetTopicByEvent(1, "push"); got != 10 {
		t.Errorf("GetTopicByEvent = %d, want 10", got)
	}
	if got := s.GetTopicByEvent(1, "ci"); got != 0 {
		t.Errorf("unmapped event: got %d, want 0", got)
	}
}

func TestGetEventMappings(t *testing.T) {
	s := openTest(t)

	if err := s.SaveChat(ChatConfig{ChatID: 1, Language: "en", IsForum: true}); err != nil {
		t.Fatal(err)
	}
	for _, topic := range []TopicInfo{{TopicID: 10, ChatID: 1, Name: "Pushes"}, {TopicID: 20, ChatID: 1, Name: "CI/CD"}} {
		if err := s.SaveTopic(topic); err != nil {
			t.Fatal(err)
		}
	}
	for event, topic := range map[string]int{"push": 10, "pipeline": 20, "workflow_run": 20} {
		if err := s.SetEventTopic(1, event, topic); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.GetEventMappings(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got["push"] != 10 || got["pipeline"] != 20 || got["workflow_run"] != 20 {
		t.Errorf("GetEventMappings(1) = %v", got)
	}

	empty, err := s.GetEventMappings(2)
	if err != nil || len(empty) != 0 {
		t.Errorf("GetEventMappings(2) = %v, %v; want empty", empty, err)
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
		platform, id string
		want         bool
	}{
		{"GitHub", "a", true},
		{"GitLab", "a", false}, // IDs are per platform
		{"GitHub", "b", false}, // failed, so a redelivery must go through
		{"GitHub", "", false},
		{"GitLab", "", false}, // empty IDs never dedupe
	} {
		got, err := s.WasDelivered(c.platform, c.id)
		must(err)
		if got != c.want {
			t.Errorf("WasDelivered(%s, %q) = %v, want %v", c.platform, c.id, got, c.want)
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
}
