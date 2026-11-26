package discord

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"GitHub-GitLab-Notifier/internal/messenger"
)

// postAt sends an interaction signed with the given timestamp.
func (e *env) postAt(t *testing.T, body, ts string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/discord/interactions", strings.NewReader(body))
	req.Header.Set("X-Signature-Timestamp", ts)
	req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(e.priv, []byte(ts+body))))
	w := httptest.NewRecorder()
	e.d.ServeHTTP(w, req)
	return w
}

func TestStaleSignatureRefused(t *testing.T) {
	e := newEnv(t)
	old := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	if w := e.postAt(t, `{"type":1}`, old); w.Code != http.StatusUnauthorized {
		t.Errorf("old timestamp: %d", w.Code)
	}
	if w := e.postAt(t, `{"type":1}`, "not-a-time"); w.Code != http.StatusUnauthorized {
		t.Errorf("bad timestamp: %d", w.Code)
	}
	now := strconv.FormatInt(time.Now().Unix(), 10)
	if w := e.postAt(t, `{"type":1}`, now); w.Code != http.StatusOK {
		t.Errorf("fresh timestamp: %d", w.Code)
	}
}

type panicky struct{ recorder }

func (*panicky) HandleCommand(context.Context, messenger.Command) { panic("boom") }

func TestHandlePanicRecovered(t *testing.T) {
	d := newWithClient(newClient("http://127.0.0.1:0", testToken, "test"), "app1", nil, "en")
	in := &interaction{Type: interactionCommand, ChannelID: "10"}
	d.handle(context.Background(), &panicky{}, in)
}

// ctxHandler reports whether its context was still alive after block.
type ctxHandler struct {
	recorder
	alive chan bool
}

func (h *ctxHandler) HandleCommand(ctx context.Context, c messenger.Command) {
	<-h.block
	h.alive <- ctx.Err() == nil
}

func TestShutdownLetsInteractionsFinish(t *testing.T) {
	e := newEnv(t)
	h := &ctxHandler{recorder: *newRecorder(), alive: make(chan bool, 1)}
	h.block = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { _ = e.d.Run(ctx, h); close(stopped) }()
	deadline := time.Now().Add(2 * time.Second)
	for e.d.handler() != h {
		if time.Now().After(deadline) {
			t.Fatal("Run did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	e.post(t, command("status", "10", ""), true)
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-stopped:
		t.Fatal("Run returned while an interaction was running")
	case <-time.After(50 * time.Millisecond):
	}
	close(h.block)
	if !<-h.alive {
		t.Error("the interaction lost its context on shutdown")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return")
	}
}

func TestFailedParentLookupUsesTags(t *testing.T) {
	e := newEnv(t)
	body := `{"type":2,"token":"tok","guild_id":"1","channel_id":"41",` +
		`"channel":{"id":"41","type":11,"name":"post","parent_id":"49","applied_tags":[]},` +
		`"member":{"user":{"id":"u1"},"permissions":"0"},"data":{"name":"status"}}`
	e.post(t, body, true)
	if c := gotCommand(t, e.h); c.ChatID != "49" || c.ThreadID != "41" || !c.IsForum {
		t.Errorf("forum post with an unknown parent: %+v", c)
	}
}

func TestCachesBounded(t *testing.T) {
	d := newWithClient(newClient("http://127.0.0.1:0", testToken, "test"), "app1", nil, "en")
	for i := range cacheMax + 5 {
		in := &interaction{Member: &struct {
			User        user   `json:"user"`
			Permissions string `json:"permissions"`
		}{User: user{ID: fmt.Sprint(i)}, Permissions: "8"}, AppPermissions: "8"}
		d.remember(fmt.Sprint("c", i), in)
	}
	if len(d.perms) > cacheMax || len(d.appPerms) > cacheMax {
		t.Errorf("perms %d, app perms %d", len(d.perms), len(d.appPerms))
	}
}

func TestQuoteLineStartsEscaped(t *testing.T) {
	for in, want := range map[string]string{
		"# title":   `\# title`,
		"-# small":  `\-# small`,
		"- item":    `\- item`,
		"1. first":  `1\. first`,
		"  + plus":  `  \+ plus`,
		"plain #1":  "plain #1",
		"2024 year": "2024 year",
	} {
		if got := escLineStart(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestCutKeepsMarkdownWhole(t *testing.T) {
	long := "<b>" + strings.Repeat("y", 2500) + "</b>"
	got := toMarkdown(long)
	if runes(got) > maxContent || !strings.HasPrefix(got, "**") || !strings.HasSuffix(got, "…**") {
		t.Errorf("bold cut: %d runes, ends %q", runes(got), got[len(got)-10:])
	}
	link := strings.Repeat("z", 1990) + `<a href="https://example.com/very/long/path">link text</a>`
	got = toMarkdown(link)
	if strings.Contains(got, "[") || runes(got) > maxContent {
		t.Errorf("link cut: ends %q", got[len(got)-20:])
	}
	code := "<code>" + strings.Repeat("a*b ", 600) + "</code>"
	got = toMarkdown(code)
	if !strings.HasSuffix(got, "…`") || strings.Count(got, "`")%2 != 0 {
		t.Errorf("code cut: ends %q", got[len(got)-10:])
	}
}
