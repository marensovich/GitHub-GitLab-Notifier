// Package web serves the local, read-only admin panel.
//
// The panel is not tied to any messenger and is meant to be reached only
// from the server itself or through an SSH tunnel:
//
//	ssh -L 8081:127.0.0.1:8081 user@server   →   http://localhost:8081
//
// It therefore refuses to listen on a non-loopback address unless
// AllowRemote is set, and rejects requests whose Host header is not a
// loopback name, which blocks DNS rebinding from a page the admin happens
// to have open in the browser.
package web

import (
	"GitHub-GitLab-Notifier/storage"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Source is the read-only view of the storage the panel needs.
type Source interface {
	Destinations() ([]storage.Destination, error)
	Repos() ([]storage.Repo, error)
	Threads(destID int64) ([]storage.Thread, error)
	Routes(destID int64) (map[string]string, error)
	SubscriptionsOf(destID int64) ([]storage.Repo, error)
	OutboxCounts() (pending, dead int, err error)
	DeadItems(limit int) ([]storage.OutboxItem, error)
	UnconnectedRepos(since time.Time) ([]storage.RepoCount, error)
	Links() ([]storage.Recipient, error)
}

// unconnectedWindow is how far back the overview looks for webhooks of
// repositories nobody subscribed to.
const unconnectedWindow = 7 * 24 * time.Hour

// deadShown is how many messages given up on the overview lists.
const deadShown = 20

// Config holds the panel settings.
type Config struct {
	// Addr is the listen address, e.g. "127.0.0.1:8081".
	Addr string
	// AllowRemote permits a non-loopback Addr and any Host header.
	// Only for setups that put their own authentication in front.
	AllowRemote bool
	// GlobalSecretSet tells whether WEBHOOK_SECRET is configured,
	// so repositories without their own secret are not reported as unprotected.
	GlobalSecretSet bool
	// Version and StartedAt are shown on the overview page.
	Version   string
	StartedAt time.Time
	// RichAvailable reports whether rich messages can be sent right now
	// (rich_messages is on and the Bot API server supports them); nil
	// means they can.
	RichAvailable func() bool
	// Secrets are masked in every error text the panel shows.
	Secrets []string
}

// Server is the panel HTTP server.
type Server struct {
	cfg   Config
	src   Source
	pages map[string]*template.Template
	http  *http.Server
}

// ErrRemoteAddr is returned by New for a non-loopback address without AllowRemote.
var ErrRemoteAddr = errors.New("web panel must listen on a loopback address (127.0.0.1, ::1 or localhost); set PANEL_ALLOW_REMOTE=true to override")

// New validates the config and prepares the panel.
func New(cfg Config, src Source) (*Server, error) {
	if !cfg.AllowRemote && !isLoopbackAddr(cfg.Addr) {
		return nil, fmt.Errorf("%w (got %q)", ErrRemoteAddr, cfg.Addr)
	}
	if cfg.StartedAt.IsZero() {
		cfg.StartedAt = time.Now()
	}

	pages := map[string]*template.Template{}
	for _, name := range []string{"overview", "repos", "chats", "people", "notfound"} {
		t, err := template.New(name).Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		pages[name] = t
	}

	s := &Server{cfg: cfg, src: src, pages: pages}
	s.http = &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	return s, nil
}

// Handler returns the panel routes wrapped in the security middleware.
func (s *Server) Handler() http.Handler {
	static, _ := fs.Sub(staticFS, "static")

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /{$}", s.handleOverview)
	mux.HandleFunc("GET /repos", s.handleRepos)
	mux.HandleFunc("GET /chats", s.handleChats)
	mux.HandleFunc("GET /people", s.handlePeople)
	mux.HandleFunc("GET /", s.handleNotFound)
	return s.secure(mux)
}

// ListenAndServe starts the panel. It returns nil after Shutdown.
func (s *Server) ListenAndServe() error {
	if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown gracefully stops the panel.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// secure adds the security headers, allows only GET/HEAD
// and rejects non-loopback Host headers (DNS rebinding).
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")

		if !s.cfg.AllowRemote && !isLoopbackHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			h.Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ─── Pages ───

type nav struct {
	Active  string
	Version string
}

type repoRow struct {
	URL      string
	Platform string
	Chats    []string // "messenger chat_id" of every subscribed chat
	Secret   string   // "repo", "global" or "none"
}

type chatRow struct {
	Messenger string
	ChatID    string
	Title     string
	Inactive  bool
	Language  string
	Format    string
	RichOff   bool // the chat chose rich, but it gets classic messages
	UseTopics bool
	Repos     []string
	Topics    []topicRow
	Err       string
}

// chatGroup is the chats of one messenger.
type chatGroup struct {
	Name  string
	Chats []chatRow
}

// deadRow is a message given up on.
type deadRow struct {
	Chat     string
	Event    string
	Attempts int
	Error    string
}

type topicRow struct {
	ID     string
	Name   string
	Events []string
}

// snapshot is everything the pages show, read once per request.
type snapshot struct {
	dests  []storage.Destination
	repos  []storage.Repo
	subs   map[int64][]storage.Repo // by destination
	chats  map[int64][]string       // subscribed chats by repository
	errors []error
}

func (s *Server) load() snapshot {
	var snap snapshot
	var err error
	snap.dests, err = s.src.Destinations()
	snap.errors = append(snap.errors, err)
	snap.repos, err = s.src.Repos()
	snap.errors = append(snap.errors, err)
	snap.subs = map[int64][]storage.Repo{}
	snap.chats = map[int64][]string{}
	for _, d := range snap.dests {
		repos, err := s.src.SubscriptionsOf(d.ID)
		snap.errors = append(snap.errors, err)
		snap.subs[d.ID] = repos
		for _, r := range repos {
			snap.chats[r.ID] = append(snap.chats[r.ID], d.Messenger+" "+d.ChatID)
		}
	}
	return snap
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	snap := s.load()
	repos := s.rows(snap)

	byPlatform := map[string]int{}
	unprotected, orphans := 0, 0
	for _, r := range repos {
		byPlatform[r.Platform]++
		if r.Secret == "none" {
			unprotected++
		}
		if len(r.Chats) == 0 {
			orphans++
		}
	}
	idle := 0
	labels := map[int64]string{}
	for _, d := range snap.dests {
		if len(snap.subs[d.ID]) == 0 {
			idle++
		}
		labels[d.ID] = d.Messenger + " " + d.ChatID
	}

	errs := snap.errors
	pending, dead, err := s.src.OutboxCounts()
	errs = append(errs, err)
	items, err := s.src.DeadItems(deadShown)
	errs = append(errs, err)
	deadRows := make([]deadRow, 0, len(items))
	for _, it := range items {
		chat := labels[it.DestinationID]
		if chat == "" {
			chat = "deleted chat"
		}
		deadRows = append(deadRows, deadRow{Chat: chat, Event: it.EventType, Attempts: it.Attempts, Error: s.mask(it.LastError)})
	}
	unconnected, err := s.src.UnconnectedRepos(time.Now().Add(-unconnectedWindow))
	errs = append(errs, err)

	s.render(w, http.StatusOK, "overview", map[string]any{
		"Nav":         nav{Active: "overview", Version: s.cfg.Version},
		"Version":     s.cfg.Version,
		"Uptime":      time.Since(s.cfg.StartedAt).Truncate(time.Second),
		"StartedAt":   s.cfg.StartedAt,
		"Chats":       len(snap.dests),
		"Repos":       len(repos),
		"ByPlatform":  sortedCounts(byPlatform),
		"Unprotected": unprotected,
		"IdleChats":   idle,
		"Orphans":     orphans,
		"Pending":     pending,
		"Dead":        dead,
		"DeadItems":   deadRows,
		"Unconnected": unconnected,
		"Attention":   unprotected > 0 || orphans > 0 || len(unconnected) > 0 || dead > 0,
		"Err":         s.maskErr(errors.Join(errs...)),
	})
}

func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) {
	snap := s.load()
	s.render(w, http.StatusOK, "repos", map[string]any{
		"Nav":   nav{Active: "repos", Version: s.cfg.Version},
		"Repos": s.rows(snap),
		"Err":   s.maskErr(errors.Join(snap.errors...)),
	})
}

func (s *Server) handleChats(w http.ResponseWriter, r *http.Request) {
	snap := s.load()
	richOff := s.cfg.RichAvailable != nil && !s.cfg.RichAvailable()
	rows := make([]chatRow, 0, len(snap.dests))
	for _, d := range snap.dests {
		format := d.Format
		if format == "" && d.Messenger == "telegram" {
			format = storage.FormatRich
		}
		row := chatRow{Messenger: d.Messenger, ChatID: d.ChatID, Title: d.Title, Inactive: d.Inactive,
			Language: d.Language, Format: format, RichOff: richOff && format == storage.FormatRich, UseTopics: d.UseThreads}
		for _, repo := range snap.subs[d.ID] {
			row.Repos = append(row.Repos, repo.URL)
		}
		sort.Strings(row.Repos)

		threads, err := s.src.Threads(d.ID)
		routes, routeErr := s.src.Routes(d.ID)
		if err = errors.Join(err, routeErr); err != nil {
			row.Err = s.mask(err.Error())
		}
		events := map[string][]string{}
		for event, thread := range routes {
			events[thread] = append(events[thread], event)
		}
		for _, t := range threads {
			ev := events[t.ThreadID]
			sort.Strings(ev)
			row.Topics = append(row.Topics, topicRow{ID: t.ThreadID, Name: t.Name, Events: ev})
		}
		sort.Slice(row.Topics, func(i, j int) bool { return lessID(row.Topics[i].ID, row.Topics[j].ID) })
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Messenger != rows[j].Messenger {
			return rows[i].Messenger < rows[j].Messenger
		}
		return lessID(rows[i].ChatID, rows[j].ChatID)
	})

	var groups []chatGroup
	for _, row := range rows {
		if len(groups) == 0 || groups[len(groups)-1].Name != row.Messenger {
			groups = append(groups, chatGroup{Name: row.Messenger})
		}
		g := &groups[len(groups)-1]
		g.Chats = append(g.Chats, row)
	}

	s.render(w, http.StatusOK, "chats", map[string]any{
		"Nav":     nav{Active: "chats", Version: s.cfg.Version},
		"Groups":  groups,
		"RichOff": richOff,
		"Err":     s.maskErr(errors.Join(snap.errors...)),
	})
}

