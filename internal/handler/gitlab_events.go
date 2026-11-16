package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/notify"
	"GitHub-GitLab-Notifier/internal/platform"
	"fmt"
	"time"
)

// ─── Merge Request Hook ───

// GitLabMergeRequestDto is the payload of the "Merge Request Hook" event.
type GitLabMergeRequestDto struct {
	User             GitLabUser    `json:"user"`
	Project          GitLabProject `json:"project"`
	ObjectAttributes struct {
		IID            int       `json:"iid"`
		Title          string    `json:"title"`
		Description    string    `json:"description"`
		URL            string    `json:"url"`
		SourceBranch   string    `json:"source_branch"`
		TargetBranch   string    `json:"target_branch"`
		Action         string    `json:"action"`
		Draft          bool      `json:"draft"`
		WorkInProgress bool      `json:"work_in_progress"`
		UpdatedAt      Timestamp `json:"updated_at"`
	} `json:"object_attributes"`
}

// GitLabMergeRequestHandler handles merge request lifecycle events.
type GitLabMergeRequestHandler struct{}

func (h *GitLabMergeRequestHandler) GetEventType() platform.EventType {
	return platform.GitLabMergeRequest
}

func (h *GitLabMergeRequestHandler) Handle(lang i18n.Lang, payload []byte) (notify.Notification, error) {
	var dto GitLabMergeRequestDto
	if err := decode(lang, "Merge Request Hook", payload, &dto); err != nil {
		return notify.Notification{}, err
	}

	mr := dto.ObjectAttributes
	var headingKey string
	withBody := false
	switch mr.Action {
	case "open":
		headingKey, withBody = i18n.MsgMROpened, true
		if mr.Draft || mr.WorkInProgress {
			headingKey = i18n.MsgMROpenedDraft
		}
	case "close":
		headingKey = i18n.MsgMRClosed
	case "reopen":
		headingKey = i18n.MsgMRReopened
	case "merge":
		headingKey = i18n.MsgMRMerged
	case "approved":
		headingKey = i18n.MsgMRApproved
	default:
		// update, approval, unapproved, unapproval, ...
		return notify.Notification{}, ErrSkip
	}

	v := itemView{
		Heading:  i18n.T(lang, headingKey),
		Repo:     dto.Project.PathWithNamespace,
		RepoURL:  dto.Project.WebURL,
		Ref:      fmt.Sprintf("!%d", mr.IID),
		URL:      mr.URL,
		Title:    mr.Title,
		Actor:    dto.User.Username,
		ActorURL: gitlabUserURL(dto.Project.WebURL, dto.User.Username),
		Fields:   []notify.Field{branchesField(lang, mr.SourceBranch, mr.TargetBranch)},
		Buttons:  []notify.Button{button(lang, i18n.MsgBtnOpenMR, mr.URL, notify.StylePrimary)},
		Time:     mr.UpdatedAt.Time,
	}
	if mr.URL != "" {
		v.Buttons = append(v.Buttons, button(lang, i18n.MsgBtnFiles, mr.URL+"/diffs", notify.StyleDefault))
	}
	if withBody {
		v.Body = mr.Description
	}
	return v.notification(), nil
}

// ─── Issue Hook / Confidential Issue Hook ───

// GitLabIssueDto is the payload of the "Issue Hook" event.
type GitLabIssueDto struct {
	User             GitLabUser    `json:"user"`
	Project          GitLabProject `json:"project"`
	ObjectAttributes struct {
		IID         int    `json:"iid"`
		Title       string `json:"title"`
		Description string `json:"description"`
		URL         string `json:"url"`
		Action      string `json:"action"`
		// Confidential issues are visible only to project members.
		Confidential bool      `json:"confidential"`
		UpdatedAt    Timestamp `json:"updated_at"`
	} `json:"object_attributes"`
	Labels []struct {
		Title string `json:"title"`
	} `json:"labels"`
}

// GitLabIssueHandler handles issue events. Event is either
// platform.GitLabIssue or platform.GitLabConfidentialIssue.
type GitLabIssueHandler struct {
	Event platform.EventType
}

func (h *GitLabIssueHandler) GetEventType() platform.EventType {
	return h.Event
}

