package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/notify"
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

	// TotalCommits is the real number of pushed commits when the payload
	// lists only some of them (GitLab sends at most 20).
	TotalCommits int `json:"-"`
	// TreeURL is the branch page; GitLab sets it, GitHub derives it.
	TreeURL string `json:"-"`
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
	return max(len(d.Commits), d.TotalCommits)
}

// maxListedCommits is the number of commits listed in a classic push
// notification; rich messages list up to 20 in a collapsed section.
const maxListedCommits = 5

// maxListedFiles is the number of changed files listed in a classic message.
const maxListedFiles = 10

// GitHubPushHandler handles GitHub push events.
type GitHubPushHandler struct{}

func (h *GitHubPushHandler) GetEventType() platform.EventType {
	return platform.GitHubPush
}

func (h *GitHubPushHandler) Handle(lang i18n.Lang, payload []byte) (notify.Notification, error) {
	var dto GitHubPushDto
	if err := json.Unmarshal(payload, &dto); err != nil {
		return notify.Notification{}, fmt.Errorf(i18n.T(lang, i18n.MsgFailedToParsePushEvent), err)
	}
	return pushNotification(lang, &dto), nil
}

func pushNotification(lang i18n.Lang, dto *GitHubPushDto) notify.Notification {
	actorLogin := firstNonEmpty(dto.Sender.Login, dto.Pusher.Name)
	if strings.HasPrefix(dto.Ref, "refs/tags/") {
		tag := dto.Branch()
		return tagNotification(lang, tagView{
			Repo:     dto.Repository.FullName,
			RepoURL:  dto.Repository.HTMLURL,
			Tag:      tag,
			URL:      dto.Repository.HTMLURL + "/releases/tag/" + tag,
			Deleted:  dto.Deleted,
			Actor:    actorLogin,
			ActorURL: dto.Sender.HTMLURL,
		})
	}

	n := notify.Notification{
		Heading: pushHeading(lang, dto),
		Repo:    notify.Link{Text: dto.Repository.FullName, URL: dto.Repository.HTMLURL},
		Actor:   actor(actorLogin, dto.Sender.HTMLURL),
	}
	if dto.HeadCommit != nil {
		n.Time = parseTime(dto.HeadCommit.Timestamp)
	}

	n.Item = notify.Link{Text: dto.Branch()}
	if !dto.Deleted {
		n.Item.URL = dto.TreeURL
		if n.Item.URL == "" && dto.Repository.HTMLURL != "" {
			n.Item.URL = dto.Repository.HTMLURL + "/tree/" + dto.Branch()
		}
		n.Fields = append(n.Fields, countField(lang, i18n.MsgFieldCommits, dto.CommitCount()))
	}
	if dto.Pusher.Email != "" {
		n.Fields = append(n.Fields, field(lang, i18n.MsgFieldEmail, notify.C(dto.Pusher.Email)))
	}

	if !dto.Deleted {
		n.Sections = append(n.Sections, commitsSection(lang, dto), filesSection(lang, dto))
		n.Buttons = append(n.Buttons, button(lang, i18n.MsgBtnCompare, dto.Compare, notify.StylePrimary))
	}
	return n
}

func pushHeading(lang i18n.Lang, dto *GitHubPushDto) string {
	switch {
	case dto.Deleted:
		return i18n.T(lang, i18n.MsgPushDeleted)
	case dto.Created:
		return i18n.T(lang, i18n.MsgPushCreated)
	case dto.Forced:
		return i18n.T(lang, i18n.MsgPushForce)
	}
	return i18n.T(lang, i18n.MsgPushNew)
}

// commitsSection lists the pushed commits; commits the payload does not
// include (GitLab sends at most 20) are counted as "more".
func commitsSection(lang i18n.Lang, dto *GitHubPushDto) notify.Section {
	s := notify.Section{Summary: i18n.T(lang, i18n.MsgSectionCommits), ClassicMax: maxListedCommits}
	if len(dto.Commits) == 0 {
		// The count is already a field; a list of only "…and N more" adds nothing.
		return s
	}
	s.More = dto.CommitCount() - len(dto.Commits)
	for _, c := range dto.Commits {
		author := notify.T(firstNonEmpty(c.Author.Name, "unknown"))
		if c.Author.Username != "" {
			author = notify.L(author.Text, "https://github.com/"+c.Author.Username)
		}
		s.Items = append(s.Items, []notify.Part{
			notify.L(shortSHA(c.ID), c.URL),
			notify.T(" " + truncate(firstLine(c.Message), 70) + " — "),
			author,
		})
	}
	return s
}

// filesSection lists the files changed by the head commit.
func filesSection(lang i18n.Lang, dto *GitHubPushDto) notify.Section {
	s := notify.Section{Summary: i18n.T(lang, i18n.MsgSectionFiles), ClassicMax: maxListedFiles}
	if dto.HeadCommit == nil {
		return s
	}
	groups := []struct {
		mark  string
		files []string
	}{
		{"➕ ", dto.HeadCommit.Added},
		{"✏️ ", dto.HeadCommit.Modified},
		{"➖ ", dto.HeadCommit.Removed},
	}
	for _, g := range groups {
		for _, f := range g.files {
			s.Items = append(s.Items, []notify.Part{notify.T(g.mark), notify.C(f)})
		}
	}
	return s
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

// tagNotification is a tag push notification (shared by GitHub and GitLab).
func tagNotification(lang i18n.Lang, v tagView) notify.Notification {
	view := itemView{
		Heading:  i18n.T(lang, i18n.MsgTagCreated),
		Repo:     v.Repo,
		RepoURL:  v.RepoURL,
		Ref:      v.Tag,
		URL:      v.URL,
		Actor:    v.Actor,
		ActorURL: v.ActorURL,
		Buttons:  []notify.Button{button(lang, i18n.MsgBtnOpenTag, v.URL, notify.StyleDefault)},
	}
	if v.Deleted {
		view.Heading, view.URL, view.Buttons = i18n.T(lang, i18n.MsgTagDeleted), "", nil
	}
	return view.notification()
}
