package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"fmt"
)

// GitHubIssue is the issue object of GitHub payloads.
// For comments on pull requests GitHub also sends an "issue" with a
// non-nil pull_request field.
type GitHubIssue struct {
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	HTMLURL     string     `json:"html_url"`
	StateReason string     `json:"state_reason"`
	User        GitHubUser `json:"user"`
	PullRequest *struct{}  `json:"pull_request"`
}

// GitHubIssuesDto is the payload of the issues event.
type GitHubIssuesDto struct {
	Action     string           `json:"action"`
	Issue      GitHubIssue      `json:"issue"`
	Repository GitHubRepository `json:"repository"`
	Sender     GitHubUser       `json:"sender"`
}

// GitHubIssuesHandler handles GitHub issues events (opened, closed, reopened).
type GitHubIssuesHandler struct{}

func (h *GitHubIssuesHandler) GetEventType() platform.EventType {
	return platform.GitHubIssues
}

func (h *GitHubIssuesHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitHubIssuesDto
	if err := decode(lang, "issues", payload, &dto); err != nil {
		return "", err
	}

	var headerKey string
	withBody := false
	switch dto.Action {
	case "opened":
		headerKey, withBody = i18n.MsgIssueOpened, true
	case "closed":
		headerKey = i18n.MsgIssueClosed
		if dto.Issue.StateReason == "not_planned" {
			headerKey = i18n.MsgIssueClosedNotPlanned
		}
	case "reopened":
		headerKey = i18n.MsgIssueReopened
	default:
		return "", ErrSkip
	}

	v := itemView{
		Header:   i18n.T(lang, headerKey),
		Repo:     dto.Repository.FullName,
		RepoURL:  dto.Repository.HTMLURL,
		Ref:      fmt.Sprintf("#%d", dto.Issue.Number),
		URL:      dto.Issue.HTMLURL,
		Title:    dto.Issue.Title,
		Actor:    dto.Sender.Login,
		ActorURL: dto.Sender.HTMLURL,
	}
	if withBody {
		v.Body = dto.Issue.Body
	}
	return formatItem(lang, v), nil
}

// GitHubIssueCommentDto is the payload of the issue_comment event.
type GitHubIssueCommentDto struct {
	Action  string `json:"action"`
	Comment struct {
		Body    string     `json:"body"`
		HTMLURL string     `json:"html_url"`
		User    GitHubUser `json:"user"`
	} `json:"comment"`
	Issue      GitHubIssue      `json:"issue"`
	Repository GitHubRepository `json:"repository"`
}

// GitHubIssueCommentHandler handles new comments on issues and pull requests.
type GitHubIssueCommentHandler struct{}

func (h *GitHubIssueCommentHandler) GetEventType() platform.EventType {
	return platform.GitHubIssueComment
}

func (h *GitHubIssueCommentHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitHubIssueCommentDto
	if err := decode(lang, "issue_comment", payload, &dto); err != nil {
		return "", err
	}
	if dto.Action != "created" {
		return "", ErrSkip
	}

	headerKey := i18n.MsgCommentIssue
	if dto.Issue.PullRequest != nil {
		headerKey = i18n.MsgCommentPR
	}

	return formatItem(lang, itemView{
		Header:   i18n.T(lang, headerKey),
		Repo:     dto.Repository.FullName,
		RepoURL:  dto.Repository.HTMLURL,
		Ref:      fmt.Sprintf("#%d", dto.Issue.Number),
		URL:      firstNonEmpty(dto.Comment.HTMLURL, dto.Issue.HTMLURL),
		Title:    dto.Issue.Title,
		Actor:    dto.Comment.User.Login,
		ActorURL: dto.Comment.User.HTMLURL,
		Body:     dto.Comment.Body,
	}), nil
}
