package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
)

var _ messenger.MemberChecker = (*Discord)(nil)
var _ messenger.DirectMessenger = (*Discord)(nil)

// routed answers each request path with a fixed status and body; the
// rest get {"id":"m1"}. It records method+path and bodies.
func routed(t *testing.T, routes map[string][2]string) (*Discord, *[]string, *[]map[string]any) {
	var mu sync.Mutex
	var seen []string
	var bodies []map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		bodies = append(bodies, body)
		mu.Unlock()
		if a, ok := routes[r.URL.Path]; ok {
			if a[0] != "200" {
				code := map[string]int{"404": 404, "403": 403, "500": 500}[a[0]]
				w.WriteHeader(code)
			}
			_, _ = io.WriteString(w, a[1])
			return
		}
		_, _ = io.WriteString(w, `{"id":"m1"}`)
	})
	return newWithClient(c, "app1", nil, i18n.EN), &seen, &bodies
}

func TestIsMember(t *testing.T) {
	d, seen, _ := routed(t, map[string][2]string{
		"/channels/30":           {"200", `{"id":"30","type":0,"name":"text","guild_id":"g1"}`},
		"/guilds/g1/members/u1":  {"200", `{"user":{"id":"u1"}}`},
		"/guilds/g1/members/u2":  {"404", `{"code":10007,"message":"Unknown Member"}`},
		"/guilds/g1/members/bad": {"500", `oops`},
	})
	ctx := context.Background()
	if ok, err := d.IsMember(ctx, "30", "u1"); !ok || err != nil {
		t.Errorf("u1: %v %v", ok, err)
	}
	if ok, err := d.IsMember(ctx, "30", "u2"); ok || err != nil {
		t.Errorf("u2: %v %v", ok, err)
	}
	if _, err := d.IsMember(ctx, "30", "bad"); err == nil {
		t.Error("server error: no error")
	}
	if n := strings.Count(strings.Join(*seen, "\n"), "GET /channels/30"); n != 1 {
		t.Errorf("channel fetched %d times", n)
	}
}

func TestDirectChat(t *testing.T) {
	d, seen, bodies := routed(t, map[string][2]string{"/users/@me/channels": {"200", `{"id":"dm9","type":1}`}})
	id, err := d.DirectChat(context.Background(), "u1")
	if err != nil || id != "dm9" || (*seen)[0] != "POST /users/@me/channels" || (*bodies)[0]["recipient_id"] != "u1" {
		t.Errorf("%q %v %v %v", id, err, *seen, *bodies)
	}
	if err := d.DeleteCommand(context.Background(), messenger.Command{}); err != nil {
		t.Errorf("delete: %v", err)
	}
}

func TestClosedDMsAreChatGone(t *testing.T) {
	d := failing(t, 403, `{"code":50007,"message":"Cannot send messages to this user"}`)
	_, err := d.Send(context.Background(), messenger.Target{ChatID: "dm9"}, "", i18n.EN, sample())
	if messenger.Classify(err) != messenger.ClassChatGone {
		t.Errorf("class %v (%v)", messenger.Classify(err), err)
	}
}

type personalLister struct{}

func (personalLister) Commands(lang i18n.Lang) []messenger.CommandInfo {
	return []messenger.CommandInfo{{Name: "status", Description: "s"}, {Name: "link", Description: "l"},
		{Name: "unlink", Description: "u"}, {Name: "me", Description: "m"}}
}

func TestPersonalCommandsInDMContext(t *testing.T) {
	e := newEnv(t)
	e.d.registerCommands(context.Background(), personalLister{})
	var put apiCall
	for _, c := range e.api.all() {
		if c.Method == "PUT" {
			put = c
		}
	}
	var cmds []struct {
		Name     string `json:"name"`
		Contexts []int  `json:"contexts"`
		Options  []struct {
			Name     string `json:"name"`
			Required bool   `json:"required"`
		} `json:"options"`
	}
	raw, _ := json.Marshal(put.Body["__array"])
	_ = json.Unmarshal(raw, &cmds)
	got := map[string]string{}
	for _, c := range cmds {
		var opts []string
		for _, o := range c.Options {
			r := ""
			if o.Required {
				r = "!"
			}
			opts = append(opts, o.Name+r)
		}
		ctx := "guild"
		if slices.Equal(c.Contexts, []int{0, 1}) {
			ctx = "guild+dm"
		}
		got[c.Name] = ctx + " " + strings.Join(opts, ",")
	}
	want := map[string]string{"status": "guild ", "link": "guild+dm platform!,token!,host", "unlink": "guild+dm platform!,login", "me": "guild+dm "}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
}
