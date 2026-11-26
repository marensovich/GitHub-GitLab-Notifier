package handler

import (
	"errors"
	"strings"
	"testing"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
)

const ghRepoJSON = `"repository":{"full_name":"octo/hello","html_url":"https://github.com/octo/hello"},"sender":{"login":"boss"}`
const glProjectJSON = `"project":{"path_with_namespace":"octo/hello","web_url":"https://gitlab.com/octo/hello"},"user":{"username":"boss"}`

func TestPersonalOnlyEvents(t *testing.T) {
	cases := []struct {
		name    string
		h       EventHandler
		payload string
		heading string
		item    string
	}{
		{"pr assigned", &GitHubPullRequestHandler{},
			`{"action":"assigned","pull_request":{"number":3,"title":"Dark mode","html_url":"https://github.com/octo/hello/pull/3"},` + ghRepoJSON + `}`,
			"Pull request assigned", "#3 Dark mode"},
		{"pr review requested", &GitHubPullRequestHandler{},
			`{"action":"review_requested","pull_request":{"number":3,"title":"Dark mode"},` + ghRepoJSON + `}`,
			"Review requested", "#3 Dark mode"},
		{"issue assigned", &GitHubIssuesHandler{},
			`{"action":"assigned","issue":{"number":4,"title":"Crash"},` + ghRepoJSON + `}`,
			"Issue assigned", "#4 Crash"},
		{"mr reviewers", &GitLabMergeRequestHandler{},
			`{"object_attributes":{"iid":5,"title":"Fix","action":"update"},"changes":{"reviewers":{"previous":[],"current":[{"id":2}]}},` + glProjectJSON + `}`,
			"Review requested", "!5 Fix"},
		{"mr assignees", &GitLabMergeRequestHandler{},
			`{"object_attributes":{"iid":5,"title":"Fix","action":"update"},"changes":{"assignees":{"previous":[],"current":[{"id":2}]}},` + glProjectJSON + `}`,
			"Merge request assigned", "!5 Fix"},
		{"issue assignees", &GitLabIssueHandler{Event: platform.GitLabIssue},
			`{"object_attributes":{"iid":6,"title":"Bug","action":"update"},"changes":{"assignees":{"previous":[],"current":[{"id":2}]}},` + glProjectJSON + `}`,
			"Issue assigned", "#6 Bug"},
	}
	for _, tc := range cases {
		n, err := tc.h.Handle(i18n.EN, []byte(tc.payload))
		if !errors.Is(err, ErrPersonalOnly) {
			t.Errorf("%s: err %v", tc.name, err)
			continue
		}
		if !strings.Contains(n.Heading, tc.heading) || !strings.Contains(n.Item.Text, tc.item) || n.Repo.Text != "octo/hello" {
			t.Errorf("%s: %+v", tc.name, n)
		}
	}
}

func TestUpdateWithoutPeopleIsSkipped(t *testing.T) {
	for _, h := range []EventHandler{&GitLabMergeRequestHandler{}, &GitLabIssueHandler{Event: platform.GitLabIssue}} {
		_, err := h.Handle(i18n.EN, []byte(`{"object_attributes":{"iid":5,"action":"update"},"changes":{"title":{"previous":"a","current":"b"}},`+glProjectJSON+`}`))
		if !errors.Is(err, ErrSkip) {
			t.Errorf("%T: %v", h, err)
		}
	}
}
