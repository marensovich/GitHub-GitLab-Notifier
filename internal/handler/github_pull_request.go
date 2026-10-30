package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"fmt"
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
	Head         struct {
		Ref string `json:"ref"`
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
// Only lifecycle actions are reported; noisy ones (labeled, assigned,
// synchronize, edited, ...) are skipped.
type GitHubPullRequestHandler struct{}

func (h *GitHubPullRequestHandler) GetEventType() platform.EventType {
	return platform.GitHubPullRequest
}

func (h *GitHubPullRequestHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitHubPullRequestDto
	if err := decode(lang, "pull_request", payload, &dto); err != nil {
		return "", err
	}

	pr := dto.PullRequest
	var headerKey string
	withBody, withStats := false, false

	switch dto.Action {
	case "opened":
		headerKey, withBody, withStats = i18n.MsgPROpened, true, true
		if pr.Draft {
			headerKey = i18n.MsgPROpenedDraft
		}
	case "closed":
		headerKey = i18n.MsgPRClosed
		if pr.Merged {
			headerKey, withStats = i18n.MsgPRMerged, true
		}
	case "reopened":
		headerKey = i18n.MsgPRReopened
	case "ready_for_review":
		headerKey = i18n.MsgPRReady
	default:
		return "", ErrSkip
	}

	v := itemView{
		Header:   i18n.T(lang, headerKey),
		Repo:     dto.Repository.FullName,
		RepoURL:  dto.Repository.HTMLURL,
		Ref:      fmt.Sprintf("#%d", pr.Number),
		URL:      pr.HTMLURL,
		Title:    pr.Title,
		Source:   pr.Head.Ref,
		Target:   pr.Base.Ref,
		Actor:    dto.Sender.Login,
		ActorURL: dto.Sender.HTMLURL,
	}
	if withStats && pr.ChangedFiles > 0 {
		v.Extra = append(v.Extra, i18n.Tf(lang, i18n.MsgPRStats,
			pr.Additions, pr.Deletions, pr.ChangedFiles, pr.Commits))
	}
	if withBody {
		v.Body = pr.Body
	}

	return formatItem(lang, v), nil
}

// GitHubPullRequestReviewDto is the payload of the pull_request_review event.
type GitHubPullRequestReviewDto struct {
	Action string `json:"action"`
	Review struct {
		State   string     `json:"state"`
		Body    string     `json:"body"`
		HTMLURL string     `json:"html_url"`
		User    GitHubUser `json:"user"`
	} `json:"review"`
	PullRequest GitHubPullRequest `json:"pull_request"`
	Repository  GitHubRepository  `json:"repository"`
}

// GitHubPullRequestReviewHandler handles submitted pull request reviews.
type GitHubPullRequestReviewHandler struct{}

func (h *GitHubPullRequestReviewHandler) GetEventType() platform.EventType {
	return platform.GitHubPullRequestReview
}

func (h *GitHubPullRequestReviewHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitHubPullRequestReviewDto
	if err := decode(lang, "pull_request_review", payload, &dto); err != nil {
		return "", err
	}
	if dto.Action != "submitted" {
		return "", ErrSkip
	}

	var headerKey string
	switch dto.Review.State {
	case "approved":
		headerKey = i18n.MsgReviewApproved
	case "changes_requested":
		headerKey = i18n.MsgReviewChangesRequested
	case "commented":
		// Reviews with only inline comments have an empty body: nothing to show.
		if dto.Review.Body == "" {
			return "", ErrSkip
		}
		headerKey = i18n.MsgReviewCommented
	default:
		return "", ErrSkip
	}

	pr := dto.PullRequest
	return formatItem(lang, itemView{
		Header:   i18n.T(lang, headerKey),
		Repo:     dto.Repository.FullName,
		RepoURL:  dto.Repository.HTMLURL,
		Ref:      fmt.Sprintf("#%d", pr.Number),
		URL:      firstNonEmpty(dto.Review.HTMLURL, pr.HTMLURL),
		Title:    pr.Title,
		Actor:    dto.Review.User.Login,
		ActorURL: dto.Review.User.HTMLURL,
		Body:     dto.Review.Body,
	}), nil
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
