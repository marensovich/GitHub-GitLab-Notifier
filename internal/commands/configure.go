package commands

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/platform"
	"GitHub-GitLab-Notifier/storage"
)

// configurePrefix versions the callback data of the setup buttons, so a
// later change of their meaning can tell old buttons apart.
const configurePrefix = "cfg1"

// Setup steps, as they appear in callback data ("cfg1:<step>:<value>").
const (
	stepTopics = "topics"
	stepFormat = "format"
)

// parseConfigureCallback splits "cfg1:<step>:<value>" and accepts only the
// steps and values the setup offers.
func parseConfigureCallback(data string) (step, value string, ok bool) {
	parts := strings.Split(data, ":")
	if len(parts) != 3 || parts[0] != configurePrefix {
		return "", "", false
	}
	step, value = parts[1], parts[2]
	switch {
	case step == stepTopics && (value == "yes" || value == "no"):
	case step == stepFormat && (value == storage.FormatRich || value == storage.FormatClassic):
	default:
		return "", "", false
	}
	return step, value, true
}

// hasLookStep reports whether the messenger offers a look to choose.
func hasLookStep(m messenger.Messenger) bool { return len(m.Caps().Looks) > 1 }

// configure starts the chat setup: one message with buttons, edited in
// place as the user answers. A chat with threads is asked about them first,
// then about the look. Running it again shows the current values and
// changes nothing until a button is pressed.
func (c *Core) configure(r *request) {
	d, err := c.ensure(r)
	if err != nil {
		c.reply(r, i18n.T(r.lang, i18n.MsgCfgFailed), nil)
		return
	}
	switch {
	case threadsInChat(r.m, r.cmd.IsForum) && r.m.Caps().ForumOnlyThreads:
		c.configureForum(r, d)
	case threadsInChat(r.m, r.cmd.IsForum):
		text, choices := topicsStep(r.lang, d.UseThreads)
		c.reply(r, text, choices)
	case hasLookStep(r.m):
		// Nothing is stored until a button is pressed; the look press turns
		// thread routing off for a Telegram chat that is not a forum.
		text, choices := formatStep(r.lang, d.Format, "")
		c.reply(r, text, choices)
	default:
		c.reply(r, summary(r.lang, d.Format, d.UseThreads && r.m.Caps().Threads), nil)
	}
}

// HandlePress applies a press on a setup button. Presses in chats that never
// ran /configure are ignored; presses by users who may not configure the
// chat are refused with an alert and change nothing.
func (c *Core) HandlePress(ctx context.Context, p messenger.Press) {
	m := c.ms[p.Messenger]
	if m == nil {
		return
	}
	if strings.HasPrefix(p.Data, pressMe) {
		c.mePress(ctx, m, p)
		return
	}
	step, value, ok := parseConfigureCallback(p.Data)
	if !ok {
		c.answer(ctx, m, p, "", false)
		return
	}
	d, err := c.store.GetDestination(p.Messenger, p.ChatID)
	if err != nil || d == nil {
		c.answer(ctx, m, p, "", false)
		return
	}
	lang := c.langOf(d)

	if refusal := c.canConfigure(ctx, m, p); refusal != "" {
		c.answer(ctx, m, p, i18n.T(lang, refusal), true)
		return
	}

	switch step {
	case stepTopics:
		useThreads := value == "yes"
		if !useThreads && m.Caps().ForumOnlyThreads && threadsInChat(m, p.IsForum) {
			// A forum without a main chat cannot turn threads off.
			c.answer(ctx, m, p, "", false)
			return
		}
		intro := ""
		if useThreads {
			// Creating threads takes a few seconds: stop the button's
			// spinner first. Failures are shown in the message from here on.
			c.answer(ctx, m, p, "", false)
			created, skipped, err := c.createThreads(ctx, m, d, lang)
			if err != nil {
				// Keep the buttons, so the choice can be made again once
				// the cause (usually the bot's rights) is fixed.
				question, choices := topicsStep(lang, d.UseThreads)
				c.edit(ctx, m, p, err.Error()+"\n\n"+question, choices)
				return
			}
			intro = topicsResult(lang, created, skipped)
		}
		if err := c.store.SetDestinationSettings(d.ID, d.Format, useThreads); err != nil {
			log.Print(i18n.Tf(c.defaultLang(), i18n.MsgFailedToSaveChatConfig, err))
			if useThreads {
				question, choices := topicsStep(lang, d.UseThreads)
				c.edit(ctx, m, p, i18n.T(lang, i18n.MsgCfgFailed)+"\n\n"+question, choices)
			} else {
				c.answer(ctx, m, p, i18n.T(lang, i18n.MsgCfgFailed), true)
			}
			return
		}
		if hasLookStep(m) {
			text, choices := formatStep(lang, d.Format, intro)
			c.edit(ctx, m, p, text, choices)
		} else {
			text := summary(lang, d.Format, useThreads)
			if intro != "" {
				text = intro + "\n\n" + text
			}
			c.edit(ctx, m, p, text, nil)
		}
		if useThreads {
			return
		}

	case stepFormat:
		// A Telegram chat that is not a forum has no topics to route to.
		useThreads := d.UseThreads && threadsInChat(m, p.IsForum)
		if err := c.store.SetDestinationSettings(d.ID, value, useThreads); err != nil {
			log.Print(i18n.Tf(c.defaultLang(), i18n.MsgFailedToSaveChatConfig, err))
			c.answer(ctx, m, p, i18n.T(lang, i18n.MsgCfgFailed), true)
			return
		}
		c.edit(ctx, m, p, summary(lang, value, useThreads), nil)
	}
	c.answer(ctx, m, p, "", false)
}

