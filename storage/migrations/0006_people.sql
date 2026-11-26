-- 2.0: personal DMs (NTF-30). A person is a messenger user with a DM
-- destination; an account is a GitHub/GitLab account that person proved
-- with a token. The token itself is never stored.

CREATE TABLE people (
	id INTEGER PRIMARY KEY,
	messenger TEXT NOT NULL,
	user_id TEXT NOT NULL,
	destination_id INTEGER NOT NULL REFERENCES destinations(id) ON DELETE CASCADE,
	muted TEXT NOT NULL DEFAULT '', -- comma-separated reasons
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (messenger, user_id)
);

CREATE TABLE accounts (
	id INTEGER PRIMARY KEY,
	person_id INTEGER NOT NULL REFERENCES people(id) ON DELETE CASCADE,
	messenger TEXT NOT NULL,
	platform TEXT NOT NULL,   -- github, gitlab
	host TEXT NOT NULL,
	account_id TEXT NOT NULL, -- the platform's numeric user ID
	login TEXT NOT NULL COLLATE NOCASE,
	linked_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (messenger, platform, host, account_id)
);

CREATE INDEX idx_accounts_id ON accounts(platform, host, account_id);
CREATE INDEX idx_accounts_login ON accounts(platform, host, login);
CREATE INDEX idx_accounts_person ON accounts(person_id);
