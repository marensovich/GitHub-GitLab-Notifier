package commands

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"strings"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/storage"
)

// connect subscribes the chat to a repository and explains how to create
// the webhook. A repository already connected in another chat is only
// subscribed with its webhook secret or by a bot administrator, so a stranger
// who knows the URL cannot read its events.
func (c *Core) connect(r *request) {
	lang := r.lang
	if len(r.cmd.Args) < 1 {
		c.reply(r, i18n.T(lang, i18n.MsgConnectUsage), nil)
		return
	}
	repoURL, platformName, ok := parseRepoURL(r.cmd.Args[0])
	if !ok {
		c.reply(r, i18n.T(lang, i18n.MsgConnectInvalidURL), nil)
		return
	}
	hookURL := c.webhookURL()
	if hookURL == "" {
		c.reply(r, i18n.T(lang, i18n.MsgConnectNoDomain), nil)
		return
	}
	d, err := c.ensure(r)
	if err != nil {
		c.reply(r, i18n.T(lang, i18n.MsgConnectFailed), nil)
		return
	}

	repo, err := c.store.FindRepo(repoURL)
	if err != nil {
		c.failConnect(r, err)
		return
	}
	if repo == nil {
		created, err := c.store.CreateRepo(repoURL, platformName, generateSecret())
		if err != nil {
			c.failConnect(r, err)
			return
		}
		repo = &created
	} else if !c.subscribed(d.ID, repo.ID) {
		if refusal := c.mayJoin(r, repo); refusal != "" {
			c.reply(r, i18n.T(lang, refusal), nil)
			return
		}
	}
	if err := c.store.Subscribe(repo.ID, d.ID); err != nil {
		c.failConnect(r, err)
		return
	}

	c.reply(r, instructions(lang, repo, hookURL), nil)
	log.Print(i18n.Tf(c.defaultLang(), i18n.MsgConnectLogged, r.cmd.Messenger+":"+r.cmd.ChatID, repo.URL, hookURL))
}

// mayJoin reports why the chat may not subscribe to a repository connected
// elsewhere, as an i18n key, or "" when it may.
func (c *Core) mayJoin(r *request, repo *storage.Repo) string {
	if c.cfg.IsAdmin(r.cmd.Messenger, r.cmd.UserID) {
		return ""
	}
	if repo.Secret == "" {
		return i18n.MsgConnectAdminOnly
	}
	if len(r.cmd.Args) > 1 && subtle.ConstantTimeCompare([]byte(r.cmd.Args[1]), []byte(repo.Secret)) == 1 {
		return ""
	}
	return i18n.MsgConnectNeedsSecret
}

func (c *Core) subscribed(destID, repoID int64) bool {
	repos, err := c.store.SubscriptionsOf(destID)
	if err != nil {
		return false
	}
	for _, r := range repos {
		if r.ID == repoID {
			return true
		}
	}
	return false
}

func (c *Core) failConnect(r *request, err error) {
	log.Printf(i18n.T(c.defaultLang(), i18n.MsgFailedToSaveRepo), err)
	c.reply(r, i18n.T(r.lang, i18n.MsgConnectFailed), nil)
}

// webhookURL is the public webhook address, or "" when no public URL is set.
func (c *Core) webhookURL() string {
	base := strings.TrimRight(strings.TrimSpace(c.cfg.Server.PublicURL), "/")
	if base == "" {
		return ""
	}
	return base + "/webhook"
}

// instructions explains how to create the webhook of a repository.
func instructions(lang i18n.Lang, repo *storage.Repo, hookURL string) string {
	var sb strings.Builder
	sb.WriteString(i18n.Tf(lang, i18n.MsgConnectSuccess, repo.Platform, html.EscapeString(repo.URL)))
	sb.WriteString("\n\n")
	sb.WriteString(i18n.T(lang, i18n.MsgConnectURL))
	sb.WriteString(fmt.Sprintf("\n<code>%s</code>\n\n", html.EscapeString(hookURL)))
	sb.WriteString(i18n.T(lang, i18n.MsgConnectContentType))
	sb.WriteString("\n<code>application/json</code>\n\n")
	sb.WriteString(i18n.Tf(lang, i18n.MsgConnectSecret, html.EscapeString(repo.Secret)))
	sb.WriteString("\n\n")
	if repo.Platform == "GitHub" {
		sb.WriteString(i18n.T(lang, i18n.MsgConnectGitHub))
	} else {
		sb.WriteString(i18n.T(lang, i18n.MsgConnectGitLab))
	}
	sb.WriteString("\n\n")
	sb.WriteString(i18n.Tf(lang, i18n.MsgConnectTest, html.EscapeString(hookURL)))
	return sb.String()
}

// disconnect removes the chat's subscription; the repository stays while
// other chats are subscribed to it.
func (c *Core) disconnect(r *request) {
	lang := r.lang
	if len(r.cmd.Args) < 1 {
		c.reply(r, i18n.T(lang, i18n.MsgDisconnectUsage), nil)
		return
	}
	arg := html.EscapeString(r.cmd.Args[0])
	if r.dest == nil {
		c.reply(r, i18n.Tf(lang, i18n.MsgDisconnectNotFound, arg), nil)
		return
	}
	removed, err := c.store.Unsubscribe(r.dest.ID, r.cmd.Args[0])
	switch {
	case err != nil:
		c.failConnect(r, err)
	case !removed:
		c.reply(r, i18n.Tf(lang, i18n.MsgDisconnectNotFound, arg), nil)
	default:
		c.reply(r, i18n.Tf(lang, i18n.MsgDisconnectSuccess, arg), nil)
	}
}

// generateSecret creates a random secret for webhook verification.
func generateSecret() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
