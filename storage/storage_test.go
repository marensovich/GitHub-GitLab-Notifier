package storage

import (
	"path/filepath"
	"testing"
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
