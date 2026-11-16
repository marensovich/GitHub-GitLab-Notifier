package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/notify"
	"GitHub-GitLab-Notifier/internal/platform"
	"fmt"
	"time"
)

// ─── ping ───

// GitHubPingDto is the payload GitHub sends right after a webhook is created.
type GitHubPingDto struct {
	Zen  string `json:"zen"`
	Hook struct {
		Events    []string  `json:"events"`
		CreatedAt Timestamp `json:"created_at"`
	} `json:"hook"`
	Repository GitHubRepository `json:"repository"`
	Sender     GitHubUser       `json:"sender"`
}

// GitHubPingHandler confirms that a webhook was set up correctly.
type GitHubPingHandler struct{}

func (h *GitHubPingHandler) GetEventType() platform.EventType {
	return platform.GitHubPing
}

func (h *GitHubPingHandler) Handle(lang i18n.Lang, payload []byte) (notify.Notification, error) {
	var dto GitHubPingDto
	if err := decode(lang, "ping", payload, &dto); err != nil {
		return notify.Notification{}, err
	}

	repo := dto.Repository.FullName
	if repo == "" {
		// Organization-level webhooks have no repository.
		repo = "GitHub"
	}

	return notify.Notification{
		Heading: i18n.T(lang, i18n.MsgPing),
		Repo:    notify.Link{Text: repo, URL: dto.Repository.HTMLURL},
		Notes:   []string{i18n.T(lang, i18n.MsgPingNote)},
		Quote:   dto.Zen,
		Actor:   actor(dto.Sender.Login, dto.Sender.HTMLURL),
		Buttons: []notify.Button{button(lang, i18n.MsgBtnOpenRepo, dto.Repository.HTMLURL, notify.StyleDefault)},
		Time:    dto.Hook.CreatedAt.Time,
	}, nil
}

// ─── fork ───

// GitHubForkDto is the payload of the fork event.
type GitHubForkDto struct {
	Forkee struct {
		GitHubRepository
		CreatedAt Timestamp `json:"created_at"`
	} `json:"forkee"`
	Repository GitHubRepository `json:"repository"`
	Sender     GitHubUser       `json:"sender"`
}

// GitHubForkHandler handles repository forks.
type GitHubForkHandler struct{}

func (h *GitHubForkHandler) GetEventType() platform.EventType {
	return platform.GitHubFork
}

func (h *GitHubForkHandler) Handle(lang i18n.Lang, payload []byte) (notify.Notification, error) {
	var dto GitHubForkDto
	if err := decode(lang, "fork", payload, &dto); err != nil {
		return notify.Notification{}, err
	}

	return notify.Notification{
		Heading: i18n.T(lang, i18n.MsgFork),
		Item:    notify.Link{Text: dto.Forkee.FullName, URL: dto.Forkee.HTMLURL},
		Repo:    notify.Link{Text: dto.Repository.FullName, URL: dto.Repository.HTMLURL},
		Fields:  []notify.Field{countField(lang, i18n.MsgFieldForks, dto.Repository.ForksCount)},
		Actor:   actor(dto.Sender.Login, dto.Sender.HTMLURL),
		Buttons: []notify.Button{button(lang, i18n.MsgBtnOpenFork, dto.Forkee.HTMLURL, notify.StyleDefault)},
		Time:    dto.Forkee.CreatedAt.Time,
	}, nil
}

// ─── release ───

// GitHubReleaseDto is the payload of the release event.
type GitHubReleaseDto struct {
	Action  string `json:"action"`
	Release struct {
		TagName     string     `json:"tag_name"`
		Name        string     `json:"name"`
		Body        string     `json:"body"`
		HTMLURL     string     `json:"html_url"`
		Prerelease  bool       `json:"prerelease"`
		Author      GitHubUser `json:"author"`
		PublishedAt Timestamp  `json:"published_at"`
	} `json:"release"`
	Repository GitHubRepository `json:"repository"`
}

// GitHubReleaseHandler handles published releases.
// GitHub sends several actions for one release (created, published,
// released, ...); only "published" is reported to avoid duplicates.
type GitHubReleaseHandler struct{}

func (h *GitHubReleaseHandler) GetEventType() platform.EventType {
	return platform.GitHubRelease
}

func (h *GitHubReleaseHandler) Handle(lang i18n.Lang, payload []byte) (notify.Notification, error) {
	var dto GitHubReleaseDto
	if err := decode(lang, "release", payload, &dto); err != nil {
		return notify.Notification{}, err
	}
	if dto.Action != "published" {
		return notify.Notification{}, ErrSkip
	}

	r := dto.Release
	headingKey := i18n.MsgReleasePublished
	if r.Prerelease {
		headingKey = i18n.MsgReleasePrerelease
	}

	return releaseNotification(lang, itemView{
		Heading:  i18n.T(lang, headingKey),
		Repo:     dto.Repository.FullName,
		RepoURL:  dto.Repository.HTMLURL,
		URL:      r.HTMLURL,
		Title:    firstNonEmpty(r.Name, r.TagName),
		Actor:    r.Author.Login,
		ActorURL: r.Author.HTMLURL,
		Body:     r.Body,
		Time:     r.PublishedAt.Time,
	}, r.TagName), nil
}

