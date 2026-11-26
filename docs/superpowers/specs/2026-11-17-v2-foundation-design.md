# Notifier 2.0: several messengers, subscriptions, outbox, config.yaml — design

Date: 2026-11-17 · Status: draft for approval · Cards: NTF-10…17, NTF-50 (MAX), NTF-51 (Discord)

## Why

The owner's goal is "a great service that people enjoy and use". Today the notifier is tied to Telegram:
- every package from `server` to `storage` speaks Telegram chat IDs and topics;
- a repository can be connected to exactly one chat (`repos.repo_url` is the primary key);
- a notification that Telegram does not accept right now (outage, `429`) is lost, because it is sent inside the
  webhook request;
- configuration is a flat `.env` that cannot describe several messengers.

2.0 makes the notifier a self-hosted service that posts the same GitHub/GitLab events to **Telegram, Discord
and MAX**, to as many chats as the owner wants, and never drops a message because a messenger was briefly
unavailable.

## Who uses it

One installation has one owner: a person or a team that runs the service, registers their own bots and uses
them. There is no multi-tenancy: no accounts, no ownership of records, one shared database. Bot administrators
are listed in `config.yaml`; a chat is managed by its own administrators, as `/configure` already does.

## Goals

1. **Three messengers in 2.0.0:** Telegram (everything 1.1 does), Discord and MAX, each optional and turned on in
   `config.yaml`. The same commands (`/connect`, `/disconnect`, `/status`, `/configure`, `/lang`, `/help`,
   `/start`, `/version`) work in each, in the form native to the messenger.
2. **Subscriptions:** a repository can be connected to any number of chats in any messengers. One webhook from
   GitHub/GitLab produces a message in every subscribed chat, each in its own language and look.
3. **Neutral messages:** handlers keep returning `notify.Notification`; each messenger renders it itself
   (Telegram rich/classic HTML, Discord embeds, MAX HTML) and keeps it within its own limits.
4. **Outbox:** a verified webhook is stored with its outgoing messages in one transaction and answered at once;
   a worker delivers them with retries, per-chat rate limits and per-chat ordering.
5. **`config.yaml`** replaces `.env`, with `${VAR}` substitution for secrets, strict validation and
   `notifier migrate-config` that converts a 1.x `.env`. The installer and Docker setup migrate automatically.
6. **CLI subcommands:** `serve` (default), `version`, `config check`, `migrate-config`, `db backup`.
7. **Upgrade without loss:** a 1.0.x or 1.1.x database is migrated automatically; every chat, topic, routing
   choice, repository and secret survives. A migration guide and a 2.0 README.

## Non-goals (later 2.x)

- Editing earlier messages, live CI and PR cards (NTF-20…24).
- People, personal DMs, mentions, subscription filters, fine-grained permissions (NTF-30…34). Personal DMs
  (NTF-30) were added later in 2.0: see `2026-11-26-personal-dms-design.md`.
- Per-destination quiet hours, digests, string overrides (NTF-41, NTF-42).
- `notifier update/rollback`, release signing, update checks (NTF-60…63); webhook auto-registration (NTF-70/71);
  panel management and login (NTF-73/74); GitHub App, GitLab group hooks, Gitea (NTF-80…82).

The schema must leave room for these (they add tables and columns later), but 2.0 does not build them.

## Anti-goals (a failure even if it "works")

- A 1.x user upgrades and loses a chat, a topic mapping, a repository or its secret, or has to reconfigure
  webhooks on GitHub/GitLab.
- Telegram notifications look different from 1.1.1 (the Telegram goldens must not change).
- Anyone who adds the bot to their own chat can read the events of a repository that someone else connected.
- Heavy dependencies: no Discord or MAX SDKs, no ORM, no CLI framework.

## Approaches considered

- **A. One 2.0 release, developed in `development` (chosen by the owner).** All cards land in `development`; the
  next release from it is 2.0.0. If 1.1.x needs an urgent fix meanwhile, it is made on a `hotfix/…` branch from
  `master`, released as 1.1.x and merged back into `development`.
