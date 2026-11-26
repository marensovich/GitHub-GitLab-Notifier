package personal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"GitHub-GitLab-Notifier/internal/platform"
)

func TestConfidentialAndUnknownVisibilityArePrivate(t *testing.T) {
	public := `"project":{"web_url":"https://gitlab.example.com/acme/app","visibility_level":20},"user":{"id":1,"username":"boss"}`
	for name, c := range map[string][2]string{
		"confidential issue hook": {"Confidential Issue Hook", `{"object_attributes":{"action":"open","description":"@a"},` + public + `}`},
		"confidential note hook":  {"Confidential Note Hook", `{"object_attributes":{"note":"@a"},` + public + `}`},
		"confidential attribute":  {"Issue Hook", `{"object_attributes":{"action":"open","description":"@a","confidential":true},` + public + `}`},
		"no visibility":           {"Note Hook", `{"object_attributes":{"note":"@a"},"project":{"web_url":"https://gitlab.example.com/acme/app"}}`},
	} {
		if e := Extract(platform.GitLab, c[0], []byte(c[1])); !e.Private {
			t.Errorf("%s: public", name)
		}
	}
	if e := Extract(platform.GitLab, "Note Hook", []byte(`{"object_attributes":{"note":"@a"},`+public+`}`)); e.Private {
		t.Error("public note: private")
	}
	gh := `{"action":"created","comment":{"body":"@a"},"repository":{"html_url":"https://github.com/acme/app"}}`
	if e := Extract(platform.GitHub, "issue_comment", []byte(gh)); !e.Private {
		t.Error("github without private field: public")
	}
}

func TestHostKeepsPort(t *testing.T) {
	body := `{"object_attributes":{"note":"@a"},"project":{"web_url":"https://Git.Corp:8443/acme/app","visibility_level":20}}`
	if e := Extract(platform.GitLab, "Note Hook", []byte(body)); e.Host != "git.corp:8443" {
		t.Errorf("host %q", e.Host)
	}
}

func TestRedirectNotFollowed(t *testing.T) {
	var leaked []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = append(leaked, r.Header.Get("PRIVATE-TOKEN")+r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"id":1,"username":"evil"}`))
	}))
	defer other.Close()
	v, _ := verifier(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/user", http.StatusFound)
	})
	v.HTTP = &http.Client{}
	_, err := v.WhoAmI(context.Background(), "gitlab", "git.example.com", token)
	if err == nil || len(leaked) != 0 || !strings.Contains(err.Error(), "answered 302") {
		t.Errorf("err %v, leaked %v", err, leaked)
	}
}
