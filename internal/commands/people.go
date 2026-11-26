package commands

import (
	"context"
	"errors"
	"html"
	"log"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/personal"
	"GitHub-GitLab-Notifier/storage"
)

// hostPattern is a host name with at least one dot and an optional port.
var hostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+(:[0-9]{1,5})?$`)

// pressMe is the press data prefix of the /me toggles: "me:<reason>".
const pressMe = "me:"

// verifyTimeout bounds the one request /link makes with a token.
const verifyTimeout = 15 * time.Second

// reasonButtons are the /me toggle labels, in personal.Order.
var reasonButtons = map[personal.Reason]string{
	personal.Review:   i18n.MsgMeBtnReview,
	personal.Assigned: i18n.MsgMeBtnAssigned,
	personal.Mention:  i18n.MsgMeBtnMention,
	personal.Pipeline: i18n.MsgMeBtnPipeline,
}

// link ties a GitHub or GitLab account to the sender, proved by a token.
// The token is used for one request and dropped; the message carrying it
// is deleted where the messenger keeps one.
func (c *Core) link(r *request) {
	dm, _ := r.m.(messenger.DirectMessenger)
	undeleted := false
	if len(r.cmd.Args) > 0 && dm != nil {
		if err := dm.DeleteCommand(r.ctx, r.cmd); err != nil {
			log.Printf("❌ %s: delete the /link message in chat %s: %v", r.cmd.Messenger, r.cmd.ChatID, err)
			undeleted = true
		}
	}
	// say answers; a token the bot could not delete is pointed out.
	say := func(text string) {
		if undeleted {
			text += "\n\n" + i18n.T(r.lang, i18n.MsgLinkDeleteToken)
		}
		c.reply(r, text, nil)
	}
	if !r.cmd.IsPrivate && !r.m.Caps().HiddenCommands {
		say(i18n.T(r.lang, i18n.MsgLinkPrivateOnly))
		return
	}
	if len(r.cmd.Args) < 2 || dm == nil {
		say(i18n.T(r.lang, i18n.MsgLinkUsage))
		return
	}
	platform := strings.ToLower(r.cmd.Args[0])
	if platform != "github" && platform != "gitlab" {
		say(i18n.T(r.lang, i18n.MsgLinkUsage))
		return
	}
	token := r.cmd.Args[1]
	host := ""
	if len(r.cmd.Args) > 2 {
		// A token typed in the host's place must not be looked up or
		// echoed back: only a host name with a dot is tried.
		if host = hostOf(r.cmd.Args[2]); !hostPattern.MatchString(host) {
			say(i18n.T(r.lang, i18n.MsgLinkBadHost))
			return
		}
	}
	if host == "" {
		if platform == "github" {
			host = "github.com"
		} else {
			hosts := c.gitlabHosts()
			switch len(hosts) {
			case 0:
				say(i18n.T(r.lang, i18n.MsgLinkNeedHost))
				return
			case 1:
				host = hosts[0]
			default:
				say(i18n.Tf(r.lang, i18n.MsgLinkPickHost, html.EscapeString(strings.Join(hosts, ", "))))
				return
			}
		}
	}

	ctx, cancel := context.WithTimeout(r.ctx, verifyTimeout)
	acc, err := c.Verifier.WhoAmI(ctx, platform, host, token)
	cancel()
	switch {
	case errors.Is(err, personal.ErrBadToken):
		say(i18n.T(r.lang, i18n.MsgLinkBadToken))
		return
	case err != nil:
		say(i18n.Tf(r.lang, i18n.MsgLinkFailed, html.EscapeString(err.Error())))
		return
	}

	chat, err := dm.DirectChat(r.ctx, r.cmd.UserID)
	if err != nil {
		log.Printf("❌ %s: open the DM of user %s: %v", r.cmd.Messenger, r.cmd.UserID, err)
		say(i18n.T(r.lang, i18n.MsgLinkNoDM))
		return
	}
	d, err := c.store.EnsureDestination(r.cmd.Messenger, chat, storage.Destination{Language: string(r.lang), Format: defaultLook(r.m)})
	if err == nil {
		_, err = c.store.Link(r.cmd.Messenger, r.cmd.UserID, d.ID, acc)
	}
	if err != nil {
		log.Print(i18n.Tf(c.defaultLang(), i18n.MsgFailedToSaveChatConfig, err))
		say(i18n.T(r.lang, i18n.MsgCfgFailed))
		return
	}
	log.Printf("🔗 %s user %s linked %s account %s on %s", r.cmd.Messenger, r.cmd.UserID, platform, acc.Login, host)
	say(i18n.Tf(r.lang, i18n.MsgLinkDone, platformTitle(platform), html.EscapeString(acc.Login), html.EscapeString(host)))
}

// unlink removes the sender's links on a platform, or one by login; a bot
// administrator may name anyone's login.
func (c *Core) unlink(r *request) {
	if len(r.cmd.Args) < 1 {
		c.reply(r, i18n.T(r.lang, i18n.MsgUnlinkUsage), nil)
		return
	}
	platform := strings.ToLower(r.cmd.Args[0])
	if platform != "github" && platform != "gitlab" {
		c.reply(r, i18n.T(r.lang, i18n.MsgUnlinkUsage), nil)
		return
	}
	login := ""
	if len(r.cmd.Args) > 1 {
		login = strings.TrimPrefix(r.cmd.Args[1], "@")
	}
	n, err := c.store.Unlink(r.cmd.Messenger, r.cmd.UserID, platform, login)
	if err == nil && n == 0 && login != "" && c.cfg.IsAdmin(r.cmd.Messenger, r.cmd.UserID) {
		n, err = c.store.UnlinkLogin(platform, login)
	}
	switch {
	case err != nil:
		log.Print(i18n.Tf(c.defaultLang(), i18n.MsgFailedToSaveChatConfig, err))
		c.reply(r, i18n.T(r.lang, i18n.MsgCfgFailed), nil)
	case n == 0:
		c.reply(r, i18n.T(r.lang, i18n.MsgUnlinkNone), nil)
	default:
		c.reply(r, i18n.Tf(r.lang, i18n.MsgUnlinkDone, n), nil)
	}
}

// me shows the sender's links and the toggles of their personal DMs.
func (c *Core) me(r *request) {
	p, accs, err := c.store.PersonOf(r.cmd.Messenger, r.cmd.UserID)
	if err != nil {
		log.Printf("❌ links of %s user %s: %v", r.cmd.Messenger, r.cmd.UserID, err)
	}
	if p == nil || len(accs) == 0 {
		c.reply(r, i18n.T(r.lang, i18n.MsgMeNone), nil)
		return
	}
	text, choices := meView(r.lang, accs, p.Muted)
	c.reply(r, text, choices)
}

// mePress flips one kind of personal DM. Only the person the /me message
// answers may press its buttons.
func (c *Core) mePress(ctx context.Context, m messenger.Messenger, p messenger.Press) {
	var lang i18n.Lang
	if d, err := c.store.GetDestination(p.Messenger, p.ChatID); err == nil {
		lang = c.langOf(d)
	} else {
		lang = c.defaultLang()
	}
	if !p.IsPrivate && (p.OriginUserID == "" || p.OriginUserID != p.UserID) {
		c.answer(ctx, m, p, i18n.T(lang, i18n.MsgMeNotYours), true)
		return
	}
	reason := personal.Reason(strings.TrimPrefix(p.Data, pressMe))
	person, accs, err := c.store.PersonOf(p.Messenger, p.UserID)
	if err != nil || person == nil || len(accs) == 0 || !slices.Contains(personal.Order, reason) {
		c.answer(ctx, m, p, "", false)
		return
	}
	muted := slices.Clone(person.Muted)
	if i := slices.Index(muted, string(reason)); i >= 0 {
		muted = slices.Delete(muted, i, i+1)
	} else {
		muted = append(muted, string(reason))
	}
	if err := c.store.SetMuted(person.ID, muted); err != nil {
		log.Print(i18n.Tf(c.defaultLang(), i18n.MsgFailedToSaveChatConfig, err))
		c.answer(ctx, m, p, i18n.T(lang, i18n.MsgCfgFailed), true)
		return
	}
	text, choices := meView(lang, accs, muted)
	c.edit(ctx, m, p, text, choices)
	c.answer(ctx, m, p, "", false)
}

// meView is the /me message: the links, then one toggle per reason, marked
// ✓ when on.
func meView(lang i18n.Lang, accs []storage.Account, muted []string) (string, [][]messenger.Choice) {
	var sb strings.Builder
	sb.WriteString(i18n.T(lang, i18n.MsgMeHeader))
	for _, a := range accs {
		sb.WriteString(i18n.Tf(lang, i18n.MsgMeLine, platformTitle(a.Platform), html.EscapeString(a.Login), html.EscapeString(a.Host)))
	}
	sb.WriteString(i18n.T(lang, i18n.MsgMeFooter))
	var choices []messenger.Choice
	for _, reason := range personal.Order {
		mark := "✓ "
		if slices.Contains(muted, string(reason)) {
			mark = "✗ "
		}
		choices = append(choices, messenger.Choice{Label: mark + i18n.T(lang, reasonButtons[reason]), Data: pressMe + string(reason)})
	}
	return sb.String(), rows(choices...)
}

// gitlabHosts lists the hosts of the connected GitLab repositories.
func (c *Core) gitlabHosts() []string {
	repos, err := c.store.Repos()
	if err != nil {
		log.Printf("❌ list repositories: %v", err)
		return nil
	}
	var hosts []string
	for _, repo := range repos {
		if repo.Platform != "GitLab" {
			continue
		}
		if h := hostOf(repo.URL); h != "" && !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	slices.Sort(hosts)
	return hosts
}

// hostOf returns the lower-case host of "git.example.com",
// "https://git.example.com/" or a repository URL.
func hostOf(s string) string {
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

func platformTitle(platform string) string {
	if platform == "github" {
		return "GitHub"
	}
	return "GitLab"
}

// defaultLook is the first look of a messenger that offers a choice.
func defaultLook(m messenger.Messenger) string {
	if looks := m.Caps().Looks; len(looks) > 1 {
		return looks[0]
	}
	return ""
}
