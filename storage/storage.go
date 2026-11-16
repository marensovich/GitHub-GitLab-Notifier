package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database connection.
//
// Обёртка над SQLite базой данных.
type Store struct {
	db *sql.DB
}

// ─── Модели ───

// ChatConfig holds configuration for a specific chat.
type ChatConfig struct {
	ChatID   int64  `json:"chat_id"`
	Language string `json:"language"`
	IsForum  bool   `json:"is_forum"`
	// Format is the message look the chat chose: FormatRich or FormatClassic.
	Format string `json:"format"`
	// UseTopics routes events to forum topics; off sends all to the general chat.
	UseTopics bool `json:"use_topics"`
}

// Message formats a chat can choose.
const (
	FormatRich    = "rich"
	FormatClassic = "classic"
)

const chatColumns = "chat_id, language, is_forum, format, use_topics"

// scanChat reads one row selected with chatColumns.
func scanChat(row interface{ Scan(...any) error }) (ChatConfig, error) {
	var chat ChatConfig
	var isForum, useTopics int
	err := row.Scan(&chat.ChatID, &chat.Language, &isForum, &chat.Format, &useTopics)
	chat.IsForum = intToBool(isForum)
	chat.UseTopics = intToBool(useTopics)
	return chat, err
}

// SetChatSettings stores the message format and topic routing of a chat.
//
// Сохраняет формат сообщений и маршрутизацию по темам для чата.
func (s *Store) SetChatSettings(chatID int64, format string, useTopics bool) error {
	if format != FormatRich && format != FormatClassic {
		return fmt.Errorf("unknown message format %q", format)
	}
	res, err := s.db.Exec("UPDATE chats SET format = ?, use_topics = ? WHERE chat_id = ?",
		format, boolToInt(useTopics), chatID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("chat %d not found", chatID)
	}
	return nil
}

// TopicInfo holds information about a created forum topic.
type TopicInfo struct {
	TopicID   int    `json:"topic_id"`
	ChatID    int64  `json:"chat_id"`
	Name      string `json:"name"`
	EventType string `json:"event_type"`
}

// RepoInfo holds information about a connected repository.
type RepoInfo struct {
	ChatID   int64  `json:"chat_id"`
	RepoURL  string `json:"repo_url"`
	Platform string `json:"platform"`
	Secret   string `json:"secret"`
}

// Open opens or creates a SQLite database and migrates its schema.
//
// Открывает или создаёт базу данных SQLite и применяет миграции.
func Open(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Проверяем соединение
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Применяем миграции схемы
	list, err := loadMigrations(embeddedMigrations, "migrations")
	if err == nil {
		err = migrate(context.Background(), db, list)
	}
	if err != nil {
		db.Close()
		return nil, err
	}

	log.Println("✅ Storage initialized (SQLite)")
	return &Store{db: db}, nil
}

// Ping checks that the database answers.
//
// Проверяет, что база данных отвечает.
func (s *Store) Ping(ctx context.Context) error {
	var one int
	return s.db.QueryRowContext(ctx, "SELECT 1").Scan(&one)
}

// Close closes the database.
//
// Закрывает базу данных.
func (s *Store) Close() error {
	return s.db.Close()
}

// SaveChat inserts or updates a chat configuration.
//
// Вставляет или обновляет конфигурацию чата.
func (s *Store) SaveChat(chat ChatConfig) error {
	_, err := s.db.Exec(`
		INSERT INTO chats (chat_id, language, is_forum)
		VALUES (?, ?, ?)
		ON CONFLICT(chat_id) DO UPDATE SET
			language = excluded.language,
			is_forum = excluded.is_forum
	`, chat.ChatID, chat.Language, boolToInt(chat.IsForum))
	return err
}

// GetChat retrieves a chat by ID.
//
// Получает чат по ID.
func (s *Store) GetChat(chatID int64) (*ChatConfig, error) {
	chat, err := scanChat(s.db.QueryRow("SELECT "+chatColumns+" FROM chats WHERE chat_id = ?", chatID))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("chat %d not found", chatID)
	}
	if err != nil {
		return nil, err
	}
	return &chat, nil
}

