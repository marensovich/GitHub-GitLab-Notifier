package handler

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"strings"
)

// maxBodyLength limits issue/PR/comment bodies quoted in notifications.
const maxBodyLength = 500

// itemView is a platform-independent description of a notification about
// a repository item: pull/merge request, issue, comment, release, CI run.
type itemView struct {
	// Header is the localized action phrase, including an emoji.
	Header  string
	Repo    string
	RepoURL string

	// Ref is the short item reference ("#12", "!3", "v1.0.0") linked to URL.
	Ref   string
	URL   string
	Title string

	// Source and Target are branch names for pull/merge requests.
	Source string
	Target string

	Actor    string
	ActorURL string

	// Extra holds pre-formatted (already escaped) lines.
	Extra []string

	// Body is raw user text, quoted after escaping and truncation.
	Body string
}

// formatItem renders an itemView as Telegram HTML.
func formatItem(lang i18n.Lang, v itemView) string {
	var sb strings.Builder

	sb.WriteString(i18n.Tf(lang, i18n.MsgEventHeader, v.Header, link(v.RepoURL, v.Repo)))

	switch {
	case v.Title != "":
		sb.WriteString(i18n.Tf(lang, i18n.MsgItemTitle, link(v.URL, v.Ref), esc(truncate(v.Title, 200))))
	case v.Ref != "":
		sb.WriteString(link(v.URL, v.Ref) + "\n")
	}
	if v.Source != "" && v.Target != "" {
		sb.WriteString(i18n.Tf(lang, i18n.MsgItemBranches, esc(v.Source), esc(v.Target)))
	}
	if v.Actor != "" {
		sb.WriteString(i18n.Tf(lang, i18n.MsgItemActor, link(v.ActorURL, "@"+v.Actor)))
	}
	for _, line := range v.Extra {
		sb.WriteString(line)
	}
	if body := excerpt(v.Body, maxBodyLength); body != "" {
		sb.WriteString(i18n.Tf(lang, i18n.MsgItemBody, body))
	}

	return strings.TrimRight(sb.String(), "\n")
}
