package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"encoding/json"
	"fmt"
)

// GitHubUser is the user object shared by most GitHub webhook payloads.
type GitHubUser struct {
	Login   string `json:"login"`
	HTMLURL string `json:"html_url"`
}

// GitHubRepository is the repository object shared by GitHub payloads.
type GitHubRepository struct {
	FullName        string `json:"full_name"`
	HTMLURL         string `json:"html_url"`
	StargazersCount int    `json:"stargazers_count"`
	ForksCount      int    `json:"forks_count"`
}

// decode unmarshals a webhook payload, wrapping errors with a localized message.
func decode(lang i18n.Lang, event string, payload []byte, v any) error {
	if err := json.Unmarshal(payload, v); err != nil {
		return fmt.Errorf(i18n.T(lang, i18n.MsgFailedToParseEvent), event, err)
	}
	return nil
}
