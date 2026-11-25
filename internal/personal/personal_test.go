package personal

import (
	"reflect"
	"testing"

	"GitHub-GitLab-Notifier/internal/platform"
)

const ghRepo = `"repository":{"html_url":"https://github.com/acme/app","private":true},"sender":{"login":"boss","id":1}`
const glProject = `"project":{"web_url":"https://gitlab.example.com/acme/app","visibility_level":10},"user":{"id":1,"username":"boss"}`

func TestExtract(t *testing.T) {
	cases := []struct {
		name    string
		p       platform.Platform
		event   string
		payload string
		hits    []Hit
	}{
		{"review requested", platform.GitHub, "pull_request",
			`{"action":"review_requested","requested_reviewer":{"login":"alice","id":10},"pull_request":{"body":""},` + ghRepo + `}`,
			[]Hit{{"10", "alice", Review}}},
		{"team review requested", platform.GitHub, "pull_request",
			`{"action":"review_requested","requested_team":{"slug":"core"},"pull_request":{},` + ghRepo + `}`, nil},
		{"pr assigned", platform.GitHub, "pull_request",
			`{"action":"assigned","assignee":{"login":"alice","id":10},"pull_request":{},` + ghRepo + `}`,
			[]Hit{{"10", "alice", Assigned}}},
		{"issue assigned", platform.GitHub, "issues",
			`{"action":"assigned","assignee":{"login":"alice","id":10},"issue":{},` + ghRepo + `}`,
			[]Hit{{"10", "alice", Assigned}}},
		{"pr opened mention", platform.GitHub, "pull_request",
			`{"action":"opened","pull_request":{"body":"cc @alice"},` + ghRepo + `}`,
			[]Hit{{"", "alice", Mention}}},
		{"pr edited no mention", platform.GitHub, "pull_request",
			`{"action":"edited","pull_request":{"body":"cc @alice"},` + ghRepo + `}`, nil},
		{"issue opened mention", platform.GitHub, "issues",
			`{"action":"opened","issue":{"body":"@alice look"},` + ghRepo + `}`,
			[]Hit{{"", "alice", Mention}}},
		{"comment mention", platform.GitHub, "issue_comment",
			`{"action":"created","comment":{"body":"@alice @bob"},` + ghRepo + `}`,
			[]Hit{{"", "alice", Mention}, {"", "bob", Mention}}},
		{"review mention", platform.GitHub, "pull_request_review",
			`{"action":"submitted","review":{"body":"@alice fixed?"},` + ghRepo + `}`,
			[]Hit{{"", "alice", Mention}}},
		{"workflow failed", platform.GitHub, "workflow_run",
			`{"action":"completed","workflow_run":{"conclusion":"failure","actor":{"login":"x","id":5},"triggering_actor":{"login":"alice","id":10}},` + ghRepo + `}`,
			[]Hit{{"10", "alice", Pipeline}}},
		{"workflow failed no triggering actor", platform.GitHub, "workflow_run",
			`{"action":"completed","workflow_run":{"conclusion":"failure","actor":{"login":"x","id":5}},` + ghRepo + `}`,
			[]Hit{{"5", "x", Pipeline}}},
		{"workflow ok", platform.GitHub, "workflow_run",
			`{"action":"completed","workflow_run":{"conclusion":"success","actor":{"login":"x","id":5}},` + ghRepo + `}`, nil},
		{"mr open", platform.GitLab, "Merge Request Hook",
			`{"object_attributes":{"action":"open","description":"for @carol"},"assignees":[{"id":10,"username":"alice"}],"reviewers":[{"id":20,"username":"bob"}],` + glProject + `}`,
			[]Hit{{"20", "bob", Review}, {"10", "alice", Assigned}, {"", "carol", Mention}}},
		{"mr update reviewers", platform.GitLab, "Merge Request Hook",
			`{"object_attributes":{"action":"update","description":"@carol"},"changes":{"reviewers":{"previous":[{"id":20,"username":"bob"}],"current":[{"id":20,"username":"bob"},{"id":30,"username":"dan"}]},"assignees":{"previous":[],"current":[{"id":10,"username":"alice"}]}},` + glProject + `}`,
			[]Hit{{"30", "dan", Review}, {"10", "alice", Assigned}}},
		{"mr update title only", platform.GitLab, "Merge Request Hook",
			`{"object_attributes":{"action":"update"},"changes":{"title":{"previous":"a","current":"b"}},` + glProject + `}`, nil},
		{"issue update assignees", platform.GitLab, "Issue Hook",
			`{"object_attributes":{"action":"update"},"changes":{"assignees":{"previous":[],"current":[{"id":10,"username":"alice"}]}},` + glProject + `}`,
			[]Hit{{"10", "alice", Assigned}}},
		{"confidential issue open", platform.GitLab, "Confidential Issue Hook",
			`{"object_attributes":{"action":"open","description":"@carol"},"assignees":[{"id":10,"username":"alice"}],` + glProject + `}`,
			[]Hit{{"10", "alice", Assigned}, {"", "carol", Mention}}},
		{"note", platform.GitLab, "Note Hook",
			`{"object_attributes":{"note":"@alice ok?"},` + glProject + `}`,
			[]Hit{{"", "alice", Mention}}},
		{"pipeline failed", platform.GitLab, "Pipeline Hook",
			`{"object_attributes":{"status":"failed"},` + glProject + `}`,
			[]Hit{{"1", "boss", Pipeline}}},
		{"pipeline running", platform.GitLab, "Pipeline Hook",
			`{"object_attributes":{"status":"running"},` + glProject + `}`, nil},
		{"push", platform.GitHub, "push", `{"head_commit":{"message":"@alice"},` + ghRepo + `}`, nil},
		{"broken json", platform.GitHub, "issue_comment", `{`, nil},
	}
	for _, tc := range cases {
		e := Extract(tc.p, tc.event, []byte(tc.payload))
		if !reflect.DeepEqual(e.Hits, tc.hits) {
			t.Errorf("%s: hits %+v, want %+v", tc.name, e.Hits, tc.hits)
		}
	}
}

func TestExtractEvent(t *testing.T) {
	e := Extract(platform.GitHub, "issue_comment", []byte(`{"action":"created","comment":{"body":"@a"},`+ghRepo+`}`))
	want := Event{Platform: "github", Host: "github.com", Private: true, ActorID: "1", ActorLogin: "boss", Hits: e.Hits}
	if !reflect.DeepEqual(e, want) {
		t.Errorf("event %+v", e)
	}
	e = Extract(platform.GitLab, "Note Hook", []byte(`{"object_attributes":{"note":"x"},`+glProject+`}`))
	if e.Platform != "gitlab" || e.Host != "gitlab.example.com" || !e.Private || e.ActorID != "1" || e.ActorLogin != "boss" {
		t.Errorf("gitlab event %+v", e)
	}
	public := Extract(platform.GitLab, "Note Hook", []byte(`{"object_attributes":{"note":"x"},"project":{"web_url":"https://GitLab.com/a/b","visibility_level":20}}`))
	if public.Private || public.Host != "gitlab.com" {
		t.Errorf("public gitlab %+v", public)
	}
	pub := Extract(platform.GitHub, "issues", []byte(`{"action":"opened","issue":{},"repository":{"html_url":"https://github.com/a/b","private":false}}`))
	if pub.Private {
		t.Errorf("public github %+v", pub)
	}
}
