// Package server implements the HTTP endpoint that receives GitHub and
// GitLab webhooks and forwards them as Telegram notifications.
package server

import (
	"GitHub-GitLab-Notifier/internal/dispatcher"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"GitHub-GitLab-Notifier/storage"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// maxPayloadSize limits webhook bodies (GitHub caps payloads at 25 MB,
// but notification-relevant events are far smaller).
const maxPayloadSize = 10 << 20

// Sender delivers a notification to a Telegram chat (optionally to a forum topic).
type Sender interface {
	SendMessageToTopic(chatID int64, topicID int, text string) error
}

// Config holds the server settings.
type Config struct {
	// Addr is the listen address, e.g. "0.0.0.0:8080".
	Addr string
	// DefaultChatID receives notifications when no chat is configured (0 = disabled).
	DefaultChatID int64
	// DefaultLang is used for logs and for chats without a stored language.
	DefaultLang i18n.Lang
	// WebhookSecret, if set, is required for every webhook (GitHub signature
	// or GitLab token). Per-repository secrets from /connect are always accepted.
	WebhookSecret string
	// Version is reported by the health endpoint.
	Version string
	// DeliveriesRetention is how long the delivery log is kept (0 = forever).
	DeliveriesRetention time.Duration
}

// Server receives webhooks and delivers notifications.
type Server struct {
	cfg        Config
	dispatcher *dispatcher.Dispatcher
	store      *storage.Store
	sender     Sender
	http       *http.Server

	// inflight holds delivery IDs being processed right now, so two copies
	// of one delivery arriving at the same time are not both sent.
	mu       sync.Mutex
	inflight map[string]bool
}

// New creates a webhook server.
func New(cfg Config, d *dispatcher.Dispatcher, store *storage.Store, sender Sender) *Server {
	s := &Server{cfg: cfg, dispatcher: d, store: store, sender: sender, inflight: map[string]bool{}}
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
	mux.HandleFunc("/webhook/health", s.handleHealth)
	mux.HandleFunc("/health", s.handleHealth)
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

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("pong"))
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
	repo := s.store.FindRepo(repoURL)
	if repo != nil {
		repoURL = repo.RepoURL
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
		if err := s.store.RecordDelivery(entry); err != nil {
			log.Printf(i18n.T(lang, i18n.MsgDeliveryLogFailed), err)
		}
		if status != http.StatusOK {
			http.Error(w, text, status)
			return
		}
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
		if s.alreadyDelivered(p, id) {
			log.Print(i18n.Tf(lang, i18n.MsgWebhookDuplicate, p, eventName, id))
			reply(http.StatusOK, "DUPLICATE", storage.DeliveryDuplicate, "")
			return
		}
	}

	out := s.deliver(p, eventName, body, repo)
	switch {
	case out.sent > 0 && out.failed > 0:
		reply(http.StatusOK, "OK", storage.DeliveryDelivered,
			fmt.Sprintf("%d of %d sends failed: %v", out.failed, out.sent+out.failed, out.err))
	case out.sent > 0:
		reply(http.StatusOK, "OK", storage.DeliveryDelivered, "")
	case out.failed > 0:
		// Still 200: GitLab disables hooks that keep failing, and the sender
		// is not at fault. Redelivering the webhook tries again.
		reply(http.StatusOK, "SKIPPED", storage.DeliveryFailed, out.err.Error())
	default:
		reply(http.StatusOK, "SKIPPED", storage.DeliveryIgnored, "")
	}
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

// alreadyDelivered checks the log. If the log cannot be read the delivery
// is processed: a possible duplicate is better than a lost notification.
func (s *Server) alreadyDelivered(p platform.Platform, id string) bool {
	done, err := s.store.WasDelivered(string(p), id)
	if err != nil {
		log.Printf(i18n.T(s.cfg.DefaultLang, i18n.MsgDeliveryLogFailed), err)
		return false
	}
	return done
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
func (s *Server) verify(r *http.Request, p platform.Platform, body []byte, repo *storage.RepoInfo) bool {
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

// target is a chat that should receive a notification.
type target struct {
	chatID int64
	lang   i18n.Lang
}

// targets returns the chats that receive notifications for a repository:
// the chat the repository is connected to, otherwise every configured chat,
// otherwise the default CHAT_ID.
func (s *Server) targets(repo *storage.RepoInfo) []target {
	if repo != nil {
		return []target{{repo.ChatID, s.chatLang(repo.ChatID)}}
	}

	var result []target
	for _, chat := range s.store.GetAllChats() {
		result = append(result, target{chat.ChatID, s.langOrDefault(chat.Language)})
	}
	if len(result) == 0 && s.cfg.DefaultChatID != 0 {
		log.Println(i18n.T(s.cfg.DefaultLang, i18n.MsgNoChatsConfigured))
		result = append(result, target{s.cfg.DefaultChatID, s.cfg.DefaultLang})
	}
	return result
}

// outcome counts the messages of one delivery.
type outcome struct {
	sent, failed int
	err          error // last send error
}

// deliver formats the event for each target chat and sends it.
func (s *Server) deliver(p platform.Platform, eventName string, body []byte, repo *storage.RepoInfo) outcome {
	results := make(map[i18n.Lang]dispatcher.Result)
	var out outcome

	for _, t := range s.targets(repo) {
		res, ok := results[t.lang]
		if !ok {
			res = s.dispatcher.Dispatch(t.lang, p, eventName, body)
			results[t.lang] = res
		}
		if res.Skip {
			continue
		}

		topicID := 0
		if res.Topic != platform.TopicGeneral {
			topicID = s.store.GetTopicByEvent(t.chatID, string(res.Topic))
		}

		err := s.sender.SendMessageToTopic(t.chatID, topicID, res.Message)
		if err != nil && topicID != 0 {
			// The topic may have been deleted or closed: fall back to the general chat.
			err = s.sender.SendMessageToTopic(t.chatID, 0, res.Message)
		}
		if err != nil {
			out.failed++
			out.err = err
			continue
		}
		out.sent++
		log.Print(i18n.Tf(s.cfg.DefaultLang, i18n.MsgWebhookNotificationSent, t.chatID))
	}
	return out
}

func (s *Server) chatLang(chatID int64) i18n.Lang {
	chat, err := s.store.GetChat(chatID)
	if err != nil {
		return s.cfg.DefaultLang
	}
	return s.langOrDefault(chat.Language)
}

func (s *Server) langOrDefault(language string) i18n.Lang {
	if l := i18n.Lang(language); i18n.Valid(l) {
		return l
	}
	return s.cfg.DefaultLang
}
