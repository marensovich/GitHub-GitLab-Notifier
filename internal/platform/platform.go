package platform

import "encoding/json"

// Platform represents a hosting platform.
type Platform string

const (
	GitHub Platform = "GITHUB"
	GitLab Platform = "GITLAB"
)

// Topic identifies the forum topic a notification is routed to.
// The values are stored in the event_mapping table, so they must stay stable.
type Topic string

const (
	TopicGeneral      Topic = ""
	TopicStars        Topic = "star"
	TopicPushes       Topic = "push"
	TopicPullRequests Topic = "pull_request"
	TopicIssues       Topic = "issues"
	TopicReleases     Topic = "release"
	TopicCI           Topic = "ci"
)

// TopicDef describes a forum topic created by /configure.
type TopicDef struct {
	Topic Topic
	Name  string
}

// Topics lists the forum topics in the order they are created.
// Names are used to detect already existing topics, so do not rename them.
var Topics = []TopicDef{
	{TopicStars, "⭐ Stars"},
	{TopicPushes, "🚀 Pushes"},
	{TopicPullRequests, "🔀 Pull Requests"},
	{TopicIssues, "🐛 Issues"},
	{TopicReleases, "📦 Releases"},
	{TopicCI, "⚙️ CI/CD"},
}

// EventType represents a specific webhook event.
type EventType struct {
	Name     string
	Platform Platform
	Topic    Topic
}

// Supported events.
var (
	// GitHub (X-GitHub-Event header)
	GitHubPing              = EventType{"ping", GitHub, TopicGeneral}
	GitHubPush              = EventType{"push", GitHub, TopicPushes}
	GitHubStar              = EventType{"star", GitHub, TopicStars}
	GitHubFork              = EventType{"fork", GitHub, TopicStars}
	GitHubPullRequest       = EventType{"pull_request", GitHub, TopicPullRequests}
	GitHubPullRequestReview = EventType{"pull_request_review", GitHub, TopicPullRequests}
	GitHubIssues            = EventType{"issues", GitHub, TopicIssues}
	GitHubIssueComment      = EventType{"issue_comment", GitHub, TopicIssues}
	GitHubRelease           = EventType{"release", GitHub, TopicReleases}
	GitHubWorkflowRun       = EventType{"workflow_run", GitHub, TopicCI}

	// GitLab (X-Gitlab-Event header)
	GitLabPush              = EventType{"Push Hook", GitLab, TopicPushes}
	GitLabTagPush           = EventType{"Tag Push Hook", GitLab, TopicPushes}
	GitLabMergeRequest      = EventType{"Merge Request Hook", GitLab, TopicPullRequests}
	GitLabIssue             = EventType{"Issue Hook", GitLab, TopicIssues}
	GitLabConfidentialIssue = EventType{"Confidential Issue Hook", GitLab, TopicIssues}
	GitLabNote              = EventType{"Note Hook", GitLab, TopicIssues}
	GitLabConfidentialNote  = EventType{"Confidential Note Hook", GitLab, TopicIssues}
	GitLabPipeline          = EventType{"Pipeline Hook", GitLab, TopicCI}
	GitLabRelease           = EventType{"Release Hook", GitLab, TopicReleases}
)

var registry = func() map[Platform]map[string]EventType {
	all := []EventType{
		GitHubPing, GitHubPush, GitHubStar, GitHubFork,
		GitHubPullRequest, GitHubPullRequestReview, GitHubIssues, GitHubIssueComment,
		GitHubRelease, GitHubWorkflowRun,
		GitLabPush, GitLabTagPush, GitLabMergeRequest, GitLabIssue, GitLabConfidentialIssue,
		GitLabNote, GitLabConfidentialNote, GitLabPipeline, GitLabRelease,
	}
	m := make(map[Platform]map[string]EventType)
	for _, e := range all {
		if m[e.Platform] == nil {
			m[e.Platform] = make(map[string]EventType)
		}
		m[e.Platform][e.Name] = e
	}
	return m
}()

// FromEventName finds EventType by platform and event name string.
// Returns nil for unsupported events.
func FromEventName(p Platform, name string) *EventType {
	if e, ok := registry[p][name]; ok {
		return &e
	}
	return nil
}

// RepositoryURL extracts the repository web URL from a webhook payload.
// Returns an empty string if the payload has no repository information.
func RepositoryURL(p Platform, payload []byte) string {
	var body struct {
		Repository struct {
			HTMLURL  string `json:"html_url"`
			Homepage string `json:"homepage"`
		} `json:"repository"`
		Project struct {
			WebURL string `json:"web_url"`
		} `json:"project"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return ""
	}

	switch p {
	case GitHub:
		return body.Repository.HTMLURL
	case GitLab:
		if body.Project.WebURL != "" {
			return body.Project.WebURL
		}
		return body.Repository.Homepage
	}
	return ""
}
