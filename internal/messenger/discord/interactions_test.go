package discord

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
)

// apiCall is one request the fake Discord API received.
type apiCall struct {
	Method, Path string
	Body         map[string]any
}

// fakeAPI is a Discord REST API that records requests.
type fakeAPI struct {
	mu    sync.Mutex
	calls []apiCall
	srv   *httptest.Server
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
			var a []any
			_ = json.Unmarshal(raw, &a)
			body = map[string]any{"__array": a}
		} else {
			_ = json.Unmarshal(raw, &body)
		}
		f.mu.Lock()
		f.calls = append(f.calls, apiCall{r.Method, r.URL.Path, body})
		f.mu.Unlock()
		switch r.URL.Path {
		case "/channels/20":
			_, _ = io.WriteString(w, `{"id":"20","type":15,"name":"forum"}`)
		case "/channels/30":
			_, _ = io.WriteString(w, `{"id":"30","type":0,"name":"text"}`)
		default:
			_, _ = io.WriteString(w, `{"id":"m1"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) all() []apiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeAPI) count(method, path string) int {
	n := 0
	for _, c := range f.all() {
		if c.Method == method && c.Path == path {
			n++
		}
	}
	return n
}

// recorder is a messenger.Handler that passes what it gets to channels.
type recorder struct {
	commands chan messenger.Command
	presses  chan messenger.Press
	block    chan struct{}
}

func newRecorder() *recorder {
	return &recorder{commands: make(chan messenger.Command, 10), presses: make(chan messenger.Press, 10)}
}

func (r *recorder) HandleCommand(ctx context.Context, c messenger.Command) {
	if r.block != nil {
		<-r.block
	}
	r.commands <- c
}

func (r *recorder) HandlePress(ctx context.Context, p messenger.Press) { r.presses <- p }

func (r *recorder) Commands(lang i18n.Lang) []messenger.CommandInfo {
	return []messenger.CommandInfo{
		{Name: "status", Description: string(lang) + " status"},
		{Name: "connect", Description: string(lang) + " connect"},
		{Name: "disconnect", Description: string(lang) + " disconnect"},
		{Name: "lang", Description: string(lang) + " lang"},
	}
}

type env struct {
	d    *Discord
	api  *fakeAPI
	h    *recorder
	priv ed25519.PrivateKey
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	api := newFakeAPI(t)
	d := newWithClient(newClient(api.srv.URL, testToken, "test"), "app1", pub, i18n.EN)
	e := &env{d: d, api: api, h: newRecorder(), priv: priv}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = d.Run(ctx, e.h) }()
	deadline := time.Now().Add(2 * time.Second)
	for d.handler() == nil {
		if time.Now().After(deadline) {
			t.Fatal("Run did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return e
}

// post sends a signed (or unsigned) interaction to the endpoint.
func (e *env) post(t *testing.T, body string, signed bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/discord/interactions", strings.NewReader(body))
	if signed {
		ts := "1700000000"
		req.Header.Set("X-Signature-Timestamp", ts)
		req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(e.priv, []byte(ts+body))))
	}
	w := httptest.NewRecorder()
	e.d.ServeHTTP(w, req)
	return w
}

func command(name, channel string, extra string) string {
	return `{"type":2,"id":"i1","token":"tok1","application_id":"app1","guild_id":"1",` +
		`"channel_id":"` + channel + `","channel":{"id":"` + channel + `","type":0,"name":"general"},` +
		`"member":{"user":{"id":"u1"},"permissions":"0"},"app_permissions":"0",` +
		`"data":{"name":"` + name + `"` + extra + `}}`
}

func gotCommand(t *testing.T, h *recorder) messenger.Command {
	t.Helper()
	select {
	case c := <-h.commands:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("no command")
	}
	return messenger.Command{}
}

func TestPing(t *testing.T) {
	e := newEnv(t)
	if w := e.post(t, `{"type":1}`, true); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"type":1}` {
		t.Errorf("ping: %d %s", w.Code, w.Body)
	}
	if w := e.post(t, command("status", "10", ""), false); w.Code != 401 {
		t.Errorf("unsigned: %d", w.Code)
	}
	select {
	case c := <-e.h.commands:
		t.Errorf("unsigned command handled: %+v", c)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestCommandDeferredThenHandled(t *testing.T) {
	e := newEnv(t)
	e.h.block = make(chan struct{})
	w := e.post(t, command("status", "10", ""), true)
	if strings.TrimSpace(w.Body.String()) != `{"type":5,"data":{"flags":64}}` {
		t.Fatalf("response %s", w.Body)
	}
	close(e.h.block)
	c := gotCommand(t, e.h)
	if c.Messenger != "discord" || c.ChatID != "10" || c.UserID != "u1" || c.ChatTitle != "#general" || c.Name != "status" || c.IsPrivate || c.IsForum || c.ThreadID != "" {
		t.Errorf("command %+v", c)
	}
}

func TestRepliesAreEphemeral(t *testing.T) {
	e := newEnv(t)
	for _, name := range []string{"status", "connect", "configure", "help"} {
		if w := e.post(t, command(name, "10", ""), true); strings.TrimSpace(w.Body.String()) != `{"type":5,"data":{"flags":64}}` {
			t.Errorf("%s: %s", name, w.Body)
		}
		gotCommand(t, e.h)
	}
	e.post(t, command("connect", "10", `,"options":[{"name":"secret","type":3,"value":"s3"},{"name":"url","type":3,"value":"https://x/y"}]`), true)
	if c := gotCommand(t, e.h); !slices.Equal(c.Args, []string{"https://x/y", "s3"}) {
		t.Errorf("args %q", c.Args)
	}
}

func TestCommandInThreadActsOnParent(t *testing.T) {
	e := newEnv(t)
	thread := func(id, parent string) string {
		return `{"type":2,"token":"tok","guild_id":"1","channel_id":"` + id + `",` +
			`"channel":{"id":"` + id + `","type":11,"name":"post","parent_id":"` + parent + `"},` +
			`"member":{"user":{"id":"u1"},"permissions":"0"},"data":{"name":"status"}}`
	}
	e.post(t, thread("21", "20"), true)
	if c := gotCommand(t, e.h); c.ChatID != "20" || c.ThreadID != "21" || !c.IsForum || c.ChatTitle != "#forum" {
		t.Errorf("forum thread: %+v", c)
	}
	e.post(t, thread("22", "20"), true)
	gotCommand(t, e.h)
	if n := e.api.count("GET", "/channels/20"); n != 1 {
		t.Errorf("parent fetched %d times", n)
	}
	e.post(t, thread("31", "30"), true)
	if c := gotCommand(t, e.h); c.ChatID != "30" || c.ThreadID != "31" || c.IsForum {
		t.Errorf("text thread: %+v", c)
	}
}

func TestDMRefused(t *testing.T) {
	e := newEnv(t)
	e.post(t, `{"type":2,"token":"tok","channel_id":"5","channel":{"id":"5","type":1},"user":{"id":"u9"},"data":{"name":"status"}}`, true)
	if c := gotCommand(t, e.h); !c.IsPrivate || c.UserID != "u9" || c.ChatID != "5" {
		t.Errorf("dm: %+v", c)
	}
}

func press(user, origin string) string {
	return `{"type":3,"token":"tokp","guild_id":"1","channel_id":"10","channel":{"id":"10","type":0,"name":"general"},` +
		`"member":{"user":{"id":"` + user + `"},"permissions":"0"},"data":{"custom_id":"cfg:look:rich","component_type":2},` +
		`"message":{"id":"m1","interaction_metadata":{"user":{"id":"` + origin + `"}}}}`
}

func TestPressDeferred(t *testing.T) {
	e := newEnv(t)
	if w := e.post(t, press("u1", "u2"), true); strings.TrimSpace(w.Body.String()) != `{"type":6}` {
		t.Fatalf("response %s", w.Body)
	}
	select {
	case p := <-e.h.presses:
		if p.Messenger != "discord" || p.ChatID != "10" || p.UserID != "u1" || p.Data != "cfg:look:rich" || p.OriginUserID != "u2" {
			t.Errorf("press %+v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no press")
	}
}

func TestReplyEditAnswer(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.post(t, command("configure", "10", ""), true)
	c := gotCommand(t, e.h)

	choices := [][]messenger.Choice{{{Label: "a", Data: "d:a"}, {Label: "b", Data: "d:b"}, {Label: "c", Data: "d:c"}, {Label: "d", Data: "d:d"}, {Label: "e", Data: "d:e"}, {Label: "f", Data: "d:f"}}}
	if _, err := e.d.Reply(ctx, c, messenger.Reply{Text: "<b>Hi</b>", Choices: choices}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.d.Reply(ctx, c, messenger.Reply{Text: "second"}); err != nil {
		t.Fatal(err)
	}
	calls := e.api.all()
	first, second := calls[len(calls)-2], calls[len(calls)-1]
	if first.Method != "PATCH" || first.Path != "/webhooks/app1/tok1/messages/@original" || first.Body["content"] != "**Hi**" {
		t.Errorf("first reply %+v", first)
	}
	rows, _ := first.Body["components"].([]any)
	if len(rows) != 2 {
		t.Fatalf("rows %v", first.Body["components"])
	}
	btn := rows[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	if len(rows[0].(map[string]any)["components"].([]any)) != 5 || btn["custom_id"] != "d:a" || btn["style"] != float64(2) {
		t.Errorf("buttons %v", rows)
	}
	if second.Method != "POST" || second.Path != "/webhooks/app1/tok1" || second.Body["flags"] != float64(64) {
		t.Errorf("second reply %+v", second)
	}

	e.post(t, press("u1", "u1"), true)
	p := <-e.h.presses
	if err := e.d.Edit(ctx, p, messenger.Reply{Text: "done"}); err != nil {
		t.Fatal(err)
	}
	last := e.api.all()[len(e.api.all())-1]
	if last.Method != "PATCH" || last.Path != "/webhooks/app1/tokp/messages/@original" || last.Body["content"] != "done" {
		t.Errorf("edit %+v", last)
	}
	if comps, ok := last.Body["components"].([]any); !ok || len(comps) != 0 {
		t.Errorf("edit components %v", last.Body["components"])
	}
	before := len(e.api.all())
	if err := e.d.Answer(ctx, p, "", false); err != nil || len(e.api.all()) != before {
		t.Errorf("empty answer sent a request: %v", err)
	}
	if err := e.d.Answer(ctx, p, "nope", true); err != nil {
		t.Fatal(err)
	}
	last = e.api.all()[len(e.api.all())-1]
	if last.Method != "POST" || last.Path != "/webhooks/app1/tokp" || last.Body["flags"] != float64(64) || last.Body["content"] != "nope" {
		t.Errorf("answer %+v", last)
	}
}

func TestIsChatAdmin(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.d.IsChatAdmin(ctx, "10", "u1"); err == nil {
		t.Error("unknown user: no error")
	}
	for perms, want := range map[string]bool{"16": true, "8": true, "32": true, "2048": false, "0": false} {
		body := strings.Replace(command("status", "10", ""), `"permissions":"0"`, `"permissions":"`+perms+`"`, 1)
		e.post(t, body, true)
		gotCommand(t, e.h)
		if got, err := e.d.IsChatAdmin(ctx, "10", "u1"); err != nil || got != want {
			t.Errorf("permissions %s: %v, %v", perms, got, err)
		}
	}
}

func TestRegisterCommands(t *testing.T) {
	e := newEnv(t)
	deadline := time.Now().Add(2 * time.Second)
	var put apiCall
	for put.Path == "" && time.Now().Before(deadline) {
		for _, c := range e.api.all() {
			if c.Method == "PUT" {
				put = c
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if put.Path != "/applications/app1/commands" {
		t.Fatalf("no PUT: %+v", e.api.all())
	}
	var cmds []struct {
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Localized   map[string]string `json:"description_localizations"`
		Contexts    []int             `json:"contexts"`
		Options     []struct {
			Name     string `json:"name"`
			Type     int    `json:"type"`
			Required bool   `json:"required"`
		} `json:"options"`
	}
	raw, _ := json.Marshal(put.Body["__array"])
	_ = json.Unmarshal(raw, &cmds)
	if len(cmds) != 4 {
		t.Fatalf("commands %s", raw)
	}
	byName := map[string]int{}
	for i, c := range cmds {
		byName[c.Name] = i
		if !strings.HasPrefix(c.Description, "en ") || c.Localized["ru"] != "ru "+c.Name || !slices.Equal(c.Contexts, []int{0}) {
			t.Errorf("command %+v", c)
		}
	}
	opts := func(name string) string {
		var s []string
		for _, o := range cmds[byName[name]].Options {
			r := ""
			if o.Required {
				r = "!"
			}
			s = append(s, o.Name+r)
		}
		return strings.Join(s, ",")
	}
	if opts("connect") != "url!,secret" || opts("disconnect") != "url!" || opts("lang") != "code" || opts("status") != "" {
		t.Errorf("options: connect %q disconnect %q lang %q status %q", opts("connect"), opts("disconnect"), opts("lang"), opts("status"))
	}
}
