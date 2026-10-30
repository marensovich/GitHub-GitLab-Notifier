package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
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
		IID            int    `json:"iid"`
		Title          string `json:"title"`
		Description    string `json:"description"`
		URL            string `json:"url"`
		SourceBranch   string `json:"source_branch"`
		TargetBranch   string `json:"target_branch"`
		Action         string `json:"action"`
		Draft          bool   `json:"draft"`
		WorkInProgress bool   `json:"work_in_progress"`
	} `json:"object_attributes"`
}

// GitLabMergeRequestHandler handles merge request lifecycle events.
type GitLabMergeRequestHandler struct{}

func (h *GitLabMergeRequestHandler) GetEventType() platform.EventType {
	return platform.GitLabMergeRequest
}

func (h *GitLabMergeRequestHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitLabMergeRequestDto
	if err := decode(lang, "Merge Request Hook", payload, &dto); err != nil {
		return "", err
	}

	mr := dto.ObjectAttributes
	var headerKey string
	withBody := false
	switch mr.Action {
	case "open":
		headerKey, withBody = i18n.MsgMROpened, true
		if mr.Draft || mr.WorkInProgress {
			headerKey = i18n.MsgMROpenedDraft
		}
	case "close":
		headerKey = i18n.MsgMRClosed
	case "reopen":
		headerKey = i18n.MsgMRReopened
	case "merge":
		headerKey = i18n.MsgMRMerged
	case "approved":
		headerKey = i18n.MsgMRApproved
	default:
		// update, approval, unapproved, unapproval, ...
		return "", ErrSkip
	}

	v := itemView{
		Header:   i18n.T(lang, headerKey),
		Repo:     dto.Project.PathWithNamespace,
		RepoURL:  dto.Project.WebURL,
		Ref:      fmt.Sprintf("!%d", mr.IID),
		URL:      mr.URL,
		Title:    mr.Title,
		Source:   mr.SourceBranch,
		Target:   mr.TargetBranch,
		Actor:    dto.User.Username,
		ActorURL: gitlabUserURL(dto.Project.WebURL, dto.User.Username),
	}
	if withBody {
		v.Body = mr.Description
	}
	return formatItem(lang, v), nil
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
	} `json:"object_attributes"`
}

// GitLabIssueHandler handles issue events. Event is either
// platform.GitLabIssue or platform.GitLabConfidentialIssue.
type GitLabIssueHandler struct {
	Event platform.EventType
}

func (h *GitLabIssueHandler) GetEventType() platform.EventType {
	return h.Event
}

