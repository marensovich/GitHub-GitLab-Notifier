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
			fixture: "github/push.json",
			event:   platform.GitHubPush,
			contains: []string{
				"<b>octo/hello</b>",
				"<code>main</code>",
				`<a href="https://github.com/octo/hello/commit/59b20b8d5c6ff8d09518454d4dd8b7b30f095ab5">59b20b8</a>`,
				"fix: handle &lt;nil&gt; &amp; empty values",
				"<code>new.go</code>",
				"View changes",
			},
			absent: []string{"Longer description"},
		},
		{
			fixture:  "github/push_tag.json",
			event:    platform.GitHubPush,
			contains: []string{"New tag", `<a href="https://github.com/octo/hello/releases/tag/v1.2.0">v1.2.0</a>`},
			absent:   []string{"to branch"},
		},
		{
			fixture:  "github/star.json",
			event:    platform.GitHubStar,
			contains: []string{"new star", "@fan", "<b>42</b>"},
		},
		{
			fixture:  "github/ping.json",
			event:    platform.GitHubPing,
			contains: []string{"Webhook connected", "octo/hello", "Keep it logically awesome."},
		},
		{
			fixture:  "github/fork.json",
			event:    platform.GitHubFork,
			contains: []string{"new fork", "@fan", "fan/hello", "<b>7</b>"},
		},
		{
			fixture: "github/pull_request_opened.json",
			event:   platform.GitHubPullRequest,
			contains: []string{
				"New pull request",
				`<a href="https://github.com/octo/hello/pull/12">#12</a> <b>Add &lt;dark&gt; mode</b>`,
				"<code>feature/dark-mode</code> → <code>main</code>",
				"<b>+120</b> / <b>−8</b>",
				"<blockquote>This PR adds dark mode.\n\nCloses #10</blockquote>",
			},
		},
		{
			fixture:  "github/pull_request_merged.json",
			event:    platform.GitHubPullRequest,
			contains: []string{"Pull request merged", "@maintainer"},
			absent:   []string{"blockquote"},
		},
		{
			fixture: "github/pull_request_labeled.json",
			event:   platform.GitHubPullRequest,
			skip:    true,
		},
		{
			fixture:  "github/pull_request_review.json",
			event:    platform.GitHubPullRequestReview,
			contains: []string{"Pull request approved", "@reviewer", "LGTM!"},
		},
		{
			fixture:  "github/issues_opened.json",
			event:    platform.GitHubIssues,
			contains: []string{"New issue", "#10", "App crashes on start", "2. &lt;crash&gt;"},
		},
		{
			fixture:  "github/issues_closed.json",
			event:    platform.GitHubIssues,
			contains: []string{"not planned", "@maintainer"},
		},
		{
			fixture:  "github/issue_comment_pr.json",
			event:    platform.GitHubIssueComment,
			contains: []string{"New comment on pull request", "Could you add a test?", "issuecomment-1"},
		},
		{
			fixture:  "github/release.json",
			event:    platform.GitHubRelease,
			contains: []string{"New release", "v1.2.0 — Dark mode", "@maintainer"},
		},
		{
			fixture: "github/workflow_run.json",
			event:   platform.GitHubWorkflowRun,
			contains: []string{
				"CI failed",
				`<a href="https://github.com/octo/hello/actions/runs/1">#57</a> <b>CI</b>`,
				"<code>main</code>",
				"fix: handle nil values",
				"2m 5s",
			},
		},
		{
			fixture: "github/workflow_run_requested.json",
			event:   platform.GitHubWorkflowRun,
			skip:    true,
		},
	})
}
