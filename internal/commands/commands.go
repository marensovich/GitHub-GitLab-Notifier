// Package commands is the messenger-neutral core of the chat commands:
// every adapter passes its commands and button presses here.
package commands

import (
	"context"
	"html"
	"log"
	"net/url"
	"slices"
	"strings"

	"GitHub-GitLab-Notifier/internal/config"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/storage"
)

// commandNames lists the commands of the command menu, in order. Each has
// a "cmd_<name>" translation used as its description.
var commandNames = []string{"start", "help", "configure", "connect", "disconnect", "status", "lang", "version"}

// Core handles commands and button presses for every messenger.
type Core struct {
	store   *storage.Store
	cfg     *config.Config
	version string
	ms      map[string]messenger.Messenger
}

var (
	_ messenger.Handler       = (*Core)(nil)
	_ messenger.CommandLister = (*Core)(nil)
)

// New returns the core for the given adapters, keyed by messenger name.
func New(store *storage.Store, cfg *config.Config, version string, ms map[string]messenger.Messenger) *Core {
	return &Core{store: store, cfg: cfg, version: version, ms: ms}
}

// Commands returns the command menu in lang.
func (c *Core) Commands(lang i18n.Lang) []messenger.CommandInfo {
	list := make([]messenger.CommandInfo, 0, len(commandNames))
	for _, name := range commandNames {
		list = append(list, messenger.CommandInfo{Name: name, Description: i18n.T(lang, "cmd_"+name)})
	}
	return list
}

// request is one command being handled.
type request struct {
	ctx  context.Context
	m    messenger.Messenger
	cmd  messenger.Command
	dest *storage.Destination // nil until the chat is stored
	lang i18n.Lang
}

// HandleCommand runs a command. A command typed in a thread acts on its
// chat; only the reply goes to the thread.
func (c *Core) HandleCommand(ctx context.Context, cmd messenger.Command) {
	m := c.ms[cmd.Messenger]
	if m == nil {
		return
	}
	d, err := c.store.GetDestination(cmd.Messenger, cmd.ChatID)
	if err != nil {
		log.Print(i18n.Tf(c.defaultLang(), i18n.MsgFailedToSaveChatConfig, err))
	}
	if d != nil && d.Inactive {
		// The bot is back in the chat: deliver to it again.
		if err := c.store.SetDestinationInactive(d.ID, false); err != nil {
			log.Print(i18n.Tf(c.defaultLang(), i18n.MsgFailedToSaveChatConfig, err))
		}
		d.Inactive = false
	}
	r := &request{ctx: ctx, m: m, cmd: cmd, dest: d, lang: c.langOf(d)}

	switch cmd.Name {
	case "start":
		c.reply(r, i18n.T(r.lang, i18n.MsgWelcome), nil)
	case "help":
		c.reply(r, i18n.T(r.lang, i18n.MsgHelp), nil)
	case "configure":
		c.configure(r)
	case "connect":
		c.connect(r)
	case "disconnect":
		c.disconnect(r)
	case "status":
		c.status(r)
	case "lang":
		c.setLang(r)
	case "version":
		c.reply(r, i18n.Tf(r.lang, i18n.MsgVersion, html.EscapeString(c.version)), nil)
	default:
		c.reply(r, i18n.T(r.lang, i18n.MsgUnknownCmd), nil)
	}
}

func (c *Core) reply(r *request, text string, choices [][]messenger.Choice) {
	if _, err := r.m.Reply(r.ctx, r.cmd, messenger.Reply{Text: text, Choices: choices}); err != nil {
		log.Printf("❌ %s: reply to chat %s: %v", r.cmd.Messenger, r.cmd.ChatID, err)
	}
}

func (c *Core) defaultLang() i18n.Lang {
	if l := i18n.Lang(c.cfg.Defaults.Language); i18n.Valid(l) {
		return l
	}
	return i18n.EN
}

// langOf returns the language of a destination, or the default one.
func (c *Core) langOf(d *storage.Destination) i18n.Lang {
	if d != nil {
		if l := i18n.Lang(d.Language); i18n.Valid(l) {
			return l
		}
	}
	return c.defaultLang()
}

// ensure stores the chat of the command when it is new, with the defaults
// of its messenger, and keeps its title current.
func (c *Core) ensure(r *request) (*storage.Destination, error) {
	caps := r.m.Caps()
	format := ""
	if len(caps.Looks) > 1 {
		format = caps.Looks[0]
	}
	d, err := c.store.EnsureDestination(r.cmd.Messenger, r.cmd.ChatID, storage.Destination{
		Title:      r.cmd.ChatTitle,
		Language:   string(r.lang),
		Format:     format,
		UseThreads: caps.Threads,
	})
	if err != nil {
		log.Print(i18n.Tf(c.defaultLang(), i18n.MsgFailedToSaveChatConfig, err))
		return nil, err
	}
	r.dest = &d
	return &d, nil
}

// threadsInChat reports whether events of this chat can go to threads:
// the messenger has them and, on Telegram, the chat is a forum.
func threadsInChat(m messenger.Messenger, isForum bool) bool {
	return m.Caps().Threads && isForum
}

