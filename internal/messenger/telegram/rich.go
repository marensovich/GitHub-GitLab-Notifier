package telegram

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/notify"
	"fmt"
	"strings"
)

// RenderRich renders n as the html of a Telegram rich message, cut down to the
// rich message limit when needed.
func RenderRich(lang i18n.Lang, n notify.Notification) string {
	return fit(n, func(n notify.Notification) string { return rich(lang, n) }, runeLen, richMaxHTML)
}

func rich(lang i18n.Lang, n notify.Notification) string {
	var sb strings.Builder

	sb.WriteString("<h4>" + esc(strings.TrimSpace(n.Emoji+" "+n.Heading)) + "</h4>\n")

	// Without an item the repository takes its place, so the table leaves
	// out the repository row instead of showing it twice.
	item, repoRow := n.Item, n.Repo.Text != ""
	if item.Text == "" {
		item, repoRow = n.Repo, false
	}
	if item.Text != "" {
		sb.WriteString("<p><b>" + link(item, maxTitle) + "</b></p>\n")
	}

	fields := nonEmpty(n.Fields)
	if repoRow || len(fields) > 0 {
		sb.WriteString("<table compact>\n")
		if repoRow {
			sb.WriteString("<tr><td>" + esc(i18n.T(lang, i18n.MsgFieldRepo)) + "</td><td>" + link(n.Repo, maxTitle) + "</td></tr>\n")
		}
		for _, f := range fields {
			sb.WriteString("<tr><td>" + esc(f.Label) + "</td><td>" + parts(f.Value) + "</td></tr>\n")
		}
		sb.WriteString("</table>\n")
	}

	for _, note := range n.Notes {
		sb.WriteString("<p>" + esc(note) + "</p>\n")
	}

	if q := quote(n.Quote); q != "" {
		sb.WriteString("<blockquote expandable>" + strings.ReplaceAll(q, "\n", "<br>") + "</blockquote>\n")
	}

	for _, s := range n.Sections {
		if len(s.Items) == 0 && s.More == 0 {
			continue
		}
		items, more := s.Items, s.More
		if len(items) > maxRichItems {
			more += len(items) - maxRichItems
			items = items[:maxRichItems]
		}
		fmt.Fprintf(&sb, "<details><summary>%s (%d)</summary><ul>", esc(s.Summary), len(s.Items)+s.More)
		for _, it := range items {
			sb.WriteString("<li>" + parts(it) + "</li>")
		}
		if more > 0 {
			sb.WriteString("<li><i>" + esc(i18n.Tf(lang, i18n.MsgSectionMore, more)) + "</i></li>")
		}
		sb.WriteString("</ul></details>\n")
	}

	if bs := buttons(n.Buttons); len(bs) > 0 {
		sb.WriteString("<p>")
		for i, b := range bs {
			if i > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(`<tg-button type="url"`)
			if b.Style != notify.StyleDefault {
				sb.WriteString(` style="` + esc(string(b.Style)) + `"`)
			}
			sb.WriteString(` url="` + esc(b.URL) + `">` + esc(b.Label) + `</tg-button>`)
		}
		sb.WriteString("</p>\n")
	}

	var footer []string
	if n.Actor.Text != "" {
		footer = append(footer, link(n.Actor, maxTitle))
	}
	if !n.Time.IsZero() {
		footer = append(footer, fmt.Sprintf(`<tg-time unix="%d" format="r">%s</tg-time>`,
			n.Time.Unix(), esc(n.Time.UTC().Format(timeFallbackFm))))
	}
	if len(footer) > 0 {
		sb.WriteString("<footer>" + strings.Join(footer, " · ") + "</footer>")
	}

	return strings.TrimRight(sb.String(), "\n")
}
