// Package server implements the HTTP endpoint that receives GitHub and
// GitLab webhooks and queues their notifications in the outbox.
package server

import (
	"GitHub-GitLab-Notifier/internal/dispatcher"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/metrics"
	"GitHub-GitLab-Notifier/internal/outbox"
	"GitHub-GitLab-Notifier/internal/personal"
	"GitHub-GitLab-Notifier/internal/platform"
	"GitHub-GitLab-Notifier/storage"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// maxPayloadSize limits webhook bodies (GitHub caps payloads at 25 MB,
// but notification-relevant events are far smaller).
const maxPayloadSize = 10 << 20

// Queue is told when new messages were stored in the outbox.
type Queue interface {
	Notify()
}

// Config holds the server settings.
type Config struct {
	// Addr is the listen address, e.g. "0.0.0.0:8080".
	Addr string
	// DefaultLang is used for logs and for chats without a stored language.
	DefaultLang i18n.Lang
	// WebhookSecret, if set, is required for every webhook (GitHub signature
	// or GitLab token). Per-repository secrets from /connect are always accepted.
	WebhookSecret string
	// Version is reported by the health endpoint.
	Version string
	// DeliveriesRetention is how long the delivery log is kept (0 = forever).
	DeliveriesRetention time.Duration
	// Metrics receives counters; nil creates a private set.
	Metrics *metrics.Metrics
	// ServeMetrics exposes GET /metrics on this server. Leave it false when
	// the metrics are served on their own address.
	ServeMetrics bool
	// Checks are run by GET /healthz, by name ("db", "telegram").
	Checks map[string]Check
	// Routes are more handlers, mounted on their paths as they are (the
	// Discord interactions endpoint).
	Routes map[string]http.Handler
}

// Check reports whether a dependency works; nil means healthy.
type Check func(ctx context.Context) error

// Checks returns the health checks: "db" and one per messenger, by name.
func Checks(db Check, ms map[string]messenger.Messenger) map[string]Check {
	checks := map[string]Check{"db": db}
	for name, m := range ms {
		checks[name] = m.Health
	}
	return checks
}

// checkTimeout bounds each health check.
const checkTimeout = 2 * time.Second

// Server receives webhooks and delivers notifications.
type Server struct {
	cfg        Config
	dispatcher *dispatcher.Dispatcher
	store      *storage.Store
	queue      Queue
	http       *http.Server

	// inflight holds delivery IDs being processed right now, so two copies
	// of one delivery arriving at the same time are not both sent.
	mu       sync.Mutex
	inflight map[string]bool

	started time.Time
}

// New creates a webhook server that stores messages and wakes q.
func New(cfg Config, d *dispatcher.Dispatcher, store *storage.Store, q Queue) *Server {
	if cfg.Metrics == nil {
		cfg.Metrics = metrics.New(cfg.Version)
	}
	s := &Server{cfg: cfg, dispatcher: d, store: store, queue: q, inflight: map[string]bool{}, started: time.Now()}
	s.http = &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	return s
}

// Handler returns the HTTP routes of the server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", s.handleWebhook)
	// All three paths return the same report; /health and /webhook/health
	// are kept for existing monitors and the installer.
	mux.HandleFunc("/health", s.handleHealthz)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/webhook/health", s.handleHealthz)
	if s.cfg.ServeMetrics {
		mux.Handle("/metrics", s.cfg.Metrics.Handler())
	}
	for path, h := range s.cfg.Routes {
		mux.Handle(path, h)
	}
	return mux
}

