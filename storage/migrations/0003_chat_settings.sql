-- Per-chat message format and topic routing (NTF-91).
ALTER TABLE chats ADD COLUMN format TEXT NOT NULL DEFAULT 'rich';
ALTER TABLE chats ADD COLUMN use_topics INTEGER NOT NULL DEFAULT 1;
