package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/notify"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// itemView is a platform-independent description of a notification about
// a repository item: pull/merge request, issue, comment, release, CI run.
type itemView struct {
	// Heading is the localized action phrase, including an emoji.
	Heading string
	Repo    string
	RepoURL string

	// Ref is the short item reference ("#12", "!3", "v1.0.0") linked to URL.
	Ref   string
	URL   string
	Title string

	Actor    string
	ActorURL string

	Fields  []notify.Field
	Notes   []string
	Body    string
	Buttons []notify.Button
	Time    time.Time
}

// notification builds the notification of an itemView.
func (v itemView) notification() notify.Notification {
	item := strings.TrimSpace(strings.Join([]string{v.Ref, v.Title}, " "))
	return notify.Notification{
		Heading: v.Heading,
		Item:    notify.Link{Text: item, URL: v.URL},
		Repo:    notify.Link{Text: v.Repo, URL: v.RepoURL},
		Fields:  v.Fields,
		Quote:   v.Body,
		Notes:   v.Notes,
		Buttons: v.Buttons,
		Actor:   actor(v.Actor, v.ActorURL),
		Time:    v.Time,
	}
}

// actor is the "@login" link of a user, empty when the login is unknown.
func actor(login, url string) notify.Link {
	if login == "" {
		return notify.Link{}
	}
	return notify.Link{Text: "@" + login, URL: url}
}

// button is a localized URL button; it is dropped by the renderer when url is empty.
func button(lang i18n.Lang, key, url string, style notify.Style) notify.Button {
	return notify.Button{Label: i18n.T(lang, key), URL: url, Style: style}
}

// field is a localized field; a field without value parts is not shown.
func field(lang i18n.Lang, key string, value ...notify.Part) notify.Field {
	return notify.Field{Label: i18n.T(lang, key), Value: value}
}

// branchesField is "source → target" for pull and merge requests.
func branchesField(lang i18n.Lang, source, target string) notify.Field {
	if source == "" || target == "" {
		return notify.Field{}
	}
	return field(lang, i18n.MsgFieldBranches, notify.C(source), notify.T(" → "), notify.C(target))
}

// countField shows a positive number in bold; zero hides the field.
func countField(lang i18n.Lang, key string, n int) notify.Field {
	if n <= 0 {
		return notify.Field{}
	}
	return field(lang, key, notify.B(fmt.Sprint(n)))
}

// Timestamp is a payload time that tolerates the formats GitHub and GitLab
// use ("2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05 UTC"). A missing or
// unknown value leaves it zero instead of failing the whole payload.
type Timestamp struct{ time.Time }

var timestampLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05 MST",
	"2006-01-02 15:04:05 -0700",
}

func (ts *Timestamp) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	ts.Time = parseTime(s)
	return nil
}

// parseTime parses s with the known payload layouts; zero when none fits.
func parseTime(s string) time.Time {
	for _, layout := range timestampLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
