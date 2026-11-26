package personal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"GitHub-GitLab-Notifier/storage"
)

const token = "ghp_SECRET123"

func verifier(t *testing.T, h http.HandlerFunc) (Verifier, *[]string) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+" "+r.Header.Get("Authorization")+r.Header.Get("PRIVATE-TOKEN"))
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return Verifier{HTTP: srv.Client(), BaseURL: func(platform, host string) string { return srv.URL + "/" + platform + "/" + host }}, &paths
}

func TestWhoAmIGitHub(t *testing.T) {
	v, paths := verifier(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":10,"login":"Alice"}`))
	})
	a, err := v.WhoAmI(context.Background(), "github", "github.com", token)
	if err != nil || a != (storage.Account{Platform: "github", Host: "github.com", AccountID: "10", Login: "Alice"}) {
		t.Fatalf("%+v %v", a, err)
	}
	if (*paths)[0] != "/github/github.com/user Bearer "+token {
		t.Errorf("request %q", (*paths)[0])
	}
}

func TestWhoAmIGitLab(t *testing.T) {
	v, paths := verifier(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":7,"username":"bob"}`))
	})
	a, err := v.WhoAmI(context.Background(), "gitlab", "git.example.com", token)
	if err != nil || a != (storage.Account{Platform: "gitlab", Host: "git.example.com", AccountID: "7", Login: "bob"}) {
		t.Fatalf("%+v %v", a, err)
	}
	if (*paths)[0] != "/gitlab/git.example.com/user "+token {
		t.Errorf("request %q", (*paths)[0])
	}
}

func TestWhoAmIBadToken(t *testing.T) {
	for _, code := range []int{401, 403} {
		v, _ := verifier(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) })
		if _, err := v.WhoAmI(context.Background(), "github", "github.com", token); !errors.Is(err, ErrBadToken) {
			t.Errorf("%d: %v", code, err)
		}
	}
}

func TestWhoAmIErrorsHideToken(t *testing.T) {
	v, _ := verifier(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("bad token " + token))
	})
	_, err := v.WhoAmI(context.Background(), "github", "github.com", token)
	if err == nil || strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "github.com answered 500") {
		t.Errorf("%v", err)
	}
	gone := Verifier{HTTP: http.DefaultClient, BaseURL: func(string, string) string { return "http://127.0.0.1:1/" + token }}
	_, err = gone.WhoAmI(context.Background(), "gitlab", "nowhere.test", token)
	if err == nil || strings.Contains(err.Error(), token) || err.Error() != "nowhere.test: unreachable" {
		t.Errorf("unreachable: %v", err)
	}
}

func TestDefaultBaseURL(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"github", "github.com"}:  "https://api.github.com",
		{"github", "gh.corp"}:     "https://gh.corp/api/v3",
		{"gitlab", "gitlab.com"}:  "https://gitlab.com/api/v4",
		{"gitlab", "git.example"}: "https://git.example/api/v4",
	} {
		if got := DefaultBaseURL(in[0], in[1]); got != want {
			t.Errorf("%v: %s", in, got)
		}
	}
}
