# GitHub-GitLab Notifier

![Go](https://img.shields.io/badge/go-%2300ADD8.svg?style=for-the-badge&logo=go&logoColor=white)
![Telegram](https://img.shields.io/badge/Telegram-2CA5E0?style=for-the-badge&logo=telegram&logoColor=white)

A lightweight, self-hosted Telegram bot that sends real-time notifications from your GitHub and GitLab repositories directly to your Telegram chat.

**Why?** Because sometimes you don't want to check email or open an app — you just want a message when something happens in your repo.

---

## Features

- **GitHub webhooks:** push, star
- **GitLab webhooks:** in development
- **Auto-detect platform:** single webhook endpoint for both GitHub and GitLab
- **Long Polling:** bot listens for commands like `/start`, `/help`, `/connect`
- **Proxy support:** works behind HTTP or SOCKS5 proxies (useful in restricted networks)
- **i18n:** Russian and English (easy to add more)
- **Cross-platform:** Linux, macOS, Windows, FreeBSD — single binary, no dependencies
- **No database required:** notifications go straight to your Telegram chat
- **MIT licensed:** free for any use

---

## Quick Start

### Download binary

Go to [Releases](https://github.com/marensovich/github-gitlab-notifier/releases) and download the binary for your platform.

### Or build from source

```bash
git clone https://github.com/marensovich/github-gitlab-notifier.git
cd github-gitlab-notifier
go mod tidy
make build
```

### Configure

```bash
cp .env.example .env
```

Edit `.env`:

```bash
BOT_TOKEN=token
TELEGRAM_BOT_USERNAME=username
CHAT_ID=chatid
SERVER_ADDRESS=0.0.0.0
SERVER_PORT=port

PROXY_HOST=127.0.0.1
PROXY_PORT=port
PROXY_TYPE=HTTP/SOCKS
PROXY_ENABLED=true/false

DEV_MODE=true/false
```

### Run

```bash
./GitHub-GitLab-Notifier --lang=en
```

Expected output:

```
✅ Bot using direct connection
✅ Bot @YourGitBot registered. Session active
✅ Dispatcher initialized with 2 handlers
🚀 Bot @YourGitBot started, listening for commands...
🚀 Server started on port 8080 (version v0.0.1)
```

---

## Environment Variables

| Variable               | Description                                                  | Required | Default |
|:-----------------------|:-------------------------------------------------------------|:---------|:--------|
| `BOT_TOKEN`            | Telegram bot token from [@BotFather](https://t.me/BotFather) | **Yes**  | —       |
| `TELEGRAM_BOT_USERNAME`| Telegram bot username                                        | No       |  —       |
| `CHAT_ID`              | Telegram chat ID where notifications will be sent            | **Yes**  | —       |
| `SERVER_PORT`          | HTTP server port for webhooks                                | No       | `8080`  |
| `PROXY_ENABLED`        | Enable proxy (`true` or `false`)                             | No       | `false` |
| `PROXY_HOST`           | Proxy host (e.g., `127.0.0.1`)                               | No       | —       |
| `PROXY_PORT`           | Proxy port (e.g., `1080`)                                    | No       | —       |
| `PROXY_TYPE`           | Proxy type: `HTTP` or `SOCKS`                                | No       | `HTTP`  |

### How to get your Chat ID

1. Start a chat with your bot on Telegram
2. Send any message
3. Visit: `https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getUpdates`
4. Find `"chat":{"id":123456789}` in the response

---

## Setting Up Webhooks

### Single endpoint for both platforms

```
https://your-server.com/webhook
```

### GitHub

1. Go to your repository → **Settings** → **Webhooks** → **Add webhook**
2. **Payload URL:** `https://your-server.com/webhook`
3. **Content type:** `application/json`
4. **Events:** select what you need (recommended: `Send me everything`)
5. Click **Add webhook**

### GitLab

1. Go to your project → **Settings** → **Webhooks**
2. **URL:** `https://your-server.com/webhook`
3. **Trigger:** select events (Push, Merge Request, Issues, etc.)
4. Click **Add webhook**

The bot automatically detects the platform from request headers — no separate endpoints needed.

---

## Supported Events

### GitHub

| Event | Header (`X-GitHub-Event`) | Status |
|:---|:---|:---|
| Push | `push` | ✅ Implemented |
| Pull Request | `pull_request` | ⏳ Planned |
| Issues | `issues` |⏳ Planned |
| Star | `star` | ✅ Implemented |
| Fork | `fork` | ⏳ Planned |
| Release | `release` | ⏳ Planned |
| Workflow Run | `workflow_run` | ⏳ Planned |

### GitLab

| Event | Header (`X-Gitlab-Event`) | Status |
|:---|:---|:---|
| Push Hook | `Push Hook` | ⏳ Planned |
| Merge Request Hook | `Merge Request Hook` | ⏳ Planned |
| Issue Hook | `Issue Hook` | ⏳ Planned |
| Pipeline Hook | `Pipeline Hook` | ⏳ Planned |
| Release Hook | `Release Hook` | ⏳ Planned |

> Want more events? Contributions welcome! Adding a new event handler takes ~100 lines of Go — see [CONTRIBUTING.md](CONTRIBUTING.md).

---

## Proxy Setup

If Telegram API is blocked in your country, use a proxy:

```bash
PROXY_ENABLED=true
PROXY_HOST=127.0.0.1
PROXY_PORT=10808
PROXY_TYPE=HTTP
```

The bot will route all Telegram API calls through the proxy. Webhook endpoint is not affected — it still receives requests directly.

Tested with:
- [GoXRay VPN](https://github.com/XTLS/Xray-core) (TUN mode — no proxy config needed)
- [V2Ray](https://www.v2ray.com/) (SOCKS5 proxy)
- [Clash](https://github.com/Dreamacro/clash) (HTTP proxy)

---

## Command-Line Flags

| Flag | Description | Default |
|:---|:---|:--------|
| `--lang` | Language for notifications (`ru` or `en`) | `en`    |
| `--version` | Show version and exit | —       |

```bash
./GitHub-GitLab-Notifier --lang=ru
./GitHub-GitLab-Notifier --version
```

---

## Building from Source

### Prerequisites

- [Go](https://go.dev/dl/) 1.21 or higher
- `make`

### Build for current platform

```bash
make build
```

### Build for all platforms

```bash
make build-all
```

Outputs:

```
build/
├── GitHub-GitLab-Notifier-linux-amd64
├── GitHub-GitLab-Notifier-linux-arm64
├── GitHub-GitLab-Notifier-darwin-amd64
├── GitHub-GitLab-Notifier-darwin-arm64
├── GitHub-GitLab-Notifier-windows-amd64.exe
└── GitHub-GitLab-Notifier-freebsd-amd64
```

### Create release archives

```bash
make release
```

Outputs: `.tar.gz` for Unix, `.zip` for Windows.

---

## Local Testing

### Without a public server

Use [localhost.run](https://localhost.run/) or [serveo](https://serveo.net/) to expose your local port:

```bash
# No registration, no download — just SSH
ssh -R 80:localhost:8080 nokey@localhost.run
```

You'll get a public URL like:

```
https://random-string.localhost.run
```

Use it as your webhook URL: `https://random-string.localhost.run/webhook`

### Simulate a webhook

```bash
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: push" \
  -d '{
    "ref": "refs/heads/main",
    "pusher": {"name": "test"},
    "repository": {"full_name": "user/repo"},
    "commits": []
  }'
```

---

## Project Structure

```
.
├── main.go                  # Entry point
├── Makefile                 # Build scripts
├── build.sh                 # Alternative build script (bash)
├── .env.example             # Example environment file
├── locales/                 # Localization files
│   ├── ru.json
│   └── en.json
└── internal/
    ├── bot/
    │   └── bot.go           # Telegram bot (Long Polling)
    ├── handler/             # Webhook event handlers
    │   ├── handler.go       # EventHandler interface
    │   ├── github_push.go   # GitHub Push handler
    │   ├── github_star.go   # GitHub Star handler
    │   └── gitlab_push.go   # GitLab Push handler
    ├── dispatcher/
    │   └── dispatcher.go    # Routes events to handlers
    ├── platform/
    │   └── platform.go      # Platform & EventType definitions
    └── i18n/
        └── i18n.go          # Internationalization engine
```

---

## Adding a New Language

1. Copy `locales/en.json` to `locales/xx.json`
2. Translate all string values (keep the keys)
3. Add the language code in `internal/i18n/i18n.go`:

```go
const (
    RU Lang = "ru"
    EN Lang = "en"
    XX Lang = "xx" // your language
)
```

4. Add it to `Load()`:

```go
{XX, "xx.json"},
```

5. Add validation in `Valid()`:

```go
func Valid(lang Lang) bool {
    return lang == RU || lang == EN || lang == XX
}
```

Pull requests for new languages are welcome!

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.
1. Fork the repo
2. Create a branch (`feature/something`)
3. Run `go fmt ./...`
4. Open a Pull Request

---

## Tech Stack

- **Language:** Go 1.21+
- **Telegram library:** [telego](https://github.com/mymmrac/telego) — high-performance, fully compliant with Bot API
- **Environment:** [godotenv](https://github.com/joho/godotenv)
- **No database** — notifications go directly to Telegram

### Why Go?

- Single binary, no runtime dependencies
- Cross-compilation out of the box
- Fast startup (~10 MB binary, starts in milliseconds)
- Ideal for self-hosted DevOps tools

---

## License

MIT © [marensovich](https://github.com/marensovich)

See [LICENSE](LICENSE) for full text.

---
