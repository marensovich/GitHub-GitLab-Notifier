package discord

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"GitHub-GitLab-Notifier/internal/messenger"
)

// codeUnknownMember: the user is not in the server.
const codeUnknownMember = 10007

// IsMember reports whether a user is in the server the channel belongs to.
func (d *Discord) IsMember(ctx context.Context, chatID, userID string) (bool, error) {
	ch, err := d.channel(ctx, chatID)
	if err != nil {
		return false, err
	}
	if ch.GuildID == "" {
		return false, fmt.Errorf("discord: channel %s is in no server", chatID)
	}
	err = d.c.do(ctx, http.MethodGet, "/guilds/"+ch.GuildID+"/members/"+userID, nil, nil)
	var ae *apiError
	if errors.As(err, &ae) && (ae.Code == codeUnknownMember || ae.Status == http.StatusNotFound) {
		return false, nil
	}
	return err == nil, err
}

// DirectChat opens (or finds) the DM channel with a user.
func (d *Discord) DirectChat(ctx context.Context, userID string) (string, error) {
	var ch struct {
		ID string `json:"id"`
	}
	if err := d.c.do(ctx, http.MethodPost, "/users/@me/channels", map[string]string{"recipient_id": userID}, &ch); err != nil {
		return "", err
	}
	if ch.ID == "" {
		return "", errors.New("discord: no DM channel in the answer")
	}
	return ch.ID, nil
}

// DeleteCommand does nothing: a slash command leaves no message, and its
// options are seen by nobody but the bot.
func (d *Discord) DeleteCommand(context.Context, messenger.Command) error { return nil }
