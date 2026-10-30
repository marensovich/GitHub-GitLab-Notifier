package handler

import (
	"GitHub-GitLab-Notifier/internal/platform"
	"testing"
)

func TestGitLabHandlers(t *testing.T) {
	runHandlerCases(t, []handlerCase{
		{
			fixture: "gitlab/push.json",
			event:   platform.GitLabPush,
			contains: []string{
				`<b>group/app</b>`,
				"<code>master</code>",
				`<a href="https://gitlab.example.com/dev1">@dev1</a>`,
				"Commits: <b>25</b>",
				"Update Catalan translation",
				"...and 23 more commits",
				"<code>CHANGELOG</code>",
				`https://gitlab.example.com/group/app/-/compare/95790bf891e76fee5e1747ab589903a6a1f80f22...da1560886d4f094c3e6c9ef40349f7d38b5d27d7`,
			},
			absent: []string{"github.com"},
		},
		{
			fixture:  "gitlab/tag_push.json",
			event:    platform.GitLabTagPush,
			contains: []string{"New tag", `<a href="https://gitlab.example.com/group/app/-/tags/v1.0.0">v1.0.0</a>`, "@dev1"},
		},
		{
			fixture: "gitlab/merge_request_open.json",
			event:   platform.GitLabMergeRequest,
			contains: []string{
				"New merge request",
				`<a href="https://gitlab.example.com/group/app/-/merge_requests/3">!3</a> <b>MS-Viewer update</b>`,
				"<code>ms-viewer</code> → <code>master</code>",
				"Adds a &lt;viewer&gt; &amp; fixes bugs",
			},
		},
		{
			fixture:  "gitlab/merge_request_merge.json",
			event:    platform.GitLabMergeRequest,
			contains: []string{"Merge request merged", "!3"},
			absent:   []string{"blockquote"},
		},
		{
			fixture: "gitlab/merge_request_update.json",
			event:   platform.GitLabMergeRequest,
			skip:    true,
		},
		{
			fixture:  "gitlab/issue_open.json",
			event:    platform.GitLabIssue,
			contains: []string{"New issue", "#23", "New API: create/update/delete file", "Create new API"},
		},
		{
			fixture:  "gitlab/issue_open.json",
			event:    platform.GitLabConfidentialIssue,
			contains: []string{"New issue", "#23"},
		},
		{
			fixture:  "gitlab/note_mr.json",
			event:    platform.GitLabNote,
			contains: []string{"New comment on merge request", "!1", "This MR needs work.", "note_1244"},
		},
		{
			fixture:  "gitlab/note_commit.json",
			event:    platform.GitLabNote,
			contains: []string{"New comment on commit", ">cfe32cf<", "<b>Add submodule</b>"},
		},
		{
			fixture: "gitlab/note_snippet.json",
			event:   platform.GitLabNote,
			skip:    true,
		},
		{
			fixture: "gitlab/pipeline_failed.json",
			event:   platform.GitLabPipeline,
			contains: []string{
				"CI failed",
				`<a href="https://gitlab.example.com/group/app/-/pipelines/31">#3</a>`,
				"Pipeline for branch: master",
				">bcbb5ec<",
				"1h 2m 5s",
			},
		},
		{
			fixture: "gitlab/pipeline_running.json",
			event:   platform.GitLabPipeline,
			skip:    true,
		},
		{
			fixture:  "gitlab/release_create.json",
			event:    platform.GitLabRelease,
			contains: []string{"New release", "Release 1.0", "First stable release", "/-/releases/v1.0.0"},
		},
	})
}

func TestGitLabUserURL(t *testing.T) {
	tests := []struct{ project, user, want string }{
		{"https://gitlab.com/group/app", "dev", "https://gitlab.com/dev"},
		{"http://git.local:8080/a/b/c", "dev", "http://git.local:8080/dev"},
		{"https://gitlab.com/group/app", "", ""},
		{"not a url", "dev", ""},
	}
	for _, tt := range tests {
		if got := gitlabUserURL(tt.project, tt.user); got != tt.want {
			t.Errorf("gitlabUserURL(%q, %q) = %q, want %q", tt.project, tt.user, got, tt.want)
		}
	}
}
