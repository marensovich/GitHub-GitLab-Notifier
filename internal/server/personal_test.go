package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/outbox"
	"GitHub-GitLab-Notifier/storage"
)

// dm stores the DM of a Telegram user and links an account to it.
func (e *env) dm(t *testing.T, userID, lang, id, login string) storage.Destination {
	t.Helper()
	d, err := e.store.EnsureDestination("telegram", userID, storage.Destination{Language: lang, Format: storage.FormatRich})
	must(t, err)
	_, err = e.store.Link("telegram", userID, d.ID, storage.Account{Platform: "github", Host: "github.com", AccountID: id, Login: login})
	must(t, err)
	return d
}

func repoJSON(private bool) string {
	p := "false"
	if private {
		p = "true"
	}
	return `"repository":{"full_name":"octo/hello","html_url":"https://github.com/octo/hello","private":` + p + `},"sender":{"login":"boss","id":1}`
}

func commentPayload(body string) string {
	return `{"action":"created","issue":{"number":1,"title":"Bug","html_url":"https://github.com/octo/hello/issues/1"},` +
		`"comment":{"body":"` + body + `","html_url":"https://github.com/octo/hello/issues/1#c"},` + repoJSON(false) + `}`
}

// queued claims every queued row, so their payloads can be read.
func (e *env) queued(t *testing.T) map[string]outbox.Payload {
	t.Helper()
	out := map[string]outbox.Payload{}
	for {
		it, err := e.store.ClaimNext("telegram", time.Now().Add(time.Hour), nil)
		must(t, err)
		if it == nil {
			return out
		}
		d, err := e.store.DestinationByID(it.DestinationID)
		must(t, err)
		var p outbox.Payload
		must(t, json.Unmarshal(it.Payload, &p))
		must(t, e.store.MarkDone(it.ID))
		out[d.ChatID] = p
	}
}

func TestMentionGoesToLinkedDM(t *testing.T) {
	e := newEnv(t, Config{})
	e.hello(t)
	e.dm(t, "5", "ru", "10", "alice")
	e.post(t, ghHeaders("issue_comment"), commentPayload("@Alice look"))

	sent := e.m.SentList()
	if len(sent) != 2 {
		t.Fatalf("sent %+v", sent)
	}
	dm := sent[1]
	if sent[0].Target.ChatID != "100" || dm.Target.ChatID != "5" || dm.Target.ThreadID != "" || dm.Lang != i18n.RU {
		t.Fatalf("targets %+v / %+v", sent[0].Target, dm)
	}
	if len(dm.N.Notes) == 0 || dm.N.Notes[0] != i18n.T(i18n.RU, i18n.MsgPersonalMention) {
		t.Errorf("notes %q", dm.N.Notes)
	}
	if len(sent[0].N.Notes) > 0 && sent[0].N.Notes[0] == i18n.T(i18n.EN, i18n.MsgPersonalMention) {
		t.Errorf("chat message got the personal note")
	}
}

func TestActorGetsNoDM(t *testing.T) {
	e := newEnv(t, Config{})
	e.hello(t)
	e.dm(t, "5", "en", "1", "boss")
	e.post(t, ghHeaders("issue_comment"), commentPayload("note to self @boss"))
	if n := len(e.m.SentList()); n != 1 {
		t.Errorf("sent %d messages, want the chat one only", n)
	}
}

func TestFailedPipelineGoesToActor(t *testing.T) {
	e := newEnv(t, Config{})
	e.hello(t)
	e.dm(t, "5", "en", "1", "boss")
	run := `{"action":"completed","workflow_run":{"name":"CI","conclusion":"failure","html_url":"https://github.com/octo/hello/actions/runs/1","head_branch":"main",` +
		`"actor":{"login":"boss","id":1},"triggering_actor":{"login":"boss","id":1}},` + repoJSON(false) + `}`
	e.post(t, ghHeaders("workflow_run"), run)
	sent := e.m.SentList()
	if len(sent) != 2 || sent[1].Target.ChatID != "5" || sent[1].N.Notes[0] != i18n.T(i18n.EN, i18n.MsgPersonalPipeline) {
		t.Fatalf("sent %+v", sent)
	}
}

