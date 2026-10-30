package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"encoding/json"
	"fmt"
)

type StarAction string

const (
	StarCreated StarAction = "created"
	StarDeleted StarAction = "deleted"
)

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

type GitHubStarHandler struct{}

func (h *GitHubStarHandler) GetEventType() platform.EventType {
	return platform.GitHubStar
}

func (h *GitHubStarHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitHubStarDto
	if err := json.Unmarshal(payload, &dto); err != nil {
		return "", fmt.Errorf(i18n.T(lang, i18n.MsgFailedToParseStarEvent), err)
	}
	return formatStarMessage(lang, &dto), nil
}

func formatStarMessage(lang i18n.Lang, dto *GitHubStarDto) string {
	repoURL := dto.Repository.HTMLURL

	msgKey, verbKey := i18n.MsgStarCreated, i18n.MsgStarVerbCreated
	if dto.StarAction() == StarDeleted {
		msgKey, verbKey = i18n.MsgStarDeleted, i18n.MsgStarVerbDeleted
	}

	msg := i18n.Tf(lang, msgKey,
		esc(dto.Repository.FullName),
		esc(dto.Sender.HTMLURL),
		"@"+esc(dto.Sender.Login),
		i18n.T(lang, verbKey),
		esc(repoURL),
		esc(repoURL+"/stargazers"),
	)
	if dto.Repository.StargazersCount > 0 {
		msg += i18n.Tf(lang, i18n.MsgStarTotal, dto.Repository.StargazersCount)
	}
	return msg
}
