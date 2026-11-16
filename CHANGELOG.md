# Changelog

All notable changes to this project are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Local read-only web panel (`PANEL_ADDRESS`, e.g. `127.0.0.1:8081`): overview with warnings for repositories
  without a webhook secret, the list of connected repositories and their chats, and every chat with its topics
  and event routing. It listens only on loopback unless `PANEL_ALLOW_REMOTE=true`, rejects non-loopback `Host`
  headers, accepts only `GET`/`HEAD`, never shows secrets, and needs no internet access. Open it through an SSH
  tunnel: `ssh -L 8081:127.0.0.1:8081 user@server`.
- Delivery log and deduplication: every incoming webhook is recorded (metadata only) with its result —
  `delivered`, `ignored`, `rejected`, `failed` or `duplicate`. A redelivery of an already delivered webhook
  (`X-GitHub-Delivery`, GitLab `Idempotency-Key` / `X-Gitlab-Event-UUID`) is answered with `200 DUPLICATE` and
  not posted again. The log is pruned hourly after `DELIVERIES_RETENTION_DAYS` (default 14, `0` keeps it).
- Health report: JSON with version, uptime and `db` / `telegram` checks; `503` when one fails, with the error.
  The Telegram check uses a `getMe` probe once a minute and successful sends, never a call per request.
- Prometheus metrics at `/metrics` (`METRICS_ENABLED`, default on): webhooks by platform/event/status, webhook
  handling time, send attempts by result, build info and start time. `METRICS_ADDR` serves them on a separate
  address such as `127.0.0.1:9090`. No new dependencies. Grafana dashboard in `docs/grafana-dashboard.json`.
- Docker image on GitHub Container Registry for `linux/amd64`, `linux/arm64` and `linux/arm/v7`:
  `ghcr.io/marensovich/github-gitlab-notifier` with tags `X.Y.Z`, `X.Y`, `latest` (stable releases only) and
  `edge` (every push to `development`). `docker-compose.yml` now pulls this image; `--build` still builds locally.
  The Dockerfile cross-compiles in the build stage instead of emulating the target platform.

- Golden tests for notifications: every handler is rendered from payload fixtures in
  `internal/handler/testdata/<platform>/<event>/` in every language and compared with `.golden` files
  (`go test ./internal/handler -run TestGolden -update` regenerates them). All 19 events are covered, including
  force pushes, branch creation/deletion, 50-commit pushes, pull requests from forks, drafts, pre-releases,
  confidential GitLab issues and notes, notes on issues and skipped actions. `scripts/scrub-fixture.sh` cleans a
  real payload before it becomes a fixture.

- GitLab confidential issues, comments on them and internal notes are marked with 🔒 "Confidential — visible only
  to project members in GitLab", so the chat sees that the text was not public in the project.

- New notification look using Telegram rich messages: a heading, the item as a bold link, a compact table of
  repository, branch and other details, long text in an expandable quote, commit and file lists collapsed under a
  summary, coloured buttons ("Open PR", "Compare", "Open run"…) and a footer with the author and the event time,
  shown relative in the reader's language and time zone. If Telegram rejects a rich message, the classic message
  is sent instead (counted in `notifier_rich_fallbacks_total`). `RICH_MESSAGES=false` turns rich messages off for
  the whole installation.
- `/configure` is a short setup with buttons, edited in place: in a forum chat it asks whether to create topics or
  post everything to the main chat, then every chat picks the new look or the classic one (for old and third-party
  Telegram clients, which show rich messages as unsupported). Current choices are marked with ✓; run it again to
  change them. Only chat administrators can press the buttons. The web panel and `/status` show each chat's look
  and topic routing; `/status` also says when the new look is off on the server.

### Changed

- Classic notifications use the same layout as the new look and carry their links as coloured inline buttons.
- `/configure` works in regular groups too (it skips the topics step and posts to the main chat) instead of
  refusing them.
- `notifier_messages_sent_total` has a `format` label (`rich` / `classic`); dashboards that sum it by
  `messenger, result` keep working. The Grafana dashboard splits "Messages sent" by format and shows rich fallbacks.
- Database migration `0003` adds `format` (default `rich`) and `use_topics` (default on) to every chat, so an
  upgraded installation gets the new look with the same routing.
- `GET /health` and `/webhook/health` return the health report (same as the new `/healthz`) instead of `pong`,
  and answer `503` when the database or Telegram is not working. Monitors that compared the body with `pong`
  need to check the status code instead. The installer prints the report when the check fails.
- The database schema is now versioned (`schema_version` table, numbered SQL files in `storage/migrations/`).
  Pending migrations run at start-up, each in its own transaction; a failed one is rolled back and the bot stops
  with the name of the migration. Databases from 1.0.x are recognised and marked as version 1 without changes.
  A database written by a newer version is refused instead of being opened by an older binary.

### Fixed

- Push notifications for created and deleted branches read "Branch deleted to branch …" / "Удалена ветка в ветку …";
  they now say "Deleted branch …" / "Удалена ветка …".
- Pull requests from forks showed the source branch without its owner (often `main` → `main`); they now show
  `owner:branch`.
- A slow `setMyCommands` at start-up (Telegram answering `504` after minutes) kept the webhook port closed until it
  gave up; the command menu is now registered in the background.

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
