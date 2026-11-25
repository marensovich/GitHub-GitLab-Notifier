package storage

import (
	"database/sql"
	"errors"
	"fmt"
)

// Destination is a chat in one messenger that receives notifications.
type Destination struct {
	ID        int64
	Messenger string
	ChatID    string
	Title     string
	// Language is the chat's notification language ("" = the default).
	Language string
	// Format is the messenger-specific look (FormatRich/FormatClassic for
	// Telegram, "" where the messenger has only one).
	Format string
	// UseThreads routes events to threads (forum topics, forum posts).
	UseThreads bool
	// Inactive is set when the bot was removed from the chat.
	Inactive bool
}

// Thread is a thread of a destination that events are routed to.
type Thread struct {
	DestinationID int64
	ThreadID      string
	Name          string
}

// Repo is a repository that sends webhooks.
type Repo struct {
	ID       int64
	URL      string
	Platform string
	Secret   string
}

const destColumns = "id, messenger, chat_id, title, language, format, use_threads, inactive"

func scanDest(row interface{ Scan(...any) error }) (Destination, error) {
	var d Destination
	var threads, inactive int
	err := row.Scan(&d.ID, &d.Messenger, &d.ChatID, &d.Title, &d.Language, &d.Format, &threads, &inactive)
	d.UseThreads, d.Inactive = threads != 0, inactive != 0
	return d, err
}

