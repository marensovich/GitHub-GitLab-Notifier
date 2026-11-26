package server

import (
	"net/http"
	"testing"

	"GitHub-GitLab-Notifier/storage"
)

func TestAllChatsInactiveIsNotUnconnected(t *testing.T) {
	e := newEnv(t, Config{})
	repo, err := e.store.CreateRepo("https://github.com/Octo/Hello", "GitHub", "")
	if err != nil {
		t.Fatal(err)
	}
	d, _ := e.store.EnsureDestination("telegram", "-1", storage.Destination{Language: "en"})
	if err := e.store.Subscribe(repo.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SetDestinationInactive(d.ID, true); err != nil {
		t.Fatal(err)
	}
	if rec := e.post(t, ghHeaders("star"), starPayload); rec.Code != http.StatusOK {
		t.Errorf("all chats inactive: %d %q", rec.Code, rec.Body.String())
	}
}

func TestStoreErrorIsServerError(t *testing.T) {
	e := newEnv(t, Config{})
	e.store.Close()
	if rec := e.postOnly(t, ghHeaders("star"), starPayload); rec.Code != http.StatusInternalServerError {
		t.Errorf("store down: %d %q", rec.Code, rec.Body.String())
	}
}
