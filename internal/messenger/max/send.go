package max

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/notify"
)

// button is an inline keyboard button.
type button struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	URL     string `json:"url,omitempty"`
	Payload string `json:"payload,omitempty"`
}

type attachment struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

type keyboard struct {
	Buttons [][]button `json:"buttons"`
}

type replyLink struct {
	Type string `json:"type"`
	Mid  string `json:"mid"`
}

// newMessage is a message body as MAX takes it. Attachments is nil to
// leave them as they are and empty to remove them.
type newMessage struct {
	Text        string       `json:"text"`
	Format      string       `json:"format"`
	Attachments []attachment `json:"attachments"`
	Link        *replyLink   `json:"link,omitempty"`
}

func withKeyboard(rows [][]button) []attachment {
	if len(rows) == 0 {
		return []attachment{}
	}
	return []attachment{{Type: "inline_keyboard", Payload: keyboard{Buttons: rows}}}
}

func linkRow(bs []notify.Button) [][]button {
	if len(bs) == 0 {
		return nil
	}
	row := make([]button, 0, len(bs))
	for _, b := range bs {
		row = append(row, button{Type: "link", Text: b.Label, URL: b.URL})
	}
	return [][]button{row}
}

func choiceRows(choices [][]messenger.Choice) [][]button {
	var rows [][]button
	for _, cs := range choices {
		var row []button
		for _, c := range cs {
			row = append(row, button{Type: "callback", Text: truncate(c.Label, 128), Payload: c.Data})
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
	}
	return rows
}

// post sends a message to a chat and returns it.
func (m *Max) post(ctx context.Context, chatID string, msg newMessage) (*message, error) {
	q := url.Values{"chat_id": {chatID}, "disable_link_preview": {"true"}}
	var out struct {
		Message message `json:"message"`
	}
	if err := m.c.do(ctx, http.MethodPost, "/messages", q, msg, &out); err != nil {
		return nil, classify(err)
	}
	return &out.Message, nil
}

// Send implements messenger.Messenger. MAX has no threads and one look.
func (m *Max) Send(ctx context.Context, t messenger.Target, _ string, lang i18n.Lang, n notify.Notification) ([]notify.Attempt, error) {
	r := Render(lang, n)
	var atts []attachment
	if rows := linkRow(r.Buttons); rows != nil {
		atts = withKeyboard(rows)
	}
	_, err := m.post(ctx, t.ChatID, newMessage{Text: r.Text, Format: FormatHTML, Attachments: atts})
	return []notify.Attempt{{Format: FormatHTML, Err: err}}, err
}

// Reply implements messenger.Messenger. MAX has no messages seen by one
// user only; a reply with buttons quotes the command, so a press can be
// matched to whoever sent it.
func (m *Max) Reply(ctx context.Context, c messenger.Command, r messenger.Reply) (any, error) {
	msg := newMessage{Text: r.Text, Format: FormatHTML}
	if rows := choiceRows(r.Choices); rows != nil {
		msg.Attachments = withKeyboard(rows)
		if cmd, ok := c.Ref.(*message); ok && cmd.Body.Mid != "" {
			msg.Link = &replyLink{Type: "reply", Mid: cmd.Body.Mid}
		}
	}
	return m.post(ctx, c.ChatID, msg)
}

func stateOf(p messenger.Press) (*pressState, error) {
	s, ok := p.Ref.(*pressState)
	if !ok || s == nil {
		return nil, errors.New("max: the press has no callback")
	}
	return s, nil
}

// answer posts an answer to a callback.
func (m *Max) answer(ctx context.Context, s *pressState, body any) error {
	err := m.c.do(ctx, http.MethodPost, "/answers", url.Values{"callback_id": {s.callbackID}}, body, nil)
	if err == nil {
		s.mu.Lock()
		s.answered = true
		s.mu.Unlock()
	}
	return classify(err)
}

// Edit implements messenger.Messenger: the answer to the callback replaces
// the message, keeping the command it quotes.
func (m *Max) Edit(ctx context.Context, p messenger.Press, r messenger.Reply) error {
	s, err := stateOf(p)
	if err != nil {
		return err
	}
	msg := newMessage{Text: r.Text, Format: FormatHTML, Attachments: withKeyboard(choiceRows(r.Choices))}
	if s.msg != nil && s.msg.Link != nil && s.msg.Link.Type == "reply" && s.msg.Link.Message != nil {
		msg.Link = &replyLink{Type: "reply", Mid: s.msg.Link.Message.Mid}
	}
	return m.answer(ctx, s, map[string]any{"message": msg})
}

// Answer implements messenger.Messenger: text goes to the presser as a
// one-time notification. MAX has no alert dialogs, so alert is ignored;
// an empty answer after an Edit has nothing left to do.
func (m *Max) Answer(ctx context.Context, p messenger.Press, text string, _ bool) error {
	s, err := stateOf(p)
	if err != nil {
		return err
	}
	if text == "" {
		return nil
	}
	return m.answer(ctx, s, map[string]any{"notification": text})
}
