package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"encoding/json"
	"fmt"
)

// ─── Action ───

type StarAction string

const (
	StarCreated StarAction = "created"
	StarDeleted StarAction = "deleted"
)

// ─── DTO ───

type GitHubStarDto struct {
	Action     string           `json:"action"`
	StarredAt  *string          `json:"starred_at"`
	Repository GitHubStarRepo   `json:"repository"`
	Sender     GitHubStarSender `json:"sender"`
}

func (d *GitHubStarDto) StarAction() StarAction {
	return StarAction(d.Action)
}

type GitHubStarRepo struct {
	ID              int    `json:"id"`
	NodeID          string `json:"node_id"`
	Name            string `json:"name"`
	FullName        string `json:"full_name"`
	HTMLURL         string `json:"html_url"`
	Private         bool   `json:"private"`
	StargazersCount int    `json:"stargazers_count"`
}

type GitHubStarSender struct {
	Login     string `json:"login"`
	ID        int    `json:"id"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
}

// ─── Handler ───

type GitHubStarHandler struct {
	Lang i18n.Lang
}

func (h *GitHubStarHandler) GetEventType() platform.EventType {
	return platform.GitHubStar
}

func (h *GitHubStarHandler) Handle(rawJSON string) (string, error) {
	var dto GitHubStarDto
	if err := json.Unmarshal([]byte(rawJSON), &dto); err != nil {
		return "", fmt.Errorf(i18n.T(h.Lang, i18n.MsgFailedToParseStarEvent), err)
	}
	return h.formatMessage(&dto), nil
}

func (h *GitHubStarHandler) formatMessage(dto *GitHubStarDto) string {
	action := dto.StarAction()
	repoName := dto.Repository.FullName
	senderLogin := dto.Sender.Login
	senderURL := dto.Sender.HTMLURL
	repoURL := dto.Repository.HTMLURL
	stargazersURL := repoURL + "/stargazers"

	var verbKey string
	var msgKey string

	switch action {
	case StarCreated:
		msgKey = i18n.MsgStarCreated
		verbKey = i18n.MsgStarVerbCreated
	case StarDeleted:
		msgKey = i18n.MsgStarDeleted
		verbKey = i18n.MsgStarVerbDeleted
	default:
		msgKey = i18n.MsgStarCreated
		verbKey = i18n.MsgStarVerbCreated
	}

	verb := i18n.T(h.Lang, verbKey)

	return i18n.Tf(h.Lang, msgKey,
		repoName,
		senderURL,
		"@"+senderLogin,
		verb,
		repoURL,
		stargazersURL,
	)
}