// configureForum sets up a forum that has no main chat: there is nothing
// to choose, so its threads are created at once.
func (c *Core) configureForum(r *request, d *storage.Destination) {
	if !r.cmd.IsPrivate {
		admin, err := r.m.IsChatAdmin(r.ctx, r.cmd.ChatID, r.cmd.UserID)
		if err != nil {
			log.Print(i18n.Tf(c.defaultLang(), i18n.MsgFailedToCheckBotAdminStatus, err))
		}
		if err != nil || !admin {
			c.reply(r, i18n.T(r.lang, i18n.MsgCfgNotAdmin), nil)
			return
		}
	}
	created, skipped, err := c.createThreads(r.ctx, r.m, d, r.lang)
	if err != nil {
		c.reply(r, err.Error(), nil)
		return
	}
	if err := c.store.SetDestinationSettings(d.ID, d.Format, true); err != nil {
		log.Print(i18n.Tf(c.defaultLang(), i18n.MsgFailedToSaveChatConfig, err))
		c.reply(r, i18n.T(r.lang, i18n.MsgCfgFailed), nil)
		return
	}
	c.reply(r, topicsResult(r.lang, created, skipped)+"\n\n"+summary(r.lang, d.Format, true), nil)
}

// GeneralThreadName names the thread for events without a category, in a
// forum that has no main chat.
const GeneralThreadName = "💬 General"

// MakeThread returns the thread for an event category in a destination.
// The outbox calls it when a forum without a main chat had no thread for a
// message (failed is "") or the message's thread is gone (failed is its
// ID). A stored thread other than failed is used as it is: an earlier
// message already made it. Otherwise a new thread is created, replaces any
// stored one of the same name and gets the category's route.
func (c *Core) MakeThread(ctx context.Context, destID int64, topic, failed string) (string, error) {
	route := topic
	if route == "" {
		route = storage.GeneralRoute
	}
	if id := c.store.ThreadFor(destID, route); id != "" && id != failed {
		return id, nil
	}
	d, err := c.store.DestinationByID(destID)
	if err != nil {
		return "", err
	}
	if d == nil {
		return "", fmt.Errorf("destination %d not found", destID)
	}
	m := c.ms[d.Messenger]
	if m == nil {
		return "", fmt.Errorf("messenger %s is not running", d.Messenger)
	}
	name := GeneralThreadName
	for _, def := range platform.Topics {
		if string(def.Topic) == topic {
			name = def.Name
		}
	}
	id, err := m.CreateThread(ctx, d.ChatID, name)
	if err != nil {
		return "", err
	}
	if old, err := c.store.Threads(d.ID); err == nil {
		for _, t := range old {
			if t.Name == name && t.ThreadID != id {
				if err := c.store.DeleteThread(d.ID, t.ThreadID); err != nil {
					log.Printf("❌ %s: %v", d.Messenger, err)
				}
			}
		}
	}
	if err := c.store.SaveThread(storage.Thread{DestinationID: d.ID, ThreadID: id, Name: name}, route); err != nil {
		return "", err
	}
	log.Print(i18n.Tf(c.defaultLang(), i18n.MsgTopicCreated, name, id))
	return id, nil
}

