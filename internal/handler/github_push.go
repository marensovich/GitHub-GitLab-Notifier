package handler

import (
	"GitHub-GitLab-Notifier/internal/platform"
	"encoding/json"
	"fmt"
	"strings"
)

// ──────────────────────────────────────────────────────────────
// DTO
// ──────────────────────────────────────────────────────────────

// GitHubPushDto represents the GitHub push event webhook payload.
//
// DTO для вебхук-события push от GitHub.
type GitHubPushDto struct {
	Ref        string             `json:"ref"`
	Before     string             `json:"before"`
	After      string             `json:"after"`
	Repository GitHubPushRepo     `json:"repository"`
	Pusher     GitHubPushPusher   `json:"pusher"`
	Forced     bool               `json:"forced"`
	Created    bool               `json:"created"`
	Deleted    bool               `json:"deleted"`
	BaseRef    *string            `json:"base_ref"`
	Compare    string             `json:"compare"`
	Commits    []GitHubPushCommit `json:"commits"`
	HeadCommit *GitHubPushCommit  `json:"head_commit"`
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
	Name  string `json:"name"`
	Email string `json:"email"`
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

// Branch extracts branch name from ref.
func (d *GitHubPushDto) Branch() string {
	return strings.TrimPrefix(d.Ref, "refs/heads/")
}

// CommitCount returns the number of commits.
func (d *GitHubPushDto) CommitCount() int {
	return len(d.Commits)
}

// ──────────────────────────────────────────────────────────────
// Handler
// ──────────────────────────────────────────────────────────────

// GitHubPushHandler handles GitHub push events.
//
// Обрабатывает события push от GitHub.
type GitHubPushHandler struct{}

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
	action := getAction(dto)
	repoName := dto.Repository.FullName

	// Заголовок
	sb.WriteString(fmt.Sprintf("%s <b>%s</b>\n", emoji, repoName))
	sb.WriteString(fmt.Sprintf("%s в ветку <code>%s</code>\n\n", action, dto.Branch()))

	// Пушер
	if dto.Pusher.Name != "" {
		sb.WriteString(fmt.Sprintf("👤 <b>%s</b>", dto.Pusher.Name))
		if dto.Pusher.Email != "" {
			sb.WriteString(fmt.Sprintf(" (<code>%s</code>)", dto.Pusher.Email))
		}
		sb.WriteString("\n")
	}

	// Статистика
	sb.WriteString(fmt.Sprintf("📦 Коммитов: <b>%d</b>", dto.CommitCount()))
	if dto.Forced {
		sb.WriteString(" ⚠️ FORCE PUSH")
	}
	sb.WriteString("\n\n")

	// Список коммитов
	if len(dto.Commits) > 0 {
		sb.WriteString("<b>📝 Коммиты:</b>\n")
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
			authorName := "unknown"
			if c.Author.Name != "" {
				authorName = c.Author.Name
			}
			sb.WriteString(fmt.Sprintf("  <code>%s</code> %s — <i>%s</i>\n", shortID, msg, authorName))
		}
		if len(dto.Commits) > max {
			sb.WriteString(fmt.Sprintf("  <i>...и ещё %d коммитов</i>\n", len(dto.Commits)-max))
		}
		sb.WriteString("\n")
	}

	// Изменённые файлы
	if dto.HeadCommit != nil {
		added := dto.HeadCommit.Added
		removed := dto.HeadCommit.Removed
		modified := dto.HeadCommit.Modified

		if len(added) > 0 || len(removed) > 0 || len(modified) > 0 {
			sb.WriteString("<b>📁 Изменённые файлы:</b>\n")
			if len(added) > 0 {
				tagged := wrapWithCode(added)
				sb.WriteString(fmt.Sprintf("  ➕ Добавлено: %s\n", strings.Join(tagged, ", ")))
			}
			if len(modified) > 0 {
				tagged := wrapWithCode(modified)
				sb.WriteString(fmt.Sprintf("  ✏️ Изменено: %s\n", strings.Join(tagged, ", ")))
			}
			if len(removed) > 0 {
				tagged := wrapWithCode(removed)
				sb.WriteString(fmt.Sprintf("  ➖ Удалено: %s\n", strings.Join(tagged, ", ")))
			}
			sb.WriteString("\n")
		}
	}

	// Ссылка
	if dto.Compare != "" {
		sb.WriteString(fmt.Sprintf("🔗 <a href=\"%s\">Смотреть изменения</a>", dto.Compare))
	}

	return sb.String()
}

// ── Helpers ──

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

func getAction(dto *GitHubPushDto) string {
	if dto.Deleted {
		return "Удалена ветка"
	}
	if dto.Created {
		return "Создана новая ветка"
	}
	if dto.Forced {
		return "Force push"
	}
	return "Новый пуш"
}

func wrapWithCode(items []string) []string {
	result := make([]string, len(items))
	for i, item := range items {
		result[i] = "<code>" + item + "</code>"
	}
	return result
}
