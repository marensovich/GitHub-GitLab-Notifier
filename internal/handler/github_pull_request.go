package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/notify"
	"GitHub-GitLab-Notifier/internal/platform"
	"fmt"
	"strings"
)

// GitHubPullRequest is the pull_request object of GitHub payloads.
type GitHubPullRequest struct {
	Number       int        `json:"number"`
	Title        string     `json:"title"`
	Body         string     `json:"body"`
	HTMLURL      string     `json:"html_url"`
	Draft        bool       `json:"draft"`
	Merged       bool       `json:"merged"`
	User         GitHubUser `json:"user"`
	Additions    int        `json:"additions"`
	Deletions    int        `json:"deletions"`
	ChangedFiles int        `json:"changed_files"`
	Commits      int        `json:"commits"`
	UpdatedAt    Timestamp  `json:"updated_at"`
	Head         struct {
		Ref string `json:"ref"`
		// Label is "owner:branch"; it is shown for pull requests from forks,
		// where the branch name alone is ambiguous (often "main" → "main").
		Label string `json:"label"`
		Repo  *struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

// GitHubPullRequestDto is the payload of the pull_request event.
type GitHubPullRequestDto struct {
	Action      string            `json:"action"`
	PullRequest GitHubPullRequest `json:"pull_request"`
	Repository  GitHubRepository  `json:"repository"`
	Sender      GitHubUser        `json:"sender"`
}

// GitHubPullRequestHandler handles GitHub pull_request events.
// Only lifecycle actions are reported to chats; assigned and
// review_requested go only to the people concerned; noisy ones (labeled,
// synchronize, edited, ...) are skipped.
type GitHubPullRequestHandler struct{}

func (h *GitHubPullRequestHandler) GetEventType() platform.EventType {
	return platform.GitHubPullRequest
}

func (h *GitHubPullRequestHandler) Handle(lang i18n.Lang, payload []byte) (notify.Notification, error) {
	var dto GitHubPullRequestDto
	if err := decode(lang, "pull_request", payload, &dto); err != nil {
		return notify.Notification{}, err
	}

	pr := dto.PullRequest
	var headingKey string
	withBody, withStats, personalOnly := false, false, false

	switch dto.Action {
	case "opened":
		headingKey, withBody, withStats = i18n.MsgPROpened, true, true
		if pr.Draft {
			headingKey = i18n.MsgPROpenedDraft
		}
	case "closed":
		headingKey = i18n.MsgPRClosed
		if pr.Merged {
			headingKey, withStats = i18n.MsgPRMerged, true
		}
	case "reopened":
		headingKey = i18n.MsgPRReopened
	case "ready_for_review":
		headingKey = i18n.MsgPRReady
	case "assigned":
		headingKey, personalOnly = i18n.MsgPRAssigned, true
	case "review_requested":
		headingKey, personalOnly = i18n.MsgPRReviewRequested, true
	default:
		return notify.Notification{}, ErrSkip
	}

	v := itemView{
		Heading:  i18n.T(lang, headingKey),
		Repo:     dto.Repository.FullName,
		RepoURL:  dto.Repository.HTMLURL,
		Ref:      fmt.Sprintf("#%d", pr.Number),
		URL:      pr.HTMLURL,
		Title:    pr.Title,
		Actor:    dto.Sender.Login,
		ActorURL: dto.Sender.HTMLURL,
		Fields:   []notify.Field{branchesField(lang, prSource(pr, dto.Repository.FullName), pr.Base.Ref)},
		Time:     pr.UpdatedAt.Time,
	}
	if withStats && pr.ChangedFiles > 0 {
		v.Fields = append(v.Fields, field(lang, i18n.MsgFieldChanges,
			notify.B(fmt.Sprintf("+%d", pr.Additions)), notify.T(" / "), notify.B(fmt.Sprintf("−%d", pr.Deletions)),
			notify.T(i18n.Tf(lang, i18n.MsgChangesTail, pr.ChangedFiles, pr.Commits))))
	}
	if withBody {
		v.Body = pr.Body
	}
	v.Buttons = []notify.Button{button(lang, i18n.MsgBtnOpenPR, pr.HTMLURL, notify.StylePrimary)}
	if pr.HTMLURL != "" {
		v.Buttons = append(v.Buttons, button(lang, i18n.MsgBtnFiles, pr.HTMLURL+"/files", notify.StyleDefault))
	}
	if personalOnly {
		return v.notification(), ErrPersonalOnly
	}
	return v.notification(), nil
}

// GitHubPullRequestReviewDto is the payload of the pull_request_review event.
type GitHubPullRequestReviewDto struct {
	Action string `json:"action"`
	Review struct {
		State       string     `json:"state"`
		Body        string     `json:"body"`
		HTMLURL     string     `json:"html_url"`
		User        GitHubUser `json:"user"`
		SubmittedAt Timestamp  `json:"submitted_at"`
	} `json:"review"`
	PullRequest GitHubPullRequest `json:"pull_request"`
	Repository  GitHubRepository  `json:"repository"`
}

// GitHubPullRequestReviewHandler handles submitted pull request reviews.
type GitHubPullRequestReviewHandler struct{}

func (h *GitHubPullRequestReviewHandler) GetEventType() platform.EventType {
	return platform.GitHubPullRequestReview
}

func (h *GitHubPullRequestReviewHandler) Handle(lang i18n.Lang, payload []byte) (notify.Notification, error) {
	var dto GitHubPullRequestReviewDto
	if err := decode(lang, "pull_request_review", payload, &dto); err != nil {
		return notify.Notification{}, err
	}
	if dto.Action != "submitted" {
		return notify.Notification{}, ErrSkip
	}

	var headingKey string
	switch dto.Review.State {
	case "approved":
		headingKey = i18n.MsgReviewApproved
	case "changes_requested":
		headingKey = i18n.MsgReviewChangesRequested
	case "commented":
		// Reviews with only inline comments have an empty body: nothing to show.
		if dto.Review.Body == "" {
			return notify.Notification{}, ErrSkip
		}
		headingKey = i18n.MsgReviewCommented
	default:
		return notify.Notification{}, ErrSkip
	}

	pr := dto.PullRequest
	reviewURL := firstNonEmpty(dto.Review.HTMLURL, pr.HTMLURL)
	return itemView{
		Heading:  i18n.T(lang, headingKey),
		Repo:     dto.Repository.FullName,
		RepoURL:  dto.Repository.HTMLURL,
		Ref:      fmt.Sprintf("#%d", pr.Number),
		URL:      firstNonEmpty(pr.HTMLURL, reviewURL),
		Title:    pr.Title,
		Actor:    dto.Review.User.Login,
		ActorURL: dto.Review.User.HTMLURL,
		Body:     dto.Review.Body,
		Buttons:  []notify.Button{button(lang, i18n.MsgBtnOpenReview, reviewURL, notify.StyleDefault)},
		Time:     dto.Review.SubmittedAt.Time,
	}.notification(), nil
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// prSource is the source branch of a pull request: "branch" when it comes from
// the same repository, "owner:branch" when it comes from a fork.
func prSource(pr GitHubPullRequest, repo string) string {
	if pr.Head.Repo != nil && pr.Head.Label != "" && !strings.EqualFold(pr.Head.Repo.FullName, repo) {
		return pr.Head.Label
	}
	return pr.Head.Ref
}
