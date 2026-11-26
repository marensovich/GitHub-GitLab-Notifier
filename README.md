# GitHub-GitLab Notifier

[![CI](https://github.com/marensovich/GitHub-GitLab-Notifier/actions/workflows/ci.yml/badge.svg)](https://github.com/marensovich/GitHub-GitLab-Notifier/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/marensovich/GitHub-GitLab-Notifier?sort=semver)](https://github.com/marensovich/GitHub-GitLab-Notifier/releases)
[![Docker](https://img.shields.io/badge/ghcr.io-amd64%20%7C%20arm64%20%7C%20armv7-2496ED?logo=docker&logoColor=white)](https://github.com/marensovich/GitHub-GitLab-Notifier/pkgs/container/github-gitlab-notifier)
![Go](https://img.shields.io/badge/go-%2300ADD8.svg?logo=go&logoColor=white)
![Telegram](https://img.shields.io/badge/Telegram-2CA5E0?logo=telegram&logoColor=white)
![Discord](https://img.shields.io/badge/Discord-5865F2?logo=discord&logoColor=white)
![MAX](https://img.shields.io/badge/MAX-7B3FE4)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

A lightweight, self-hosted bot that delivers real-time notifications from your **GitHub** and **GitLab**
repositories to **Telegram**, **Discord** and **MAX** chats — in a Telegram forum or a Discord forum every kind of
event gets its own topic.

**Why?** Because sometimes you don't want to check email or open an app — you just want a message when something
happens in your repo.

- 🔔 **19 event types** from GitHub and GitLab: pushes, tags, pull/merge requests, reviews, issues, comments,
  releases, CI pipelines, stars, forks
- 💬 **Three messengers, one bot**: Telegram, Discord and MAX, alone or together; a repository can post to
  several chats
- 🗂 **Forum topics**: Stars, Pushes, Pull Requests, Issues, Releases and CI/CD are created automatically
- 📬 **Send queue**: every message is stored first and retried, so nothing is lost on a restart or an outage
- 🔔 **Personal DMs** about review requests, assignments, mentions and your failed pipelines
- 🔐 **Signed webhooks**: GitHub HMAC-SHA256 signatures and GitLab secret tokens are verified
- 🌍 **Per-chat language**: English and Russian, switchable with `/lang`
- 🧩 **Single static binary**: embedded translations, pure-Go SQLite, no runtime dependencies
- 🐧 **One-command Linux install** as a hardened systemd service, or run it with Docker
- 🏠 **Self-hosted GitLab** and self-hosted Telegram Bot API server supported

Upgrading from 1.x? See [docs/MIGRATION-2.0.md](docs/MIGRATION-2.0.md).

---

## Table of contents

- [Screenshots](#-screenshots)
- [Supported events](#-supported-events)
- [Quick start](#-quick-start)
- [Discord](#-discord)
- [MAX](#-max)
- [Installation on Linux (systemd)](#-installation-on-linux-systemd)
- [Running with Docker](#-running-with-docker)
- [Building from source](#-building-from-source)
- [Bot commands](#-bot-commands)
- [Personal DMs](#-personal-dms)
- [Configuration](#-configuration)
- [Web panel](#-web-panel)
- [Monitoring](#-monitoring)
- [Webhook security](#-webhook-security)
- [How notifications are routed](#-how-notifications-are-routed)
- [HTTPS with a reverse proxy](#-https-with-a-reverse-proxy)
- [Project structure](#-project-structure)
- [Upgrading from 1.x](#-upgrading-from-1x)
- [Development](#-development)
- [Troubleshooting](#-troubleshooting)

---

## 📸 Screenshots

| New commits                   | New star                        |
|:------------------------------|:--------------------------------|
| ![New commit](images/img.png) | ![New star](images/img_1.png)   |

| Automatic forum setup             | Connecting a repository             |
|:----------------------------------|:------------------------------------|
| ![Forum setup](images/img_2.png)  | ![Connect repo](images/img_3.png)   |

---

## 🔔 Supported events

### GitHub (`X-GitHub-Event`)

| Event                 | Reported actions                                                     | Topic            |
|:----------------------|:---------------------------------------------------------------------|:-----------------|
| `push`                | new commits, force push, branch created/deleted, tag created/deleted | 🚀 Pushes        |
| `pull_request`        | opened (incl. draft), closed, merged, reopened, ready for review     | 🔀 Pull Requests |
| `pull_request_review` | approved, changes requested, commented                               | 🔀 Pull Requests |
| `issues`              | opened, closed (incl. "not planned"), reopened                       | 🐛 Issues        |
| `issue_comment`       | new comment on an issue or pull request                              | 🐛 Issues        |
| `release`             | published release / pre-release                                      | 📦 Releases      |
| `workflow_run`        | completed GitHub Actions run: success, failure, cancelled            | ⚙️ CI/CD         |
| `star`                | starred, unstarred                                                   | ⭐ Stars          |
| `fork`                | repository forked                                                    | ⭐ Stars          |
| `ping`                | webhook created (setup confirmation)                                 | General          |

### GitLab (`X-Gitlab-Event`)

| Event                                         | Reported actions                                  | Topic            |
|:----------------------------------------------|:--------------------------------------------------|:-----------------|
| `Push Hook`                                   | new commits, branch created/deleted               | 🚀 Pushes        |
| `Tag Push Hook`                               | tag created/deleted                               | 🚀 Pushes        |
| `Merge Request Hook`                          | opened (incl. draft), closed, merged, reopened, approved | 🔀 Pull Requests |
| `Issue Hook`, `Confidential Issue Hook`       | opened, closed, reopened                          | 🐛 Issues        |
| `Note Hook`, `Confidential Note Hook`         | comment on an issue, merge request or commit      | 🐛 Issues        |
| `Pipeline Hook`                               | finished pipeline: success, failed, canceled      | ⚙️ CI/CD         |
| `Release Hook`                                | release created                                   | 📦 Releases      |

Noisy actions (labels, assignments, edits, PR synchronization, in-progress pipelines, …) are intentionally skipped.
Events the bot does not know (e.g. `check_suite`, `deployment`) are ignored, so you can safely select
**"Send me everything"** on GitHub. Set `defaults.notify_unknown_events: true` to get a short message for them
instead.

---

## 🚀 Quick start

This is the Telegram setup. For Discord and MAX, install the bot the same way and follow [Discord](#-discord) or
[MAX](#-max) instead of steps 1–2.

### 1. Create a Telegram bot

1. Open [@BotFather](https://t.me/BotFather) and send `/newbot`
2. Choose a name and a username
3. Copy the **token**

### 2. Create a forum group

1. Create a Telegram group and enable **Topics** (group settings → *Topics*)
2. Add your bot to the group
3. **Make the bot an administrator** with the *Manage topics* right

> Regular groups and private chats work too, but all notifications then arrive in one chat without topics.

### 3. Install the bot

Pick one:

- **Linux server (recommended):** `curl -fsSL https://raw.githubusercontent.com/marensovich/GitHub-GitLab-Notifier/master/scripts/install.sh | sudo bash`
  — see [Installation on Linux](#-installation-on-linux-systemd)
- **Docker:** see [Running with Docker](#-running-with-docker)
- **From source:** see [Building from source](#-building-from-source)

The bot must be reachable from the internet (GitHub/GitLab send webhooks to it). Set `server.public_url` in
`config.yaml` to the address they should use, e.g. `https://bot.example.com` or `http://203.0.113.5:8080`.

### 4. Set up the chat

In the forum group, send:

```
/configure
/connect https://github.com/your-username/your-repo
```

`/configure` asks two questions with buttons — whether to create a topic per event type (forum chats only) and
which look to use — and `/connect` replies with the **webhook URL** and a **secret**.

**Message look.** The new look uses Telegram *rich messages*: a heading, a compact table of details, collapsible
commit and file lists and coloured buttons. It needs an up-to-date official Telegram app; old and third-party
clients show it as an unsupported message. If your chat reads notifications there, pick **Classic** in
`/configure` — the same information as plain formatted text with inline buttons.
`messengers.telegram.rich_messages: false` sends every chat the classic look.

### 5. Add the webhook

**GitHub:** repository → **Settings** → **Webhooks** → **Add webhook**

| Field        | Value                                               |
|:-------------|:----------------------------------------------------|
| Payload URL  | URL from `/connect`                                 |
| Content type | `application/json`                                  |
| Secret       | secret from `/connect`                              |
| Events       | *Send me everything* (or pick the ones you need)    |

**GitLab:** project → **Settings** → **Webhooks** → **Add new webhook**

| Field        | Value                                                                       |
|:-------------|:----------------------------------------------------------------------------|
| URL          | URL from `/connect`                                                         |
| Secret token | secret from `/connect`                                                      |
| Triggers     | Push, Tag push, Merge request, Issues, Comments, Pipeline, Releases          |

GitHub immediately sends a `ping` event — a "Webhook connected" message appears in the chat. 🎉

---

## 💬 Discord

The same bot can post to Discord channels and forums, next to Telegram or on its own. Discord reaches the bot
over HTTPS, so `server.public_url` must be reachable from the internet.

1. Open the [Discord developer portal](https://discord.com/developers/applications) → **New Application**.
2. **Bot** → **Reset Token** and copy it. No privileged intents are needed.
3. **General Information**: copy the **Application ID** and the **Public Key**.
4. Fill in `config.yaml` and restart the bot:

   ```yaml
   messengers:
     discord:
       enabled: true
       token: "${DISCORD_TOKEN}"
       application_id: "112233445566778899"
       public_key: "<64 hex characters>"
   ```

5. Back in **General Information**, set **Interactions Endpoint URL** to
   `<server.public_url>/discord/interactions` and save; Discord checks it right away, so the bot must be running.
6. Invite the bot with the link from the log (`🔗 Discord: invite the bot with …`). It asks for: view channels,
   send messages, embed links, read message history, create public threads and send messages in threads.

Commands are slash commands (`/connect`, `/configure`, `/status`, …) and work in server channels only. Only the
person who typed a command sees the answer. `/configure` needs the **Manage Channels** permission.

In a **forum channel** every category of events gets its own post, created by `/configure` (type it inside any
post of the forum); a post that is deleted is created again with the next event. In a text channel everything
goes to the channel.

---

## 🟣 MAX

The bot can post to MAX group chats too. MAX bots are made on [MAX for partners](https://business.max.ru), and
only a **verified legal entity or individual entrepreneur** can create one there; a private person cannot.

1. Create the bot on business.max.ru and copy its token (**Chat bots** → the bot → **Settings**).
2. Fill in `config.yaml` and restart the bot:

   ```yaml
   messengers:
     max:
       enabled: true
       token: "${MAX_TOKEN}"
   ```

3. Add the bot to a group chat and **make it an administrator**: MAX shows the members and their roles only to an
   administrator, and the bot needs them to check `/configure` and `/connect` and private-repository DMs.

When `server.public_url` is HTTPS on port 443 (no other port; MAX does not accept self-signed certificates), MAX
posts updates to `<server.public_url>/max/updates`; the bot subscribes on start and checks a secret on every
request. Otherwise the bot polls MAX for updates — fine for a test, MAX recommends the webhook in production.

Commands are ordinary messages (`/connect …`, `/status`, …) in the group or in the dialog with the bot. MAX has no
replies seen by one person only and no threads, so answers are visible to the chat and every event goes to the
chat itself. `api_url` replaces the API base (`https://platform-api.max.ru`); `platform-api2.max.ru` works only on
a system that trusts the Russian Trusted Root CA.

---

## 🐧 Installation on Linux (systemd)

`scripts/install.sh` installs the bot as a systemd service that starts on boot and restarts automatically on failure.
It works on any systemd-based distribution (Debian, Ubuntu, Fedora, RHEL/Rocky/Alma, Arch, openSUSE, Raspberry Pi OS)
on **amd64**, **arm64** and **armv7** (32-bit Raspberry Pi).

### Install

Latest release, interactive setup:

```bash
curl -fsSL https://raw.githubusercontent.com/marensovich/GitHub-GitLab-Notifier/master/scripts/install.sh | sudo bash
```

From a cloned repository (builds from source, requires Go 1.26+):

```bash
git clone https://github.com/marensovich/GitHub-GitLab-Notifier.git
cd GitHub-GitLab-Notifier
sudo ./scripts/install.sh install --from-source
```

Unattended (e.g. Ansible, cloud-init) — settings come from environment variables:

```bash
sudo BOT_TOKEN=123456:ABC... DOMAIN=bot.example.com USE_HTTPS=true \
     ./scripts/install.sh install --non-interactive
```

The installer:

1. downloads the release binary for your architecture (or builds it / takes `--binary <path>`);
2. creates the unprivileged system user `notifier`;
3. asks for the Telegram bot token, port, domain, language, … and writes `config.yaml`;
4. installs and enables the `github-gitlab-notifier` systemd unit, starts it and checks `/health`.

| What          | Where                                                 |
|:--------------|:------------------------------------------------------|
| Binary        | `/opt/github-gitlab-notifier/github-gitlab-notifier`  |
| Configuration | `/etc/github-gitlab-notifier/config.yaml` (`0640`)    |
| Database      | `/var/lib/github-gitlab-notifier/notifier.db`         |
| Unit          | `/etc/systemd/system/github-gitlab-notifier.service`  |

### Manage

```bash
sudo systemctl status github-gitlab-notifier
```

```bash
sudo systemctl restart github-gitlab-notifier   # after editing the config
```

```bash
sudo journalctl -u github-gitlab-notifier -f    # live logs
```

```bash
sudo ./scripts/install.sh update                # new version, automatic rollback if it fails to start
```

```bash
sudo ./scripts/install.sh uninstall --purge     # remove everything incl. config and database
```

| Option              | Description                                                   |
|:--------------------|:--------------------------------------------------------------|
| `--version vX.Y.Z`  | Install a specific release instead of the latest              |
| `--from-source`     | Build from the local checkout with Go                         |
| `--binary <path>`   | Install an already built binary                               |
| `--non-interactive` | Never prompt; read settings from environment variables        |
| `--purge`           | With `uninstall`: also delete configuration, data and user    |

Discord and MAX are turned on in `config.yaml` after the install (see [Discord](#-discord), [MAX](#-max)), then
`sudo systemctl restart github-gitlab-notifier`. `update` from 1.x converts `notifier.env` to `config.yaml` — see
[Upgrading from 1.x](#-upgrading-from-1x).

The unit is sandboxed (`ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `NoNewPrivileges`, no capabilities, only
the data directory writable). Ports below 1024 get only `CAP_NET_BIND_SERVICE`.

---

## 🐳 Running with Docker

A ready image for `linux/amd64`, `linux/arm64` (Raspberry Pi 4/5, ARM VPS) and `linux/arm/v7` is published to
GitHub Container Registry: `ghcr.io/marensovich/github-gitlab-notifier` (distroless, non-root, ~24 MB).

With Docker Compose (uses the `docker-compose.yml` from this repository):

```bash
cp config.example.yaml config.yaml   # set server.public_url; data/notifier.db lands in the /data volume
echo 'TELEGRAM_TOKEN=123456:ABC...' > .env
docker compose up -d
```

`config.yaml` is mounted at `/etc/notifier/config.yaml`; the `${TELEGRAM_TOKEN}` it refers to comes from `.env`.
Keep `server.listen` on `":8080"` (or change the port mapping with it).

Or with plain Docker:

```bash
docker run -d --name notifier --restart unless-stopped \
  --env-file .env -p 8080:8080 -v notifier-data:/data \
  -v "$PWD/config.yaml:/etc/notifier/config.yaml:ro" \
  ghcr.io/marensovich/github-gitlab-notifier:latest
```

| Tag               | What                                                         |
|:------------------|:-------------------------------------------------------------|
| `latest`          | Latest stable release                                        |
| `2.0`, `2.0.0`    | A minor line (gets patch updates) or an exact version        |
| `edge`            | Latest `development` build — may be unstable                 |

The database is stored in the `/data` volume. Logs: `docker compose logs -f`. Update: `docker compose pull &&
docker compose up -d`. To build the image yourself from a checkout: `docker compose up -d --build`.

---

## 🛠 Building from source

Requires **Go 1.26+**.

```bash
git clone https://github.com/marensovich/GitHub-GitLab-Notifier.git
cd GitHub-GitLab-Notifier
cp config.example.yaml config.yaml    # edit it
make build
./GitHub-GitLab-Notifier
```

The binary has a few subcommands (`notifier help` lists them):

| Command                         | What it does                                                          |
|:--------------------------------|:----------------------------------------------------------------------|
| `serve [--config <file>]`       | Run the bot (the default). Without `--config`: `./config.yaml`, then `/etc/notifier/config.yaml` |
| `config check [--config <file>]` | Validate the configuration and exit                                  |
| `migrate-config --from <.env> --to <config.yaml>` | Convert a 1.x `.env` (`--force` overwrites, `--keep-secrets-in-env` writes `${TELEGRAM_TOKEN}` instead of the token) |
| `db backup <path>`              | Write a consistent copy of the database, also while the bot runs      |
| `version`                       | Print the version                                                     |

`--db` and `--lang` from 1.x still work but are deprecated.

Pre-built binaries for Linux, macOS, Windows and FreeBSD are attached to every
[release](https://github.com/marensovich/GitHub-GitLab-Notifier/releases).

---

## 🤖 Bot commands

| Command               | Description                                                       |
|:----------------------|:------------------------------------------------------------------|
| `/start`              | Welcome message and quick start                                   |
| `/help`               | List of commands                                                  |
| `/configure`          | Set up topics (forums) and the message look with buttons; run again to change |
| `/connect <url>`      | Connect a repository: returns webhook URL, secret and instructions |
| `/disconnect <url>`   | Disconnect a repository from this chat                            |
| `/status`             | Chat settings, topics, connected repositories, version            |
| `/lang en` / `/lang ru` | Change the language of this chat                                |
| `/link github <token>` / `/link gitlab <token> [host]` | Link your account for [personal DMs](#-personal-dms) |
| `/unlink github\|gitlab [login]` | Remove your link (a bot administrator may name anyone's login) |
| `/me`                 | Your linked accounts, with buttons to turn each kind of DM off or on |
| `/version`            | Bot version                                                       |

The commands also appear in Telegram's **Menu** button. In Discord they are slash commands; in MAX they are typed
as messages.

---

## 🔔 Personal DMs

Anyone in a chat can link their GitHub or GitLab account, and the bot writes to them privately when:

- 👀 their review is requested;
- 👤 they are assigned to a pull/merge request or an issue;
- 🔔 they are mentioned (`@login` in an issue, a pull/merge request, a comment or a review; code blocks are skipped,
  team mentions like `@org/team` are ignored);
- ❌ a pipeline they started fails.

Nobody gets a DM about their own action, except their own failed pipeline. Assignments and review requests go only
to DMs; chats keep getting the same messages as before.

**Linking.** The token proves the account is yours. It is used for one request to learn who you are and is never
stored or logged:

- GitHub: a [fine-grained token](https://github.com/settings/personal-access-tokens/new) with **no permissions**
  (public repositories only is enough). For GitHub Enterprise add the host: `/link github <token> ghe.example.com`.
- GitLab: a personal access token with the **`read_user`** scope. The host may be left out when the bot knows one
  GitLab server; otherwise add it: `/link gitlab <token> gitlab.example.com`.

Where to send `/link`:

- **Telegram** — only in the private chat with the bot. In a group the bot refuses, deletes the message and asks you
  to revoke the token if others saw it. The bot deletes the message with the token in the private chat too.
- **Discord** — in a server channel or in the bot's DM; the slash command and its answer are seen only by you.
- **MAX** — only in the dialog with the bot. MAX does not let a bot delete your message there, so delete the
  message with the token yourself; the bot reminds you.

The DMs come only for repositories connected to some chat with `/connect`. For a **private** repository the bot
first checks that you are still a member of a chat it is connected to; someone who left the team gets nothing.
Turn kinds of DMs off and on with `/me`. The web panel lists every link on its **People** page.

---

## ⚙️ Configuration

Everything is set in `config.yaml`; [`config.example.yaml`](config.example.yaml) explains every field. `${VAR}`
takes a value from the environment (an error when it is not set) and `${VAR:-default}` falls back to a default, so
tokens can stay out of the file:

```yaml
server:
  listen: ":8080"                          # address the webhook server listens on
  public_url: "https://bot.example.com"    # what /connect puts into the webhook URL
panel:
  listen: "127.0.0.1:8081"                 # web panel; empty turns it off
metrics:
  enabled: true
  listen: ""                               # separate address for /metrics, e.g. 127.0.0.1:9090
storage:
  path: "data/notifier.db"
  deliveries_retention_days: 14
defaults:
  language: en                             # en or ru; each chat can change it with /lang
admins: ["telegram:123456789"]             # may connect a repository that another chat already has
security:
  webhook_secret: "${WEBHOOK_SECRET:-}"    # when set, unsigned webhooks are rejected
messengers:
  telegram:
    enabled: true
    token: "${TELEGRAM_TOKEN}"
    api_url: ""                            # self-hosted Bot API server
    proxy: ""                              # socks5://host:port or http://user:pass@host:port
    rich_messages: true
  discord:
    enabled: false                         # see Discord above
  max:
    enabled: false                         # see MAX above
```

Run `notifier config check --config config.yaml` after editing it. At least one messenger must be enabled.

---

## 🖥 Web panel

A small read-only panel shows what the bot is connected to: repositories and the chat each one posts to,
whether a webhook secret protects it, every chat with its topics and which events are routed to them, and the
accounts linked for personal DMs (no tokens are stored, so none can be shown).
Chats are grouped by messenger and marked when the bot lost access to them; the overview shows the send queue,
the last messages given up on and repositories that send webhooks but are connected to no chat.
It has no login, so it only listens on a loopback address.

Enable it in `config.yaml` and restart the bot:

```yaml
panel:
  listen: "127.0.0.1:8081"
```

Open it from your computer through an SSH tunnel:

```bash
ssh -L 8081:127.0.0.1:8081 user@your-server
```

then go to <http://localhost:8081>.

- The bot refuses to start if `panel.listen` is not a loopback address (`127.0.0.1`, `::1`, `localhost`),
  unless `panel.allow_remote: true` — use that only behind your own authentication.
- Requests with a non-loopback `Host` header are rejected, which blocks DNS rebinding.
- Only `GET`/`HEAD` are accepted; webhook secrets are never displayed, only whether one is set.
- Everything is embedded in the binary — the panel works without internet access.

---

## 📈 Monitoring

**Health.** `GET /health` (also `/healthz` and `/webhook/health`) on the webhook port returns JSON and `200` when
everything works, `503` when a check fails:

```json
{"status":"ok","version":"v2.0.0","uptime_seconds":3600,
 "checks":{"db":{"ok":true},"telegram":{"ok":true},"discord":{"ok":true}}}
```

`db` pings SQLite, and every enabled messenger has its own check. A messenger is healthy while the bot has had a
successful call to its API recently (every sent message counts). When it has not, the check asks the API itself,
at most once every 30 seconds, so frequent health checks never flood the messenger. A failing check carries its
error, for example:

```json
{"status":"fail","version":"v2.0.0","uptime_seconds":900,
 "checks":{"db":{"ok":true},"telegram":{"ok":false,"error":"no successful Telegram API call for 4m0s: ..."}}}
```

**Metrics.** `GET /metrics` serves Prometheus metrics (turn off with `metrics.enabled: false`):

| Metric                                                     | Type      | What                                                            |
|:-----------------------------------------------------------|:----------|:----------------------------------------------------------------|
| `notifier_webhooks_total{platform,event,status}`           | counter   | Webhooks by result: `delivered`, `ignored`, `rejected`, `failed`, `duplicate` |
| `notifier_webhook_duration_seconds`                        | histogram | Time to handle a webhook, sending included                      |
| `notifier_messages_sent_total{messenger,format,result}`    | counter   | Send attempts by format (`rich` / `classic`): `ok`, `error` (not delivered) or `retried` (a later attempt replaced it) |
| `notifier_rich_fallbacks_total`                            | counter   | Rich messages rejected by Telegram and sent as classic ones     |
| `notifier_outbox_pending`                                  | gauge     | Messages waiting in the send queue                              |
| `notifier_build_info{version}`, `notifier_start_time_seconds` | gauge  | Version and start time                                          |

Repository URLs are never used as labels. Event names that do not look like real ones, and new ones beyond
500 label sets, are counted as `event="other"`.

On a public server, keep the metrics off the webhook port:

```yaml
metrics:
  listen: "127.0.0.1:9090"
```

```yaml
# prometheus.yml
scrape_configs:
  - job_name: notifier
    static_configs:
      - targets: ["127.0.0.1:9090"]
```

A ready Grafana dashboard is in [`docs/grafana-dashboard.json`](docs/grafana-dashboard.json)
(Dashboards → New → Import).

---

## 🔐 Webhook security

Every repository connected with `/connect` gets its own random **secret**:

- **GitHub** signs each delivery with it (`X-Hub-Signature-256`, HMAC-SHA256) — the bot verifies the signature;
- **GitLab** sends it in `X-Gitlab-Token` — the bot compares it in constant time.

A request with a wrong signature/token is rejected with `401`. Webhooks created without a secret keep working,
unless you set the global `security.webhook_secret` — then **every** webhook must be signed with either the repository
secret or that one. This is recommended for public servers.

### Redeliveries

Each incoming webhook is written to a delivery log in the database — only metadata (delivery ID, event, repository,
result, time), never the payload. If GitHub (**Redeliver**) or GitLab (**Resend request**, automatic retries) sends a
delivery that was already posted, the bot answers `200 DUPLICATE` and posts nothing. A delivery whose messages all
failed to send is not treated as delivered, so redelivering it tries again. The delivery ID comes from
`X-GitHub-Delivery`, and from `Idempotency-Key` or `X-Gitlab-Event-UUID` on GitLab; requests without one are never
dropped. The log is pruned after `storage.deliveries_retention_days` (14 by default).

---

## 🧭 How notifications are routed

1. The repository URL is taken from the payload (`repository.html_url` / `project.web_url`).
2. The webhook is checked against the repository's secret, then the messages are written to the send queue and the
   platform gets `202 QUEUED`. A repository that no chat connected with `/connect` gets `202 SKIPPED` and is listed
   on the web panel; nothing is posted.
3. One message is queued for every chat subscribed to the repository, in that chat's language, plus personal DMs
   for the people the event concerns.
4. One worker per messenger sends the queue, in order within each chat and within the messenger's rate limits. A
   forum chat gets the message in the topic (Telegram) or post (Discord) of the event category; a deleted one falls
   back to the main chat (Telegram) or is created again (Discord).
5. Telegram sends the look the chat chose; a rich message Telegram rejects is sent again in the classic look.
6. A failed send is retried with growing delays, up to 10 attempts. A chat the bot was removed from is marked
   inactive and gets nothing until a command is used in it again; a Telegram group that became a supergroup is
   followed to its new ID.

---

## 🔒 HTTPS with a reverse proxy

GitHub and GitLab recommend HTTPS for webhooks. Run the bot on localhost and put a reverse proxy in front of it:

**Caddy** (automatic Let's Encrypt certificates):

```
bot.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

**nginx:**

```nginx
server {
    listen 443 ssl;
    server_name bot.example.com;
    # ssl_certificate ... / ssl_certificate_key ...

    location /webhook {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
    }
}
```

Then set `server.listen: "127.0.0.1:8080"` and `server.public_url: "https://bot.example.com"`. Discord and MAX
call the bot too, so proxy `/discord/interactions` and `/max/updates` as well when you use them (the Caddy example
proxies everything).

---

## 📁 Project structure

```
.
├── main.go, cmd.go            # entry point and subcommands: config, wiring, graceful shutdown
├── config.example.yaml        # every setting, commented
├── locales/                   # translations (embedded into the binary)
├── internal/
│   ├── config/                # config.yaml loading, ${VAR} expansion, 1.x .env conversion
│   ├── commands/              # chat commands and buttons, the same for every messenger
│   ├── dispatcher/            # routes an event to its handler
│   ├── handler/               # one handler per GitHub/GitLab event → notify.Notification
│   │   └── testdata/          # payload fixtures + .golden messages (<platform>/<event>/<case>)
│   ├── notify/                # the messenger-neutral notification
│   ├── messenger/             # the adapter interface and error classes
│   │   ├── telegram/          # rich and classic HTML, topics, ephemeral answers
│   │   ├── discord/           # embeds, forum posts, slash commands over HTTPS
│   │   └── max/               # MAX REST client, webhook or long polling
│   ├── outbox/                # the send queue: workers, rate limits, retries
│   ├── personal/              # people an event concerns, /link token checks
│   ├── server/                # webhook endpoint, signatures, health
│   ├── web/                   # local read-only web panel (templates and CSS embedded)
│   ├── metrics/               # Prometheus metrics
│   ├── i18n/                  # translation loading and lookup
│   └── platform/              # platforms, event types, categories
├── storage/                   # SQLite and its migrations
├── scripts/install.sh         # Linux systemd installer
├── docs/                      # migration guide, architecture, Grafana dashboard
├── Dockerfile, docker-compose.yml
└── .github/workflows/         # CI and release pipelines
```

How the parts fit together, and how to add a messenger or an event: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

### Database

SQLite file (`storage.path`, default `data/notifier.db`), upgraded automatically on start:

| Table           | Description                                                    |
|:----------------|:---------------------------------------------------------------|
| `repos_v2`      | Connected repositories and their secrets                       |
| `destinations`  | Chats per messenger: language, look, topics on/off, inactive   |
| `threads`, `routes` | Topics/posts of a chat and which event category goes where |
| `subscriptions` | Which chat gets which repository                               |
| `outbox`        | The send queue                                                 |
| `people`, `accounts` | Linked GitHub/GitLab accounts for personal DMs            |
| `deliveries`    | Webhook delivery log (metadata only)                           |

`notifier db backup <path>` copies it safely while the bot runs.

---

## ⬆️ Upgrading from 1.x

2.0 reads `config.yaml` instead of `.env`; chats, repositories, secrets and settings carry over. `install.sh update`
converts the configuration and the database and rolls back by itself if 2.0 does not start. Docker, the
`CHAT_ID` change, and going back to 1.1.x are in [docs/MIGRATION-2.0.md](docs/MIGRATION-2.0.md).

---

## 👩‍💻 Development

```bash
make help     # list all targets
make test     # run tests
make golden   # regenerate handler golden files after an intended message change
make lint     # gofmt + go vet + shellcheck
make run      # run locally with ./config.yaml
```

Want another event? Adding a handler takes about 50 lines of Go — see [CONTRIBUTING.md](CONTRIBUTING.md).

---

## 🩺 Troubleshooting

| Problem                                   | Solution                                                                    |
|:------------------------------------------|:----------------------------------------------------------------------------|
| Topics are not created                    | The bot must be an **administrator** with the *Manage topics* right          |
| `/configure` does not offer topics        | Enable **Topics** in the group settings, then run `/configure` again         |
| Messages show as "unsupported"            | The client is old or third-party: pick **Classic** in `/configure`           |
| Webhook shows a delivery error on GitHub  | The URL must be reachable from the internet — check `server.public_url`, port, firewall |
| GitHub shows `401 Unauthorized`           | The webhook secret does not match the one from `/connect` (or `security.webhook_secret`) |
| GitHub shows `202 SKIPPED`                | No chat connected the repository: run `/connect <url>` in the chat           |
| Notifications go to the General topic     | Run `/configure` again and choose *Create topics* — it creates missing topics and restores the mapping |
| No message for an event                   | Check that the event is [supported](#-supported-events) and selected in the webhook settings |
| Wrong address in `/connect` output        | Set `server.public_url` in `config.yaml`                                     |
| Bot can't connect to Telegram             | Set `messengers.telegram.proxy` or `api_url`                                 |
| Discord says the interactions URL is invalid | The bot must be running and reachable at `<public_url>/discord/interactions` over HTTPS, with the right `public_key` |
| A chat stopped getting messages           | The panel marks it inactive (the bot was removed or lost rights): add the bot back and send any command there |
| Bot does not start after the 1.x update   | `notifier config check`; a `.env` passed to `--config` must be converted with `migrate-config` |
| Service does not start                    | `sudo journalctl -u github-gitlab-notifier -n 50`                           |

---

## Tech stack

- **Go** — single static binary, cross-compiled for every platform
- **[telego](https://github.com/mymmrac/telego)** — Telegram Bot API client
- **[modernc.org/sqlite](https://modernc.org/sqlite)** — pure Go SQLite, no CGO
- **[yaml.v3](https://gopkg.in/yaml.v3)** — `config.yaml`
- Discord and MAX are spoken to over their HTTP APIs with the standard library, no SDKs

## License

MIT © [marensovich](https://github.com/marensovich)
