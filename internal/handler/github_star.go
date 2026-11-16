package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/notify"
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

func (h *GitHubStarHandler) Handle(lang i18n.Lang, payload []byte) (notify.Notification, error) {
	var dto GitHubStarDto
	if err := json.Unmarshal(payload, &dto); err != nil {
		return notify.Notification{}, fmt.Errorf(i18n.T(lang, i18n.MsgFailedToParseStarEvent), err)
	}
	return starNotification(lang, &dto), nil
}

func starNotification(lang i18n.Lang, dto *GitHubStarDto) notify.Notification {
	heading := i18n.T(lang, i18n.MsgStarCreated)
	if dto.StarAction() == StarDeleted {
		heading = i18n.T(lang, i18n.MsgStarDeleted)
	}

	repoURL := dto.Repository.HTMLURL
	n := notify.Notification{
		Heading: heading,
		Repo:    notify.Link{Text: dto.Repository.FullName, URL: repoURL},
		Fields:  []notify.Field{countField(lang, i18n.MsgFieldStars, dto.Repository.StargazersCount)},
		Actor:   actor(dto.Sender.Login, dto.Sender.HTMLURL),
		Buttons: []notify.Button{
			button(lang, i18n.MsgBtnOpenRepo, repoURL, notify.StyleDefault),
		},
	}
	if repoURL != "" {
		n.Buttons = append(n.Buttons, button(lang, i18n.MsgBtnStargazers, repoURL+"/stargazers", notify.StyleDefault))
	}
	if dto.StarredAt != nil {
		n.Time = parseTime(*dto.StarredAt)
	}
	return n
}