// handlePeople lists the linked accounts. A Recipient holds no token: none
// is ever stored.
func (s *Server) handlePeople(w http.ResponseWriter, r *http.Request) {
	links, err := s.src.Links()
	s.render(w, http.StatusOK, "people", map[string]any{
		"Nav":   nav{Active: "people", Version: s.cfg.Version},
		"Links": links,
		"Err":   s.maskErr(err),
	})
}

// mask hides the configured secrets in a text shown on a page.
func (s *Server) mask(text string) string {
	for _, secret := range s.cfg.Secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "***")
		}
	}
	return text
}

// maskErr returns the masked text of err, or "" for nil.
func (s *Server) maskErr(err error) string {
	if err == nil {
		return ""
	}
	return s.mask(err.Error())
}

// lessID orders numeric IDs by value and anything else as text.
func lessID(a, b string) bool {
	x, errA := strconv.ParseInt(a, 10, 64)
	y, errB := strconv.ParseInt(b, 10, 64)
	if errA == nil && errB == nil {
		return x < y
	}
	return a < b
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusNotFound, "notfound", map[string]any{
		"Nav": nav{Version: s.cfg.Version},
	})
}

// rows lists repositories with their chats and reports how each is
// protected. The secret itself never leaves this function.
func (s *Server) rows(snap snapshot) []repoRow {
	out := make([]repoRow, 0, len(snap.repos))
	for _, r := range snap.repos {
		secret := "none"
		switch {
		case r.Secret != "":
			secret = "repo"
		case s.cfg.GlobalSecretSet:
			secret = "global"
		}
		chats := append([]string(nil), snap.chats[r.ID]...)
		sort.Strings(chats)
		out = append(out, repoRow{URL: r.URL, Platform: r.Platform, Chats: chats, Secret: secret})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URL < out[j].URL })
	return out
}

func (s *Server) render(w http.ResponseWriter, status int, page string, data any) {
	var buf strings.Builder
	if err := s.pages[page].ExecuteTemplate(&buf, "layout", data); err != nil {
		log.Printf("❌ web panel: render %s: %v", page, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(buf.String()))
}

// ─── Helpers ───

type count struct {
	Name  string
	Count int
}

func sortedCounts(m map[string]int) []count {
	out := make([]count, 0, len(m))
	for k, v := range m {
		out = append(out, count{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

var funcs = template.FuncMap{
	"safeURL": func(raw string) template.URL {
		// Repository URLs come from webhook payloads and /connect; only link real http(s) URLs.
		if strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://") {
			return template.URL(raw)
		}
		return ""
	},
}

// isLoopbackAddr reports whether a listen address binds only to loopback.
// An empty host (":8081") binds every interface and is not loopback.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return isLoopbackName(host)
}

// isLoopbackHost reports whether a request Host header names loopback.
func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	return isLoopbackName(strings.Trim(host, "[]"))
}

func isLoopbackName(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