// ListenAndServe starts the HTTP server. It returns nil after Shutdown.
func (s *Server) ListenAndServe() error {
	log.Printf(i18n.T(s.cfg.DefaultLang, i18n.MsgServerStarted), s.cfg.Addr, s.cfg.Version)
	if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown gracefully stops the server, waiting for in-flight webhooks.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// checkResult is one entry of the /healthz response.
type checkResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// handleHealthz runs every check and answers 200 when all pass, 503 otherwise.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, i18n.T(s.cfg.DefaultLang, i18n.MsgMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	resp := struct {
		Status  string                 `json:"status"`
		Version string                 `json:"version"`
		Uptime  int64                  `json:"uptime_seconds"`
		Checks  map[string]checkResult `json:"checks"`
	}{
		Status:  "ok",
		Version: s.cfg.Version,
		Uptime:  int64(time.Since(s.started).Seconds()),
		Checks:  map[string]checkResult{},
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for name, check := range s.cfg.Checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), checkTimeout)
			defer cancel()
			res := checkResult{OK: true}
			if err := check(ctx); err != nil {
				res = checkResult{Error: err.Error()}
			}
			mu.Lock()
			resp.Checks[name] = res
			mu.Unlock()
		}()
	}
	wg.Wait()

	code := http.StatusOK
	for _, c := range resp.Checks {
		if !c.OK {
			resp.Status, code = "fail", http.StatusServiceUnavailable
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	lang := s.cfg.DefaultLang
	start := time.Now()

	if r.Method != http.MethodPost {
		http.Error(w, i18n.T(lang, i18n.MsgMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPayloadSize))
	if err != nil {
		log.Printf(i18n.T(lang, i18n.MsgFailedToReadBody), err)
		http.Error(w, i18n.T(lang, i18n.MsgBadRequest), http.StatusBadRequest)
		return
	}

	p, eventName := detectPlatform(r)
	if p == "" {
		log.Println(i18n.T(lang, i18n.MsgUnknownPlatform))
		http.Error(w, i18n.T(lang, i18n.MsgUnknownPlatform), http.StatusBadRequest)
		return
	}
	log.Print(i18n.Tf(lang, i18n.MsgWebhookReceived, p, eventName))

	repoURL := platform.RepositoryURL(p, body)
	repo, err := s.store.FindRepo(repoURL)
	if err != nil {
		log.Printf(i18n.T(lang, i18n.MsgDeliveryLogFailed), err)
	}
	if repo != nil {
		repoURL = repo.URL
	}

	entry := storage.Delivery{
		DeliveryID: deliveryID(r, p),
		Platform:   string(p),
		Event:      eventName,
		RepoURL:    repoURL,
		ReceivedAt: start,
	}
	reply := func(status int, text, result, errText string) {
		entry.Status, entry.HTTPStatus, entry.Error = result, status, errText
		entry.Duration = time.Since(start)
		s.cfg.Metrics.ObserveWebhook(entry.Platform, entry.Event, result, entry.Duration)
		if err := s.store.RecordDelivery(entry); err != nil {
			log.Printf(i18n.T(lang, i18n.MsgDeliveryLogFailed), err)
		}
		if status >= http.StatusMultipleChoices {
			http.Error(w, text, status)
			return
		}
		w.WriteHeader(status)
		w.Write([]byte(text))
	}

	if !s.verify(r, p, body, repo) {
		log.Print(i18n.Tf(lang, i18n.MsgWebhookUnauthorized, p, eventName, repoURL))
		reply(http.StatusUnauthorized, "Unauthorized", storage.DeliveryRejected, "invalid or missing signature")
		return
	}

	if id := entry.DeliveryID; id != "" {
		if !s.claim(p, id) {
			log.Print(i18n.Tf(lang, i18n.MsgWebhookDuplicate, p, eventName, id))
			reply(http.StatusOK, "DUPLICATE", storage.DeliveryDuplicate, "")
			return
		}
		defer s.release(p, id)
		switch status, rowID := s.deliveryState(p, id); status {
		case storage.DeliveryQueued, storage.DeliveryDelivered:
			log.Print(i18n.Tf(lang, i18n.MsgWebhookDuplicate, p, eventName, id))
			reply(http.StatusOK, "DUPLICATE", storage.DeliveryDuplicate, "")
			return
		case storage.DeliveryFailed:
			// Redelivering a failed webhook sends its failed messages again.
			n, err := s.store.RequeueDead(rowID)
			if err != nil {
				log.Printf(i18n.T(lang, i18n.MsgDeliveryLogFailed), err)
			}
			if n > 0 {
				reply(http.StatusAccepted, "REQUEUED", storage.DeliveryDuplicate, "")
				s.queue.Notify()
				return
			}
			// Nothing left to requeue (pruned): process it as a new one.
		}
	}

	var items []storage.OutboxItem
	if repo != nil {
		items = s.items(p, eventName, body, repo)
	}
	switch {
	case items == nil && !s.dispatcher.Dispatch(lang, p, eventName, body).Skip:
		// Nobody connected this repository: its events are not sent anywhere
		// (1.x broadcast them to every chat).
		reply(http.StatusAccepted, "SKIPPED", storage.DeliveryIgnored, "not connected")
		return
	case len(items) == 0:
		reply(http.StatusOK, "SKIPPED", storage.DeliveryIgnored, "")
		return
	}

	// 202: the messages are stored and the outbox sends them. A failed send
	// is retried there, so GitLab never sees an error and disables the hook.
	entry.HTTPStatus, entry.Duration = http.StatusAccepted, time.Since(start)
	if _, err := s.store.EnqueueDelivery(entry, items); err != nil {
		log.Printf(i18n.T(lang, i18n.MsgDeliveryLogFailed), err)
		reply(http.StatusInternalServerError, "Internal Server Error", storage.DeliveryFailed, err.Error())
		return
	}
	s.cfg.Metrics.ObserveWebhook(entry.Platform, entry.Event, storage.DeliveryQueued, entry.Duration)
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("QUEUED"))
	s.queue.Notify()
}

// deliveryID returns the ID the platform gives this delivery, which stays
// the same when it is delivered again. Empty if the platform sends none.
func deliveryID(r *http.Request, p platform.Platform) string {
	switch p {
	case platform.GitHub:
		return r.Header.Get("X-GitHub-Delivery")
	case platform.GitLab:
		if id := r.Header.Get("Idempotency-Key"); id != "" {
			return id
		}
		return r.Header.Get("X-Gitlab-Event-UUID")
	}
	return ""
}

// claim marks a delivery as being processed. It returns false when another
// request with the same ID is in progress.
func (s *Server) claim(p platform.Platform, id string) bool {
	key := string(p) + "/" + id
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight[key] {
		return false
	}
	s.inflight[key] = true
	return true
}

func (s *Server) release(p platform.Platform, id string) {
	s.mu.Lock()
	delete(s.inflight, string(p)+"/"+id)
	s.mu.Unlock()
}

// deliveryState returns the logged status of a delivery ID. If the log
// cannot be read the delivery is processed: a possible duplicate is better
// than a lost notification.
func (s *Server) deliveryState(p platform.Platform, id string) (string, int64) {
	status, rowID, err := s.store.DeliveryState(string(p), id)
	if err != nil {
		log.Printf(i18n.T(s.cfg.DefaultLang, i18n.MsgDeliveryLogFailed), err)
		return "", 0
	}
	return status, rowID
}

// PruneDeliveries removes log entries older than DeliveriesRetention right
// away and then every hour, until ctx is cancelled. It does nothing when
// the retention is 0.
func (s *Server) PruneDeliveries(ctx context.Context) {
	if s.cfg.DeliveriesRetention <= 0 {
		return
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if _, err := s.store.PruneDeliveries(time.Now().Add(-s.cfg.DeliveriesRetention)); err != nil {
			log.Printf(i18n.T(s.cfg.DefaultLang, i18n.MsgDeliveryLogFailed), err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// detectPlatform returns the platform and event name from webhook headers.
func detectPlatform(r *http.Request) (platform.Platform, string) {
	if e := r.Header.Get("X-GitHub-Event"); e != "" {
		return platform.GitHub, e
	}
	if e := r.Header.Get("X-Gitlab-Event"); e != "" {
		return platform.GitLab, e
	}
	return "", ""
}

// verify checks the webhook signature (GitHub) or token (GitLab).
//
// Rules:
//   - a request carrying a signature/token must match the repository secret
//     or the global WEBHOOK_SECRET (if any of them is known);
//   - a request without a signature is rejected only when WEBHOOK_SECRET is set.
func (s *Server) verify(r *http.Request, p platform.Platform, body []byte, repo *storage.Repo) bool {
	var secrets []string
	if repo != nil && repo.Secret != "" {
		secrets = append(secrets, repo.Secret)
	}
	if s.cfg.WebhookSecret != "" {
		secrets = append(secrets, s.cfg.WebhookSecret)
	}

	var provided string
	switch p {
	case platform.GitHub:
		provided = r.Header.Get("X-Hub-Signature-256")
	case platform.GitLab:
		provided = r.Header.Get("X-Gitlab-Token")
	}

	if provided == "" {
		return s.cfg.WebhookSecret == ""
	}
	if len(secrets) == 0 {
		// A secret is configured on the GitHub/GitLab side but unknown here:
		// nothing to verify against, behave as if no secret was configured.
		return true
	}

	for _, secret := range secrets {
		if p == platform.GitHub && validGitHubSignature(secret, body, provided) {
			return true
		}
		if p == platform.GitLab && subtle.ConstantTimeCompare([]byte(secret), []byte(provided)) == 1 {
			return true
		}
	}
	return false
}

// validGitHubSignature checks an X-Hub-Signature-256 header ("sha256=<hex>").
func validGitHubSignature(secret string, body []byte, header string) bool {
	sig, ok := strings.CutPrefix(header, "sha256=")
	if !ok {
		return false
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// items formats the event for every destination subscribed to the
// repository: one outbox row each. It returns nil when nobody is
// subscribed and an empty list when the event is not sent to anyone.
func (s *Server) items(p platform.Platform, eventName string, body []byte, repo *storage.Repo) []storage.OutboxItem {
	subs, err := s.store.Subscribers(repo.ID)
	if err != nil {
		log.Printf(i18n.T(s.cfg.DefaultLang, i18n.MsgDeliveryLogFailed), err)
	}
	if len(subs) == 0 {
		return nil
	}
	results := make(map[i18n.Lang]dispatcher.Result)
	items := []storage.OutboxItem{}
	for _, d := range subs {
		lang := s.langOrDefault(d.Language)
		res, ok := results[lang]
		if !ok {
			res = s.dispatcher.Dispatch(lang, p, eventName, body)
			results[lang] = res
		}
		if res.Skip {
			continue
		}

		thread := ""
		if d.UseThreads {
			route := string(res.Topic)
			if res.Topic == platform.TopicGeneral {
				// Only a forum without a main chat has a general thread.
				route = storage.GeneralRoute
			}
			thread = s.store.ThreadFor(d.ID, route)
		}
		look := d.Format
		if look == "" && d.Messenger == "telegram" {
			look = storage.FormatRich
		}
		payload, err := json.Marshal(outbox.Payload{Lang: lang, Look: look, N: res.Notification, Topic: string(res.Topic)})
		if err != nil {
			log.Printf(i18n.T(s.cfg.DefaultLang, i18n.MsgDeliveryLogFailed), err)
			continue
		}
		items = append(items, storage.OutboxItem{
			DestinationID: d.ID, ThreadID: thread, EventType: eventName,
			Payload: payload, NextAttemptAt: time.Now(),
		})
	}
	return append(items, s.personalItems(p, eventName, body, repo, subs, results)...)
}

// reasonNotes is the note that tells a person why they got a DM.
var reasonNotes = map[personal.Reason]string{
	personal.Review:   i18n.MsgPersonalReview,
	personal.Assigned: i18n.MsgPersonalAssigned,
	personal.Mention:  i18n.MsgPersonalMention,
	personal.Pipeline: i18n.MsgPersonalPipeline,
}

// personalItems queues a DM for each linked person the event concerns: at
// most one per person, none for their own action (except their failed
// pipeline), none for a muted reason, and none to a DM that already gets
// the chat message because it is subscribed itself.
func (s *Server) personalItems(p platform.Platform, eventName string, body []byte, repo *storage.Repo,
	subs []storage.Destination, results map[i18n.Lang]dispatcher.Result) []storage.OutboxItem {
	ev := personal.Extract(p, eventName, body)
	if len(ev.Hits) == 0 {
		return nil
	}
	var ids, logins []string
	for _, h := range ev.Hits {
		if h.AccountID != "" {
			ids = append(ids, h.AccountID)
		} else {
			logins = append(logins, h.Login)
		}
	}
	recipients, err := s.store.Recipients(ev.Platform, ev.Host, ids, logins)
	if err != nil {
		log.Printf(i18n.T(s.cfg.DefaultLang, i18n.MsgDeliveryLogFailed), err)
		return nil
	}
	subscribed := map[int64]bool{}
	for _, d := range subs {
		subscribed[d.ID] = true
	}

	best := map[int64]int{} // person -> index in personal.Order
	people := map[int64]storage.Recipient{}
	var order []int64
	for _, r := range recipients {
		for _, h := range ev.Hits {
			if !hitMatches(h, r.Account) || isActor(ev, r.Account) && h.Reason != personal.Pipeline ||
				slices.Contains(r.Muted, string(h.Reason)) {
				continue
			}
			rank := slices.Index(personal.Order, h.Reason)
			if cur, seen := best[r.Person.ID]; !seen {
				order = append(order, r.Person.ID)
				best[r.Person.ID], people[r.Person.ID] = rank, r
			} else if rank < cur {
				best[r.Person.ID] = rank
			}
		}
	}

	var items []storage.OutboxItem
	for _, id := range order {
		r := people[id]
		if subscribed[r.DestinationID] {
			continue
		}
		d, err := s.store.DestinationByID(r.DestinationID)
		if err != nil || d == nil {
			continue
		}
		lang := s.langOrDefault(d.Language)
		res, ok := results[lang]
		if !ok {
			res = s.dispatcher.Dispatch(lang, p, eventName, body)
			results[lang] = res
		}
		if res.Skip && !res.PersonalOnly {
			continue
		}
		n := res.Notification
		n.Notes = append([]string{i18n.T(lang, reasonNotes[personal.Order[best[id]]])}, n.Notes...)
		look := d.Format
		if look == "" && d.Messenger == "telegram" {
			look = storage.FormatRich
		}
		pl := outbox.Payload{Lang: lang, Look: look, N: n, Topic: string(res.Topic)}
		if ev.Private {
			pl.Member = &outbox.MemberCheck{RepoID: repo.ID, UserID: r.UserID}
		}
		payload, err := json.Marshal(pl)
		if err != nil {
			log.Printf(i18n.T(s.cfg.DefaultLang, i18n.MsgDeliveryLogFailed), err)
			continue
		}
		items = append(items, storage.OutboxItem{
			DestinationID: d.ID, EventType: eventName, Payload: payload, NextAttemptAt: time.Now(),
		})
	}
	return items
}

// hitMatches tells whether a hit names the account: by ID when it has one,
// else by login in any case.
func hitMatches(h personal.Hit, a storage.Account) bool {
	if h.AccountID != "" {
		return h.AccountID == a.AccountID
	}
	return strings.EqualFold(h.Login, a.Login)
}

func isActor(ev personal.Event, a storage.Account) bool {
	if ev.ActorID != "" {
		return ev.ActorID == a.AccountID
	}
	return ev.ActorLogin != "" && strings.EqualFold(ev.ActorLogin, a.Login)
}

func (s *Server) langOrDefault(language string) i18n.Lang {
	if l := i18n.Lang(language); i18n.Valid(l) {
		return l
	}
	return s.cfg.DefaultLang
}
