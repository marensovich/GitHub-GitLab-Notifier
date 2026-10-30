package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"fmt"
	"time"
)

// ─── ping ───

// GitHubPingDto is the payload GitHub sends right after a webhook is created.
type GitHubPingDto struct {
	Zen  string `json:"zen"`
	Hook struct {
		Events []string `json:"events"`
	} `json:"hook"`
	Repository GitHubRepository `json:"repository"`
	Sender     GitHubUser       `json:"sender"`
}

// GitHubPingHandler confirms that a webhook was set up correctly.
type GitHubPingHandler struct{}

func (h *GitHubPingHandler) GetEventType() platform.EventType {
	return platform.GitHubPing
}

func (h *GitHubPingHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitHubPingDto
	if err := decode(lang, "ping", payload, &dto); err != nil {
		return "", err
	}

	repo := dto.Repository.FullName
	if repo == "" {
		// Organization-level webhooks have no repository.
		repo = "GitHub"
	}

	msg := i18n.Tf(lang, i18n.MsgPing, link(dto.Repository.HTMLURL, repo))
	if dto.Zen != "" {
		msg += i18n.Tf(lang, i18n.MsgPingZen, esc(dto.Zen))
	}
	return msg, nil
}

// ─── fork ───

// GitHubForkDto is the payload of the fork event.
type GitHubForkDto struct {
	Forkee     GitHubRepository `json:"forkee"`
	Repository GitHubRepository `json:"repository"`
	Sender     GitHubUser       `json:"sender"`
}

// GitHubForkHandler handles repository forks.
type GitHubForkHandler struct{}

func (h *GitHubForkHandler) GetEventType() platform.EventType {
	return platform.GitHubFork
}

func (h *GitHubForkHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitHubForkDto
	if err := decode(lang, "fork", payload, &dto); err != nil {
		return "", err
	}

	msg := i18n.Tf(lang, i18n.MsgFork,
		link(dto.Repository.HTMLURL, dto.Repository.FullName),
		link(dto.Sender.HTMLURL, "@"+dto.Sender.Login),
		link(dto.Forkee.HTMLURL, dto.Forkee.FullName),
	)
	if dto.Repository.ForksCount > 0 {
		msg += i18n.Tf(lang, i18n.MsgForkTotal, dto.Repository.ForksCount)
	}
	return msg, nil
}

// ─── release ───

// GitHubReleaseDto is the payload of the release event.
type GitHubReleaseDto struct {
	Action  string `json:"action"`
	Release struct {
		TagName    string     `json:"tag_name"`
		Name       string     `json:"name"`
		Body       string     `json:"body"`
		HTMLURL    string     `json:"html_url"`
		Prerelease bool       `json:"prerelease"`
		Author     GitHubUser `json:"author"`
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

func (h *GitHubReleaseHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitHubReleaseDto
	if err := decode(lang, "release", payload, &dto); err != nil {
		return "", err
	}
	if dto.Action != "published" {
		return "", ErrSkip
	}

	r := dto.Release
	headerKey := i18n.MsgReleasePublished
	if r.Prerelease {
		headerKey = i18n.MsgReleasePrerelease
	}

	return formatItem(lang, itemView{
		Header:   i18n.T(lang, headerKey),
		Repo:     dto.Repository.FullName,
		RepoURL:  dto.Repository.HTMLURL,
		Ref:      r.TagName,
		URL:      r.HTMLURL,
		Title:    firstNonEmpty(r.Name, r.TagName),
		Actor:    r.Author.Login,
		ActorURL: r.Author.HTMLURL,
		Body:     r.Body,
	}), nil
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

func (h *GitHubWorkflowRunHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitHubWorkflowRunDto
	if err := decode(lang, "workflow_run", payload, &dto); err != nil {
		return "", err
	}
	if dto.Action != "completed" {
		return "", ErrSkip
	}

	run := dto.WorkflowRun
	headerKey, ok := ciHeaderKey(run.Conclusion)
	if !ok {
		return "", ErrSkip
	}

	commitURL := ""
	if run.HeadSHA != "" && dto.Repository.HTMLURL != "" {
		commitURL = dto.Repository.HTMLURL + "/commit/" + run.HeadSHA
	}

	return formatCI(lang, ciView{
		Header:    i18n.T(lang, headerKey),
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
	}), nil
}

// ─── CI helpers shared with GitLab pipelines ───

// ciHeaderKey maps a CI result to its message key.
// Returns false for results that should not be reported (skipped, neutral, running...).
func ciHeaderKey(result string) (string, bool) {
	switch result {
	case "success":
		return i18n.MsgCISuccess, true
	case "failure", "failed", "timed_out", "startup_failure":
		return i18n.MsgCIFailed, true
	case "cancelled", "canceled":
		return i18n.MsgCICancelled, true
	}
	return "", false
}

// ciView describes a finished CI run (GitHub workflow run or GitLab pipeline).
type ciView struct {
	Header    string
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
}

func formatCI(lang i18n.Lang, v ciView) string {
	var extra []string
	if v.Branch != "" {
		extra = append(extra, i18n.Tf(lang, i18n.MsgCIBranch,
			esc(v.Branch), link(v.CommitURL, shortSHA(v.SHA))))
	}
	if msg := firstLine(v.CommitMsg); msg != "" {
		extra = append(extra, i18n.Tf(lang, i18n.MsgCICommit, esc(truncate(msg, 70))))
	}
	if v.Duration > 0 {
		extra = append(extra, i18n.Tf(lang, i18n.MsgCIDuration, formatDuration(v.Duration)))
	}

	return formatItem(lang, itemView{
		Header:   v.Header,
		Repo:     v.Repo,
		RepoURL:  v.RepoURL,
		Ref:      fmt.Sprintf("#%d", v.Number),
		URL:      v.URL,
		Title:    v.Name,
		Actor:    v.Actor,
		ActorURL: v.ActorURL,
		Extra:    extra,
	})
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
