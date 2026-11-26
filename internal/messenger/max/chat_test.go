package max

import (
	"context"
	"strings"
	"testing"

	"GitHub-GitLab-Notifier/internal/messenger"
)

func TestIsChatAdmin(t *testing.T) {
	m, f := newFake(t)
	f.answer("GET /chats/7", `{"chat_id":7,"type":"dialog","dialog_with_user":{"user_id":42}}`)
	if ok, err := m.IsChatAdmin(context.Background(), "7", "42"); !ok || err != nil {
		t.Errorf("dialog: %v %v", ok, err)
	}
	f.answer("GET /chats/-5", `{"chat_id":-5,"type":"chat","title":"Team"}`)
	for body, want := range map[string]bool{
		`{"members":[{"user_id":42,"is_admin":true}]}`: true,
		`{"members":[{"user_id":42,"is_owner":true}]}`: true,
		`{"members":[{"user_id":42}]}`:                 false,
		`{"members":[]}`:                               false,
	} {
		f.answer("GET /chats/-5/members", body)
		ok, err := m.IsChatAdmin(context.Background(), "-5", "42")
		if ok != want || err != nil {
			t.Errorf("%s: %v %v", body, ok, err)
		}
	}
	if c := f.last(t); c.Query != "user_ids=42" {
		t.Errorf("query %q", c.Query)
	}
	f.status("GET /chats/-5/members", 403, `{"code":"chat.denied","message":"not an admin"}`)
	if _, err := m.IsChatAdmin(context.Background(), "-5", "42"); err == nil {
		t.Error("403: no error")
	}
	if _, err := m.IsChatAdmin(context.Background(), "-5", ""); err == nil {
		t.Error("no user: no error")
	}
}

func TestIsMember(t *testing.T) {
	m, f := newFake(t)
	f.answer("GET /chats/-5", `{"chat_id":-5,"type":"chat"}`)
	f.answer("GET /chats/-5/members", `{"members":[]}`)
	if ok, err := m.IsMember(context.Background(), "-5", "42"); ok || err != nil {
		t.Errorf("not a member: %v %v", ok, err)
	}
	f.answer("GET /chats/-5/members", `{"members":[{"user_id":42}]}`)
	if ok, err := m.IsMember(context.Background(), "-5", "42"); !ok || err != nil {
		t.Errorf("member: %v %v", ok, err)
	}
	f.status("GET /chats/-5/members", 403, `{"code":"chat.denied"}`)
	if _, err := m.IsMember(context.Background(), "-5", "42"); messenger.Classify(err) != messenger.ClassChatGone {
		t.Errorf("403: %v", err)
	}
}

func TestChatInfoCached(t *testing.T) {
	m, f := newFake(t)
	f.answer("GET /chats/-5", `{"chat_id":-5,"type":"chat","title":"Team"}`)
	for range 2 {
		if ch, err := m.chat(context.Background(), "-5"); err != nil || ch.Title != "Team" {
			t.Fatalf("%+v %v", ch, err)
		}
	}
	if n := len(f.all()); n != 1 {
		t.Errorf("%d calls", n)
	}
}

func TestDirectChatFromDialog(t *testing.T) {
	m, _ := newFake(t)
	if _, err := m.DirectChat(context.Background(), "42"); err == nil {
		t.Error("unknown dialog: no error")
	}
	m.sawDialog("42", "7")
	if chat, err := m.DirectChat(context.Background(), "42"); chat != "7" || err != nil {
		t.Errorf("%q %v", chat, err)
	}
}

func TestDeleteCommand(t *testing.T) {
	m, f := newFake(t)
	c := messenger.Command{Ref: &message{Body: body{Mid: "mid.tok"}}}
	if err := m.DeleteCommand(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if call := f.last(t); call.Method != "DELETE" || call.Path != "/messages" || call.Query != "message_id=mid.tok" {
		t.Errorf("call %+v", call)
	}
	f.answer("DELETE /messages", `{"success":false,"code":"access.denied","message":"cannot delete"}`)
	if err := m.DeleteCommand(context.Background(), c); err == nil {
		t.Error("refused delete: no error")
	}
}

func TestNoThreads(t *testing.T) {
	m, _ := newFake(t)
	if ok, err := m.CanManageThreads(context.Background(), "-5"); ok || err != nil {
		t.Errorf("%v %v", ok, err)
	}
	if _, err := m.CreateThread(context.Background(), "-5", "x"); err == nil {
		t.Error("CreateThread: no error")
	}
	if caps := m.Caps(); caps.Threads || caps.HiddenCommands || caps.PerChatRate != 2 {
		t.Errorf("caps %+v", caps)
	}
}

func TestHealthAsksMe(t *testing.T) {
	m, f := newFake(t)
	f.status("GET /me", 401, `{"code":"verify.token"}`)
	if err := m.Health(context.Background()); err == nil || strings.Contains(err.Error(), testToken) {
		t.Errorf("health %v", err)
	}
	f.answer("GET /me", `{"user_id":1,"username":"notifier_bot"}`)
	m.probe = messenger.Probe{} // the next check is due
	if err := m.Health(context.Background()); err != nil {
		t.Errorf("health %v", err)
	}
}
