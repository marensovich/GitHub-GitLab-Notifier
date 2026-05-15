# GitHub-GitLab Notifier

![Go](https://img.shields.io/badge/go-%2300ADD8.svg?style=for-the-badge&logo=go&logoColor=white)
![Telegram](https://img.shields.io/badge/Telegram-2CA5E0?style=for-the-badge&logo=telegram&logoColor=white)
![License](https://img.shields.io/badge/license-MIT-green?style=for-the-badge)

A lightweight, self-hosted Telegram bot that sends real-time notifications from your GitHub and GitLab repositories directly to your Telegram **Forum** chat.

**Why?** Because sometimes you don't want to check email or open an app — you just want a message when something happens in your repo.

---

## ⚠️ Important: Forum Required

This bot is designed to work with **Telegram Forum Groups** (topics enabled). It creates separate topics for each event type:

- ⭐ **Stars** — new stars on your repository
- 🚀 **Pushes** — new commits pushed

**Regular groups and channels are not supported** for automatic topic creation. You can still use them, but all notifications will arrive in the general chat (no topic separation).

### How to enable Forum mode

1. Open your Telegram group settings
2. Click **Edit** → **Administrators**
3. Enable **Topics**
4. Now your group is a Forum

---

## 🚀 Step-by-Step Setup

### 1. Create a Telegram bot

1. Open [@BotFather](https://t.me/BotFather)
2. Send `/newbot`
3. Choose a name and username
4. Copy the **token** you receive

### 2. Create a Forum group

1. Create a new Telegram group
2. Enable **Topics** in group settings
3. Add your bot to the group
4. **Make the bot an administrator** (required for creating topics)

### 3. Get your Chat ID

1. Send any message to the bot
2. Visit: `https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getUpdates`
3. Find `"chat":{"id":-1001234567890}` — this is your `CHAT_ID`

### 4. Clone and configure

```bash
git clone https://github.com/marensovich/github-gitlab-notifier.git
cd github-gitlab-notifier
cp .env.example .env
```

Edit `.env` with your values:

```bash
BOT_TOKEN=123456:ABCdefGHIjklMNOpqrsTUVwxyz
TELEGRAM_BOT_USERNAME=YourGitBot
CHAT_ID=-1001234567890
DOMAIN=your-server.com    # optional, for webhook URL
```

### 5. Build and run

```bash
make build
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

### 6. Configure the chat

Send these commands to your bot in Telegram:

```
/configure
```

This will:
- Save your chat in the database
- Create 4 forum topics (Stars, Pushes, Pull Requests, Issues)
- Map each event type to its topic

### 7. Connect a repository

```
/connect https://github.com/your-username/your-repo
```

The bot will respond with:
- Webhook URL
- Content type
- Setup instructions for GitHub/GitLab

### 8. Add the webhook on GitHub/GitLab

**GitHub:**
1. Go to your repository → **Settings** → **Webhooks** → **Add webhook**
2. **Payload URL:** the URL from `/connect` output
3. **Content type:** `application/json`
4. **Events:** `Send me everything`
5. Click **Add webhook**

**GitLab:**
1. Go to your project → **Settings** → **Webhooks**
2. **URL:** the URL from `/connect` output
3. **Trigger:** select events (Push, Merge Request, Issues, etc.)
4. Click **Add webhook**

### 9. Test

Make a push, create an issue, or star your repository. You should receive a notification in the corresponding forum topic.

---

## 🤖 Bot Commands

| Command | Description |
|:---|:---|
| `/start` | Welcome message and getting started |
| `/help` | List of available commands |
| `/configure` | Set up forum topics for notifications |
| `/connect <repo_url>` | Connect a GitHub/GitLab repository |
| `/status` | Show database contents (chats, topics, repos) |
| `/lang ru` or `/lang en` | Change notification language |
| `/version` | Show bot version |

---

## 📸 Screenshots

### New commit notification
![New commit](images/img.png)

### New star notification
![New star](images/img_1.png)

### Automatic forum setup
![Forum setup](images/img_2.png)

### Connecting a repository
![Connect repo](images/img_3.png)

### Unknown event fallback
![Unknown event](images/img_4.png)

---

## Features

- **GitHub webhooks:** push, star
- **GitLab webhooks:** in development
- **Auto-detect platform:** single webhook endpoint for both GitHub and GitLab
- **Long Polling:** bot listens for commands like `/start`, `/help`, `/connect`
- **Proxy support:** works behind HTTP or SOCKS5 proxies (useful in restricted networks)
- **i18n:** Russian and English (easy to add more)
- **Cross-platform:** Linux, macOS, Windows, FreeBSD — single binary, no dependencies
- **SQLite database:** stores chats, topics, repos, event mappings
- **MIT licensed:** free for any use

---

## Environment Variables

| Variable | Required | Default | Description |
|:---|:---|:---|:---|
| `BOT_TOKEN` | **Yes** | — | Telegram bot token from [@BotFather](https://t.me/BotFather) |
| `TELEGRAM_BOT_USERNAME` | **Yes** | — | Bot username without `@` (e.g., `YourGitBot`) |
| `CHAT_ID` | **Yes** | — | Telegram chat ID for notifications |
| `SERVER_ADDRESS` | No | `0.0.0.0` | Listen address (`0.0.0.0` = all, `127.0.0.1` = localhost) |
| `SERVER_PORT` | No | `8080` | HTTP server port for webhooks |
| `PROXY_ENABLED` | No | `false` | Enable proxy for Telegram API |
| `PROXY_HOST` | No | — | Proxy host (for restricted networks) |
| `PROXY_PORT` | No | — | Proxy port |
| `PROXY_TYPE` | No | `HTTP` | Proxy type: `HTTP` or `SOCKS` |
| `DEV_MODE` | No | `false` | Enable debug logging |
| `DOMAIN` | No | — | Public domain/IP for webhook URL generation |
| `USE_HTTPS` | No | `true` | Use `https://` in webhook URL |

---

## Supported Events

### GitHub

| Event | Header (`X-GitHub-Event`) | Status |
|:---|:---|:---|
| Push | `push` | ✅ Implemented |
| Star | `star` | ✅ Implemented |
| Pull Request | `pull_request` | ⏳ Planned |
| Issues | `issues` | ⏳ Planned |
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

> Want more events? Contributions welcome! Adding a new event handler takes ~100 lines of Go — see [CONTRIBUTING.md](CONTRIBUTING.md).

---

## Database

The bot uses **SQLite** (file: `data/notifier.db`).

| Table | Description |
|:---|:---|
| `chats` | Configured Telegram chats |
| `topics` | Created forum topics |
| `repos` | Connected repositories |
| `event_mapping` | Which event goes to which topic |

View data:
- Send `/status` to the bot in Telegram
- Or use SQLite CLI: `sqlite3 data/notifier.db "SELECT * FROM chats"`

---

## Troubleshooting

| Problem | Solution |
|:---|:---|
| "FOREIGN KEY constraint failed" | Run `/configure` again — chat must be saved before topics |
| Topics not created | Make sure the bot is **administrator** in the forum group |
| "This chat is not a forum" | Enable **Topics** in group settings |
| Webhook not receiving events | Check that URL is accessible from internet |
| Notifications go to general chat | Run `/configure` to create topics and map events |
| "Unknown event: watch" | This is GitHub's watch event — add a handler or ignore |
| Wrong IP in `/connect` output | Set `DOMAIN=your-server.com` in `.env` |
| Bot can't connect to Telegram | Enable proxy: `PROXY_ENABLED=true` |


---

## Tech Stack

- **Language:** Go 1.21+
- **Telegram library:** [telego](https://github.com/mymmrac/telego) — high-performance, fully compliant with Bot API
- **Database:** SQLite via [modernc.org/sqlite](https://modernc.org/sqlite) — pure Go, no CGO
- **Environment:** [godotenv](https://github.com/joho/godotenv)

### Why Go?

- Single binary, no runtime dependencies
- Cross-compilation out of the box
- Fast startup (~10 MB binary, starts in milliseconds)
- Ideal for self-hosted DevOps tools

---

## License

MIT © [marensovich](https://github.com/marensovich)