- **B. Step by step through 1.2–1.4, breaking changes last.** Smaller risk per release, but the owner prefers
  one release.
- **C. Visible features first (subscriptions on the current Telegram code).** Rejected: the data model would be
  built on `internal/bot` and redone when Telegram moves behind the messenger interface.

## Decisions

### Data model

New migration(s) after `0003`, run automatically at start-up like the existing ones:

```
repos          (id INTEGER PK, url TEXT UNIQUE, platform, secret, created_at)
destinations   (id INTEGER PK, messenger TEXT, chat_id TEXT, title, language, format, use_threads,
                created_at, UNIQUE(messenger, chat_id))
threads        (destination_id, thread_id TEXT, name, created_at, PK(destination_id, thread_id))
routes         (destination_id, event_type, thread_id TEXT, PK(destination_id, event_type))
subscriptions  (repo_id, destination_id, created_at, PK(repo_id, destination_id))
```

- Chat and thread IDs are **strings**: Discord snowflakes and MAX IDs do not fit Telegram's `int64` semantics.
- `format`: messenger-specific look (`rich`/`classic` for Telegram; Discord and MAX have one look in 2.0 and
  store `""`).
- `use_threads`: Telegram forum topics, Discord forum-channel posts; always off in MAX (it has no threads).
- Data migration from 1.x: `chats` → `destinations` (`messenger='telegram'`, chat ID as text), `topics` →
  `threads`, `event_mapping` → `routes`, `repos` → `repos` + one `subscriptions` row each. Old tables are
  dropped in the same migration. `deliveries` is kept as is.
- `storage` gets a new API around these entities; the old chat/topic/repo methods are removed.

### Connecting a repository

- `/connect <url>` in a chat where the repository is not connected yet:
  - if the repository is new, it is created with a fresh secret and the webhook instructions are shown, as today;
  - **if the repository already exists** (connected from another chat), the chat is subscribed only when the
    command proves access: `/connect <url> <secret>` with the repository's webhook secret, or the command comes
    from a bot administrator listed in `config.yaml`. Otherwise the bot refuses and explains how. The secret is
    never shown to a chat that did not prove it knows it.
- This also closes a 1.x hole: today `/connect` of an already connected URL from another chat moves the
  repository to that chat and prints its secret.
- `/disconnect <url>` removes this chat's subscription; the repository row and its secret are deleted when the
  last subscription goes.
- *(Design call)* A repository whose secret is empty (allowed in 1.x) proves nothing through the secret path:
  only a bot administrator can subscribe another chat to it. `/status` and the panel already warn about it.
- *(Design call)* A command typed inside a thread (Telegram topic, Discord forum post or thread) acts on the
  parent chat or channel.

### Unconnected repositories (owner's decision)

1.x sends events of a repository that no chat connected to every known chat, or to `CHAT_ID` when there are
none. 2.0 **drops them**: the webhook is answered `202`, recorded in `deliveries` as `ignored` with the reason
"not connected", and nothing is sent. The panel overview warns about repositories that keep sending webhooks
without being connected. The migration guide and the release notes list this as a breaking change.

### Messenger interface

New package `internal/messenger` with the interface the core uses; one package per adapter
(`internal/messenger/telegram`, `…/discord`, `…/max`). The core (`server`, `commands`, `outbox`, `storage`,
`web`) does not import any messenger SDK.

The interface covers, in the builder's chosen shape:
- `Name()` and `Caps()` (threads, buttons per row, text limit, rate limits);
- starting the adapter and delivering incoming **commands** and **button presses** to the core;
- sending a `notify.Notification` to a destination (optionally a thread) in a language and look, rendering it
  itself; returning a classified error: rate limited (with retry-after), thread not found, chat not found or bot
  removed, rejected content, temporary;
- replying to a command with text and optional choice buttons, editing that reply, answering a button press;
- checking whether a user is an administrator of a chat;
- creating a thread for an event type (Telegram topic, Discord forum post).

Telegram's current behaviour moves into the Telegram adapter unchanged: rich first, classic on a content
rejection, the `sendRichMessage`-missing switch, button URLs resent as links, `RICH_MESSAGES` (now
`messengers.telegram.rich_messages`).

