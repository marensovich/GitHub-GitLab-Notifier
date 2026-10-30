package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"encoding/json"
	"fmt"
	"strings"
)

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
	return branchName(d.Ref)
}

func (d *GitHubPushDto) CommitCount() int {
	return len(d.Commits)
}

// maxListedCommits is the number of commits shown in a push notification.
const maxListedCommits = 5

// GitHubPushHandler handles GitHub push events.
type GitHubPushHandler struct{}

func (h *GitHubPushHandler) GetEventType() platform.EventType {
	return platform.GitHubPush
}

func (h *GitHubPushHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitHubPushDto
	if err := json.Unmarshal(payload, &dto); err != nil {
		return "", fmt.Errorf(i18n.T(lang, i18n.MsgFailedToParsePushEvent), err)
	}
	return formatPushMessage(lang, &dto), nil
}

func formatPushMessage(lang i18n.Lang, dto *GitHubPushDto) string {
	if strings.HasPrefix(dto.Ref, "refs/tags/") {
		tag := dto.Branch()
		return formatTag(lang, tagView{
			Repo:     dto.Repository.FullName,
			RepoURL:  dto.Repository.HTMLURL,
			Tag:      tag,
			URL:      dto.Repository.HTMLURL + "/releases/tag/" + tag,
			Deleted:  dto.Deleted,
			Actor:    firstNonEmpty(dto.Sender.Login, dto.Pusher.Name),
			ActorURL: dto.Sender.HTMLURL,
		})
	}

	var sb strings.Builder

	sb.WriteString(i18n.Tf(lang, i18n.MsgPushHeader,
		getEmoji(dto), esc(dto.Repository.FullName), getActionText(lang, dto), esc(dto.Branch())))

	sb.WriteString(formatPusher(lang, dto))
	sb.WriteString("\n")

	if !dto.Deleted {
		sb.WriteString(formatCommits(lang, dto))
		sb.WriteString(formatFiles(lang, dto))
	}

	if dto.Compare != "" && !dto.Deleted {
		sb.WriteString(i18n.Tf(lang, i18n.MsgViewChanges, esc(dto.Compare)))
	}

	return strings.TrimRight(sb.String(), "\n")
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
	login := dto.Sender.Login
	if login == "" {
		login = dto.Pusher.Name
	}

	result := i18n.Tf(lang, i18n.MsgPusher, esc(dto.Sender.HTMLURL), esc(login))
	if dto.Pusher.Email != "" {
		result += i18n.Tf(lang, i18n.MsgPusherEmail, esc(dto.Pusher.Email))
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

	if len(dto.Commits) == 0 {
		return sb.String()
	}

	sb.WriteString(i18n.T(lang, i18n.MsgCommitsHeader))
	shown := min(len(dto.Commits), maxListedCommits)
	for _, c := range dto.Commits[:shown] {
		author := c.Author.Name
		if author == "" {
			author = "unknown"
		}
		authorText := "<i>" + esc(author) + "</i>"
		if c.Author.Username != "" {
			authorText = "<i>" + link("https://github.com/"+c.Author.Username, author) + "</i>"
		}
		sb.WriteString(fmt.Sprintf("  %s %s — %s\n",
			link(c.URL, shortSHA(c.ID)), esc(truncate(firstLine(c.Message), 70)), authorText))
	}
	if len(dto.Commits) > shown {
		sb.WriteString(i18n.Tf(lang, i18n.MsgCommitsMore, len(dto.Commits)-shown))
	}
	sb.WriteString("\n")

	return sb.String()
}

// maxListedFiles limits each changed-files line to keep messages short.
const maxListedFiles = 10

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
		sb.WriteString(i18n.Tf(lang, i18n.MsgFilesAdded, joinFiles(added)))
	}
	if len(modified) > 0 {
		sb.WriteString(i18n.Tf(lang, i18n.MsgFilesModified, joinFiles(modified)))
	}
	if len(removed) > 0 {
		sb.WriteString(i18n.Tf(lang, i18n.MsgFilesRemoved, joinFiles(removed)))
	}
	sb.WriteString("\n")

	return sb.String()
}

// joinFiles renders a comma-separated list of <code> file names,
// collapsing the tail into "+N" when the list is long.
func joinFiles(items []string) string {
	shown := min(len(items), maxListedFiles)
	result := make([]string, 0, shown+1)
	for _, item := range items[:shown] {
		result = append(result, "<code>"+esc(item)+"</code>")
	}
	if len(items) > shown {
		result = append(result, fmt.Sprintf("+%d", len(items)-shown))
	}
	return strings.Join(result, ", ")
}

// tagView describes a created or deleted git tag.
type tagView struct {
	Repo     string
	RepoURL  string
	Tag      string
	URL      string
	Deleted  bool
	Actor    string
	ActorURL string
}

// formatTag renders a tag push notification (shared by GitHub and GitLab).
func formatTag(lang i18n.Lang, v tagView) string {
	header, url := i18n.T(lang, i18n.MsgTagCreated), v.URL
	if v.Deleted {
		header, url = i18n.T(lang, i18n.MsgTagDeleted), ""
	}
	return formatItem(lang, itemView{
		Header:   header,
		Repo:     v.Repo,
		RepoURL:  v.RepoURL,
		Ref:      v.Tag,
		URL:      url,
		Actor:    v.Actor,
		ActorURL: v.ActorURL,
	})
}
