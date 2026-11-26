// Package personal finds the people a webhook event concerns: who was
// mentioned, assigned or asked for a review, and whose pipeline failed.
package personal

import (
	"encoding/json"
	"net/url"
	"strings"

	"GitHub-GitLab-Notifier/internal/platform"
)

// Reason is why a person gets a DM about an event.
type Reason string

const (
	Review   Reason = "review"
	Assigned Reason = "assigned"
	Mention  Reason = "mention"
	Pipeline Reason = "pipeline"
)

// Order is the priority of reasons: a person who has several gets one DM
// with the first of them.
var Order = []Reason{Review, Assigned, Mention, Pipeline}

// Hit is one person an event concerns. A mention has only a login.
type Hit struct {
	AccountID, Login string
	Reason           Reason
}

// Event is what an event tells about people.
type Event struct {
	// Platform is "github" or "gitlab"; Host is the repository's host,
	// lower-case.
	Platform, Host string
	// Private is true for a repository only its members can read.
	Private bool
	// ActorID and ActorLogin are who caused the event.
	ActorID, ActorLogin string
	Hits                []Hit
}

// id is a numeric user ID that may come as a number or a string.
type id string

func (v *id) UnmarshalJSON(b []byte) error {
	*v = id(strings.Trim(string(b), `"`))
	if *v == "null" {
		*v = ""
	}
	return nil
}

type ghUser struct {
	Login string `json:"login"`
	ID    id     `json:"id"`
}

type glUser struct {
	Username string `json:"username"`
	ID       id     `json:"id"`
}

type glChange struct {
	Previous []glUser `json:"previous"`
	Current  []glUser `json:"current"`
}

// added returns the users in Current that were not in Previous.
func (c *glChange) added() []glUser {
	if c == nil {
		return nil
	}
	was := map[id]bool{}
	for _, u := range c.Previous {
		was[u.ID] = true
	}
	var out []glUser
	for _, u := range c.Current {
		if !was[u.ID] {
			out = append(out, u)
		}
	}
	return out
}

type payload struct {
	// GitHub
	Action            string  `json:"action"`
	Sender            ghUser  `json:"sender"`
	Assignee          *ghUser `json:"assignee"`
	RequestedReviewer *ghUser `json:"requested_reviewer"`
	Repository        struct {
		HTMLURL string `json:"html_url"`
		Private *bool  `json:"private"`
	} `json:"repository"`
	PullRequest struct{ Body string } `json:"pull_request"`
	Issue       struct{ Body string } `json:"issue"`
	Comment     struct{ Body string } `json:"comment"`
	Review      struct{ Body string } `json:"review"`
	WorkflowRun struct {
		Conclusion      string  `json:"conclusion"`
		Actor           ghUser  `json:"actor"`
		TriggeringActor *ghUser `json:"triggering_actor"`
	} `json:"workflow_run"`

	// GitLab
	User             glUser `json:"user"`
	ObjectAttributes struct {
		Action       string `json:"action"`
		Description  string `json:"description"`
		Note         string `json:"note"`
		Status       string `json:"status"`
		Confidential bool   `json:"confidential"`
	} `json:"object_attributes"`
	Assignees []glUser `json:"assignees"`
	Reviewers []glUser `json:"reviewers"`
	Changes   struct {
		Assignees *glChange `json:"assignees"`
		Reviewers *glChange `json:"reviewers"`
	} `json:"changes"`
	Project struct {
		WebURL          string `json:"web_url"`
		VisibilityLevel *int   `json:"visibility_level"`
	} `json:"project"`
}

// glPublic is GitLab's visibility level of a public project.
const glPublic = 20

// glPrivate: a project that is not public, one whose visibility is not
// given, or a confidential issue or note in any project.
func glPrivate(event string, d *payload) bool {
	return d.Project.VisibilityLevel == nil || *d.Project.VisibilityLevel < glPublic ||
		event == "Confidential Issue Hook" || event == "Confidential Note Hook" || d.ObjectAttributes.Confidential
}

// Extract reads an event's payload. An event about nobody has no hits.
func Extract(p platform.Platform, eventName string, body []byte) Event {
	var d payload
	if err := json.Unmarshal(body, &d); err != nil {
		return Event{}
	}
	var e Event
	switch p {
	case platform.GitHub:
		// No "private" field: assume the worst.
		e = Event{Platform: "github", Private: d.Repository.Private == nil || *d.Repository.Private,
			ActorID: string(d.Sender.ID), ActorLogin: d.Sender.Login}
		e.Hits = githubHits(eventName, &d)
	case platform.GitLab:
		e = Event{Platform: "gitlab", ActorID: string(d.User.ID), ActorLogin: d.User.Username,
			Private: glPrivate(eventName, &d)}
		e.Hits = gitlabHits(eventName, &d)
	default:
		return Event{}
	}
	if u, err := url.Parse(platform.RepositoryURL(p, body)); err == nil {
		// With the port: the API of a server on another port is there too.
		e.Host = strings.ToLower(u.Host)
	}
	return e
}

func mentions(text string) []Hit {
	var out []Hit
	for _, login := range Mentions(text) {
		out = append(out, Hit{Login: login, Reason: Mention})
	}
	return out
}

func ghHit(u *ghUser, r Reason) []Hit {
	if u == nil || u.Login == "" {
		return nil
	}
	return []Hit{{AccountID: string(u.ID), Login: u.Login, Reason: r}}
}

func githubHits(event string, d *payload) []Hit {
	switch event {
	case "pull_request":
		switch d.Action {
		case "review_requested":
			return ghHit(d.RequestedReviewer, Review)
		case "assigned":
			return ghHit(d.Assignee, Assigned)
		case "opened":
			return mentions(d.PullRequest.Body)
		}
	case "issues":
		switch d.Action {
		case "assigned":
			return ghHit(d.Assignee, Assigned)
		case "opened":
			return mentions(d.Issue.Body)
		}
	case "issue_comment":
		if d.Action == "created" {
			return mentions(d.Comment.Body)
		}
	case "pull_request_review":
		if d.Action == "submitted" {
			return mentions(d.Review.Body)
		}
	case "workflow_run":
		run := d.WorkflowRun
		if d.Action == "completed" && run.Conclusion == "failure" {
			if run.TriggeringActor != nil && run.TriggeringActor.Login != "" {
				return ghHit(run.TriggeringActor, Pipeline)
			}
			return ghHit(&run.Actor, Pipeline)
		}
	}
	return nil
}

func glHits(users []glUser, r Reason) []Hit {
	var out []Hit
	for _, u := range users {
		if u.Username != "" {
			out = append(out, Hit{AccountID: string(u.ID), Login: u.Username, Reason: r})
		}
	}
	return out
}

func gitlabHits(event string, d *payload) []Hit {
	attrs := d.ObjectAttributes
	switch event {
	case "Merge Request Hook", "Issue Hook", "Confidential Issue Hook":
		switch attrs.Action {
		case "open":
			hits := glHits(d.Reviewers, Review)
			hits = append(hits, glHits(d.Assignees, Assigned)...)
			return append(hits, mentions(attrs.Description)...)
		case "update":
			return append(glHits(d.Changes.Reviewers.added(), Review), glHits(d.Changes.Assignees.added(), Assigned)...)
		}
	case "Note Hook", "Confidential Note Hook":
		return mentions(attrs.Note)
	case "Pipeline Hook":
		if attrs.Status == "failed" && d.User.Username != "" {
			return []Hit{{AccountID: string(d.User.ID), Login: d.User.Username, Reason: Pipeline}}
		}
	}
	return nil
}