func (h *GitLabIssueHandler) Handle(lang i18n.Lang, payload []byte) (notify.Notification, error) {
	var dto GitLabIssueDto
	if err := decode(lang, h.Event.Name, payload, &dto); err != nil {
		return notify.Notification{}, err
	}

	issue := dto.ObjectAttributes
	var headingKey string
	withBody := false
	switch issue.Action {
	case "open":
		headingKey, withBody = i18n.MsgIssueOpened, true
	case "close":
		headingKey = i18n.MsgIssueClosed
	case "reopen":
		headingKey = i18n.MsgIssueReopened
	default:
		return notify.Notification{}, ErrSkip
	}

	var labels []string
	for _, l := range dto.Labels {
		labels = append(labels, l.Title)
	}

	v := itemView{
		Heading:  i18n.T(lang, headingKey),
		Repo:     dto.Project.PathWithNamespace,
		RepoURL:  dto.Project.WebURL,
		Ref:      fmt.Sprintf("#%d", issue.IID),
		URL:      issue.URL,
		Title:    issue.Title,
		Actor:    dto.User.Username,
		ActorURL: gitlabUserURL(dto.Project.WebURL, dto.User.Username),
		Fields:   []notify.Field{labelsField(lang, labels)},
		Buttons:  []notify.Button{button(lang, i18n.MsgBtnOpenIssue, issue.URL, notify.StyleDefault)},
		Time:     issue.UpdatedAt.Time,
	}
	if withBody {
		v.Body = issue.Description
	}
	if h.Event == platform.GitLabConfidentialIssue || issue.Confidential {
		v.Notes = append(v.Notes, i18n.T(lang, i18n.MsgItemConfidential))
	}
	return v.notification(), nil
}

// ─── Note Hook / Confidential Note Hook ───

// GitLabNoteDto is the payload of the "Note Hook" (comment) event.
type GitLabNoteDto struct {
	User             GitLabUser    `json:"user"`
	Project          GitLabProject `json:"project"`
	ObjectAttributes struct {
		Note         string `json:"note"`
		NoteableType string `json:"noteable_type"`
		URL          string `json:"url"`
		// Confidential (internal) notes are visible only to project members.
		Confidential bool      `json:"confidential"`
		Internal     bool      `json:"internal"`
		CreatedAt    Timestamp `json:"created_at"`
	} `json:"object_attributes"`
	Issue *struct {
		IID          int    `json:"iid"`
		Title        string `json:"title"`
		Confidential bool   `json:"confidential"`
	} `json:"issue"`
	MergeRequest *struct {
		IID   int    `json:"iid"`
		Title string `json:"title"`
	} `json:"merge_request"`
	Commit *struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	} `json:"commit"`
}

// GitLabNoteHandler handles comments on issues, merge requests and commits.
// Event is either platform.GitLabNote or platform.GitLabConfidentialNote.
type GitLabNoteHandler struct {
	Event platform.EventType
}

func (h *GitLabNoteHandler) GetEventType() platform.EventType {
	return h.Event
}

func (h *GitLabNoteHandler) Handle(lang i18n.Lang, payload []byte) (notify.Notification, error) {
	var dto GitLabNoteDto
	if err := decode(lang, h.Event.Name, payload, &dto); err != nil {
		return notify.Notification{}, err
	}

	note := dto.ObjectAttributes
	v := itemView{
		Repo:     dto.Project.PathWithNamespace,
		RepoURL:  dto.Project.WebURL,
		URL:      note.URL,
		Actor:    dto.User.Username,
		ActorURL: gitlabUserURL(dto.Project.WebURL, dto.User.Username),
		Body:     note.Note,
		Buttons:  []notify.Button{button(lang, i18n.MsgBtnOpenComment, note.URL, notify.StyleDefault)},
		Time:     note.CreatedAt.Time,
	}

	switch {
	case note.NoteableType == "Issue" && dto.Issue != nil:
		v.Heading = i18n.T(lang, i18n.MsgCommentIssue)
		v.Ref, v.Title = fmt.Sprintf("#%d", dto.Issue.IID), dto.Issue.Title
	case note.NoteableType == "MergeRequest" && dto.MergeRequest != nil:
		v.Heading = i18n.T(lang, i18n.MsgCommentMR)
		v.Ref, v.Title = fmt.Sprintf("!%d", dto.MergeRequest.IID), dto.MergeRequest.Title
	case note.NoteableType == "Commit" && dto.Commit != nil:
		v.Heading = i18n.T(lang, i18n.MsgCommentCommit)
		v.Ref, v.Title = shortSHA(dto.Commit.ID), firstLine(dto.Commit.Message)
	default:
		// Snippets and unknown noteable types.
		return notify.Notification{}, ErrSkip
	}

	if h.Event == platform.GitLabConfidentialNote || note.Confidential || note.Internal ||
		(dto.Issue != nil && dto.Issue.Confidential) {
		v.Notes = append(v.Notes, i18n.T(lang, i18n.MsgItemConfidential))
	}
	return v.notification(), nil
}

