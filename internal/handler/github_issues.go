package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/notify"
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
	UpdatedAt   Timestamp  `json:"updated_at"`
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
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

func (h *GitHubIssuesHandler) Handle(lang i18n.Lang, payload []byte) (notify.Notification, error) {
	var dto GitHubIssuesDto
	if err := decode(lang, "issues", payload, &dto); err != nil {
		return notify.Notification{}, err
	}

	var headingKey string
	withBody := false
	switch dto.Action {
	case "opened":
		headingKey, withBody = i18n.MsgIssueOpened, true
	case "closed":
		headingKey = i18n.MsgIssueClosed
		if dto.Issue.StateReason == "not_planned" {
			headingKey = i18n.MsgIssueClosedNotPlanned
		}
	case "reopened":
		headingKey = i18n.MsgIssueReopened
	default:
		return notify.Notification{}, ErrSkip
	}

	var labels []string
	for _, l := range dto.Issue.Labels {
		labels = append(labels, l.Name)
	}

	v := itemView{
		Heading:  i18n.T(lang, headingKey),
		Repo:     dto.Repository.FullName,
		RepoURL:  dto.Repository.HTMLURL,
		Ref:      fmt.Sprintf("#%d", dto.Issue.Number),
		URL:      dto.Issue.HTMLURL,
		Title:    dto.Issue.Title,
		Actor:    dto.Sender.Login,
		ActorURL: dto.Sender.HTMLURL,
		Fields:   []notify.Field{labelsField(lang, labels)},
		Buttons:  []notify.Button{button(lang, i18n.MsgBtnOpenIssue, dto.Issue.HTMLURL, notify.StyleDefault)},
		Time:     dto.Issue.UpdatedAt.Time,
	}
	if withBody {
		v.Body = dto.Issue.Body
	}
	return v.notification(), nil
}

// labelsField lists issue labels as code; no labels hides the field.
func labelsField(lang i18n.Lang, labels []string) notify.Field {
	var value []notify.Part
	for i, l := range labels {
		if i > 0 {
			value = append(value, notify.T(", "))
		}
		value = append(value, notify.C(l))
	}
	return field(lang, i18n.MsgFieldLabels, value...)
}

// GitHubIssueCommentDto is the payload of the issue_comment event.
type GitHubIssueCommentDto struct {
	Action  string `json:"action"`
	Comment struct {
		Body      string     `json:"body"`
		HTMLURL   string     `json:"html_url"`
		User      GitHubUser `json:"user"`
		CreatedAt Timestamp  `json:"created_at"`
	} `json:"comment"`
	Issue      GitHubIssue      `json:"issue"`
	Repository GitHubRepository `json:"repository"`
}

// GitHubIssueCommentHandler handles new comments on issues and pull requests.
type GitHubIssueCommentHandler struct{}

func (h *GitHubIssueCommentHandler) GetEventType() platform.EventType {
	return platform.GitHubIssueComment
}

func (h *GitHubIssueCommentHandler) Handle(lang i18n.Lang, payload []byte) (notify.Notification, error) {
	var dto GitHubIssueCommentDto
	if err := decode(lang, "issue_comment", payload, &dto); err != nil {
		return notify.Notification{}, err
	}
	if dto.Action != "created" {
		return notify.Notification{}, ErrSkip
	}

	headingKey := i18n.MsgCommentIssue
	if dto.Issue.PullRequest != nil {
		headingKey = i18n.MsgCommentPR
	}

	commentURL := firstNonEmpty(dto.Comment.HTMLURL, dto.Issue.HTMLURL)
	return itemView{
		Heading:  i18n.T(lang, headingKey),
		Repo:     dto.Repository.FullName,
		RepoURL:  dto.Repository.HTMLURL,
		Ref:      fmt.Sprintf("#%d", dto.Issue.Number),
		URL:      firstNonEmpty(dto.Issue.HTMLURL, commentURL),
		Title:    dto.Issue.Title,
		Actor:    dto.Comment.User.Login,
		ActorURL: dto.Comment.User.HTMLURL,
		Body:     dto.Comment.Body,
		Buttons:  []notify.Button{button(lang, i18n.MsgBtnOpenComment, commentURL, notify.StyleDefault)},
		Time:     dto.Comment.CreatedAt.Time,
	}.notification(), nil
}
