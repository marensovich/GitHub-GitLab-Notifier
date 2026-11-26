package commands

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/personal"
)

// accounts answers /user for the tokens tok-<login> of known logins.
type accounts struct {
	mu   sync.Mutex
	hits []string
}

func (e *env) accounts(t *testing.T) *accounts {
	t.Helper()
	a := &accounts{}
	ids := map[string]string{"alice": "10", "bob": "20"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.hits = append(a.hits, r.URL.Path)
		a.mu.Unlock()
		tok := r.Header.Get("Authorization") + r.Header.Get("PRIVATE-TOKEN")
		login := strings.TrimPrefix(strings.TrimPrefix(tok, "Bearer "), "tok-")
		id, ok := ids[login]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"id":` + id + `,"login":"` + login + `","username":"` + login + `"}`))
	}))
	t.Cleanup(srv.Close)
	e.core.Verifier = personal.Verifier{HTTP: srv.Client(), BaseURL: func(platform, host string) string {
		return srv.URL + "/" + platform + "/" + host
	}}
	return a
}

func (a *accounts) calls() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.hits)
}

// dm is a command in the private chat of user id.
func dm(id, text string) messenger.Command {
	c := in(text)
	c.ChatID, c.UserID, c.ChatTitle, c.IsPrivate, c.IsForum = id, id, "", true, false
	return c
}

func inDM(id string) func(*messenger.Press) {
	return func(p *messenger.Press) { p.ChatID, p.UserID, p.IsPrivate, p.IsForum = id, id, true, false }
}

func TestLinkInGroupRefused(t *testing.T) {
	e := newEnv(t, telegramCaps)
	a := e.accounts(t)
	got := e.run(in("/link github tok-alice"))
	if got != i18n.T(i18n.EN, i18n.MsgLinkPrivateOnly) || len(e.m.Deleted) != 1 || len(a.calls()) != 0 {
		t.Errorf("reply %q, deleted %d, calls %v", got, len(e.m.Deleted), a.calls())
	}
	if p, _, _ := e.store.PersonOf("telegram", "5"); p != nil {
		t.Errorf("linked %+v", p)
	}
}

func TestLinkInDM(t *testing.T) {
	e := newEnv(t, telegramCaps)
	a := e.accounts(t)
	got := e.run(dm("5", "/link github tok-alice"))
	if !strings.Contains(got, "alice") || strings.Contains(got, "tok-alice") || len(e.m.Deleted) != 1 {
		t.Errorf("reply %q, deleted %d", got, len(e.m.Deleted))
	}
	if c := a.calls(); len(c) != 1 || c[0] != "/github/github.com/user" {
		t.Errorf("calls %v", c)
	}
	p, accs, err := e.store.PersonOf("telegram", "5")
	if err != nil || p == nil || len(accs) != 1 || accs[0].Login != "alice" || accs[0].AccountID != "10" {
		t.Fatalf("%+v %+v %v", p, accs, err)
	}
	if d := e.dest(t, "5"); d == nil || d.ID != p.DestinationID {
		t.Errorf("DM destination %+v", d)
	}
}

func TestLinkBadToken(t *testing.T) {
	e := newEnv(t, telegramCaps)
	e.accounts(t)
	got := e.run(dm("5", "/link github tok-nobody"))
	if got != i18n.T(i18n.EN, i18n.MsgLinkBadToken) || strings.Contains(got, "tok-nobody") || len(e.m.Deleted) != 1 {
		t.Errorf("reply %q", got)
	}
}

func TestLinkUsage(t *testing.T) {
	e := newEnv(t, telegramCaps)
	for _, text := range []string{"/link", "/link svn tok"} {
		if got := e.run(dm("5", text)); got != i18n.T(i18n.EN, i18n.MsgLinkUsage) {
			t.Errorf("%s: %q", text, got)
		}
	}
}

func TestLinkGitLabHost(t *testing.T) {
	e := newEnv(t, telegramCaps)
	a := e.accounts(t)
	if got := e.run(dm("5", "/link gitlab tok-bob")); got != i18n.T(i18n.EN, i18n.MsgLinkNeedHost) {
		t.Errorf("no GitLab repos: %q", got)
	}
	_, _ = e.store.CreateRepo("https://git.one.test/a/b", "GitLab", "")
	e.run(dm("5", "/link gitlab tok-bob"))
	if c := a.calls(); len(c) != 1 || c[0] != "/gitlab/git.one.test/user" {
		t.Errorf("one host: calls %v", c)
	}
	_, _ = e.store.CreateRepo("https://git.two.test/a/b", "GitLab", "")
	got := e.run(dm("5", "/link gitlab tok-bob"))
	if !strings.Contains(got, "git.one.test") || !strings.Contains(got, "git.two.test") || len(a.calls()) != 1 {
		t.Errorf("two hosts: %q, calls %v", got, a.calls())
	}
	e.run(dm("5", "/link gitlab tok-bob https://git.two.test/"))
	if c := a.calls(); len(c) != 2 || c[1] != "/gitlab/git.two.test/user" {
		t.Errorf("named host: calls %v", c)
	}
}

func TestUnlink(t *testing.T) {
	e := newEnv(t, telegramCaps)
	e.accounts(t)
	e.run(dm("5", "/link github tok-alice"))
	e.run(dm("6", "/link github tok-bob"))

	// Someone else's login: nothing of the sender's to remove.
	if got := e.run(dm("5", "/unlink github bob")); got != i18n.T(i18n.EN, i18n.MsgUnlinkNone) {
		t.Errorf("non-admin: %q", got)
	}
	if _, accs, _ := e.store.PersonOf("telegram", "6"); len(accs) != 1 {
		t.Errorf("bob unlinked by alice")
	}
	if got := e.run(dm("5", "/unlink github")); !strings.Contains(got, "1") {
		t.Errorf("own: %q", got)
	}
	if _, accs, _ := e.store.PersonOf("telegram", "5"); len(accs) != 0 {
		t.Errorf("alice still linked")
	}
	// A bot administrator removes anyone's link.
	e.run(dm("900", "/unlink github bob"))
	if _, accs, _ := e.store.PersonOf("telegram", "6"); len(accs) != 0 {
		t.Errorf("admin could not unlink bob")
	}
	if got := e.run(dm("5", "/unlink")); got != i18n.T(i18n.EN, i18n.MsgUnlinkUsage) {
		t.Errorf("usage: %q", got)
	}
}

func TestMeListsAndToggles(t *testing.T) {
	e := newEnv(t, telegramCaps)
	if got := e.run(dm("5", "/me")); got != i18n.T(i18n.EN, i18n.MsgMeNone) {
		t.Errorf("nothing linked: %q", got)
	}
	e.accounts(t)
	e.run(dm("5", "/link github tok-alice"))
	got := e.run(dm("5", "/me"))
	if !strings.Contains(got, "alice") || len(marked(e.lastChoices())) != 4 {
		t.Fatalf("me: %q %+v", got, e.lastChoices())
	}
	e.press("me:mention", inDM("5"))
	p, _, _ := e.store.PersonOf("telegram", "5")
	if !slices.Equal(p.Muted, []string{"mention"}) {
		t.Errorf("muted %v", p.Muted)
	}
	if on := marked(e.lastChoices()); len(on) != 3 || slices.Contains(on, "me:mention") {
		t.Errorf("after press %v", on)
	}
	e.press("me:mention", inDM("5"))
	if p, _, _ := e.store.PersonOf("telegram", "5"); len(p.Muted) != 0 {
		t.Errorf("toggled back: %v", p.Muted)
	}
}

func TestMePressBySomeoneElse(t *testing.T) {
	e := newEnv(t, telegramCaps)
	e.accounts(t)
	e.run(dm("5", "/link github tok-alice"))
	e.run(in("/me"))
	edits := len(e.m.Edits)
	e.press("me:review", func(p *messenger.Press) { p.UserID, p.OriginUserID = "6", "5" })
	if p, _, _ := e.store.PersonOf("telegram", "5"); len(p.Muted) != 0 || len(e.m.Edits) != edits || !e.lastAnswer().Alert {
		t.Errorf("muted %v, edits %d, answer %+v", p.Muted, len(e.m.Edits)-edits, e.lastAnswer())
	}
	// The person themself may press in the group.
	e.press("me:review", func(p *messenger.Press) { p.OriginUserID = "5" })
	if p, _, _ := e.store.PersonOf("telegram", "5"); !slices.Equal(p.Muted, []string{"review"}) {
		t.Errorf("own press: %v", p.Muted)
	}
}

func TestLinkInServerWhereCommandsAreHidden(t *testing.T) {
	caps := messenger.Caps{Looks: []string{"embed"}, HiddenCommands: true}
	e := newEnvNamed(t, "discord", caps)
	e.accounts(t)
	c := in("/link github tok-alice")
	c.Messenger = "discord"
	e.run(c)
	p, accs, _ := e.store.PersonOf("discord", "5")
	if p == nil || len(accs) != 1 {
		t.Fatalf("not linked: %+v", p)
	}
	d, _ := e.store.GetDestination("discord", "dm-5")
	if d == nil || d.ID != p.DestinationID {
		t.Errorf("DM destination %+v", d)
	}
}

func TestLinkTokenWithoutPlatformDeleted(t *testing.T) {
	e := newEnv(t, telegramCaps)
	a := e.accounts(t)
	got := e.run(dm("5", "/link ghp_ABC"))
	if len(e.m.Deleted) != 1 || strings.Contains(got, "ghp_ABC") || len(a.calls()) != 0 {
		t.Errorf("deleted %d, reply %q", len(e.m.Deleted), got)
	}
}

func TestLinkSwappedHostAndToken(t *testing.T) {
	e := newEnv(t, telegramCaps)
	a := e.accounts(t)
	got := e.run(dm("5", "/link gitlab git.corp glpat-XYZ"))
	if got != i18n.T(i18n.EN, i18n.MsgLinkBadHost) || strings.Contains(strings.ToLower(got), "glpat") || len(a.calls()) != 0 {
		t.Errorf("reply %q, calls %v", got, a.calls())
	}
	// A host with a port is kept whole.
	e.run(dm("5", "/link gitlab tok-bob git.corp:8443"))
	if c := a.calls(); len(c) != 1 || c[0] != "/gitlab/git.corp:8443/user" {
		t.Errorf("port: calls %v", c)
	}
}
