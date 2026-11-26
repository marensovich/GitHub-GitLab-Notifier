package commands

import (
	"testing"

	"GitHub-GitLab-Notifier/storage"
)

func TestCommandInGroupReactivatesDM(t *testing.T) {
	e := newEnv(t, telegramCaps)
	dmDest, err := e.store.EnsureDestination("telegram", "5", storage.Destination{Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.Link("telegram", "5", dmDest.ID, storage.Account{Platform: "github", Host: "github.com", AccountID: "10", Login: "alice"}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SetDestinationInactive(dmDest.ID, true); err != nil {
		t.Fatal(err)
	}
	e.run(in("/help"))
	if d := e.dest(t, "5"); d == nil || d.Inactive {
		t.Errorf("DM still inactive: %+v", d)
	}
}

func TestPlainHTTPGitLab(t *testing.T) {
	e := newEnv(t, telegramCaps)
	if _, err := e.store.CreateRepo("http://gitlab.lan/g/p", "GitLab", "s"); err != nil {
		t.Fatal(err)
	}
	if got := e.core.apiBase("gitlab", "gitlab.lan"); got != "http://gitlab.lan/api/v4" {
		t.Errorf("plain-http GitLab: %q", got)
	}
	if got := e.core.apiBase("gitlab", "gitlab.example.com"); got != "https://gitlab.example.com/api/v4" {
		t.Errorf("other GitLab: %q", got)
	}
	if got := e.core.apiBase("github", "github.com"); got != "https://api.github.com" {
		t.Errorf("github: %q", got)
	}
}
