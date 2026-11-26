# Architecture

One process, one SQLite file. A webhook becomes a messenger-neutral notification, is stored in a send queue and
is sent by the adapter of each messenger. Chat commands go the opposite way: an adapter turns a messenger's
update into a `messenger.Command`, and the core answers it the same way for every messenger.

```
GitHub / GitLab ──► server ──► dispatcher ──► handler ──► notify.Notification
                       │                                        │
                       │  one row per subscribed chat (+ DMs)   ▼
                       └──────────────────────────────►  storage.outbox
                                                                │
                                         outbox: a worker per messenger, rate limits, retries
                                                                │
                              ┌─────────────────────────────────┼──────────────────────────┐
                              ▼                                 ▼                          ▼
                    messenger/telegram                 messenger/discord             messenger/max
                              │                                 │                          │
                              └──────── updates: Command / Press ┴──────────────────────────┘
                                                                ▼
                                                commands.Core (/connect, /configure, …)
```

## Packages

| Package | Responsibility |
|:--------|:---------------|
| `main.go`, `cmd.go` | Subcommands, loading `config.yaml`, building the adapters and wiring everything together; on shutdown waits for the adapters and the queue before the database closes |
| `internal/config` | `config.yaml` with `${VAR}` expansion and validation; `migrate-config` from a 1.x `.env` |
| `internal/server` | `/webhook`: finds the repository, checks the signature, deduplicates deliveries, queues one item per subscribed chat and per concerned person; `/health`; mounts the Discord and MAX endpoints |
| `internal/dispatcher`, `internal/handler`, `internal/platform` | Event name → handler → `notify.Notification` in a given language; event types and their categories |
| `internal/notify` | The notification: heading, item, repository, fields, quote, sections, notes, buttons, actor, time. It says *what* to show, never *how* |
| `internal/outbox` | Sends queued items: one worker per messenger, in order per chat, per-chat and global rate limits, retries, inactive chats, threads created on demand |
| `internal/messenger` | The adapter interface (`Messenger`, `Caps`, `Command`, `Press`, `Reply`) and the error classes every adapter maps its errors to |
| `internal/messenger/{telegram,discord,max}` | Rendering, sending, receiving updates, permissions and health for one messenger |
| `internal/commands` | Every chat command and button, written once against `messenger.Messenger` |
| `internal/personal` | Who an event concerns (reviewers, assignees, mentions, pipeline author) and the `/link` token check |
| `storage` | SQLite access and the numbered migrations in `storage/migrations` |
| `internal/web` | The read-only panel |
| `internal/metrics`, `internal/i18n` | Prometheus text format; translations from `locales/` |

## Data model

- **Repository** (`repos_v2`): URL, platform, webhook secret.
- **Destination** (`destinations`): a chat in one messenger (`messenger`, `chat_id`), with its language, look,
  whether it uses threads, and `inactive` when the bot lost access. A private chat with a person is a destination
  too.
- **Thread** and **route** (`threads`, `routes`): a topic (Telegram) or forum post (Discord) of a destination,
  and which event category goes to which thread.
- **Subscription** (`subscriptions`): a repository posts to a destination. One repository can have many.
- **Outbox item** (`outbox`): one message for one destination: the notification with its look and language, the
  thread it should go to, the attempt count, the next attempt time, the last error and the state (`pending`,
  `sending`, `done`, `dead`).
- **Person** and **account** (`people`, `accounts`): a messenger user, their DM destination and the GitHub/GitLab
  accounts they linked.
- **Delivery** (`deliveries`): the webhook log used for deduplication and the panel; metadata only.

Migrations run on start, in order, each once. Before the first 2.0 migration a 1.x database is copied to
`<db>.1x-backup`.

## Sending and errors

An adapter's `Send` returns an error that `messenger.Classify` sorts into a class, and the outbox acts on the
class, not on the messenger:

| Class | Outbox reaction |
|:------|:----------------|
| rate limited | wait as long as the messenger asked, for that chat only |
| temporary | retry later with a growing delay, up to 10 attempts |
| thread not found | forget the thread and send to the main chat, or create it again where a forum has no main chat |
| needs thread | create the thread (through `commands.Core.MakeThread`) and send again |
| chat moved | store the chat's new ID and send there |
| chat gone | mark the destination inactive; a command in that chat makes it active again |
| rejected | the message itself is refused: give up on it, keep the chat |

The health of a messenger is its last successful API call; when there was none recently, `Health` asks the API,
at most once per 30 seconds (`messenger.Probe`).

## Adding a messenger

1. **Config.** Add a struct to `config.Messengers` in `internal/config/config.go`, a `Messenger…` name constant,
   validation, and a commented section in `config.example.yaml`.
2. **Adapter.** Create `internal/messenger/<name>` with a type that implements `messenger.Messenger`:
   - `Caps` says what the messenger can do (threads, looks, buttons per row, rate limits, hidden commands);
     the core reads it instead of checking which messenger it is talking to.
   - `Run` receives updates and calls `Handler.HandleCommand` / `HandlePress`. If updates come over HTTP, also
     implement `http.Handler` and mount it in `main.go` next to the Discord and MAX paths.
   - `Send` renders a `notify.Notification` in the messenger's markup and maps every API error to a
     `messenger` class (wrap `ErrChatGone`, `ErrRejected`, `ErrThreadNotFound`, return `*RateLimited`, …).
   - `Reply`, `Edit` and `Answer` show command answers; `Reply.Text` is the small HTML subset of the locales
     (`b`, `i`, `code`, `a`), converted to the messenger's markup.
   - Implement `DirectMessenger` for personal DMs and `MemberChecker` for private-repository checks if the
     messenger can do them.
   - Never put the token in an error or a log line.
3. **Wiring.** Build the adapter in `main.go`, add it to the `ms` map and start its `Run` with the others.
4. **Tests.** Test the adapter against an `httptest` server that imitates the API (see the existing adapters);
   `internal/messenger/fake` is the in-memory adapter the core and outbox tests use.
5. **Docs.** A section in the README and a line in `CHANGELOG.md`.

No schema change is needed: destinations are keyed by messenger name.

## Adding an event

Register the event type in `internal/platform`, write a handler in `internal/handler` that returns a
`notify.Notification`, add translations to every file in `locales/`, and add a payload fixture with its golden
file. Each adapter renders the notification, so a new event needs no messenger code. The step-by-step guide with
an example is in [CONTRIBUTING.md](../CONTRIBUTING.md#adding-a-new-event-handler). If the event should also reach
people privately, teach `personal.Extract` who it concerns.
