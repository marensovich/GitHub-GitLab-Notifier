package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"encoding/json"
	"fmt"
	"strings"
)

// ─── DTO (без изменений) ───

type GitHubPushDto struct {
	Ref        string             `json:"ref"`
	Before     string             `json:"before"`
	After      string             `json:"after"`
	Repository GitHubPushRepo     `json:"repository"`
	Pusher     GitHubPushPusher   `json:"pusher"`
	Sender     GitHubPushSender   `json:"sender"`
	Forced     bool               `json:"forced"`
	Created    bool               `json:"created"`
	Deleted    bool               `json:"deleted"`
	BaseRef    *string            `json:"base_ref"`
	Compare    string             `json:"compare"`
	Commits    []GitHubPushCommit `json:"commits"`
	HeadCommit *GitHubPushCommit  `json:"head_commit"`
}

type GitHubPushSender struct {
	Login     string `json:"login"`
	ID        int    `json:"id"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
}

type GitHubPushRepo struct {
	ID          int             `json:"id"`
	NodeID      string          `json:"node_id"`
	Name        string          `json:"name"`
	FullName    string          `json:"full_name"`
	HTMLURL     string          `json:"html_url"`
	Private     bool            `json:"private"`
	Owner       GitHubPushOwner `json:"owner"`
	Description *string         `json:"description"`
}

type GitHubPushOwner struct {
	Name      string `json:"name"`
	Email     string `json:"email"`
	Login     string `json:"login"`
	ID        int    `json:"id"`
	NodeID    string `json:"node_id"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
}

type GitHubPushPusher struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Username string `json:"username"`
}

type GitHubPushCommit struct {
	ID        string              `json:"id"`
	TreeID    string              `json:"tree_id"`
	Distinct  bool                `json:"distinct"`
	Message   string              `json:"message"`
	Timestamp string              `json:"timestamp"`
	URL       string              `json:"url"`
	Author    GitHubPushAuthor    `json:"author"`
	Committer GitHubPushCommitter `json:"committer"`
	Added     []string            `json:"added"`
	Removed   []string            `json:"removed"`
	Modified  []string            `json:"modified"`
}

type GitHubPushAuthor struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Date     string `json:"date"`
}

type GitHubPushCommitter struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Date     string `json:"date"`
}

func (d *GitHubPushDto) Branch() string {
	return strings.TrimPrefix(d.Ref, "refs/heads/")
}

func (d *GitHubPushDto) CommitCount() int {
	return len(d.Commits)
}

// ─── Handler ───

// GitHubPushHandler handles GitHub push events.
type GitHubPushHandler struct {
	Lang i18n.Lang
}

func (h *GitHubPushHandler) GetEventType() platform.EventType {
	return platform.GitHubPush
}

func (h *GitHubPushHandler) Handle(rawJSON string) (string, error) {
	var dto GitHubPushDto
	if err := json.Unmarshal([]byte(rawJSON), &dto); err != nil {
		return "", fmt.Errorf("failed to parse push event: %w", err)
	}
	return h.formatMessage(&dto), nil
}

func (h *GitHubPushHandler) formatMessage(dto *GitHubPushDto) string {
	var sb strings.Builder

	emoji := getEmoji(dto)
	action := getActionText(h.Lang, dto)
	repoName := dto.Repository.FullName

	// Заголовок
	sb.WriteString(i18n.Tf(h.Lang, i18n.MsgPushHeader,
		emoji, repoName, action, dto.Branch()))

	// Пушер
	sb.WriteString(fmt.Sprintf("%s\n", formatPusher(h.Lang, dto)))

	// Коммиты
	sb.WriteString(formatCommits(h.Lang, dto))

	// Файлы
	sb.WriteString(formatFiles(h.Lang, dto))

	// Ссылка
	if dto.Compare != "" {
		sb.WriteString(i18n.Tf(h.Lang, i18n.MsgViewChanges, dto.Compare))
	}

	return sb.String()
}

// ─── Helpers ───

func getEmoji(dto *GitHubPushDto) string {
	if dto.Deleted {
		return "🗑️"
	}
	if dto.Created {
		return "🆕"
	}
	if dto.Forced {
		return "⚠️"
	}
	return "🚀"
}

func getActionText(lang i18n.Lang, dto *GitHubPushDto) string {
	if dto.Deleted {
		return i18n.T(lang, i18n.MsgPushDeleted)
	}
	if dto.Created {
		return i18n.T(lang, i18n.MsgPushCreated)
	}
	if dto.Forced {
		return i18n.T(lang, i18n.MsgPushForce)
	}
	return i18n.T(lang, i18n.MsgPushNew)
}

func formatPusher(lang i18n.Lang, dto *GitHubPushDto) string {
	sender := dto.Sender
	pusher := dto.Pusher

	result := i18n.Tf(lang, i18n.MsgPusher, sender.HTMLURL, sender.Login)
	if pusher.Email != "" {
		result += i18n.Tf(lang, i18n.MsgPusherEmail, pusher.Email)
	}
	return result
}

func formatCommits(lang i18n.Lang, dto *GitHubPushDto) string {
	var sb strings.Builder

	sb.WriteString(i18n.Tf(lang, i18n.MsgCommitsCount, dto.CommitCount()))
	if dto.Forced {
		sb.WriteString(i18n.T(lang, i18n.MsgCommitsForced))
	}
	sb.WriteString("\n\n")

	if len(dto.Commits) > 0 {
		sb.WriteString(i18n.T(lang, i18n.MsgCommitsHeader))
		max := len(dto.Commits)
		if max > 5 {
			max = 5
		}
		for i := 0; i < max; i++ {
			c := dto.Commits[i]
			shortID := c.ID[:7]
			msg := strings.Split(c.Message, "\n")[0]
			if len(msg) > 70 {
				msg = msg[:67] + "..."
			}
			authorName := c.Author.Name
			if authorName == "" {
				authorName = "unknown"
			}
			sb.WriteString(fmt.Sprintf("  <a href=\"%s\">%s</a> %s — <i><a href=\"https://github.com/%s\">%s</a></i>\n",
				c.URL, shortID, msg, c.Author.Username, authorName))
		}
		if len(dto.Commits) > max {
			sb.WriteString(i18n.Tf(lang, i18n.MsgCommitsMore, len(dto.Commits)-max))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func formatFiles(lang i18n.Lang, dto *GitHubPushDto) string {
	if dto.HeadCommit == nil {
		return ""
	}

	added := dto.HeadCommit.Added
	removed := dto.HeadCommit.Removed
	modified := dto.HeadCommit.Modified

	if len(added) == 0 && len(removed) == 0 && len(modified) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(i18n.T(lang, i18n.MsgFilesHeader))

	if len(added) > 0 {
		sb.WriteString(i18n.Tf(lang, i18n.MsgFilesAdded,
			strings.Join(wrapWithCode(added), ", ")))
	}
	if len(modified) > 0 {
		sb.WriteString(i18n.Tf(lang, i18n.MsgFilesModified,
			strings.Join(wrapWithCode(modified), ", ")))
	}
	if len(removed) > 0 {
		sb.WriteString(i18n.Tf(lang, i18n.MsgFilesRemoved,
			strings.Join(wrapWithCode(removed), ", ")))
	}
	sb.WriteString("\n")

	return sb.String()
}

func wrapWithCode(items []string) []string {
	result := make([]string, len(items))
	for i, item := range items {
		result[i] = "<code>" + item + "</code>"
	}
	return result
}