// releaseNotification adds the tag field and the release button to v.
func releaseNotification(lang i18n.Lang, v itemView, tag string) notify.Notification {
	if tag != "" {
		v.Fields = append(v.Fields, field(lang, i18n.MsgFieldTag, notify.C(tag)))
	}
	v.Buttons = append(v.Buttons, button(lang, i18n.MsgBtnOpenRelease, v.URL, notify.StylePrimary))
	return v.notification()
}

// ─── workflow_run ───

// GitHubWorkflowRunDto is the payload of the workflow_run event.
type GitHubWorkflowRunDto struct {
	Action      string `json:"action"`
	WorkflowRun struct {
		Name         string     `json:"name"`
		RunNumber    int        `json:"run_number"`
		HTMLURL      string     `json:"html_url"`
		HeadBranch   string     `json:"head_branch"`
		HeadSHA      string     `json:"head_sha"`
		Event        string     `json:"event"`
		Status       string     `json:"status"`
		Conclusion   string     `json:"conclusion"`
		RunStartedAt time.Time  `json:"run_started_at"`
		UpdatedAt    time.Time  `json:"updated_at"`
		Actor        GitHubUser `json:"actor"`
		HeadCommit   struct {
			Message string `json:"message"`
		} `json:"head_commit"`
	} `json:"workflow_run"`
	Repository GitHubRepository `json:"repository"`
}

// GitHubWorkflowRunHandler reports finished GitHub Actions workflow runs.
type GitHubWorkflowRunHandler struct{}

func (h *GitHubWorkflowRunHandler) GetEventType() platform.EventType {
	return platform.GitHubWorkflowRun
}

func (h *GitHubWorkflowRunHandler) Handle(lang i18n.Lang, payload []byte) (notify.Notification, error) {
	var dto GitHubWorkflowRunDto
	if err := decode(lang, "workflow_run", payload, &dto); err != nil {
		return notify.Notification{}, err
	}
	if dto.Action != "completed" {
		return notify.Notification{}, ErrSkip
	}

	run := dto.WorkflowRun
	headingKey, style, ok := ciResult(run.Conclusion)
	if !ok {
		return notify.Notification{}, ErrSkip
	}

	commitURL := ""
	if run.HeadSHA != "" && dto.Repository.HTMLURL != "" {
		commitURL = dto.Repository.HTMLURL + "/commit/" + run.HeadSHA
	}

	return ciNotification(lang, ciView{
		Heading:   i18n.T(lang, headingKey),
		Style:     style,
		Repo:      dto.Repository.FullName,
		RepoURL:   dto.Repository.HTMLURL,
		Name:      run.Name,
		Number:    run.RunNumber,
		URL:       run.HTMLURL,
		Branch:    run.HeadBranch,
		SHA:       run.HeadSHA,
		CommitURL: commitURL,
		CommitMsg: run.HeadCommit.Message,
		Actor:     run.Actor.Login,
		ActorURL:  run.Actor.HTMLURL,
		Duration:  run.UpdatedAt.Sub(run.RunStartedAt),
		Time:      run.UpdatedAt,
	}), nil
}

// ─── CI helpers shared with GitLab pipelines ───

// ciResult maps a CI result to its heading key and the colour of the run
// button. Returns false for results that should not be reported (skipped,
// neutral, running...).
func ciResult(result string) (string, notify.Style, bool) {
	switch result {
	case "success":
		return i18n.MsgCISuccess, notify.StyleSuccess, true
	case "failure", "failed", "timed_out", "startup_failure":
		return i18n.MsgCIFailed, notify.StyleDanger, true
	case "cancelled", "canceled":
		return i18n.MsgCICancelled, notify.StyleDefault, true
	}
	return "", notify.StyleDefault, false
}

// ciView describes a finished CI run (GitHub workflow run or GitLab pipeline).
type ciView struct {
	Heading   string
	Style     notify.Style
	Repo      string
	RepoURL   string
	Name      string
	Number    int
	URL       string
	Branch    string
	SHA       string
	CommitURL string
	CommitMsg string
	Actor     string
	ActorURL  string
	Duration  time.Duration
	Time      time.Time
}

func ciNotification(lang i18n.Lang, v ciView) notify.Notification {
	var fields []notify.Field
	if v.Branch != "" {
		value := []notify.Part{notify.C(v.Branch)}
		if v.SHA != "" {
			value = append(value, notify.T(" · "), notify.L(shortSHA(v.SHA), v.CommitURL))
		}
		fields = append(fields, field(lang, i18n.MsgFieldBranch, value...))
	}
	if msg := firstLine(v.CommitMsg); msg != "" {
		fields = append(fields, field(lang, i18n.MsgFieldCommit, notify.T(truncate(msg, 70))))
	}
	if v.Duration > 0 {
		fields = append(fields, field(lang, i18n.MsgFieldDuration, notify.T(formatDuration(v.Duration))))
	}

	return itemView{
		Heading:  v.Heading,
		Repo:     v.Repo,
		RepoURL:  v.RepoURL,
		Ref:      fmt.Sprintf("#%d", v.Number),
		URL:      v.URL,
		Title:    v.Name,
		Actor:    v.Actor,
		ActorURL: v.ActorURL,
		Fields:   fields,
		Buttons:  []notify.Button{button(lang, i18n.MsgBtnOpenRun, v.URL, v.Style)},
		Time:     v.Time,
	}.notification()
}

// formatDuration renders a duration as "1h 2m 3s", "4m 5s" or "6s".
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d / time.Hour)
	m := int(d % time.Hour / time.Minute)
	s := int(d % time.Minute / time.Second)
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}