func queryDests(db *sql.DB, query string, args ...any) ([]Destination, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Destination
	for rows.Next() {
		d, err := scanDest(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}

// EnsureDestination returns the stored destination, creating it from d when
// it does not exist. A non-empty d.Title replaces the stored title; other
// stored settings are kept.
func (s *Store) EnsureDestination(messenger, chatID string, d Destination) (Destination, error) {
	_, err := s.db.Exec(`INSERT INTO destinations (messenger, chat_id, title, language, format, use_threads)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (messenger, chat_id) DO UPDATE SET title = CASE WHEN excluded.title != '' THEN excluded.title ELSE title END`,
		messenger, chatID, d.Title, d.Language, d.Format, boolToInt(d.UseThreads))
	if err != nil {
		return Destination{}, err
	}
	got, err := s.GetDestination(messenger, chatID)
	if err != nil {
		return Destination{}, err
	}
	if got == nil {
		return Destination{}, fmt.Errorf("destination %s/%s not stored", messenger, chatID)
	}
	return *got, nil
}

// GetDestination returns a destination, or nil when it does not exist.
func (s *Store) GetDestination(messenger, chatID string) (*Destination, error) {
	return s.oneDest("SELECT "+destColumns+" FROM destinations WHERE messenger = ? AND chat_id = ?", messenger, chatID)
}

// DestinationByID returns a destination, or nil when it does not exist.
func (s *Store) DestinationByID(id int64) (*Destination, error) {
	return s.oneDest("SELECT "+destColumns+" FROM destinations WHERE id = ?", id)
}

func (s *Store) oneDest(query string, args ...any) (*Destination, error) {
	d, err := scanDest(s.db.QueryRow(query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Destinations returns every destination, by messenger and chat.
func (s *Store) Destinations() ([]Destination, error) {
	return queryDests(s.db, "SELECT "+destColumns+" FROM destinations ORDER BY messenger, chat_id")
}

// SetDestinationSettings stores the look and thread routing of a destination.
func (s *Store) SetDestinationSettings(id int64, format string, useThreads bool) error {
	if format != "" && format != FormatRich && format != FormatClassic {
		return fmt.Errorf("unknown message format %q", format)
	}
	return s.updateDest(id, "UPDATE destinations SET format = ?, use_threads = ? WHERE id = ?", format, boolToInt(useThreads), id)
}

// SetDestinationLanguage stores the notification language of a destination.
func (s *Store) SetDestinationLanguage(id int64, lang string) error {
	return s.updateDest(id, "UPDATE destinations SET language = ? WHERE id = ?", lang, id)
}

// SetDestinationInactive marks a destination the bot can no longer post to,
// or active again.
func (s *Store) SetDestinationInactive(id int64, inactive bool) error {
	return s.updateDest(id, "UPDATE destinations SET inactive = ? WHERE id = ?", boolToInt(inactive), id)
}

func (s *Store) updateDest(id int64, query string, args ...any) error {
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("destination %d not found", id)
	}
	return nil
}

// GeneralRoute is the route key of events without a category, used where
// such events need a thread of their own (a forum without a main chat).
const GeneralRoute = "general"

// SaveThread stores a thread and routes eventType to it ("" = no route).
func (s *Store) SaveThread(t Thread, eventType string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO threads (destination_id, thread_id, name) VALUES (?, ?, ?)
		ON CONFLICT (destination_id, thread_id) DO UPDATE SET name = excluded.name`,
		t.DestinationID, t.ThreadID, t.Name); err != nil {
		return err
	}
	if eventType != "" {
		if _, err := tx.Exec(`INSERT INTO routes (destination_id, event_type, thread_id) VALUES (?, ?, ?)
			ON CONFLICT (destination_id, event_type) DO UPDATE SET thread_id = excluded.thread_id`,
			t.DestinationID, eventType, t.ThreadID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Threads returns the threads of a destination.
func (s *Store) Threads(destID int64) ([]Thread, error) {
	rows, err := s.db.Query("SELECT destination_id, thread_id, name FROM threads WHERE destination_id = ? ORDER BY created_at, thread_id", destID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Thread
	for rows.Next() {
		var t Thread
		if err := rows.Scan(&t.DestinationID, &t.ThreadID, &t.Name); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// Routes returns the thread of each routed event type of a destination.
func (s *Store) Routes(destID int64) (map[string]string, error) {
	rows, err := s.db.Query("SELECT event_type, thread_id FROM routes WHERE destination_id = ?", destID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	routes := map[string]string{}
	for rows.Next() {
		var event, thread string
		if err := rows.Scan(&event, &thread); err != nil {
			return nil, err
		}
		routes[event] = thread
	}
	return routes, rows.Err()
}

// ThreadFor returns the thread an event type is routed to, or "".
func (s *Store) ThreadFor(destID int64, eventType string) string {
	var thread string
	err := s.db.QueryRow("SELECT thread_id FROM routes WHERE destination_id = ? AND event_type = ?", destID, eventType).Scan(&thread)
	if err != nil {
		return ""
	}
	return thread
}

// DeleteThread removes a thread and the routes to it.
func (s *Store) DeleteThread(destID int64, threadID string) error {
	_, err := s.db.Exec("DELETE FROM threads WHERE destination_id = ? AND thread_id = ?", destID, threadID)
	return err
}

// FindRepo returns the repository matching url, ignoring scheme, case and
// trailing slashes, or nil when none matches.
func (s *Store) FindRepo(url string) (*Repo, error) {
	want := NormalizeRepoURL(url)
	if want == "" {
		return nil, nil
	}
	repos, err := s.Repos()
	if err != nil {
		return nil, err
	}
	for _, r := range repos {
		if NormalizeRepoURL(r.URL) == want {
			return &r, nil
		}
	}
	return nil, nil
}

// CreateRepo stores a new repository. A repository that matches an existing
// one after normalisation is refused.
func (s *Store) CreateRepo(url, platform, secret string) (Repo, error) {
	if existing, err := s.FindRepo(url); err != nil {
		return Repo{}, err
	} else if existing != nil {
		return Repo{}, fmt.Errorf("repository %s already exists", existing.URL)
	}
	res, err := s.db.Exec("INSERT INTO repos (url, platform, secret) VALUES (?, ?, ?)", url, platform, secret)
	if err != nil {
		return Repo{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Repo{}, err
	}
	return Repo{ID: id, URL: url, Platform: platform, Secret: secret}, nil
}

func queryRepos(db *sql.DB, query string, args ...any) ([]Repo, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Repo
	for rows.Next() {
		var r Repo
		if err := rows.Scan(&r.ID, &r.URL, &r.Platform, &r.Secret); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// Repos returns every repository, by URL.
func (s *Store) Repos() ([]Repo, error) {
	return queryRepos(s.db, "SELECT id, url, platform, secret FROM repos ORDER BY url")
}

// Subscribe makes a destination receive a repository's events.
func (s *Store) Subscribe(repoID, destID int64) error {
	_, err := s.db.Exec("INSERT OR IGNORE INTO subscriptions (repo_id, destination_id) VALUES (?, ?)", repoID, destID)
	return err
}

// Unsubscribe removes a destination's subscription to the repository
// matching url and deletes the repository when it was the last one.
// It reports whether a subscription was removed.
func (s *Store) Unsubscribe(destID int64, url string) (bool, error) {
	repo, err := s.FindRepo(url)
	if err != nil || repo == nil {
		return false, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.Exec("DELETE FROM subscriptions WHERE repo_id = ? AND destination_id = ?", repo.ID, destID)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	if _, err := tx.Exec(`DELETE FROM repos WHERE id = ?
		AND NOT EXISTS (SELECT 1 FROM subscriptions WHERE repo_id = ?)`, repo.ID, repo.ID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// Subscribers returns the active destinations subscribed to a repository.
func (s *Store) Subscribers(repoID int64) ([]Destination, error) {
	return queryDests(s.db, `SELECT d.id, d.messenger, d.chat_id, d.title, d.language, d.format, d.use_threads, d.inactive
		FROM destinations d JOIN subscriptions s ON s.destination_id = d.id
		WHERE s.repo_id = ? AND d.inactive = 0 ORDER BY d.id`, repoID)
}

// SubscribersIn returns the active chats of one messenger subscribed to a
// repository, leaving out the DMs of people.
func (s *Store) SubscribersIn(repoID int64, messenger string) ([]Destination, error) {
	return queryDests(s.db, `SELECT d.id, d.messenger, d.chat_id, d.title, d.language, d.format, d.use_threads, d.inactive
		FROM destinations d JOIN subscriptions s ON s.destination_id = d.id
		WHERE s.repo_id = ? AND d.messenger = ? AND d.inactive = 0
		AND d.id NOT IN (SELECT destination_id FROM people) ORDER BY d.id`, repoID, messenger)
}

// SubscriptionsOf returns the repositories a destination is subscribed to.
func (s *Store) SubscriptionsOf(destID int64) ([]Repo, error) {
	return queryRepos(s.db, `SELECT r.id, r.url, r.platform, r.secret
		FROM repos r JOIN subscriptions s ON s.repo_id = r.id
		WHERE s.destination_id = ? ORDER BY r.url`, destID)
}

// SubscriberCount returns how many destinations (active or not) are
// subscribed to a repository.
func (s *Store) SubscriberCount(repoID int64) (int, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM subscriptions WHERE repo_id = ?", repoID).Scan(&n)
	return n, err
}
