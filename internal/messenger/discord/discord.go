package discord

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"GitHub-GitLab-Notifier/internal/config"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
)

// Name is the messenger name used in the database and the config.
const Name = config.MessengerDiscord

// Permission bits Discord sends as decimal strings.
const (
	permAdministrator  = 1 << 3
	permManageChannels = 1 << 4
	permManageGuild    = 1 << 5
)

// Discord is the Discord adapter: it sends through REST and receives slash
// commands and button presses on its interactions endpoint, which is also
// its http.Handler.
type Discord struct {
	c     *client
	appID string
	key   ed25519.PublicKey
	lang  i18n.Lang
	self  string

	mu       sync.Mutex
	h        messenger.Handler
	ctx      context.Context
	perms    map[string]uint64      // "<chat>/<user>": a member's permissions
	appPerms map[string]uint64      // chat: the bot's permissions
	channels map[string]channelInfo // parents of threads
}

// channelInfo is what is kept of a channel.
type channelInfo struct {
	Type    int    `json:"type"`
	Name    string `json:"name"`
	GuildID string `json:"guild_id"`
}

var (
	_ messenger.Messenger = (*Discord)(nil)
	_ http.Handler        = (*Discord)(nil)
)

// New connects to Discord with cfg and checks the token.
func New(cfg config.Discord, lang i18n.Lang, version string) (*Discord, error) {
	key, err := parsePublicKey(cfg.PublicKey)
	if err != nil {
		return nil, err
	}
	c := newClient(APIBase, cfg.Token, version)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var me struct{ ID, Username string }
	if err := c.do(ctx, http.MethodGet, "/users/@me", nil, &me); err != nil {
		return nil, fmt.Errorf("discord: checking the token: %w", err)
	}
	appID := cfg.ApplicationID
	if appID == "" {
		appID = me.ID
	}
	d := newWithClient(c, appID, key, lang)
	d.self = me.Username
	return d, nil
}

func newWithClient(c *client, appID string, key ed25519.PublicKey, lang i18n.Lang) *Discord {
	return &Discord{
		c: c, appID: appID, key: key, lang: lang,
		perms:    map[string]uint64{},
		appPerms: map[string]uint64{},
		channels: map[string]channelInfo{},
	}
}

// Name implements messenger.Messenger.
func (d *Discord) Name() string { return Name }

// Caps implements messenger.Messenger.
func (d *Discord) Caps() messenger.Caps {
	return messenger.Caps{
		Threads:          true,
		ForumOnlyThreads: true,
		ButtonsPerRow:    maxRowButtons,
		PerChatRate:      1,
		PerChatBurst:     5,
		GlobalRate:       40,
	}
}

// Run registers the slash commands and passes interactions to h until ctx
// is done. Interactions arrive on ServeHTTP.
func (d *Discord) Run(ctx context.Context, h messenger.Handler) error {
	d.mu.Lock()
	d.h, d.ctx = h, ctx
	d.mu.Unlock()
	log.Printf(i18n.T(d.lang, i18n.MsgBotStarted), d.self)
	if l, ok := h.(messenger.CommandLister); ok {
		go d.registerCommands(ctx, l)
	}
	<-ctx.Done()
	return nil
}

// handler is the Handler given to Run, nil before it.
func (d *Discord) handler() messenger.Handler {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.h
}

// IsChatAdmin reports whether the user may manage the channel, as of the
// last interaction the user sent there.
func (d *Discord) IsChatAdmin(ctx context.Context, chatID, userID string) (bool, error) {
	d.mu.Lock()
	p, ok := d.perms[chatID+"/"+userID]
	d.mu.Unlock()
	if !ok {
		return false, fmt.Errorf("discord: no permissions known for user %s in channel %s", userID, chatID)
	}
	return p&(permAdministrator|permManageChannels|permManageGuild) != 0, nil
}

// remember keeps the permissions an interaction carries.
func (d *Discord) remember(chatID string, in *interaction) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if in.Member != nil {
		if p, err := strconv.ParseUint(in.Member.Permissions, 10, 64); err == nil {
			d.perms[chatID+"/"+in.Member.User.ID] = p
		}
	}
	if p, err := strconv.ParseUint(in.AppPermissions, 10, 64); err == nil && in.AppPermissions != "" {
		d.appPerms[chatID] = p
	}
}

// channel returns a channel's type and name, fetched once.
func (d *Discord) channel(ctx context.Context, id string) (channelInfo, error) {
	d.mu.Lock()
	ch, ok := d.channels[id]
	d.mu.Unlock()
	if ok {
		return ch, nil
	}
	if err := d.c.do(ctx, http.MethodGet, "/channels/"+id, nil, &ch); err != nil {
		return ch, err
	}
	d.mu.Lock()
	d.channels[id] = ch
	d.mu.Unlock()
	return ch, nil
}

// InteractionsPath is where Discord posts interactions; it goes after
// server.public_url in the developer portal.
const InteractionsPath = "/discord/interactions"

// invitePermissions: view channels, send messages, embed links, read
// message history, create public threads, send messages in threads.
const invitePermissions = "309237730304"

// InviteURL adds the bot and its slash commands to a server.
func (d *Discord) InviteURL() string {
	return "https://discord.com/oauth2/authorize?client_id=" + d.appID +
		"&scope=bot+applications.commands&permissions=" + invitePermissions
}
