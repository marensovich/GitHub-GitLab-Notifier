package discord

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
)

// Interaction types and responses.
const (
	interactionPing      = 1
	interactionCommand   = 2
	interactionComponent = 3

	responsePong           = 1
	responseDeferred       = 5
	responseDeferredUpdate = 6

	flagEphemeral = 1 << 6
)

// Channel types.
const (
	channelDM            = 1
	channelNewsThread    = 10
	channelPublicThread  = 11
	channelPrivateThread = 12
	channelForum         = 15
	channelMedia         = 16
)

// handleTimeout bounds the work after an interaction: its token lives 15
// minutes.
const handleTimeout = 14 * time.Minute

type user struct {
	ID string `json:"id"`
}

// interaction is the part of a Discord interaction the adapter reads.
type interaction struct {
	Type      int    `json:"type"`
	Token     string `json:"token"`
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	Channel   *struct {
		ID       string `json:"id"`
		Type     int    `json:"type"`
		Name     string `json:"name"`
		ParentID string `json:"parent_id"`
	} `json:"channel"`
	Member *struct {
		User        user   `json:"user"`
		Permissions string `json:"permissions"`
	} `json:"member"`
	User           *user  `json:"user"`
	AppPermissions string `json:"app_permissions"`
	Data           struct {
		Name    string `json:"name"`
		Options []struct {
			Name  string `json:"name"`
			Value any    `json:"value"`
		} `json:"options"`
		CustomID string `json:"custom_id"`
	} `json:"data"`
	Message *struct {
		InteractionMetadata *struct {
			User user `json:"user"`
		} `json:"interaction_metadata"`
	} `json:"message"`
}

func (in *interaction) userID() string {
	if in.Member != nil {
		return in.Member.User.ID
	}
	if in.User != nil {
		return in.User.ID
	}
	return ""
}

// replyState is the Ref of a Command or Press: the interaction token, and
// whether the deferred @original message has been filled yet.
type replyState struct {
	token string
	mu    sync.Mutex
	used  bool
}

// ServeHTTP is the interactions endpoint.
func (d *Discord) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	if err != nil || len(body) > 1<<20 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !verify(d.key, r.Header.Get("X-Signature-Ed25519"), r.Header.Get("X-Signature-Timestamp"), body) {
		http.Error(w, "invalid request signature", http.StatusUnauthorized)
		return
	}
	var in interaction
	if err := json.Unmarshal(body, &in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if in.Type == interactionPing {
		respond(w, response{Type: responsePong})
		return
	}
	d.mu.Lock()
	h, base := d.h, d.ctx
	d.mu.Unlock()
	if h == nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	switch in.Type {
	case interactionCommand:
		respond(w, response{Type: responseDeferred, Data: &responseData{Flags: flagEphemeral}})
	case interactionComponent:
		respond(w, response{Type: responseDeferredUpdate})
	default:
		http.Error(w, "unsupported interaction", http.StatusBadRequest)
		return
	}
	go d.handle(base, h, &in)
}

// response is an interaction response.
type response struct {
	Type int           `json:"type"`
	Data *responseData `json:"data,omitempty"`
}

type responseData struct {
	Flags int `json:"flags"`
}