func TestOneDMPerPerson(t *testing.T) {
	e := newEnv(t, Config{})
	e.hello(t)
	e.dm(t, "5", "en", "10", "alice")
	opened := `{"action":"opened","issue":{"number":1,"title":"Bug","body":"@alice @ALICE"},` + repoJSON(false) + `}`
	e.post(t, ghHeaders("issues"), opened)
	if n := len(e.m.SentList()); n != 2 {
		t.Errorf("sent %d, want chat + one DM", n)
	}
}

func TestMutedReasonSkipped(t *testing.T) {
	e := newEnv(t, Config{})
	e.hello(t)
	e.dm(t, "5", "en", "10", "alice")
	p, _, err := e.store.PersonOf("telegram", "5")
	must(t, err)
	must(t, e.store.SetMuted(p.ID, []string{"mention"}))
	e.post(t, ghHeaders("issue_comment"), commentPayload("@alice"))
	if n := len(e.m.SentList()); n != 1 {
		t.Errorf("sent %d, want only the chat", n)
	}
}

func TestPersonalOnlyEventSkipsChats(t *testing.T) {
	e := newEnv(t, Config{})
	e.hello(t)
	e.dm(t, "5", "en", "10", "alice")
	assigned := `{"action":"assigned","assignee":{"login":"alice","id":10},"pull_request":{"number":3,"title":"Dark"},` + repoJSON(false) + `}`
	rec := e.post(t, ghHeaders("pull_request"), assigned)
	sent := e.m.SentList()
	if rec.Code != http.StatusAccepted || len(sent) != 1 || sent[0].Target.ChatID != "5" ||
		sent[0].N.Notes[0] != i18n.T(i18n.EN, i18n.MsgPersonalAssigned) || !strings.Contains(sent[0].N.Heading, "assigned") {
		t.Fatalf("code %d sent %+v", rec.Code, sent)
	}
	// Nobody linked: nothing to send at all.
	unlinked := strings.Replace(assigned, `"login":"alice","id":10`, `"login":"zed","id":99`, 1)
	if rec := e.post(t, ghHeaders("pull_request"), unlinked); rec.Code != http.StatusOK || rec.Body.String() != "SKIPPED" {
		t.Errorf("unlinked: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPrivateRepoRowCarriesMemberCheck(t *testing.T) {
	e := newEnv(t, Config{})
	e.hello(t)
	e.dm(t, "5", "en", "10", "alice")
	repo, err := e.store.FindRepo("https://github.com/octo/hello")
	must(t, err)

	e.postOnly(t, ghHeaders("issue_comment"), strings.Replace(commentPayload("@alice"), `"private":false`, `"private":true`, 1))
	rows := e.queued(t)
	if m := rows["5"].Member; m == nil || m.RepoID != repo.ID || m.UserID != "5" {
		t.Errorf("private: member check %+v", m)
	}
	if rows["100"].Member != nil {
		t.Errorf("chat row has a member check")
	}

	e.postOnly(t, ghHeaders("issue_comment"), strings.Replace(commentPayload("@alice again"), "#c", "#d", 1))
	if m := e.queued(t)["5"].Member; m != nil {
		t.Errorf("public: member check %+v", m)
	}
}

func TestDMInBothMessengersAndNotTwiceInOneChat(t *testing.T) {
	e := newEnv(t, Config{})
	e.hello(t)
	e.dm(t, "5", "en", "10", "alice")
	// The same person's DM is also subscribed to the repository: the chat
	// message reaches them already.
	e.dm(t, "100", "en", "20", "bob")
	e.post(t, ghHeaders("issue_comment"), commentPayload("@alice @bob"))
	var to []string
	for _, s := range e.m.SentList() {
		to = append(to, s.Target.ChatID)
	}
	if strings.Join(to, ",") != "100,5" {
		t.Errorf("sent to %v", to)
	}
}