### Commands in the core

Package `internal/commands` implements every command once, against the messenger interface: connect,
disconnect, status, lang, help, start, version and the `/configure` flow (threads step when the messenger and
chat support threads, look step when the messenger has more than one look, summary). Texts stay in
`locales/*.json`. Each adapter only turns its native input into a command and renders the replies:
- **Telegram:** `/command` text messages and inline keyboards, as now.
- **Discord:** slash commands registered through the REST API, received on an **HTTP interactions endpoint**
  (`POST /discord/interactions` on the existing public server, Ed25519 signature checked with the standard
  library). Replies are interaction responses; choices are message components (buttons). No gateway websocket.
- **MAX:** `/command` text messages (`PATCH /me/commands` sets the menu), callback buttons answered with
  `POST /answers`. Updates come through a MAX webhook subscription to `POST /max/updates` when
  `server.public_url` is HTTPS, otherwise through long polling.

### Rendering per messenger

- **Telegram:** the current `notify.Rich` / `notify.Classic` move into the adapter; goldens unchanged.
- *(Design call)* **Discord destinations** are channels: a text channel (messages go to the channel, no threads
  in 2.0, `use_threads` off) or a forum channel (threads required: one post per event type, created by
  `/configure`; `/configure` in a forum channel has no "main chat" choice).
- **Discord:** one embed per notification: title (heading + item link), fields for the table rows, description
  for the quote and collapsed lists as plain lists, footer with the actor and `timestamp`, colour by status
  (success/failure/neutral); up to 5 link buttons as components. Limits: 6000 characters per embed, description
  4096, 25 fields, field value 1024, applied with the same "shrink until it fits" approach as `notify.fit`.
- **MAX:** text in `format: "html"` within 4000 characters, link buttons (at most 3 per row) in an
  `inline_keyboard` attachment, at most 2 messages per second per chat. The exact tag set MAX accepts is not
  documented; the renderer starts with the smallest common set (`b`, `i`, `code`, `a`, line breaks) and is
  confirmed against a live MAX bot before release.
- A plain-text rendering for logs and the panel.
- Golden tests per messenger (`*.telegram.rich.golden`, `*.telegram.classic.golden`, `*.discord.golden` as JSON,
  `*.max.golden`) for every fixture in every language.

### Outbox

- Webhook flow: verify → record the delivery → build the notifications → insert one outbox row per
  destination in the same transaction → answer `202`. The webhook response time no longer depends on any
  messenger.
- `outbox (id, destination_id, thread_id, event_type, repo_id, payload, attempts, next_attempt_at, last_error,
  state, created_at)`; `payload` is the `notify.Notification` as JSON plus the language, so a restart can
  resend it.
- One worker per messenger; per-destination ordering (a destination never has two messages in flight);
  per-chat and global rate limits from `Caps` with a small token bucket (no new dependency).