// GetAllChats returns all configured chats.
//
// Возвращает все настроенные чаты.
func (s *Store) GetAllChats() []ChatConfig {
	rows, err := s.db.Query("SELECT " + chatColumns + " FROM chats")
	if err != nil {
		log.Printf("❌ GetAllChats error: %v", err)
		return nil
	}
	defer rows.Close()

	var chats []ChatConfig
	for rows.Next() {
		chat, err := scanChat(rows)
		if err != nil {
			log.Printf("❌ GetAllChats scan error: %v", err)
			continue
		}
		chats = append(chats, chat)
	}
	return chats
}

// DeleteChat removes a chat and all related data (topics, repos, mappings).
//
// Удаляет чат и все связанные данные (темы, репозитории, привязки).
func (s *Store) DeleteChat(chatID int64) error {
	_, err := s.db.Exec("DELETE FROM chats WHERE chat_id = ?", chatID)
	return err
}

// SaveTopic inserts or updates a topic.
//
// Вставляет или обновляет тему.
func (s *Store) SaveTopic(topic TopicInfo) error {
	_, err := s.db.Exec(`
		INSERT INTO topics (topic_id, chat_id, name, event_type)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(chat_id, topic_id) DO UPDATE SET
			name = excluded.name,
			event_type = excluded.event_type
	`, topic.TopicID, topic.ChatID, topic.Name, topic.EventType)
	return err
}

