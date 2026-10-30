# Changelog

All notable changes to this project are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [1.0.1] — 2026-10-30

### Security

- Update `github.com/klauspost/compress` to 1.18.7 (indirect dependency of the Telegram client) to fix
  [GHSA-259r-337f-4rfw](https://github.com/advisories/GHSA-259r-337f-4rfw) / CVE-2026-63209
  (integer overflow and out-of-bounds write, severity high).

## [1.0.0] — 2026-10-30

First stable release: complete GitHub and GitLab support, one-command Linux installation and Docker image.

Pre-built binaries: Linux (amd64, arm64, armv7), macOS (Intel, Apple Silicon), Windows (amd64, arm64),
FreeBSD (amd64, arm64).

### Added

- **GitHub events:** `pull_request`, `pull_request_review`, `issues`, `issue_comment`, `release`, `workflow_run`,
  `fork`, `ping`; tag pushes are shown as "New tag" / "Tag deleted".
- **GitLab events:** Push, Tag Push, Merge Request, Issue, Confidential Issue, Note, Confidential Note, Pipeline and
  Release hooks. User links work with self-hosted GitLab.
- Forum topics for Pull Requests, Issues, Releases and CI/CD in addition to Stars and Pushes.
- Commands `/status`, `/lang`, `/version`, `/disconnect`; command list in Telegram's Menu button.
- Per-chat notification language.
- Webhook signature verification (GitHub `X-Hub-Signature-256`, GitLab `X-Gitlab-Token`) with per-repository secrets
  shown by `/connect` and an optional global `WEBHOOK_SECRET`.
- Notifications of a connected repository are delivered only to the chat it was connected from.
- Linux installer `scripts/install.sh` with a hardened systemd service (install, update with rollback, uninstall).
- `Dockerfile` (distroless, non-root) and `docker-compose.yml`.
- CI workflow (gofmt, vet, race tests, shellcheck, Docker build) and a Build workflow that cross-compiles all
  platforms on every push to `master`.
- Release binaries for linux/armv7, windows/arm64 and freebsd/arm64; the installer supports 32-bit ARM.
- Configuration: `BOT_LANG`, `DB_PATH`, `WEBHOOK_SECRET`, `NOTIFY_UNKNOWN_EVENTS`, `TELEGRAM_API_URL`;
  flags `--config` and `--db`; `/health` endpoint.
- Graceful shutdown on SIGINT/SIGTERM.
- Tests for handlers (real payload fixtures), server, storage, bot command parsing and translations.
- `CONTRIBUTING.md`, `SECURITY.md`, `.editorconfig`, `.gitattributes`.

### Changed

- Translations are embedded into the binary; the bot no longer depends on the working directory.
- Unsupported events (e.g. `check_suite`, `watch`) are ignored instead of posting "New event …" messages.
- `CHAT_ID` is optional (fallback only).
- `/configure` can be re-run to add new topics; it keeps the chat language.
- `/connect` accepts any GitHub/GitLab URL including self-hosted GitLab, keeps the secret on reconnect and works before
  `/configure`.
- Release workflow uses the Go version from `go.mod`, builds macOS arm64, produces static binaries and checksums.

### Fixed

- Commit messages, titles and file names containing `<`, `>` or `&` broke Telegram messages (HTML is now escaped).
- Cyrillic commit messages were cut in the middle of a character.
- `SERVER_ADDRESS` was ignored.
- Crash on commands sent by anonymous group admins.
- "FOREIGN KEY constraint failed" when `/connect` was used before `/configure`.
- `go vet` warnings; unused BoltDB dependency removed.

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

[Unreleased]: https://github.com/marensovich/GitHub-GitLab-Notifier/compare/v1.0.1...HEAD
[1.0.1]: https://github.com/marensovich/GitHub-GitLab-Notifier/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/marensovich/GitHub-GitLab-Notifier/compare/v.0.0.1...v1.0.0
[0.0.1]: https://github.com/marensovich/GitHub-GitLab-Notifier/releases/tag/v.0.0.1
