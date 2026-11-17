-- 2.0: notifications are queued and sent by a background worker with
-- retries (NTF-13). One row per message to one destination; the delivery
-- row of the webhook follows the state of its rows.

CREATE TABLE outbox (
	id INTEGER PRIMARY KEY,
	delivery_id INTEGER REFERENCES deliveries(id) ON DELETE SET NULL,
	destination_id INTEGER NOT NULL REFERENCES destinations(id) ON DELETE CASCADE,
	thread_id TEXT NOT NULL DEFAULT '',
	event_type TEXT NOT NULL DEFAULT '',
	payload TEXT NOT NULL,
	attempts INTEGER NOT NULL DEFAULT 0,
	next_attempt_at INTEGER NOT NULL, -- unix milliseconds
	last_error TEXT NOT NULL DEFAULT '',
	state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'sending', 'done', 'dead')),
	created_at INTEGER NOT NULL,      -- unix milliseconds
	finished_at INTEGER NOT NULL DEFAULT 0 -- when it became done or dead
);

CREATE INDEX idx_outbox_due ON outbox(state, next_attempt_at);
CREATE INDEX idx_outbox_destination ON outbox(destination_id, id);
CREATE INDEX idx_outbox_delivery ON outbox(delivery_id);
