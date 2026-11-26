package telegram

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/notify"
	"strings"
)

// RenderClassic renders n as a classic Telegram HTML message (parse mode HTML) and
// the inline keyboard that carries its buttons.
//
// The text is cut down to Telegram's message limit when needed.
func RenderClassic(lang i18n.Lang, n notify.Notification) (string, [][]notify.Button) {
	var kb [][]notify.Button
	text := fit(n, func(n notify.Notification) string {
		var t string
		t, kb = classic(lang, n)
		return t
	}, VisibleLen, classicMaxText)
	if VisibleLen(text) > classicMaxText {
		// Only the fixed parts are left, each cut short.
		n.Fields, n.Sections, n.Notes, n.Quote = nil, nil, nil, ""
		n.Heading = truncate(n.Heading, maxTitle)
		text, kb = classic(lang, n)
	}
	return text, kb
}

func classic(lang i18n.Lang, n notify.Notification) (string, [][]notify.Button) {
	var sb strings.Builder

	sb.WriteString(esc(strings.TrimSpace(n.Emoji+" "+n.Heading)) + "\n")
	if n.Repo.Text != "" {
		sb.WriteString("<b>" + link(n.Repo, maxTitle) + "</b>\n")
	}
	sb.WriteString("\n")

	if n.Item.Text != "" {
		sb.WriteString("<b>" + link(n.Item, maxTitle) + "</b>\n")
	}
	for _, f := range nonEmpty(n.Fields) {
		sb.WriteString(esc(truncate(f.Label, maxTitle)) + ": " + parts(f.Value) + "\n")
	}
	for _, note := range n.Notes {
		sb.WriteString(esc(note) + "\n")
	}
	if n.Actor.Text != "" {
		sb.WriteString("👤 " + link(n.Actor, maxTitle) + "\n")
	}

	if q := quote(n.Quote); q != "" {
		sb.WriteString("\n<blockquote expandable>" + q + "</blockquote>\n")
	}

	for _, s := range n.Sections {
		if len(s.Items) == 0 && s.More == 0 {
			continue
		}
		items, more := s.Items, s.More
		if s.ClassicMax > 0 && len(items) > s.ClassicMax {
			more += len(items) - s.ClassicMax
			items = items[:s.ClassicMax]
		}
		sb.WriteString("\n<b>" + esc(s.Summary) + ":</b>\n")
		for _, it := range items {
			sb.WriteString("  " + parts(it) + "\n")
		}
		if more > 0 {
			sb.WriteString("  <i>" + esc(i18n.Tf(lang, i18n.MsgSectionMore, more)) + "</i>\n")
		}
	}

	var kb [][]notify.Button
	if bs := buttons(n.Buttons); len(bs) > 0 {
		kb = [][]notify.Button{bs}
	}
	return strings.TrimRight(sb.String(), "\n"), kb
}