// canConfigure reports why the user who pressed a button may not change the
// chat settings, as an i18n key, or "" when they may: a chat administrator,
// the owner of a private chat, or, when the bot cannot read the member list,
// the user who sent /configure.
func (c *Core) canConfigure(ctx context.Context, m messenger.Messenger, p messenger.Press) string {
	if p.IsPrivate {
		// Only the owner of a private chat can press its buttons.
		return ""
	}
	admin, err := m.IsChatAdmin(ctx, p.ChatID, p.UserID)
	if err == nil {
		if admin {
			return ""
		}
		return i18n.MsgCfgNotAdmin
	}
	log.Print(i18n.Tf(c.defaultLang(), i18n.MsgFailedToCheckBotAdminStatus, err))
	switch {
	case p.OriginAnonymous:
		// Sent by an anonymous administrator: nobody can be matched to
		// the command.
		return i18n.MsgCfgAnonymous
	case p.OriginUserID != "" && p.OriginUserID == p.UserID:
		return ""
	}
	return i18n.MsgCfgNotAdmin
}

func (c *Core) edit(ctx context.Context, m messenger.Messenger, p messenger.Press, text string, choices [][]messenger.Choice) {
	if err := m.Edit(ctx, p, messenger.Reply{Text: text, Choices: choices}); err != nil {
		log.Printf("❌ %s: edit in chat %s: %v", p.Messenger, p.ChatID, err)
	}
}

func (c *Core) answer(ctx context.Context, m messenger.Messenger, p messenger.Press, text string, alert bool) {
	if err := m.Answer(ctx, p, text, alert); err != nil {
		log.Printf("❌ %s: answer a button press: %v", p.Messenger, err)
	}
}

// topicsStep asks whether to route events into threads.
func topicsStep(lang i18n.Lang, useThreads bool) (string, [][]messenger.Choice) {
	return i18n.T(lang, i18n.MsgCfgTopicsQuestion), rows(
		choice(lang, i18n.MsgCfgTopicsYes, stepTopics, "yes", useThreads),
		choice(lang, i18n.MsgCfgTopicsNo, stepTopics, "no", !useThreads),
	)
}

// formatStep asks for the message look; intro (the thread creation result)
// goes above the question.
func formatStep(lang i18n.Lang, format, intro string) (string, [][]messenger.Choice) {
	text := i18n.T(lang, i18n.MsgCfgFormatQuestion)
	if intro != "" {
		text = intro + "\n\n" + text
	}
	return text, rows(
		choice(lang, i18n.MsgCfgFormatRich, stepFormat, storage.FormatRich, format != storage.FormatClassic),
		choice(lang, i18n.MsgCfgFormatClassic, stepFormat, storage.FormatClassic, format == storage.FormatClassic),
	)
}

// summary is the last edit of the setup message.
func summary(lang i18n.Lang, format string, useThreads bool) string {
	topics := i18n.T(lang, i18n.MsgCfgTopicsOff)
	if useThreads {
		topics = i18n.T(lang, i18n.MsgCfgTopicsOn)
	}
	look := i18n.T(lang, i18n.MsgCfgFormatRichShort)
	if format == storage.FormatClassic {
		look = i18n.T(lang, i18n.MsgCfgFormatClassicShort)
	}
	return i18n.Tf(lang, i18n.MsgCfgSummary, topics, look, string(lang)) +
		"\n\n" + i18n.T(lang, i18n.MsgConfigureChatHowToUse)
}