// status shows the chat settings, threads and subscriptions.
func (c *Core) status(r *request) {
	lang := r.lang
	forum := i18n.T(lang, i18n.MsgStatusNo)
	if r.cmd.IsForum {
		forum = i18n.T(lang, i18n.MsgStatusYes)
	}
	var sb strings.Builder
	sb.WriteString(i18n.Tf(lang, i18n.MsgStatusHeader, html.EscapeString(r.cmd.ChatID), lang, forum))

	// A chat that never ran /configure has the defaults.
	caps := r.m.Caps()
	format, useThreads := "", caps.Threads
	if len(caps.Looks) > 0 {
		format = caps.Looks[0]
	}
	if d := r.dest; d != nil {
		format, useThreads = d.Format, d.UseThreads
	}
	useThreads = useThreads && threadsInChat(r.m, r.cmd.IsForum)

	look := i18n.T(lang, i18n.MsgCfgFormatClassicShort)
	if format != storage.FormatClassic {
		look = i18n.T(lang, i18n.MsgCfgFormatRichShort)
		if ra, ok := r.m.(interface{ RichAvailable() bool }); ok && !ra.RichAvailable() {
			look = i18n.Tf(lang, i18n.MsgStatusRichOff, i18n.T(lang, i18n.MsgCfgFormatClassicShort))
		}
	}
	routing := i18n.T(lang, i18n.MsgCfgTopicsOff)
	if useThreads {
		routing = i18n.T(lang, i18n.MsgCfgTopicsOn)
	}
	sb.WriteString(i18n.Tf(lang, i18n.MsgStatusSettings, look, routing))

	// With routing off the thread list would only mislead.
	if useThreads {
		lines := c.threadLines(r)
		if len(lines) > 0 {
			sb.WriteString(i18n.T(lang, i18n.MsgStatusTopicsHeader))
			sb.WriteString(strings.Join(lines, ""))
		} else {
			sb.WriteString(i18n.T(lang, i18n.MsgStatusNoTopics))
		}
	}

	var repos []storage.Repo
	if r.dest != nil {
		var err error
		if repos, err = c.store.SubscriptionsOf(r.dest.ID); err != nil {
			log.Printf("❌ subscriptions of chat %s: %v", r.cmd.ChatID, err)
		}
	}
	if len(repos) > 0 {
		sb.WriteString(i18n.Tf(lang, i18n.MsgStatusReposHeader, len(repos)))
		for _, repo := range repos {
			sb.WriteString(i18n.Tf(lang, i18n.MsgStatusRepoLine,
				html.EscapeString(repo.URL), html.EscapeString(repo.Platform)))
		}
	} else {
		sb.WriteString(i18n.T(lang, i18n.MsgStatusNoRepos))
	}

	sb.WriteString(i18n.Tf(lang, i18n.MsgStatusVersion, html.EscapeString(c.version)))
	c.reply(r, sb.String(), nil)
}

// threadLines lists each routed event type with its thread.
func (c *Core) threadLines(r *request) []string {
	if r.dest == nil {
		return nil
	}
	threads, err := c.store.Threads(r.dest.ID)
	if err != nil {
		return nil
	}
	routes, err := c.store.Routes(r.dest.ID)
	if err != nil {
		return nil
	}
	var lines []string
	for _, t := range threads {
		var events []string
		for event, id := range routes {
			if id == t.ThreadID {
				events = append(events, event)
			}
		}
		slices.Sort(events)
		for _, event := range events {
			lines = append(lines, i18n.Tf(r.lang, i18n.MsgStatusTopicLine,
				html.EscapeString(t.Name), html.EscapeString(event), html.EscapeString(t.ThreadID)))
		}
	}
	return lines
}

// setLang shows or changes the notification language of the chat.
func (c *Core) setLang(r *request) {
	current := r.lang
	if len(r.cmd.Args) < 1 {
		c.reply(r, i18n.Tf(current, i18n.MsgLangUsage, current), nil)
		return
	}
	newLang := i18n.Lang(strings.ToLower(r.cmd.Args[0]))
	if !i18n.Valid(newLang) {
		c.reply(r, i18n.Tf(current, i18n.MsgLangInvalid, html.EscapeString(r.cmd.Args[0])), nil)
		return
	}
	d, err := c.ensure(r)
	if err == nil {
		err = c.store.SetDestinationLanguage(d.ID, string(newLang))
	}
	if err != nil {
		log.Print(i18n.Tf(c.defaultLang(), i18n.MsgFailedToSaveChatConfig, err))
		c.reply(r, i18n.T(current, i18n.MsgConnectFailed), nil)
		return
	}
	c.reply(r, i18n.T(newLang, i18n.MsgLangChanged), nil)
}

// parseRepoURL validates a repository URL and detects its platform.
// Any https URL with an owner/name path is accepted, so self-hosted GitLab
// instances work too; URLs whose host contains "github" are GitHub, others GitLab.
func parseRepoURL(raw string) (repoURL, platformName string, ok bool) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", "", false
	}
	path := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	if strings.Count(path, "/") < 1 {
		return "", "", false
	}
	platformName = "GitLab"
	if strings.Contains(strings.ToLower(u.Host), "github") {
		platformName = "GitHub"
	}
	return u.Scheme + "://" + u.Host + "/" + path, platformName, true
}
