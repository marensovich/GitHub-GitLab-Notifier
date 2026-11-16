-- Log of incoming webhook deliveries: metadata only, never the payload.
-- delivery_id is not unique: a redelivery is logged again (as "duplicate",
-- or as a fresh attempt when the earlier one was not delivered).

CREATE TABLE deliveries (
	id INTEGER PRIMARY KEY,
	delivery_id TEXT NOT NULL DEFAULT '',
	platform TEXT NOT NULL,
	event TEXT NOT NULL DEFAULT '',
	repo_url TEXT NOT NULL DEFAULT '',
	received_at INTEGER NOT NULL, -- unix milliseconds, UTC
	status TEXT NOT NULL,
	http_status INTEGER NOT NULL,
	error TEXT NOT NULL DEFAULT '',
	duration_ms INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_deliveries_delivery ON deliveries(platform, delivery_id) WHERE delivery_id != '';
CREATE INDEX idx_deliveries_repo ON deliveries(repo_url, received_at);
CREATE INDEX idx_deliveries_received ON deliveries(received_at);
