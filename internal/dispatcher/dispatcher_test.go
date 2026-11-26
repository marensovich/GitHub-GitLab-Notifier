package dispatcher

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if err := i18n.Load("../../locales"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestDispatchSkip(t *testing.T) {
	d := NewDispatcher(i18n.EN, true)
	res := d.Dispatch(i18n.EN, platform.GitHub, "pull_request", []byte(`{"action":"labeled"}`))
	if !res.Skip {
		t.Fatalf("want skip, got %+v", res)
	}
	if res.Topic != platform.GitHubPullRequest.Topic {
		t.Errorf("topic = %q, want %q", res.Topic, platform.GitHubPullRequest.Topic)
	}
}

func TestDispatchNotification(t *testing.T) {
	d := NewDispatcher(i18n.EN, false)
	res := d.Dispatch(i18n.EN, platform.GitHub, "star",
		[]byte(`{"action":"created","repository":{"full_name":"octo/hello","html_url":"https://github.com/octo/hello"}}`))
	if res.Skip || res.Notification.Repo.Text != "octo/hello" || !strings.Contains(res.Notification.Heading, "star") {
		t.Fatalf("unexpected result %+v", res)
	}
}

func TestDispatchUnknownNotifies(t *testing.T) {
	d := NewDispatcher(i18n.EN, true)
	res := d.Dispatch(i18n.EN, platform.GitHub, "deployment_status", []byte(`{}`))
	if res.Skip {
		t.Fatal("unknown event must notify when notifyUnknown is on")
	}
	if !strings.Contains(res.Notification.Heading, "deployment_status") {
		t.Errorf("heading %q does not name the event", res.Notification.Heading)
	}
	if strings.Contains(res.Notification.Heading, "<") {
		t.Errorf("heading must be plain text, got %q", res.Notification.Heading)
	}
}

func TestDispatchUnknownIgnored(t *testing.T) {
	d := NewDispatcher(i18n.EN, false)
	if res := d.Dispatch(i18n.EN, platform.GitHub, "deployment_status", []byte(`{}`)); !res.Skip {
		t.Fatalf("want skip, got %+v", res)
	}
}

func TestDispatchHandlerError(t *testing.T) {
	d := NewDispatcher(i18n.EN, false)
	res := d.Dispatch(i18n.EN, platform.GitHub, "issues", []byte(`not json`))
	if res.Skip || !strings.Contains(res.Notification.Heading, "issues") {
		t.Fatalf("want an error notification naming the event, got %+v", res)
	}
}

func TestDispatchPersonalOnly(t *testing.T) {
	d := NewDispatcher(i18n.EN, true)
	res := d.Dispatch(i18n.EN, platform.GitHub, "pull_request",
		[]byte(`{"action":"assigned","pull_request":{"number":3,"title":"x"},"repository":{"full_name":"octo/hello"}}`))
	if !res.Skip || !res.PersonalOnly || res.Notification.Heading == "" {
		t.Fatalf("result %+v", res)
	}
	if res := d.Dispatch(i18n.EN, platform.GitHub, "pull_request", []byte(`{"action":"labeled"}`)); res.PersonalOnly {
		t.Errorf("skip marked personal: %+v", res)
	}
}