// GetTopicsByChat returns all topics for a given chat.
//
// Возвращает все темы для указанного чата.
func (s *Store) GetTopicsByChat(chatID int64) ([]TopicInfo, error) {
	rows, err := s.db.Query(
		"SELECT topic_id, chat_id, name, event_type FROM topics WHERE chat_id = ?",
		chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []TopicInfo
	for rows.Next() {
		var t TopicInfo
		if err := rows.Scan(&t.TopicID, &t.ChatID, &t.Name, &t.EventType); err != nil {
			return nil, err
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
}

// DeleteTopic removes a topic from the database.
//
// Удаляет тему из базы данных.
func (s *Store) DeleteTopic(chatID int64, topicID int) error {
	_, err := s.db.Exec("DELETE FROM topics WHERE chat_id = ? AND topic_id = ?", chatID, topicID)
	return err
}

// SaveRepo inserts or updates a repository.
//
// Вставляет или обновляет репозиторий.
func (s *Store) SaveRepo(repo RepoInfo) error {
	_, err := s.db.Exec(`
		INSERT INTO repos (repo_url, chat_id, platform, secret)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(repo_url) DO UPDATE SET
			chat_id = excluded.chat_id,
			platform = excluded.platform,
			secret = excluded.secret
	`, repo.RepoURL, repo.ChatID, repo.Platform, repo.Secret)
	return err
}

// GetRepo returns a repository by URL.
//
// Возвращает репозиторий по URL.
func (s *Store) GetRepo(repoURL string) (*RepoInfo, error) {
	var repo RepoInfo
	err := s.db.QueryRow(
		"SELECT repo_url, chat_id, platform, secret FROM repos WHERE repo_url = ?",
		repoURL,
	).Scan(&repo.RepoURL, &repo.ChatID, &repo.Platform, &repo.Secret)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("repo not found: %s", repoURL)
	}
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

// GetAllRepos returns all connected repositories.
//
// Возвращает все подключённые репозитории.
func (s *Store) GetAllRepos() []RepoInfo {
	rows, err := s.db.Query("SELECT repo_url, chat_id, platform, secret FROM repos")
	if err != nil {
		log.Printf("❌ GetAllRepos error: %v", err)
		return nil
	}
	defer rows.Close()

	var repos []RepoInfo
	for rows.Next() {
		var r RepoInfo
		if err := rows.Scan(&r.RepoURL, &r.ChatID, &r.Platform, &r.Secret); err != nil {
			log.Printf("❌ GetAllRepos scan error: %v", err)
			continue
		}
		repos = append(repos, r)
	}
	return repos
}

// GetTopicByEvent returns the topic ID for a given event type in a chat.
// Returns 0 if no mapping exists.
//
// Возвращает ID темы для указанного типа события в чате.
// Возвращает 0, если привязки нет.
func (s *Store) GetTopicByEvent(chatID int64, eventType string) int {
	var topicID int
	err := s.db.QueryRow(
		"SELECT topic_id FROM event_mapping WHERE chat_id = ? AND event_type = ?",
		chatID, eventType,
	).Scan(&topicID)
	if err != nil {
		return 0
	}
	return topicID
}

// GetEventMappings returns the event type → topic ID routing of a chat.
//
// Возвращает привязку типов событий к темам в чате.
func (s *Store) GetEventMappings(chatID int64) (map[string]int, error) {
	rows, err := s.db.Query(
		"SELECT event_type, topic_id FROM event_mapping WHERE chat_id = ?",
		chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	mappings := make(map[string]int)
	for rows.Next() {
		var eventType string
		var topicID int
		if err := rows.Scan(&eventType, &topicID); err != nil {
			return nil, err
		}
		mappings[eventType] = topicID
	}
	return mappings, rows.Err()
}

// SetEventTopic maps an event type to a topic in a chat.
//
// Привязывает тип события к теме в чате.
func (s *Store) SetEventTopic(chatID int64, eventType string, topicID int) error {
	if eventType == "" {
		// Удаляем все привязки для этого топика
		_, err := s.db.Exec(
			"DELETE FROM event_mapping WHERE chat_id = ? AND topic_id = ?",
			chatID, topicID,
		)
		return err
	}

	_, err := s.db.Exec(`
		INSERT INTO event_mapping (chat_id, event_type, topic_id)
		VALUES (?, ?, ?)
		ON CONFLICT(chat_id, event_type) DO UPDATE SET
			topic_id = excluded.topic_id
	`, chatID, eventType, topicID)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func intToBool(i int) bool {
	return i == 1
}

// NormalizeRepoURL converts a repository URL into a canonical form used for
// matching webhook payloads against connected repositories:
// no scheme, no "www.", lowercase, no trailing slash or ".git".
func NormalizeRepoURL(raw string) string {
	u := strings.ToLower(strings.TrimSpace(raw))
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "www.")
	u = strings.TrimRight(u, "/")
	u = strings.TrimSuffix(u, ".git")
	return strings.TrimRight(u, "/")
}

// FindRepo returns the connected repository matching the given URL,
// ignoring scheme, case and trailing slashes. Returns nil if not found.
func (s *Store) FindRepo(repoURL string) *RepoInfo {
	want := NormalizeRepoURL(repoURL)
	if want == "" {
		return nil
	}
	for _, r := range s.GetAllRepos() {
		if NormalizeRepoURL(r.RepoURL) == want {
			return &r
		}
	}
	return nil
}

// GetReposByChat returns all repositories connected to a chat.
func (s *Store) GetReposByChat(chatID int64) ([]RepoInfo, error) {
	rows, err := s.db.Query(
		"SELECT repo_url, chat_id, platform, secret FROM repos WHERE chat_id = ? ORDER BY repo_url",
		chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []RepoInfo
	for rows.Next() {
		var r RepoInfo
		if err := rows.Scan(&r.RepoURL, &r.ChatID, &r.Platform, &r.Secret); err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

// DeleteRepo disconnects a repository from a chat.
// Returns false if the repository was not connected to that chat.
func (s *Store) DeleteRepo(chatID int64, repoURL string) (bool, error) {
	repo := s.FindRepo(repoURL)
	if repo == nil || repo.ChatID != chatID {
		return false, nil
	}
	_, err := s.db.Exec("DELETE FROM repos WHERE repo_url = ?", repo.RepoURL)
	return err == nil, err
}

// SetChatLanguage updates the notification language of a chat,
// creating the chat record if it does not exist yet.
func (s *Store) SetChatLanguage(chatID int64, language string, isForum bool) error {
	_, err := s.db.Exec(`
		INSERT INTO chats (chat_id, language, is_forum)
		VALUES (?, ?, ?)
		ON CONFLICT(chat_id) DO UPDATE SET language = excluded.language
	`, chatID, language, boolToInt(isForum))
	return err
}
