package fake

import (
	"context"
	"errors"
	"testing"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/notify"
)

var _ messenger.Messenger = (*Messenger)(nil)

func TestFakeRecordsAndFails(t *testing.T) {
	ctx := context.Background()
	m := New("telegram", messenger.Caps{Threads: true})
	boom := errors.New("boom")
	m.Fail(func(call int, t messenger.Target) error {
		if call == 2 {
			return boom
		}
		return nil
	})

	n := notify.Notification{}
	if _, err := m.Send(ctx, messenger.Target{ChatID: "1"}, "rich", i18n.Lang("en"), n); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if _, err := m.Send(ctx, messenger.Target{ChatID: "2", ThreadID: "9"}, "", i18n.Lang("ru"), n); !errors.Is(err, boom) {
		t.Fatalf("second send: want boom, got %v", err)
	}
	if len(m.Sent) != 1 || m.Sent[0].Target.ChatID != "1" || m.Sent[0].Look != "rich" || m.Sent[0].Lang != "en" {
		t.Fatalf("recorded sends: %+v", m.Sent)
	}

	cmd := messenger.Command{Messenger: "telegram", ChatID: "1", UserID: "7", Name: "connect"}
	if _, err := m.Reply(ctx, cmd, messenger.Reply{Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	p := messenger.Press{ChatID: "1", UserID: "7", Data: "x"}
	if err := m.Edit(ctx, p, messenger.Reply{Text: "edited"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Answer(ctx, p, "ok", true); err != nil {
		t.Fatal(err)
	}
	if len(m.Replies) != 1 || m.Replies[0].Reply.Text != "hi" || len(m.Edits) != 1 || len(m.Answers) != 1 || !m.Answers[0].Alert {
		t.Fatalf("replies %+v edits %+v answers %+v", m.Replies, m.Edits, m.Answers)
	}

	m.Admins["1/7"] = true
	if ok, _ := m.IsChatAdmin(ctx, "1", "7"); !ok {
		t.Error("user 7 should be an admin of chat 1")
	}
	if ok, _ := m.IsChatAdmin(ctx, "1", "8"); ok {
		t.Error("user 8 should not be an admin")
	}

	id, err := m.CreateThread(ctx, "1", "Pushes")
	if err != nil || id == "" {
		t.Fatalf("CreateThread = %q, %v", id, err)
	}
	if len(m.Threads) != 1 || m.Threads[0].Name != "Pushes" || m.Threads[0].ThreadID != id {
		t.Fatalf("threads: %+v", m.Threads)
	}
	if m.Name() != "telegram" || !m.Caps().Threads {
		t.Error("name/caps not kept")
	}
}

func TestFakeRunStopsWithContext(t *testing.T) {
	m := New("max", messenger.Caps{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Run(ctx, nil); err != nil {
		t.Fatalf("Run after cancel: %v", err)
	}
}
