# Changelog

## [0.0.1] — 2026-05-16

### Added

- SQLite database with tables: chats, topics, repos, event_mapping
- Forum topic auto-creation via `/configure`
- Command `/connect` with webhook URL generation
- Command `/status` to view database contents
- Handler for GitHub `star` events
- Auto-detection of external IP for webhook URL
- HTTPS/HTTP toggle via `USE_HTTPS` env variable
- DOMAIN normalization (strips protocol and trailing slash)

### Changed

- Migrated from BoltDB to SQLite for better querying
- Improved `/configure` — saves chat first, then creates topics (fixes FK errors)
- All logs use i18n keys for localization

### Fixed

- FOREIGN KEY constraint failed on first `/configure`
- Wrong IP from VPN in `/connect` output
- HTML parsing error in `/help` (unsupported `<url>` tag)