func respond(w http.ResponseWriter, v response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// handle passes an answered interaction to the core.
func (d *Discord) handle(base context.Context, h messenger.Handler, in *interaction) {
	ctx, cancel := context.WithTimeout(base, handleTimeout)
	defer cancel()
	chatID, threadID, title, isForum := d.where(ctx, in)
	d.remember(chatID, in)
	ref := &replyState{token: in.Token}
	private := in.GuildID == ""

	if in.Type == interactionComponent {
		p := messenger.Press{
			Messenger: Name, ChatID: chatID, UserID: in.userID(), Data: in.Data.CustomID,
			IsPrivate: private, IsForum: isForum, Ref: ref,
		}
		if in.Message != nil && in.Message.InteractionMetadata != nil {
			p.OriginUserID = in.Message.InteractionMetadata.User.ID
		}
		h.HandlePress(ctx, p)
		return
	}
	h.HandleCommand(ctx, messenger.Command{
		Messenger: Name, ChatID: chatID, ThreadID: threadID, UserID: in.userID(), ChatTitle: title,
		IsPrivate: private, IsForum: isForum,
		Name: in.Data.Name, Args: args(in), Ref: ref,
	})
}

// where finds the chat an interaction belongs to. In a thread that is the
// thread's parent channel.
func (d *Discord) where(ctx context.Context, in *interaction) (chatID, threadID, title string, isForum bool) {
	ch := in.Channel
	if ch == nil {
		return in.ChannelID, "", "", false
	}
	switch ch.Type {
	case channelNewsThread, channelPublicThread, channelPrivateThread:
		if ch.ParentID == "" {
			break
		}
		parent, err := d.channel(ctx, ch.ParentID)
		if err != nil {
			log.Printf("❌ discord: channel %s: %v", ch.ParentID, err)
		}
		if parent.Name != "" {
			title = "#" + parent.Name
		}
		return ch.ParentID, ch.ID, title, parent.Type == channelForum || parent.Type == channelMedia
	case channelDM:
		return ch.ID, "", "", false
	}
	if ch.Name != "" {
		title = "#" + ch.Name
	}
	return ch.ID, "", title, false
}

// args lists a command's option values in the order the command defines
// them.
func args(in *interaction) []string {
	got := map[string]string{}
	for _, o := range in.Data.Options {
		if s, ok := o.Value.(string); ok {
			got[o.Name] = s
		}
	}
	var out []string
	for _, def := range commandOptions[in.Data.Name] {
		v, ok := got[def.Name]
		if !ok {
			break
		}
		out = append(out, v)
	}
	return out
}

// replyBody is a message sent or edited through an interaction webhook.
type replyBody struct {
	Content         string   `json:"content"`
	Components      []Row    `json:"components"`
	Flags           int      `json:"flags,omitempty"`
	AllowedMentions Mentions `json:"allowed_mentions"`
}

func newReplyBody(text string, choices [][]messenger.Choice) replyBody {
	b := replyBody{Content: toMarkdown(text), Components: []Row{}, AllowedMentions: Mentions{Parse: []string{}}}
	for _, row := range choices {
		for len(row) > 0 && len(b.Components) < maxRows {
			n := min(len(row), maxRowButtons)
			r := Row{Type: 1}
			for _, c := range row[:n] {
				r.Components = append(r.Components, Button{Type: 2, Style: 2, Label: cut(c.Label, maxLabel), CustomID: c.Data})
			}
			b.Components = append(b.Components, r)
			row = row[n:]
		}
	}
	return b
}

func stateOf(ref any) (*replyState, error) {
	st, ok := ref.(*replyState)
	if !ok || st == nil {
		return nil, errors.New("discord: no interaction to answer")
	}
	return st, nil
}

func (d *Discord) original(token string) string {
	return "/webhooks/" + d.appID + "/" + token + "/messages/@original"
}

func (d *Discord) followup(token string) string { return "/webhooks/" + d.appID + "/" + token }

// Reply answers a command. The first reply fills the deferred message;
// later ones are ephemeral follow-ups.
func (d *Discord) Reply(ctx context.Context, c messenger.Command, r messenger.Reply) (any, error) {
	st, err := stateOf(c.Ref)
	if err != nil {
		return nil, err
	}
	b := newReplyBody(r.Text, r.Choices)
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.used {
		if err := d.c.do(ctx, http.MethodPatch, d.original(st.token), b, nil); err != nil {
			return nil, err
		}
		st.used = true
		return st, nil
	}
	b.Flags = flagEphemeral
	return st, d.c.do(ctx, http.MethodPost, d.followup(st.token), b, nil)
}

// Edit replaces the message a pressed button belongs to.
func (d *Discord) Edit(ctx context.Context, p messenger.Press, r messenger.Reply) error {
	st, err := stateOf(p.Ref)
	if err != nil {
		return err
	}
	return d.c.do(ctx, http.MethodPatch, d.original(st.token), newReplyBody(r.Text, r.Choices), nil)
}

// Answer shows text to whoever pressed a button, as an ephemeral message.
// Discord has no toast or alert, so alert changes nothing.
func (d *Discord) Answer(ctx context.Context, p messenger.Press, text string, alert bool) error {
	if text == "" {
		return nil
	}
	st, err := stateOf(p.Ref)
	if err != nil {
		return err
	}
	b := newReplyBody(text, nil)
	b.Flags = flagEphemeral
	return d.c.do(ctx, http.MethodPost, d.followup(st.token), b, nil)
}

// optionDef is a slash command option (always a string).
type optionDef struct {
	Name     string
	Required bool
	EN, RU   string
}

// commandOptions are the options of the commands that take arguments, in
// the order the core reads them from Command.Args.
var commandOptions = map[string][]optionDef{
	"connect": {
		{Name: "url", Required: true, EN: "Repository URL", RU: "Ссылка на репозиторий"},
		{Name: "secret", EN: "Webhook secret, to connect a repository added elsewhere", RU: "Секрет вебхука, чтобы подключить репозиторий, добавленный в другом чате"},
	},
	"disconnect": {
		{Name: "url", Required: true, EN: "Repository URL", RU: "Ссылка на репозиторий"},
	},
	"lang": {
		{Name: "code", EN: "Language code: en or ru", RU: "Код языка: en или ru"},
	},
}

type appOption struct {
	Type        int               `json:"type"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Localized   map[string]string `json:"description_localizations,omitempty"`
	Required    bool              `json:"required,omitempty"`
}

type appCommand struct {
	Name        string            `json:"name"`
	Type        int               `json:"type"`
	Description string            `json:"description"`
	Localized   map[string]string `json:"description_localizations,omitempty"`
	Contexts    []int             `json:"contexts"`
	Options     []appOption       `json:"options,omitempty"`
}

// registerCommands publishes the slash commands, server channels only.
func (d *Discord) registerCommands(ctx context.Context, l messenger.CommandLister) {
	ru := map[string]string{}
	for _, c := range l.Commands(i18n.RU) {
		ru[c.Name] = c.Description
	}
	var cmds []appCommand
	for _, c := range l.Commands(i18n.EN) {
		ac := appCommand{Name: c.Name, Type: 1, Description: cut(c.Description, 100), Contexts: []int{0}}
		if s := ru[c.Name]; s != "" {
			ac.Localized = map[string]string{"ru": cut(s, 100)}
		}
		for _, o := range commandOptions[c.Name] {
			ac.Options = append(ac.Options, appOption{
				Type: 3, Name: o.Name, Description: cut(o.EN, 100),
				Localized: map[string]string{"ru": cut(o.RU, 100)}, Required: o.Required,
			})
		}
		cmds = append(cmds, ac)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := d.c.do(ctx, http.MethodPut, "/applications/"+d.appID+"/commands", cmds, nil); err != nil {
		log.Printf("❌ discord: registering slash commands: %v", err)
	}
}
