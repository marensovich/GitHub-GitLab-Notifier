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
	GetAllChats() []storage.ChatConfig
	GetAllRepos() []storage.RepoInfo
	GetTopicsByChat(chatID int64) ([]storage.TopicInfo, error)
	GetEventMappings(chatID int64) (map[string]int, error)
}

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
	for _, name := range []string{"overview", "repos", "chats", "notfound"} {
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
	ChatID   int64
	Language string
	Secret   string // "repo", "global" or "none"
}

type chatRow struct {
	ChatID   int64
	Language string
	IsForum  bool
	Repos    []string
	Topics   []topicRow
	Err      string
}

type topicRow struct {
	ID     int
	Name   string
	Events []string
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	chats := s.src.GetAllChats()
	repos := s.rows()

	byPlatform := map[string]int{}
	unprotected := 0
	for _, r := range repos {
		byPlatform[r.Platform]++
		if r.Secret == "none" {
			unprotected++
		}
	}

	known := map[int64]bool{}
	for _, c := range chats {
		known[c.ChatID] = true
	}
	withRepos := map[int64]bool{}
	orphans := 0
	for _, r := range repos {
		withRepos[r.ChatID] = true
		if !known[r.ChatID] {
			orphans++
		}
	}
	idle := 0
	for _, c := range chats {
		if !withRepos[c.ChatID] {
			idle++
		}
	}

	s.render(w, http.StatusOK, "overview", map[string]any{
		"Nav":         nav{Active: "overview", Version: s.cfg.Version},
		"Version":     s.cfg.Version,
		"Uptime":      time.Since(s.cfg.StartedAt).Truncate(time.Second),
		"StartedAt":   s.cfg.StartedAt,
		"Chats":       len(chats),
		"Repos":       len(repos),
		"ByPlatform":  sortedCounts(byPlatform),
		"Unprotected": unprotected,
		"IdleChats":   idle,
		"Orphans":     orphans,
	})
}

func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "repos", map[string]any{
		"Nav":   nav{Active: "repos", Version: s.cfg.Version},
		"Repos": s.rows(),
	})
}

func (s *Server) handleChats(w http.ResponseWriter, r *http.Request) {
	chats := s.src.GetAllChats()
	sort.Slice(chats, func(i, j int) bool { return chats[i].ChatID < chats[j].ChatID })

	reposByChat := map[int64][]string{}
	for _, repo := range s.src.GetAllRepos() {
		reposByChat[repo.ChatID] = append(reposByChat[repo.ChatID], repo.RepoURL)
	}

	rows := make([]chatRow, 0, len(chats))
	for _, c := range chats {
		row := chatRow{ChatID: c.ChatID, Language: c.Language, IsForum: c.IsForum, Repos: reposByChat[c.ChatID]}
		sort.Strings(row.Repos)

		topics, err := s.src.GetTopicsByChat(c.ChatID)
		mappings, mapErr := s.src.GetEventMappings(c.ChatID)
		if err = errors.Join(err, mapErr); err != nil {
			row.Err = err.Error()
		}
		events := map[int][]string{}
		for event, topicID := range mappings {
			events[topicID] = append(events[topicID], event)
		}
		for _, t := range topics {
			ev := events[t.TopicID]
			sort.Strings(ev)
			row.Topics = append(row.Topics, topicRow{ID: t.TopicID, Name: t.Name, Events: ev})
		}
		sort.Slice(row.Topics, func(i, j int) bool { return row.Topics[i].ID < row.Topics[j].ID })
		rows = append(rows, row)
	}

	s.render(w, http.StatusOK, "chats", map[string]any{
		"Nav":   nav{Active: "chats", Version: s.cfg.Version},
		"Chats": rows,
	})
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusNotFound, "notfound", map[string]any{
		"Nav": nav{Version: s.cfg.Version},
	})
}

// rows joins repositories with their chat and reports how each is protected.
// The secret itself never leaves this function.
func (s *Server) rows() []repoRow {
	langs := map[int64]string{}
	for _, c := range s.src.GetAllChats() {
		langs[c.ChatID] = c.Language
	}

	repos := s.src.GetAllRepos()
	out := make([]repoRow, 0, len(repos))
	for _, r := range repos {
		secret := "none"
		switch {
		case r.Secret != "":
			secret = "repo"
		case s.cfg.GlobalSecretSet:
			secret = "global"
		}
		out = append(out, repoRow{URL: r.RepoURL, Platform: r.Platform, ChatID: r.ChatID, Language: langs[r.ChatID], Secret: secret})
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
	"chatID": func(id int64) string { return strconv.FormatInt(id, 10) },
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
