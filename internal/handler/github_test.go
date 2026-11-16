package handler

import (
	"GitHub-GitLab-Notifier/internal/platform"
	"testing"
	"time"
)

var parseDuration = time.ParseDuration

func TestGitHubHandlers(t *testing.T) {
	runHandlerCases(t, []handlerCase{
		{
			fixture: "github/push/branch.json",
			event:   platform.GitHubPush,
			contains: []string{
				`<b><a href="https://github.com/octo/hello">octo/hello</a></b>`,
				`/tree/main">main</a>`,
				`<a href="https://github.com/octo/hello/commit/59b20b8d5c6ff8d09518454d4dd8b7b30f095ab5">59b20b8</a>`,
				"fix: handle &lt;nil&gt; &amp; empty values",
				"<code>new.go</code>",
				"[Compare](https://github.com/octo/hello/compare/",
			},
			absent: []string{"Longer description"},
		},
		{
			fixture:  "github/push/tag.json",
			event:    platform.GitHubPush,
			contains: []string{"New tag", `<a href="https://github.com/octo/hello/releases/tag/v1.2.0">v1.2.0</a>`},
			absent:   []string{"to branch"},
		},
		{
			fixture:  "github/star/created.json",
			event:    platform.GitHubStar,
			contains: []string{"New star", "@fan", "<b>42</b>"},
		},
		{
			fixture:  "github/ping/basic.json",
			event:    platform.GitHubPing,
			contains: []string{"Webhook connected", "octo/hello", "Keep it logically awesome."},
		},
		{
			fixture:  "github/fork/basic.json",
			event:    platform.GitHubFork,
			contains: []string{"New fork", "@fan", "fan/hello", "<b>7</b>"},
		},
		{
			fixture: "github/pull_request/opened.json",
			event:   platform.GitHubPullRequest,
			contains: []string{
				"New pull request",
				`<a href="https://github.com/octo/hello/pull/12">#12 Add &lt;dark&gt; mode</a>`,
				"<code>feature/dark-mode</code> → <code>main</code>",
				"<b>+120</b> / <b>−8</b>",
				"<blockquote expandable>This PR adds dark mode.\n\nCloses #10</blockquote>",
			},
		},
		{
			fixture:  "github/pull_request/merged.json",
			event:    platform.GitHubPullRequest,
			contains: []string{"Pull request merged", "@maintainer"},
			absent:   []string{"blockquote"},
		},
		{
			fixture: "github/pull_request/labeled.json",
			event:   platform.GitHubPullRequest,
			skip:    true,
		},
		{
			fixture:  "github/pull_request_review/approved.json",
			event:    platform.GitHubPullRequestReview,
			contains: []string{"Pull request approved", "@reviewer", "LGTM!"},
		},
		{
			fixture:  "github/issues/opened.json",
			event:    platform.GitHubIssues,
			contains: []string{"New issue", "#10", "App crashes on start", "2. &lt;crash&gt;"},
		},
		{
			fixture:  "github/issues/closed.json",
			event:    platform.GitHubIssues,
			contains: []string{"not planned", "@maintainer"},
		},
		{
			fixture:  "github/issue_comment/on_pull_request.json",
			event:    platform.GitHubIssueComment,
			contains: []string{"New comment on pull request", "Could you add a test?", "issuecomment-1"},
		},
		{
			fixture:  "github/release/published.json",
			event:    platform.GitHubRelease,
			contains: []string{"New release", "v1.2.0 — Dark mode", "@maintainer"},
		},
		{
			fixture: "github/workflow_run/failed.json",
			event:   platform.GitHubWorkflowRun,
			contains: []string{
				"CI failed",
				`<a href="https://github.com/octo/hello/actions/runs/1">#57 CI</a>`,
				"<code>main</code>",
				"fix: handle nil values",
				"2m 5s",
			},
		},
		{
			fixture: "github/workflow_run/requested.json",
			event:   platform.GitHubWorkflowRun,
			skip:    true,
		},
	})
}
