-- Schema of 1.0.x. Databases created before migrations existed already have it
-- and are marked as version 1 without running this file.

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
