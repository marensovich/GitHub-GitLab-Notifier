package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"
)

// Bucket names.
var (
	BucketChats   = []byte("chats")   // chat_id -> ChatConfig
	BucketTopics  = []byte("topics")  // topic_id -> TopicInfo
	BucketMapping = []byte("mapping") // event_type -> topic_id
)

// ChatConfig holds configuration for a specific chat.
//
// Настройки для конкретного чата.
type ChatConfig struct {
	ChatID        int64  `json:"chat_id"`
	Language      string `json:"language"`
	DefaultTopic  int    `json:"default_topic"`
	IsForum       bool   `json:"is_forum"`
	WebhookSecret string `json:"webhook_secret"`
}

// TopicInfo holds information about a created forum topic.
//
// Информация о созданной теме форума.
type TopicInfo struct {
	TopicID   int    `json:"topic_id"`
	ChatID    int64  `json:"chat_id"`
	Name      string `json:"name"`
	EventType string `json:"event_type"` // "", "push", "star", "pull_request", "issues"
}

// Store wraps a BoltDB connection.
//
// Обёртка над BoltDB.
type Store struct {
	db *bolt.DB
}

// Open opens or creates a BoltDB database file.
//
// Открывает или создаёт файл базы данных BoltDB.
func Open(path string) (*Store, error) {
	// Создаём папку для БД, если её нет
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Создаём бакеты, если их нет
	err = db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range [][]byte{BucketChats, BucketTopics, BucketMapping} {
			if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return fmt.Errorf("failed to create bucket %s: %w", bucket, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	log.Println("✅ Storage initialized")
	return &Store{db: db}, nil
}

// Close closes the database.
//
// Закрывает базу данных.
func (s *Store) Close() error {
	return s.db.Close()
}

// ─── Chat Config ───

// SaveChat saves or updates chat configuration.
//
// Сохраняет или обновляет конфигурацию чата.
func (s *Store) SaveChat(chat ChatConfig) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		key := itob(chat.ChatID)
		value, err := json.Marshal(chat)
		if err != nil {
			return fmt.Errorf("failed to marshal chat config: %w", err)
		}
		return tx.Bucket(BucketChats).Put(key, value)
	})
}

// GetChat retrieves chat configuration by chat ID.
//
// Получает конфигурацию чата по ID.
func (s *Store) GetChat(chatID int64) (*ChatConfig, error) {
	var chat ChatConfig
	err := s.db.View(func(tx *bolt.Tx) error {
		key := itob(chatID)
		value := tx.Bucket(BucketChats).Get(key)
		if value == nil {
			return fmt.Errorf("chat %d not found", chatID)
		}
		return json.Unmarshal(value, &chat)
	})
	if err != nil {
		return nil, err
	}
	return &chat, nil
}

// GetAllChats returns all configured chats.
//
// Возвращает все настроенные чаты.
func (s *Store) GetAllChats() ([]ChatConfig, error) {
	var chats []ChatConfig
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(BucketChats).ForEach(func(k, v []byte) error {
			var chat ChatConfig
			if err := json.Unmarshal(v, &chat); err != nil {
				return err
			}
			chats = append(chats, chat)
			return nil
		})
	})
	return chats, err
}

// ─── Topics ───

// SaveTopic saves a forum topic mapping.
//
// Сохраняет маппинг темы форума.
func (s *Store) SaveTopic(topic TopicInfo) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		key := itob(int64(topic.TopicID))
		value, err := json.Marshal(topic)
		if err != nil {
			return err
		}
		return tx.Bucket(BucketTopics).Put(key, value)
	})
}

// GetTopicByEvent returns the topic ID for a given event type in a chat.
//
// Возвращает ID темы для указанного типа события в чате.
func (s *Store) GetTopicByEvent(chatID int64, eventType string) int {
	var topicID int
	_ = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(BucketMapping)
		key := []byte(fmt.Sprintf("%d:%s", chatID, eventType))
		value := b.Get(key)
		if value != nil {
			topicID = btoi(value)
		}
		return nil
	})
	return topicID
}

// SetEventTopic maps an event type to a topic in a chat.
//
// Привязывает тип события к теме в чате.
func (s *Store) SetEventTopic(chatID int64, eventType string, topicID int) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		key := []byte(fmt.Sprintf("%d:%s", chatID, eventType))
		value := itob(int64(topicID))
		return tx.Bucket(BucketMapping).Put(key, value)
	})
}

// GetTopicsByChat returns all topics for a given chat.
//
// Возвращает все темы для указанного чата.
func (s *Store) GetTopicsByChat(chatID int64) ([]TopicInfo, error) {
	var topics []TopicInfo
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(BucketTopics).ForEach(func(k, v []byte) error {
			var topic TopicInfo
			if err := json.Unmarshal(v, &topic); err != nil {
				return err
			}
			if topic.ChatID == chatID {
				topics = append(topics, topic)
			}
			return nil
		})
	})
	return topics, err
}

// ─── Helpers ───

func itob(v int64) []byte {
	return []byte(fmt.Sprintf("%020d", v))
}

func btoi(b []byte) int {
	var v int64
	fmt.Sscanf(string(b), "%020d", &v)
	return int(v)
}