func (h *GitLabIssueHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitLabIssueDto
	if err := decode(lang, h.Event.Name, payload, &dto); err != nil {
		return "", err
	}

	issue := dto.ObjectAttributes
	var headerKey string
	withBody := false
	switch issue.Action {
	case "open":
		headerKey, withBody = i18n.MsgIssueOpened, true
	case "close":
		headerKey = i18n.MsgIssueClosed
	case "reopen":
		headerKey = i18n.MsgIssueReopened
	default:
		return "", ErrSkip
	}

	v := itemView{
		Header:   i18n.T(lang, headerKey),
		Repo:     dto.Project.PathWithNamespace,
		RepoURL:  dto.Project.WebURL,
		Ref:      fmt.Sprintf("#%d", issue.IID),
		URL:      issue.URL,
		Title:    issue.Title,
		Actor:    dto.User.Username,
		ActorURL: gitlabUserURL(dto.Project.WebURL, dto.User.Username),
	}
	if withBody {
		v.Body = issue.Description
	}
	return formatItem(lang, v), nil
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
	} `json:"object_attributes"`
	Issue *struct {
		IID   int    `json:"iid"`
		Title string `json:"title"`
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

func (h *GitLabNoteHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitLabNoteDto
	if err := decode(lang, h.Event.Name, payload, &dto); err != nil {
		return "", err
	}

	note := dto.ObjectAttributes
	v := itemView{
		Repo:     dto.Project.PathWithNamespace,
		RepoURL:  dto.Project.WebURL,
		URL:      note.URL,
		Actor:    dto.User.Username,
		ActorURL: gitlabUserURL(dto.Project.WebURL, dto.User.Username),
		Body:     note.Note,
	}

	switch {
	case note.NoteableType == "Issue" && dto.Issue != nil:
		v.Header = i18n.T(lang, i18n.MsgCommentIssue)
		v.Ref, v.Title = fmt.Sprintf("#%d", dto.Issue.IID), dto.Issue.Title
	case note.NoteableType == "MergeRequest" && dto.MergeRequest != nil:
		v.Header = i18n.T(lang, i18n.MsgCommentMR)
		v.Ref, v.Title = fmt.Sprintf("!%d", dto.MergeRequest.IID), dto.MergeRequest.Title
	case note.NoteableType == "Commit" && dto.Commit != nil:
		v.Header = i18n.T(lang, i18n.MsgCommentCommit)
		v.Ref, v.Title = shortSHA(dto.Commit.ID), firstLine(dto.Commit.Message)
	default:
		// Snippets and unknown noteable types.
		return "", ErrSkip
	}

	return formatItem(lang, v), nil
}

// ─── Pipeline Hook ───

// GitLabPipelineDto is the payload of the "Pipeline Hook" event.
type GitLabPipelineDto struct {
	User             GitLabUser    `json:"user"`
	Project          GitLabProject `json:"project"`
	ObjectAttributes struct {
		ID       int    `json:"id"`
		IID      int    `json:"iid"`
		Name     string `json:"name"`
		Ref      string `json:"ref"`
		SHA      string `json:"sha"`
		Status   string `json:"status"`
		Duration *int   `json:"duration"`
		URL      string `json:"url"`
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

func (h *GitLabPipelineHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitLabPipelineDto
	if err := decode(lang, "Pipeline Hook", payload, &dto); err != nil {
		return "", err
	}

	p := dto.ObjectAttributes
	headerKey, ok := ciHeaderKey(p.Status)
	if !ok {
		return "", ErrSkip
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
		Header:   i18n.T(lang, headerKey),
		Repo:     dto.Project.PathWithNamespace,
		RepoURL:  dto.Project.WebURL,
		Name:     firstNonEmpty(p.Name, "Pipeline"),
		Number:   number,
		URL:      pipelineURL,
		Branch:   p.Ref,
		SHA:      p.SHA,
		Actor:    dto.User.Username,
		ActorURL: gitlabUserURL(dto.Project.WebURL, dto.User.Username),
	}
	if dto.Commit != nil {
		v.CommitURL, v.CommitMsg = dto.Commit.URL, dto.Commit.Message
	}
	if p.Duration != nil {
		v.Duration = time.Duration(*p.Duration) * time.Second
	}
	return formatCI(lang, v), nil
}

// ─── Release Hook ───

// GitLabReleaseDto is the payload of the "Release Hook" event.
type GitLabReleaseDto struct {
	Action      string        `json:"action"`
	Name        string        `json:"name"`
	Tag         string        `json:"tag"`
	Description string        `json:"description"`
	URL         string        `json:"url"`
	Project     GitLabProject `json:"project"`
}

// GitLabReleaseHandler handles newly created releases.
type GitLabReleaseHandler struct{}

func (h *GitLabReleaseHandler) GetEventType() platform.EventType {
	return platform.GitLabRelease
}

func (h *GitLabReleaseHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
	var dto GitLabReleaseDto
	if err := decode(lang, "Release Hook", payload, &dto); err != nil {
		return "", err
	}
	if dto.Action != "create" {
		return "", ErrSkip
	}

	return formatItem(lang, itemView{
		Header:  i18n.T(lang, i18n.MsgReleasePublished),
		Repo:    dto.Project.PathWithNamespace,
		RepoURL: dto.Project.WebURL,
		Ref:     dto.Tag,
		URL:     dto.URL,
		Title:   firstNonEmpty(dto.Name, dto.Tag),
		Body:    dto.Description,
	}), nil
}