// ─── Pipeline Hook ───

// GitLabPipelineDto is the payload of the "Pipeline Hook" event.
type GitLabPipelineDto struct {
	User             GitLabUser    `json:"user"`
	Project          GitLabProject `json:"project"`
	ObjectAttributes struct {
		ID         int       `json:"id"`
		IID        int       `json:"iid"`
		Name       string    `json:"name"`
		Ref        string    `json:"ref"`
		SHA        string    `json:"sha"`
		Status     string    `json:"status"`
		Duration   *int      `json:"duration"`
		URL        string    `json:"url"`
		CreatedAt  Timestamp `json:"created_at"`
		FinishedAt Timestamp `json:"finished_at"`
	} `json:"object_attributes"`
	Commit *struct {
		ID      string `json:"id"`
		Message string `json:"message"`
		URL     string `json:"url"`
	} `json:"commit"`
}

// GitLabPipelineHandler reports finished pipelines (success, failed, canceled).
// Pipelines in progress (created, pending, running, manual, skipped) are ignored.
type GitLabPipelineHandler struct{}

func (h *GitLabPipelineHandler) GetEventType() platform.EventType {
	return platform.GitLabPipeline
}

func (h *GitLabPipelineHandler) Handle(lang i18n.Lang, payload []byte) (notify.Notification, error) {
	var dto GitLabPipelineDto
	if err := decode(lang, "Pipeline Hook", payload, &dto); err != nil {
		return notify.Notification{}, err
	}

	p := dto.ObjectAttributes
	headingKey, style, ok := ciResult(p.Status)
	if !ok {
		return notify.Notification{}, ErrSkip
	}

	number := p.IID
	if number == 0 {
		number = p.ID
	}
	pipelineURL := p.URL
	if pipelineURL == "" && dto.Project.WebURL != "" {
		pipelineURL = fmt.Sprintf("%s/-/pipelines/%d", dto.Project.WebURL, p.ID)
	}

	v := ciView{
		Heading:  i18n.T(lang, headingKey),
		Style:    style,
		Repo:     dto.Project.PathWithNamespace,
		RepoURL:  dto.Project.WebURL,
		Name:     firstNonEmpty(p.Name, "Pipeline"),
		Number:   number,
		URL:      pipelineURL,
		Branch:   p.Ref,
		SHA:      p.SHA,
		Actor:    dto.User.Username,
		ActorURL: gitlabUserURL(dto.Project.WebURL, dto.User.Username),
		Time:     p.FinishedAt.Time,
	}
	if v.Time.IsZero() {
		v.Time = p.CreatedAt.Time
	}
	if dto.Commit != nil {
		v.CommitURL, v.CommitMsg = dto.Commit.URL, dto.Commit.Message
	}
	if p.Duration != nil {
		v.Duration = time.Duration(*p.Duration) * time.Second
	}
	return ciNotification(lang, v), nil
}

// ─── Release Hook ───

// GitLabReleaseDto is the payload of the "Release Hook" event.
type GitLabReleaseDto struct {
	Action      string        `json:"action"`
	Name        string        `json:"name"`
	Tag         string        `json:"tag"`
	Description string        `json:"description"`
	URL         string        `json:"url"`
	ReleasedAt  Timestamp     `json:"released_at"`
	CreatedAt   Timestamp     `json:"created_at"`
	Project     GitLabProject `json:"project"`
}

// GitLabReleaseHandler handles newly created releases.
type GitLabReleaseHandler struct{}

func (h *GitLabReleaseHandler) GetEventType() platform.EventType {
	return platform.GitLabRelease
}

func (h *GitLabReleaseHandler) Handle(lang i18n.Lang, payload []byte) (notify.Notification, error) {
	var dto GitLabReleaseDto
	if err := decode(lang, "Release Hook", payload, &dto); err != nil {
		return notify.Notification{}, err
	}
	if dto.Action != "create" {
		return notify.Notification{}, ErrSkip
	}

	when := dto.ReleasedAt.Time
	if when.IsZero() {
		when = dto.CreatedAt.Time
	}
	return releaseNotification(lang, itemView{
		Heading: i18n.T(lang, i18n.MsgReleasePublished),
		Repo:    dto.Project.PathWithNamespace,
		RepoURL: dto.Project.WebURL,
		URL:     dto.URL,
		Title:   firstNonEmpty(dto.Name, dto.Tag),
		Body:    dto.Description,
		Time:    when,
	}, dto.Tag), nil
}
