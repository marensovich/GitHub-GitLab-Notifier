package discord

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
)

func TestSendEmbed(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, to := range []messenger.Target{{ChatID: "10"}, {ChatID: "20", ThreadID: "21"}} {
		attempts, err := e.d.Send(ctx, to, "", i18n.EN, sample())
		if err != nil || len(attempts) != 1 || attempts[0].Format != "embed" || attempts[0].Err != nil {
			t.Fatalf("%+v: %v %+v", to, err, attempts)
		}
		last := e.api.all()[len(e.api.all())-1]
		want := "/channels/" + to.ChatID + "/messages"
		if to.ThreadID != "" {
			want = "/channels/" + to.ThreadID + "/messages"
		}
		embeds, _ := last.Body["embeds"].([]any)
		if last.Method != "POST" || last.Path != want || len(embeds) != 1 || embeds[0].(map[string]any)["title"] != "🚀 New push · main" {
			t.Errorf("%+v: %+v", to, last)
		}
	}
}

// failing makes a Discord whose API answers every request with status and
// body.
func failing(t *testing.T, status int, body string) *Discord {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
	return newWithClient(c, "app1", nil, i18n.EN)
}

func TestSendErrors(t *testing.T) {
	chat, thread := messenger.Target{ChatID: "10"}, messenger.Target{ChatID: "10", ThreadID: "11"}
	cases := []struct {
		status int
		body   string
		to     messenger.Target
		want   messenger.Class
	}{
		{429, `{"retry_after":2.5}`, chat, messenger.ClassRateLimited},
		{404, `{"code":10003}`, thread, messenger.ClassThreadNotFound},
		{404, `{"code":10003}`, chat, messenger.ClassChatGone},
		{400, `{"code":50083}`, thread, messenger.ClassThreadNotFound},
		{403, `{"code":50001}`, chat, messenger.ClassChatGone},
		{403, `{"code":50013}`, chat, messenger.ClassChatGone},
		{403, `{"code":50013}`, thread, messenger.ClassThreadNotFound},
		{400, `{"code":220001}`, chat, messenger.ClassNeedsThread},
		{400, `{"code":50008}`, chat, messenger.ClassNeedsThread},
		{400, `{"code":50035}`, chat, messenger.ClassRejected},
		{500, `oops`, chat, messenger.ClassTemporary},
	}
	for _, tc := range cases {
		d := failing(t, tc.status, tc.body)
		attempts, err := d.Send(context.Background(), tc.to, "", i18n.EN, sample())
		if got := messenger.Classify(err); got != tc.want || len(attempts) != 1 || attempts[0].Err == nil {
			t.Errorf("%d %s %+v: %v (%v), want %v", tc.status, tc.body, tc.to, got, err, tc.want)
		}
		if strings.Contains(err.Error(), testToken) {
			t.Errorf("error shows the token: %v", err)
		}
	}
	var rl *messenger.RateLimited
	_, err := failing(t, 429, `{"retry_after":2.5}`).Send(context.Background(), chat, "", i18n.EN, sample())
	if !errors.As(err, &rl) || rl.RetryAfter != 2500*time.Millisecond {
		t.Errorf("rate limit: %v", err)
	}
}

func TestCaps(t *testing.T) {
	c := newWithClient(nil, "app1", nil, i18n.EN).Caps()
	if !c.Threads || !c.ForumOnlyThreads || c.ButtonsPerRow != 5 || c.PerChatRate != 1 || c.PerChatBurst != 5 || c.GlobalRate != 40 || len(c.Looks) != 0 {
		t.Errorf("caps %+v", c)
	}
}

func TestCreateThread(t *testing.T) {
	e := newEnv(t)
	id, err := e.d.CreateThread(context.Background(), "20", "🚀 Push")
	if err != nil || id != "m1" {
		t.Fatalf("id %q, %v", id, err)
	}
	last := e.api.all()[len(e.api.all())-1]
	msg, _ := last.Body["message"].(map[string]any)
	if last.Method != "POST" || last.Path != "/channels/20/threads" || last.Body["name"] != "🚀 Push" || msg["content"] == "" || msg["content"] == nil {
		t.Errorf("request %+v", last)
	}
}

func TestCanManageThreads(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if ok, err := e.d.CanManageThreads(ctx, "10"); ok || err != nil {
		t.Errorf("unknown channel: %v %v", ok, err)
	}
	both := uint64(1<<11 | 1<<38)
	for perms, want := range map[uint64]bool{both: true, 1 << 11: false, 1 << 38: false} {
		body := strings.Replace(command("status", "10", ""), `"app_permissions":"0"`, `"app_permissions":"`+u64(perms)+`"`, 1)
		e.post(t, body, true)
		gotCommand(t, e.h)
		if ok, err := e.d.CanManageThreads(ctx, "10"); ok != want || err != nil {
			t.Errorf("permissions %d: %v %v", perms, ok, err)
		}
	}
}

func u64(v uint64) string {
	var b []byte
	for v > 0 || len(b) == 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

func TestHealth(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.d.c.markContact(time.Now(), nil)
	if err := e.d.Health(ctx); err != nil || e.api.count("GET", "/users/@me") != 0 {
		t.Errorf("recent: %v, %d calls", err, e.api.count("GET", "/users/@me"))
	}
	e.d.c.markContact(time.Now().Add(-10*time.Minute), nil)
	if err := e.d.Health(ctx); err != nil || e.api.count("GET", "/users/@me") != 1 {
		t.Errorf("stale: %v, %d calls", err, e.api.count("GET", "/users/@me"))
	}
	d := failing(t, 500, "down")
	if err := d.Health(ctx); err == nil {
		t.Error("API down: healthy")
	}
}

func TestInviteURL(t *testing.T) {
	want := "https://discord.com/oauth2/authorize?client_id=app1&scope=bot+applications.commands&permissions=309237730304"
	if got := newWithClient(nil, "app1", nil, i18n.EN).InviteURL(); got != want || InteractionsPath != "/discord/interactions" {
		t.Errorf("invite %q, path %q", got, InteractionsPath)
	}
}

func TestErrorsHideInteractionToken(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	err := newClient(srv.URL, testToken, "test").do(context.Background(), http.MethodPost, "/webhooks/app1/SECRETTOKEN", nil, nil)
	if err == nil || strings.Contains(err.Error(), "SECRETTOKEN") {
		t.Errorf("err = %v", err)
	}
}