// choice is one setup button, marked "✓" when it is the current value.
func choice(lang i18n.Lang, labelKey, step, value string, current bool) messenger.Choice {
	label := i18n.T(lang, labelKey)
	if current {
		label = "✓ " + label
	}
	return messenger.Choice{Label: label, Data: configurePrefix + ":" + step + ":" + value}
}

// rows puts each button on its own row: the labels are long.
func rows(choices ...messenger.Choice) [][]messenger.Choice {
	out := make([][]messenger.Choice, 0, len(choices))
	for _, ch := range choices {
		out = append(out, []messenger.Choice{ch})
	}
	return out
}

// createThreads creates one thread per event category (see platform.Topics)
// and routes the events to them. Threads that already exist (by name) are
// reused, so it is safe to run again, e.g. after an upgrade that adds new
// categories. The returned error is a message for the chat.
func (c *Core) createThreads(ctx context.Context, m messenger.Messenger, d *storage.Destination, lang i18n.Lang) (created, skipped []string, err error) {
	logLang := c.defaultLang()
	if ok, err := m.CanManageThreads(ctx, d.ChatID); err != nil || !ok {
		if err != nil {
			log.Print(i18n.Tf(logLang, i18n.MsgFailedToCheckBotAdminStatus, err))
		}
		return nil, nil, errors.New(i18n.T(lang, i18n.MsgConfigureChatNotAdmin))
	}

	existing, err := c.store.Threads(d.ID)
	if err != nil {
		log.Print(i18n.Tf(logLang, i18n.MsgFailedToGetExistingTopics, err))
		return nil, nil, errors.New(i18n.T(lang, i18n.MsgFailedToGetTopics))
	}
	byName := make(map[string]storage.Thread, len(existing))
	for _, t := range existing {
		byName[t.Name] = t
	}

	for _, def := range platform.Topics {
		eventType := string(def.Topic)
		if t, ok := byName[def.Name]; ok {
			// Make sure the route exists even for threads created by older versions.
			if err := c.store.SaveThread(t, eventType); err != nil {
				log.Print(i18n.Tf(logLang, i18n.MsgFailedToMapEventToStorage, eventType, t.ThreadID, err))
			}
			skipped = append(skipped, def.Name)
			log.Print(i18n.Tf(logLang, i18n.MsgTopicAlreadyExists, def.Name))
			continue
		}

		id, err := m.CreateThread(ctx, d.ChatID, def.Name)
		if err != nil {
			log.Print(i18n.Tf(logLang, i18n.MsgFailedTopicCreate, def.Name, err))
			return created, skipped, errors.New(i18n.Tf(lang, i18n.MsgTopicCreateFailed, def.Name, err.Error()))
		}
		if err := c.store.SaveThread(storage.Thread{DestinationID: d.ID, ThreadID: id, Name: def.Name}, eventType); err != nil {
			log.Print(i18n.Tf(logLang, i18n.MsgFailedSaveTopicToStorage, def.Name, err))
		}
		created = append(created, fmt.Sprintf("  • %s — ID: <code>%s</code>", def.Name, id))
		log.Print(i18n.Tf(logLang, i18n.MsgTopicCreated, def.Name, id))
	}
	return created, skipped, nil
}

// topicsResult describes what createThreads did.
func topicsResult(lang i18n.Lang, created, skipped []string) string {
	var parts []string
	if len(created) > 0 {
		parts = append(parts, i18n.T(lang, i18n.MsgConfigureChatSuccess)+"\n\n"+strings.Join(created, "\n"))
	}
	if len(skipped) > 0 {
		parts = append(parts, i18n.Tf(lang, i18n.MsgConfigureChatSkipped, strings.Join(skipped, ", ")))
	}
	if len(created) == 0 && len(skipped) > 0 {
		parts = append(parts, i18n.T(lang, i18n.MsgConfigureChatAllExist))
	}
	return strings.Join(parts, "\n\n")
}