- Retry policy: rate limited → wait `retry_after`; temporary → 5 s, 15 s, 1 m, 5 m, 15 m, 1 h, then hourly;
  `dead` after 10 attempts; thread not found → resend to the main chat once (today's behaviour); in a Discord
  forum channel, which has no main chat, the post for that event type is recreated once instead *(design call)*;
  chat not found or bot removed → `dead` at once and the destination is marked inactive.
- *(Design call)* Outbox rows reference `deliveries.id`. A delivery is recorded as `queued`, then becomes
  `delivered` when all its rows are done or `failed` when any is dead. A redelivery of a `queued` or `delivered`
  delivery is a duplicate; a redelivery of a `failed` one requeues only the destinations whose rows went dead,
  so a redelivery from GitHub/GitLab stays the user's way to retry.
- Pending rows are picked up after a restart; `done` rows are pruned after 7 days, `dead` after 30.
- Metrics: `notifier_outbox_pending`, `notifier_outbox_dead`, `notifier_send_retries_total`; the existing
  `notifier_messages_sent_total{messenger,format,result}` keeps its meaning.
- The panel shows dead and pending messages (read-only).

### Configuration

`config.yaml` (found via `--config`, then `./config.yaml`, then `/etc/notifier/config.yaml`):

```yaml
server:   { listen: ":8080", public_url: "https://notify.example.com" }
panel:    { listen: "127.0.0.1:8081", allow_remote: false }
metrics:  { enabled: true, listen: "" }
storage:  { path: "data/notifier.db", deliveries_retention_days: 14 }
defaults: { language: en, notify_unknown_events: false }
admins:   [ "telegram:123456789", "discord:112233445566778899" ]
security: { webhook_secret: ${WEBHOOK_SECRET:-} }
messengers:
  telegram: { enabled: true, token: ${TELEGRAM_TOKEN}, api_url: "", proxy: "", rich_messages: true }
  discord:  { enabled: false, token: ${DISCORD_TOKEN}, application_id: "", public_key: "" }
  max:      { enabled: false, token: ${MAX_TOKEN} }
```

- `${VAR}` and `${VAR:-default}` from the environment; unknown keys are errors with their path; at least one
  messenger must be enabled; each adapter validates its own section.
- `gopkg.in/yaml.v3` is the one new dependency.
- 2.0 does not read `.env`. When `config.yaml` is missing and `.env` exists, start-up fails with the exact
  `notifier migrate-config` command to run.
- `migrate-config --from .env --to config.yaml` maps every 1.x variable (`BOT_TOKEN`, `TELEGRAM_API_URL`,
  `PROXY_*`, `SERVER_PORT`, `DOMAIN`/`USE_HTTPS`, `WEBHOOK_SECRET`, `DB_PATH`, `BOT_LANG`, `CHAT_ID`,
  `PANEL_*`, `METRICS_*`, `RICH_MESSAGES`, `DELIVERIES_RETENTION_DAYS`, `NOTIFY_UNKNOWN_EVENTS`). `CHAT_ID`, the
  1.x fallback chat, has no 2.0 equivalent (see "Unconnected repositories"); the migrated file carries a comment
  saying so.
- `scripts/install.sh update` and the Docker image run the migration automatically on the first 2.0 start.

### CLI

`notifier [serve] | version | config check | migrate-config | db backup <path> | help`, built on the standard
`flag` package with one `FlagSet` per subcommand. *(Design call)* `--config` now names `config.yaml`; given a
`.env` file (1.x meaning) it fails with the `migrate-config` command to run. `--db` and `--lang` keep working
with a deprecation warning and override `storage.path` and `defaults.language`. `install.sh update` migrates the
config and rewrites the systemd unit, so unattended installations keep starting.

### Panel, metrics, docs

- The panel lists destinations grouped by messenger, with their subscriptions, threads and routes, and the
  outbox; still read-only and loopback-only.
- `docs/MIGRATION-2.0.md` (systemd and Docker, rollback to 1.1.x with the database backup taken by the
  migration), README restructured (quick start per messenger, configuration, commands, panel, monitoring,
  upgrading), `docs/ARCHITECTURE.md` (model, how to add a messenger or an event), `config.example.yaml` with a
  comment on every field, CHANGELOG with **Breaking changes** first.
- The README says plainly that a MAX bot can only be created by a verified legal entity or individual
  entrepreneur (MAX for partners), and how to create the Discord application.

## Testing

- Unit tests with no network: storage and migrations (a 1.0.x and a 1.1.x database fixture migrate without
  loss), config parsing and `migrate-config` (`.env.example` → expected YAML), commands against a fake
  messenger, outbox with a fake clock and a fake adapter (outage, `429`, ordering, restart, dead letters),
  Discord signature verification, each renderer against goldens and its limits.
- The Telegram goldens from 1.1.1 must pass unchanged.
- Manual checks before release: all fixtures sent to a Telegram test group, a Discord test server (text channel
  and forum channel) and a MAX test chat; upgrade of a real 1.1.1 installation (systemd and Docker).

## Left to the builder

Package and type names, the exact interface signatures, SQL details, the order of the work and how it is split
into PRs, Discord component layout, the token bucket implementation, panel layout.
