-- 2.0: messenger-neutral model (NTF-11). A repository can be subscribed by
-- many destinations (chats in any messenger); chat and thread IDs are text.
-- 1.x data is copied: chats -> destinations (telegram), topics -> threads,
-- event_mapping -> routes, repos -> repos + one subscription each.

CREATE TABLE repos_v2 (
	id INTEGER PRIMARY KEY,
	url TEXT NOT NULL UNIQUE,
	platform TEXT NOT NULL DEFAULT 'GitHub',
	secret TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE destinations (
	id INTEGER PRIMARY KEY,
	messenger TEXT NOT NULL,
	chat_id TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	language TEXT NOT NULL DEFAULT '',
	format TEXT NOT NULL DEFAULT '',
	use_threads INTEGER NOT NULL DEFAULT 1,
	inactive INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (messenger, chat_id)
);

CREATE TABLE threads (
	destination_id INTEGER NOT NULL REFERENCES destinations(id) ON DELETE CASCADE,
	thread_id TEXT NOT NULL,
	name TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (destination_id, thread_id)
);

CREATE TABLE routes (
	destination_id INTEGER NOT NULL,
	event_type TEXT NOT NULL,
	thread_id TEXT NOT NULL,
	PRIMARY KEY (destination_id, event_type),
	FOREIGN KEY (destination_id, thread_id) REFERENCES threads(destination_id, thread_id) ON DELETE CASCADE
);

CREATE TABLE subscriptions (
	repo_id INTEGER NOT NULL REFERENCES repos_v2(id) ON DELETE CASCADE,
	destination_id INTEGER NOT NULL REFERENCES destinations(id) ON DELETE CASCADE,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (repo_id, destination_id)
);

CREATE INDEX idx_subscriptions_destination ON subscriptions(destination_id);

INSERT INTO destinations (messenger, chat_id, language, format, use_threads)
	SELECT 'telegram', CAST(chat_id AS TEXT), language, format, use_topics FROM chats;

INSERT INTO threads (destination_id, thread_id, name)
	SELECT d.id, CAST(t.topic_id AS TEXT), t.name
	FROM topics t JOIN destinations d ON d.messenger = 'telegram' AND d.chat_id = CAST(t.chat_id AS TEXT);

INSERT INTO routes (destination_id, event_type, thread_id)
	SELECT d.id, m.event_type, CAST(m.topic_id AS TEXT)
	FROM event_mapping m
	JOIN destinations d ON d.messenger = 'telegram' AND d.chat_id = CAST(m.chat_id AS TEXT)
	JOIN topics t ON t.chat_id = m.chat_id AND t.topic_id = m.topic_id;

INSERT INTO repos_v2 (url, platform, secret, created_at)
	SELECT repo_url, platform, secret, created_at FROM repos;

INSERT INTO subscriptions (repo_id, destination_id)
	SELECT r2.id, d.id
	FROM repos r
	JOIN repos_v2 r2 ON r2.url = r.repo_url
	JOIN destinations d ON d.messenger = 'telegram' AND d.chat_id = CAST(r.chat_id AS TEXT);

DROP TABLE event_mapping;
DROP TABLE topics;
DROP TABLE repos;
DROP TABLE chats;
ALTER TABLE repos_v2 RENAME TO repos;
