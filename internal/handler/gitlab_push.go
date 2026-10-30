package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
)

// GitLabPushDto is the payload of "Push Hook" and "Tag Push Hook" events.
type GitLabPushDto struct {
	Ref               string             `json:"ref"`
	Before            string             `json:"before"`
	After             string             `json:"after"`
	CheckoutSHA       string             `json:"checkout_sha"`
	UserName          string             `json:"user_name"`
	UserUsername      string             `json:"user_username"`
	UserEmail         string             `json:"user_email"`
	Project           GitLabProject      `json:"project"`
	Commits           []GitLabPushCommit `json:"commits"`
	TotalCommitsCount int                `json:"total_commits_count"`
}

// GitLabPushCommit is a commit inside a GitLab push payload.
type GitLabPushCommit struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	URL     string `json:"url"`
	Author  struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"author"`
	Added    []string `json:"added"`
	Modified []string `json:"modified"`
	Removed  []string `json:"removed"`
}

// toGitHubPush converts a GitLab push into the GitHub push model so both
// platforms share the same message layout.
func (d *GitLabPushDto) toGitHubPush() *GitHubPushDto {
	created, deleted := isZeroSHA(d.Before), isZeroSHA(d.After)

	dto := &GitHubPushDto{
		Ref:          d.Ref,
		Before:       d.Before,
		After:        d.After,
		Created:      created,
		Deleted:      deleted,
		Repository:   GitHubPushRepo{FullName: d.Project.PathWithNamespace, HTMLURL: d.Project.WebURL},
		Pusher:       GitHubPushPusher{Name: d.UserName, Email: d.UserEmail, Username: d.UserUsername},
		Sender:       GitHubPushSender{Login: d.UserUsername, HTMLURL: gitlabUserURL(d.Project.WebURL, d.UserUsername)},
		TotalCommits: d.TotalCommitsCount,
	}
	if !created && !deleted && d.Project.WebURL != "" {
		dto.Compare = d.Project.WebURL + "/-/compare/" + d.Before + "..." + d.After
	}

	head := firstNonEmpty(d.CheckoutSHA, d.After)
	for _, c := range d.Commits {
		commit := GitHubPushCommit{
			ID:       c.ID,
			Message:  c.Message,
			URL:      c.URL,
			Author:   GitHubPushAuthor{Name: c.Author.Name, Email: c.Author.Email},
			Added:    c.Added,
			Modified: c.Modified,
			Removed:  c.Removed,
		}
		dto.Commits = append(dto.Commits, commit)
		if c.ID == head {
			dto.HeadCommit = &commit
		}
	}
	return dto
}

// GitLabPushHandler handles GitLab "Push Hook" events.
type GitLabPushHandler struct{}

func (h *GitLabPushHandler) GetEventType() platform.EventType {
	return platform.GitLabPush
}

func (h *GitLabPushHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitLabPushDto
	if err := decode(lang, "Push Hook", payload, &dto); err != nil {
		return "", err
	}
	return formatPushMessage(lang, dto.toGitHubPush()), nil
}

// GitLabTagPushHandler handles GitLab "Tag Push Hook" events.
type GitLabTagPushHandler struct{}

func (h *GitLabTagPushHandler) GetEventType() platform.EventType {
	return platform.GitLabTagPush
}

func (h *GitLabTagPushHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitLabPushDto
	if err := decode(lang, "Tag Push Hook", payload, &dto); err != nil {
		return "", err
	}

	tag := branchName(dto.Ref)
	return formatTag(lang, tagView{
		Repo:     dto.Project.PathWithNamespace,
		RepoURL:  dto.Project.WebURL,
		Tag:      tag,
		URL:      dto.Project.WebURL + "/-/tags/" + tag,
		Deleted:  isZeroSHA(dto.After),
		Actor:    firstNonEmpty(dto.UserUsername, dto.UserName),
		ActorURL: gitlabUserURL(dto.Project.WebURL, dto.UserUsername),
	}), nil
}
