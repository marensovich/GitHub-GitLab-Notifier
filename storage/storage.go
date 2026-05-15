package storage

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

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

// ─── Инициализация ───

// Open opens or creates a SQLite database and creates tables.
//
// Открывает или создаёт базу данных SQLite и создаёт таблицы.
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
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	store := &Store{db: db}

	// Создаём таблицы
	if err := store.createTables(); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	log.Println("✅ Storage initialized (SQLite)")
	return store, nil
}

// createTables creates all required tables if they don't exist.
//
// Создаёт все необходимые таблицы, если они не существуют.
func (s *Store) createTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS chats (
		chat_id INTEGER PRIMARY KEY,
		language TEXT NOT NULL DEFAULT 'ru',
		is_forum INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS topics (
		topic_id INTEGER NOT NULL,
		chat_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		event_type TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (chat_id, topic_id),
		FOREIGN KEY (chat_id) REFERENCES chats(chat_id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS repos (
		repo_url TEXT PRIMARY KEY,
		chat_id INTEGER NOT NULL,
		platform TEXT NOT NULL DEFAULT 'GitHub',
		secret TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (chat_id) REFERENCES chats(chat_id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS event_mapping (
		chat_id INTEGER NOT NULL,
		event_type TEXT NOT NULL,
		topic_id INTEGER NOT NULL,
		PRIMARY KEY (chat_id, event_type),
		FOREIGN KEY (chat_id) REFERENCES chats(chat_id) ON DELETE CASCADE,
		FOREIGN KEY (chat_id, topic_id) REFERENCES topics(chat_id, topic_id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_topics_chat ON topics(chat_id);
	CREATE INDEX IF NOT EXISTS idx_repos_chat ON repos(chat_id);
	CREATE INDEX IF NOT EXISTS idx_event_mapping_chat ON event_mapping(chat_id);
	`

	_, err := s.db.Exec(schema)
	return err
}

// Close closes the database.
//
// Закрывает базу данных.
func (s *Store) Close() error {
	return s.db.Close()
}

// ─── Чаты ───

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
	var chat ChatConfig
	var isForum int
	err := s.db.QueryRow("SELECT chat_id, language, is_forum FROM chats WHERE chat_id = ?", chatID).
		Scan(&chat.ChatID, &chat.Language, &isForum)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("chat %d not found", chatID)
	}
	if err != nil {
		return nil, err
	}
	chat.IsForum = intToBool(isForum)
	return &chat, nil
}

// GetAllChats returns all configured chats.
//
// Возвращает все настроенные чаты.
func (s *Store) GetAllChats() []ChatConfig {
	rows, err := s.db.Query("SELECT chat_id, language, is_forum FROM chats")
	if err != nil {
		log.Printf("❌ GetAllChats error: %v", err)
		return nil
	}
	defer rows.Close()

	var chats []ChatConfig
	for rows.Next() {
		var chat ChatConfig
		var isForum int
		if err := rows.Scan(&chat.ChatID, &chat.Language, &isForum); err != nil {
			log.Printf("❌ GetAllChats scan error: %v", err)
			continue
		}
		chat.IsForum = intToBool(isForum)
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

// ─── Темы ───

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

// ─── Репозитории ───

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

// ─── Привязка событий к темам ───

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

// ─── Вспомогательные ───

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func intToBool(i int) bool {
	return i == 1
}
