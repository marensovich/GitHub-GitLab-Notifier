# Notifier 2.0 foundation — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the Telegram-only 1.1.1 notifier into a messenger-neutral core (subscriptions, outbox,
`config.yaml`, CLI) with Telegram as its first adapter, behaving for Telegram users exactly as 1.1.1 does except
for the documented breaking changes.

**Architecture:** `storage` gets a messenger-neutral model (repos, destinations, threads, routes, subscriptions,
outbox). `internal/messenger` defines the adapter interface; `internal/messenger/telegram` is today's
`internal/bot` sending and Telegram I/O behind it. `internal/commands` implements every chat command once against
that interface. The webhook server enqueues into the outbox; `internal/outbox` delivers. `internal/config` loads
`config.yaml`; `main.go` becomes a small subcommand dispatcher.

**Tech Stack:** Go (toolchain from `go.mod`), telego v1.13.0, modernc.org/sqlite, new: `gopkg.in/yaml.v3`.
Removed at the end: `github.com/joho/godotenv`.

**Spec:** `docs/superpowers/specs/2026-11-17-v2-foundation-design.md`

This is plan 1 of 4. Plan 2 adds the Discord adapter, plan 3 the MAX adapter, plan 4 the docs, installer/Docker
docs and the 2.0.0 release. This plan leaves `development` releasable as a Telegram-only 2.0 build.

## Global Constraints

- All work lands in `development` through PRs from `feat/…` branches; no `next` branch. Hotfixes for 1.1.x come
  from `hotfix/…` off `master`.
- Commits: Angular subject without scope, detailed English body, `Refs NTF-xx.` last; **no** `Co-Authored-By`
  trailer and no "Generated with Claude Code" anywhere (commits, PRs, tags).
- Never print or log a bot token; tokens in errors and logs are masked (`internal/config.Mask`).
- Lint is `gofmt -l .` (empty) and `go vet ./...`; tests are `go test ./...` with no network.
- The Telegram goldens in `internal/handler/testdata/**/*.golden` must not change (`git diff --exit-code
  internal/handler/testdata` after every task).
- Only one new dependency: `gopkg.in/yaml.v3`. No Discord/MAX SDK, no ORM, no CLI framework.
- Chat and thread IDs are strings everywhere outside the Telegram adapter.
- Events of a repository with no subscriptions are dropped: `202`, delivery `ignored`, error `not connected`.
- Outbox retry delays: 5 s, 15 s, 1 m, 5 m, 15 m, 1 h, then hourly; `dead` after 10 attempts. `done` rows pruned
  after 7 days, `dead` after 30.
- Metric names: `notifier_outbox_pending`, `notifier_outbox_dead`, `notifier_send_retries_total`;
  `notifier_messages_sent_total{messenger,format,result}` keeps its meaning.
- Config lookup order: `--config`, `./config.yaml`, `/etc/notifier/config.yaml`.

## Review Focus

1. A 1.x database where a repo points at a chat row that no longer exists, or `event_mapping` points at a deleted
   topic — the migration must not fail and must not invent destinations (test in Task 2).
2. Two webhooks for the same destination arriving together, or a restart while a row is `sending` — messages
   must stay in order and a row must never be lost or sent twice by two workers (tests in Task 6).
3. A Telegram `429` with `retry_after` while other chats are healthy — only that chat waits (test in Task 6).
4. `config.yaml` with a `${VAR}` whose variable is unset and no default — a clear error naming the key, never an
   empty token silently accepted (test in Task 1).
5. `/connect` of an existing repository from a second chat with a wrong secret, an empty-secret repo, or by a bot
   admin — refusal never reveals the secret (tests in Task 5).
6. A chat the bot was removed from and later re-added — the destination marked inactive becomes active again on
   the next command there (test `TestCommandReactivatesDestination` in Task 5).

---

### Task 1: `config.yaml` loading

**Files:**
- Create: `internal/config/config.go`, `internal/config/expand.go`, `internal/config/config_test.go`,
  `config.example.yaml`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces:
  - `type Config struct { Server Server; Panel Panel; Metrics Metrics; Storage Storage; Defaults Defaults;
    Admins []string; Security Security; Messengers Messengers }` with YAML keys exactly as in the spec's example.
  - `Server{Listen, PublicURL string}`, `Panel{Listen string; AllowRemote bool}`, `Metrics{Enabled *bool;
    Listen string}`, `Storage{Path string; DeliveriesRetentionDays *int}`, `Defaults{Language string;
    NotifyUnknownEvents bool}`, `Security{WebhookSecret string}`,
    `Messengers{Telegram *Telegram; Discord *Discord; Max *Max}`,
    `Telegram{Enabled bool; Token, APIURL, Proxy string; RichMessages *bool}`,
    `Discord{Enabled bool; Token, ApplicationID, PublicKey string}`, `Max{Enabled bool; Token string}`.
  - `func Load(path string) (*Config, error)` — read, expand, strict-decode, apply defaults, validate.
  - `func Parse(data []byte, env func(string) (string, bool)) (*Config, error)` — the testable core of `Load`.
  - `func Find(flagPath string) (string, error)` — the lookup order from Global Constraints; error
    `ErrNotFound` when none exists.
  - `func (c *Config) IsAdmin(messenger, userID string) bool` — matches `"<messenger>:<id>"` in `Admins`.
  - `func Mask(s, secret string) string` — replaces every occurrence of a non-empty secret with `***`.
  - Defaults: `server.listen ":8080"`, `storage.path "data/notifier.db"`, `deliveries_retention_days 14`,
    `defaults.language "en"`, `metrics.enabled true`, `telegram.rich_messages true`.

- [ ] **Step 1: Write failing tests** in `config_test.go`:
  - `TestParseExample` — `config.example.yaml` parses with env `TELEGRAM_TOKEN=x` and yields
    `Messengers.Telegram.Enabled == true`, `Server.Listen == ":8080"`.
  - `TestExpand` — `${A}`, `${A:-def}` with A unset → `def`, `$${A}` stays literal `${A}`.
  - `TestUnsetVarIsError` — `token: ${NOPE}` with NOPE unset → error containing `messengers.telegram.token` and
    `NOPE`.
  - `TestUnknownKeyIsError` — `server: {lisen: ":1"}` → error containing `lisen`.
  - `TestNoMessengerEnabled` → error `at least one messenger must be enabled`.
  - `TestEnabledTelegramNeedsToken`, `TestBadLanguage` (`defaults.language: de` → error), `TestAdminFormat`
    (`admins: ["123"]` → error asking for `messenger:id`).
  - `TestIsAdmin`, `TestMask` (token never appears in the masked output).
  - `TestFindOrder` — uses a temp dir: flag path wins; else `./config.yaml`; else `ErrNotFound`.
- [ ] **Step 2: Run** `go test ./internal/config/` — Expected: FAIL (package does not compile).
- [ ] **Step 3: Implement.** `go get gopkg.in/yaml.v3`. Expansion runs on the raw text before decoding, line by
  line, so the error can name the YAML path: decode once into `yaml.Node`, walk scalar nodes, expand their
  values, then `node.Decode` with a `yaml.Decoder` that has `KnownFields(true)` (re-encode the node to bytes and
  decode strictly — `Node.Decode` alone does not honour `KnownFields`). Write `config.example.yaml` with a comment
  on every field, Telegram enabled, Discord/MAX disabled.
- [ ] **Step 4: Run** `go test ./internal/config/` — Expected: PASS. `go vet ./...` clean.
- [ ] **Step 5: Commit** `feat: load configuration from config.yaml` — `Refs NTF-14.`

### Task 2: Messenger-neutral schema and storage API

**Files:**
- Create: `storage/migrations/0004_messengers.sql`, `storage/model.go` (types and methods below),
  `storage/model_test.go`, `storage/migrate_v1_test.go`
- Modify: `storage/storage.go` (remove the chat/topic/repo/mapping methods and types), `storage/storage_test.go`,
  `internal/bot/*.go`, `internal/server/server.go`, `internal/web/web.go` and their tests (port to the new API)

**Interfaces:**
- Produces (in `storage`):
  - `type Destination struct { ID int64; Messenger, ChatID, Title, Language, Format string; UseThreads, Inactive
    bool }`
  - `type Thread struct { DestinationID int64; ThreadID, Name string }`
  - `type Repo struct { ID int64; URL, Platform, Secret string }`
  - `func (s *Store) EnsureDestination(messenger, chatID string, d Destination) (Destination, error)` — insert if
    missing (using `d`'s Language/Title/Format/UseThreads), else return the stored row unchanged except Title.
  - `GetDestination(messenger, chatID string) (*Destination, error)` (`nil, nil` when missing),
    `DestinationByID(id int64) (*Destination, error)`, `Destinations() ([]Destination, error)`,
    `SetDestinationSettings(id int64, format string, useThreads bool) error`,
    `SetDestinationLanguage(id int64, lang string) error`, `SetDestinationInactive(id int64, inactive bool) error`.
  - `SaveThread(t Thread, eventType string) error` (also sets the route), `Threads(destID int64) ([]Thread,
    error)`, `Routes(destID int64) (map[string]string, error)`, `ThreadFor(destID int64, eventType string)
    string`, `DeleteThread(destID int64, threadID string) error`.
  - `FindRepo(url string) (*Repo, error)` (normalised lookup, as today), `CreateRepo(url, platform, secret string)
    (Repo, error)`, `Repos() ([]Repo, error)`.
  - `Subscribe(repoID, destID int64) error` (idempotent), `Unsubscribe(destID int64, url string) (bool, error)` —
    deletes the repo when its last subscription goes, `Subscribers(repoID int64) ([]Destination, error)` (active
    only), `SubscriptionsOf(destID int64) ([]Repo, error)`, `SubscriberCount(repoID int64) (int, error)`.
  - `const FormatRich = "rich"`, `FormatClassic = "classic"` stay.
- Migration `0004_messengers.sql`: create `repos_v2`, `destinations`, `threads`, `routes`, `subscriptions` with
  the columns from the spec plus `destinations.inactive INTEGER NOT NULL DEFAULT 0`; copy data; drop
  `event_mapping`, `topics`, `repos`, `chats` (in that order); rename `repos_v2` → `repos`. `deliveries`
  untouched. Telegram chat IDs become `CAST(chat_id AS TEXT)`; `use_topics` → `use_threads`; `is_forum` is not
  kept (the adapter reads it live).

- [ ] **Step 1: Write the failing migration test** `TestMigrateFrom11` in `migrate_v1_test.go`: build a database
  with migrations 0001–0003 only (load them from the embedded FS and stop at version 3), insert two chats (one
  forum with `format='classic'`, `use_topics=0`), three topics, two mappings, two repos (one with an empty
  secret), then open it with `storage.Open`. Assert: two destinations with `Messenger=="telegram"`, ChatID as
  decimal text, Format/UseThreads/Language preserved; threads and routes preserved; two repos with their secrets
  (one empty) and one subscription each; `SchemaVersion()==4`.
- [ ] **Step 2: Write** `TestMigrateFrom10` (database created by 0001 only, version-1 marking path) and
  `TestMigrateOrphans` (a repo whose chat row is missing and a mapping to a deleted topic: migration succeeds,
  the orphan repo is kept with no subscription, the dangling route is dropped).
- [ ] **Step 3: Write** `model_test.go` tests: `TestEnsureDestinationKeepsSettings`, `TestSubscribeIdempotent`,
  `TestUnsubscribeDeletesLastRepo`, `TestSubscribersSkipInactive`, `TestThreadForMissingRoute` (returns `""`).
- [ ] **Step 4: Run** `go test ./storage/` — Expected: FAIL (types and migration missing).
- [ ] **Step 5: Implement** the migration and `model.go`; delete the old methods. Port callers mechanically so the
  module builds: `internal/bot` converts `int64` ↔ string with `strconv`; `server.targets` uses
  `Subscribers(repo.ID)` and returns nothing for an unknown repository (the `DefaultChatID` broadcast is removed
  here — this is the owner's "drop" decision); `web.Source` becomes `Destinations/Repos/Threads/Routes/
  SubscriptionsOf`. Existing bot/server/web tests are updated to the new API; a server test
  `TestUnconnectedRepoIsDropped` asserts no send and delivery `ignored` with error `not connected`.
- [ ] **Step 6: Run** `go test ./...` — Expected: PASS; `git diff --exit-code internal/handler/testdata` clean.
- [ ] **Step 7: Commit** `feat: store repositories, destinations and subscriptions per messenger` —
  `Refs NTF-11.` Body notes the dropped broadcast as a breaking change.

### Task 3: Messenger interface and fake

**Files:**
- Create: `internal/messenger/messenger.go`, `internal/messenger/errors.go`, `internal/messenger/errors_test.go`,
  `internal/messenger/fake/fake.go`, `internal/messenger/fake/fake_test.go`

**Interfaces:**
- Produces:
  - `type Caps struct { Threads bool; Looks []string; ButtonsPerRow int; PerChatRate, GlobalRate float64
    (messages per second); PerChatBurst int }` — the adapter's limits; `Looks` empty or one item means no
    look step.
  - `type Target struct { ChatID, ThreadID string }`
  - `type Choice struct { Label, Data string }` — `Data` ≤ 64 bytes (Telegram's callback limit).
  - `type Reply struct { Text string; Choices [][]Choice }` — `Text` is the limited HTML used by locales today
    (`b`, `i`, `code`, `a`); each adapter converts it.
  - `type Command struct { Messenger, ChatID, ThreadID, UserID, ChatTitle string; IsPrivate, IsForum bool;
    Name string; Args []string; Ref any }` — `Ref` is adapter-private (the message to reply to).
  - `type Press struct { Messenger, ChatID, UserID, Data string; Ref any }`
  - `type Handler interface { HandleCommand(ctx context.Context, c Command); HandlePress(ctx context.Context, p
    Press) }`
  - `type Messenger interface {`
    `Name() string; Caps() Caps;`
    `Run(ctx context.Context, h Handler) error;` (blocks until ctx is done)
    `Send(ctx context.Context, t Target, look string, lang i18n.Lang, n notify.Notification) ([]notify.Attempt,
    error);`
    `Reply(ctx context.Context, c Command, r Reply) (msgRef any, err error);`
    `Edit(ctx context.Context, p Press, r Reply) error;`
    `Answer(ctx context.Context, p Press, text string, alert bool) error;`
    `IsChatAdmin(ctx context.Context, chatID, userID string) (bool, error);`
    `CanManageThreads(ctx context.Context, chatID string) (bool, error);`
    `CreateThread(ctx context.Context, chatID, name string) (threadID string, err error);`
    `Health(ctx context.Context) error }`
  - Errors (`errors.go`): `type RateLimited struct{ RetryAfter time.Duration; Err error }`, sentinel wrappers
    `ErrThreadNotFound`, `ErrChatGone`, `ErrRejected`; anything else is temporary.
    `func Classify(err error) Class` with `Class` constants `ClassOK, ClassRateLimited, ClassThreadNotFound,
    ClassChatGone, ClassRejected, ClassTemporary`, and `func RetryAfter(err error) time.Duration`.
  - `fake.Messenger` implements `Messenger`, records `Sent []fake.Sent{Target, Look, Lang, N}`, `Replies`,
    `Edits`, `Answers`, `Threads`, lets tests script errors per call (`fake.Messenger.Fail(func(call int, t
    Target) error)`), and `Admins map[string]bool` keyed `chatID+"/"+userID`.

- [ ] **Step 1: Write failing tests** `TestClassify` (each error kind, wrapped with `fmt.Errorf("%w")`, classifies
  correctly; `nil` → `ClassOK`; plain error → `ClassTemporary`; `RetryAfter` of a wrapped `RateLimited`) and
  `TestFakeRecordsAndFails` in `fake_test.go`.
- [ ] **Step 2: Run** `go test ./internal/messenger/...` — Expected: FAIL.
- [ ] **Step 3: Implement** the package and the fake. No adapter imports in `internal/messenger`.
- [ ] **Step 4: Run** `go test ./internal/messenger/...` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: add the messenger interface` — `Refs NTF-10.`

### Task 4: Telegram adapter

**Files:**
- Create: `internal/messenger/telegram/telegram.go` (client, Run, update → Command/Press),
  `internal/messenger/telegram/send.go` (moved from `internal/bot/send.go`), `…/render.go` (moved
  `notify/rich.go`, `classic.go`, `fit.go`, `htmlcheck.go`), `…/reply.go` (Reply/Edit/Answer, Choice → inline
  keyboard), `…/admin.go` (IsChatAdmin, CanManageThreads, CreateThread), and the moved tests
  (`send_test.go`, `fallback_log_test.go`, `health_test.go`, `start_test.go`, `rich_test.go`, `classic_test.go`,
  `fit_test.go`) plus `telegram_test.go`
- Modify: `internal/notify/notify.go` (keeps the model and shared helpers only), `internal/handler/golden_test.go`
  (renders through the Telegram package's exported `RenderRich`/`RenderClassic`)
- Delete (at the end of Task 5, not here): `internal/bot`

**Interfaces:**
- Consumes: `messenger.Messenger`, error classes (Task 3); `config.Telegram` (Task 1).
- Produces: `func New(cfg config.Telegram, defaultLang i18n.Lang, version string) (*Telegram, error)`;
  `func RenderRich(lang i18n.Lang, n notify.Notification) string`; `func RenderClassic(lang i18n.Lang, n
  notify.Notification) (string, [][]notify.Button)`; `func (t *Telegram) RichAvailable() bool`.
  `Caps{Threads: true, Looks: ["rich","classic"], ButtonsPerRow: 3, PerChatRate: 1, GlobalRate: 30,
  PerChatBurst: 3}`. Command menu (`setMyCommands`) registered in the background as today; the command list is
  passed in by the core through `Run`'s handler (`Handler` may implement `interface{ Commands(lang) []struct{Name,
  Description string} }`; the adapter type-asserts it).
- Error mapping in `Send`: Telegram `429` → `RateLimited{RetryAfter: parameters.retry_after}`; "message thread
  not found" / "TOPIC_CLOSED" / "TOPIC_DELETED" → `ErrThreadNotFound`; "chat not found", "bot was kicked", "bot
  is not a member", "have no rights to send" → `ErrChatGone`; everything else temporary. Rich → classic fallback,
  the `richMissing` switch, and button-URL resend stay exactly as in 1.1.1 and return their `[]notify.Attempt`.
- `Command.IsForum` from `msg.Chat.IsForum`; `ThreadID` from `MessageThreadID` when `IsTopicMessage`;
  anonymous admins (`SenderChat.ID == Chat.ID`) give `UserID == ""`, which `IsChatAdmin` treats as "cannot tell"
  and the core handles as today's `cfg_anonymous`.

- [ ] **Step 1: Move the tests first** (`git mv`) into the new package and adapt their package clause and
  constructors; add `TestSendClassifiesErrors` (httptest Telegram returning 429 with `retry_after: 7`, "message
  thread not found", "bot was kicked from the group chat", 500) and `TestUpdateToCommand` (topic message,
  `/cmd@OtherBot` ignored, anonymous admin).
- [ ] **Step 2: Run** `go test ./internal/messenger/telegram/` — Expected: FAIL (code not moved).
- [ ] **Step 3: Move the code** (`git mv`, then adapt); keep `internal/bot` compiling by having it call the new
  package's `Send` until Task 5 removes it.
- [ ] **Step 4: Run** `go test ./...` — Expected: PASS; `git diff --exit-code internal/handler/testdata` clean
  (the goldens are byte-identical because the renderers moved unchanged).
- [ ] **Step 5: Commit** `refactor: move Telegram sending behind the messenger interface` — `Refs NTF-10.`

### Task 5: Commands in the core

**Files:**
- Create: `internal/commands/commands.go` (dispatch, start/help/version/lang/status),
  `internal/commands/connect.go`, `internal/commands/configure.go`, `internal/commands/*_test.go`
- Modify: `locales/en.json`, `locales/ru.json`, `internal/i18n/i18n.go` (new keys), `main.go` (wire the core to
  the Telegram adapter)
- Delete: `internal/bot`

**Interfaces:**
- Consumes: `messenger.Messenger`, `messenger.Handler`, `fake.Messenger` (Task 3); storage API (Task 2);
  `config.Config.IsAdmin` (Task 1).
- Produces: `func New(store *storage.Store, cfg *config.Config, version string, ms map[string]messenger.Messenger)
  *Core` implementing `messenger.Handler`; `func (c *Core) Commands(lang i18n.Lang) []CommandInfo`.
- Callback data keeps the 1.1 format (`cfg:<step>:<value>`), so buttons on old `/configure` messages still work.
- `/connect <url> [secret]` rules (spec "Connecting a repository"):
  - new repo → `CreateRepo` with a fresh secret, `Subscribe`, show webhook instructions with the secret;
  - existing repo already subscribed here → show instructions with its secret (as 1.x reconnect);
  - existing repo, other chats only → subscribe when `args[1]` equals its non-empty secret, or the sender is a
    config admin; otherwise reply with new key `connect_needs_secret` (en: "This repository is already connected
    in another chat. Send <code>/connect URL SECRET</code> with its webhook secret, or ask a bot administrator.")
    and never print the secret. Empty-secret repo → admin only, key `connect_admin_only`.
  - a command typed in a thread acts on its parent chat (`Target.ThreadID` ignored except for the reply).
- `/configure`: thread step only when `Caps().Threads` and (Telegram) `Command.IsForum`; look step only when
  `len(Caps().Looks) > 1`; summary otherwise identical to 1.1.1 texts. Thread creation uses `CanManageThreads` +
  `CreateThread` for each `platform.Topics` entry and `SaveThread`.
- `/status` lists this destination's subscriptions (not "repos connected here"), look, threads; same keys as
  1.1.1 plus nothing new.

- [ ] **Step 1: Port the behaviour tests** from `internal/bot/configure_test.go` and `bot_test.go` to
  `internal/commands` against `fake.Messenger` (same scenarios and expected texts: non-admin press refused,
  anonymous admin explained, topic failure keeps buttons, regular group skips topics, rich-off status line).
- [ ] **Step 2: Write new tests**: `TestConnectNewRepo`, `TestConnectSecondChatNeedsSecret` (reply contains no
  secret), `TestConnectSecondChatWithSecret`, `TestConnectSecondChatByAdmin`, `TestConnectEmptySecretAdminOnly`,
  `TestDisconnectKeepsRepoForOtherChat`, `TestConfigureNoLookStepWithOneLook` (fake Caps with one look),
  `TestCommandInThreadActsOnChat`, `TestCommandReactivatesDestination` (any command in an inactive destination
  clears `Inactive`).
- [ ] **Step 3: Run** `go test ./internal/commands/` — Expected: FAIL.
- [ ] **Step 4: Implement**, add the two locale keys in en and ru, wire `main.go`, delete `internal/bot`.
- [ ] **Step 5: Run** `go test ./...` — Expected: PASS. Manual: `go build -o data/notifier.exe .`, run against the
  local test group with a copy of `data/local.db`, run `/status`, `/configure`, `/connect` of an existing repo.
- [ ] **Step 6: Commit** `feat: handle chat commands in the core for every messenger` — `Refs NTF-12.`

### Task 6: Outbox

**Files:**
- Create: `storage/migrations/0005_outbox.sql`, `storage/outbox.go`, `storage/outbox_test.go`,
  `internal/outbox/outbox.go`, `internal/outbox/limiter.go`, `internal/outbox/outbox_test.go`
- Modify: `internal/server/server.go`, `internal/server/server_test.go`, `storage/deliveries.go`,
  `internal/metrics/metrics.go`, `internal/metrics/metrics_test.go`, `main.go`

**Interfaces:**
- Consumes: `messenger.Messenger`, `Classify`, `RetryAfter` (Task 3); `Subscribers`, `ThreadFor`,
  `SetDestinationInactive`, `DestinationByID` (Task 2).
- Produces:
  - Table `outbox(id, delivery_id INTEGER REFERENCES deliveries(id) ON DELETE SET NULL, destination_id, thread_id
    TEXT, event_type, payload TEXT, attempts INTEGER, next_attempt_at INTEGER, last_error TEXT, state TEXT
    CHECK(state IN ('pending','sending','done','dead')), created_at INTEGER)`; index on `(state,
    next_attempt_at)` and `(destination_id, id)`.
  - `type OutboxItem struct { ID, DeliveryID, DestinationID int64; ThreadID, EventType string; Payload []byte;
    Attempts int; NextAttemptAt time.Time; LastError, State string }`
  - `func (s *Store) EnqueueDelivery(d Delivery, items []OutboxItem) (deliveryRowID int64, err error)` — one
    transaction: insert the delivery (status `queued`) and its items.
  - `func (s *Store) ClaimNext(messenger string, now time.Time, busy map[int64]bool) (*OutboxItem, error)` — the
    oldest due `pending` row of that messenger whose destination has no `sending` row and is not in `busy`, and
    with no older `pending` row of the same destination; sets it `sending`.
  - `MarkDone(id)`, `MarkRetry(id, next time.Time, errText string)`, `MarkDead(id, errText)`,
    `ResetSending()` (start-up: `sending` → `pending`), `PruneOutbox(now)`, `OutboxCounts() (pending, dead int)`,
    `RequeueDead(deliveryRowID int64) (int, error)`.
  - Delivery statuses gain `queued`; a delivery becomes `delivered` when all its rows are `done`, `failed` when
    any is `dead` and none pending (updated by `MarkDone`/`MarkDead`). `WasDelivered` is replaced by
    `DeliveryState(platform, id string) (status string, rowID int64, err error)`.
  - `internal/outbox`: `type Payload struct { Lang i18n.Lang; Look string; N notify.Notification }`;
    `func New(store *storage.Store, ms map[string]messenger.Messenger, m *metrics.Metrics, clock func()
    time.Time) *Outbox`; `func (o *Outbox) Run(ctx context.Context)` — one goroutine per messenger, waking on a
    `Notify()` channel or every second; `func Delay(attempt int) time.Duration` (Global Constraints values).
  - Limiter: per-destination and global token buckets from `Caps` (stdlib only; `limiter.go`).
- Webhook flow: verify → dedup via `DeliveryState` (`queued`/`delivered` → `200 DUPLICATE`; `failed` →
  `RequeueDead` and `202 REQUEUED`) → dispatch once per destination language → `EnqueueDelivery` → `202` →
  `outbox.Notify()`. No subscribers → `RecordDelivery` with `ignored`/`not connected`.
- Send handling: `ClassRateLimited` → `next = now + RetryAfter` (attempts not counted); `ClassTemporary` /
  `ClassRejected` → `Delay(attempts)`, `dead` at 10; `ClassThreadNotFound` with a thread → retry once with
  `ThreadID=""` immediately (Telegram), log; `ClassChatGone` → dead + `SetDestinationInactive(true)`.
  Metrics from `Attempt`s exactly as `server.send` does today, plus `notifier_send_retries_total{messenger}` on
  each retry scheduled and gauges `notifier_outbox_pending` / `notifier_outbox_dead` read at scrape time.

- [ ] **Step 1: Write storage tests**: `TestEnqueueIsAtomic` (failing item insert leaves no delivery row),
  `TestClaimKeepsDestinationOrder`, `TestClaimSkipsBusyDestination`, `TestResetSending`,
  `TestDeliveryStatusFollowsRows`, `TestRequeueDead`, `TestPruneOutbox`.
- [ ] **Step 2: Write outbox tests** with a fake clock and `fake.Messenger`: `TestDelaySchedule`
  (5s,15s,1m,5m,15m,1h,1h…), `TestOutageThenRecovery`, `TestRateLimitedOnlyDelaysThatChat`,
  `TestDeadAfterTenAttempts`, `TestThreadNotFoundFallsBack`, `TestChatGoneMarksInactive`,
  `TestRestartResumesPending`, `TestOrderPerDestination` (two webhooks for one chat → sent in order).
- [ ] **Step 3: Write server tests**: `TestWebhookAnswers202BeforeSending` (fake messenger blocks; handler
  returns), `TestRedeliveryOfQueuedIsDuplicate`, `TestRedeliveryOfFailedRequeues`, `TestFanOutToSubscribers`
  (two destinations, two languages → two items with their own language payloads).
- [ ] **Step 4: Run** `go test ./storage/ ./internal/outbox/ ./internal/server/` — Expected: FAIL.
- [ ] **Step 5: Implement**; start the outbox in `main.go`; prune hourly next to `PruneDeliveries`.
- [ ] **Step 6: Run** `go test -race ./...` — Expected: PASS.
- [ ] **Step 7: Commit** `feat: deliver notifications through an outbox with retries` — `Refs NTF-13.`

### Task 7: CLI, `migrate-config` and start-up from `config.yaml`

**Files:**
- Create: `internal/config/migrate.go`, `internal/config/migrate_test.go`,
  `internal/config/testdata/env-1.1.env`, `internal/config/testdata/env-1.1.yaml`, `cmd.go` (subcommand
  dispatch in package main), `cmd_test.go`
- Modify: `main.go` (becomes `serve`), `scripts/install.sh`, `Dockerfile`, `docker-compose.yml`, `go.mod`
  (drop godotenv), `.env.example` → delete, `internal/web/web.go` (`Config` from `config.Config`)

**Interfaces:**
- Consumes: `config.Parse`, `config.Find`, `config.Load` (Task 1).
- Produces:
  - `func FromEnv(env map[string]string) (yamlText []byte, warnings []string, err error)` — maps every 1.x
    variable listed in the spec; `DOMAIN`+`USE_HTTPS` → `server.public_url`; `SERVER_ADDRESS`+`SERVER_PORT` →
    `server.listen`; `PROXY_ENABLED/TYPE/HOST/PORT` → `messengers.telegram.proxy` URL; `BOT_TOKEN` is written as
    `${TELEGRAM_TOKEN}` reference **only when** the caller asks (`--keep-secrets-in-env`), otherwise inline (the
    1.x `.env` already held it in a root-only file); `CHAT_ID` → a YAML comment `# CHAT_ID=<id> from 1.x is no
    longer used: events of unconnected repositories are dropped.`
  - `func ReadEnvFile(path string) (map[string]string, error)` — minimal `KEY=VALUE` parser (comments, quotes,
    `export ` prefix) replacing godotenv.
  - Subcommands: `serve` (default), `version`, `config check [--config]`, `migrate-config --from .env --to
    config.yaml [--force]` (refuses to overwrite without `--force`; writes mode 0600), `db backup <path>` (`VACUUM
    INTO`), `help`. Old flags: `--version` = `version`; `--db`, `--lang` override `storage.path` /
    `defaults.language` with a deprecation warning; `--config x.env` (or a file whose first non-comment line
    is `KEY=VALUE`) fails with: `config.yaml is required since 2.0; run: notifier migrate-config --from x.env --to
    config.yaml`.
  - Start-up with no config file found **and** `BOT_TOKEN` in the process environment (Docker with `env_file`):
    build the config in memory with `FromEnv(os.Environ)`, log a deprecation warning, continue. *Ruling: this is
    how "the Docker image migrates automatically" without a writable config path in a distroless image.*
  - `install.sh update`: if `${CONFIG_DIR}/notifier.env` exists and `config.yaml` does not, run the new binary's
    `migrate-config --from notifier.env --to config.yaml`, keep `notifier.env` as `notifier.env.1x`, rewrite the
    unit's `ExecStart` to `--config ${CONFIG_DIR}/config.yaml`, `daemon-reload`. Rollback (existing mechanism)
    restores the unit and `notifier.env`. Fresh install writes `config.yaml`.
  - Before migration 0004 runs, `storage.Open` copies the database file to `<path>.1x-backup` once (only when
    the current version is < 4) — the rollback path the migration guide will describe.

- [ ] **Step 1: Write failing tests**: `TestFromEnvGolden` (`testdata/env-1.1.env` → `testdata/env-1.1.yaml`,
  `-update` flag like the handler goldens), `TestFromEnvResultLoads` (output passes `config.Parse`),
  `TestReadEnvFile`, `TestEnvConfigFlagGivesHint`, `TestMigrateConfigRefusesOverwrite`,
  `TestDBBackup` (backup opens and has the same schema version), `TestLegacyEnvStartup` (no file, `BOT_TOKEN`
  set → config built, warning logged), `TestOpenBacksUpBeforeV4` in `storage`.
- [ ] **Step 2: Run** `go test ./...` — Expected: FAIL for the new tests.
- [ ] **Step 3: Implement**; update `install.sh` (keep it ShellCheck-clean: `shellcheck scripts/install.sh`),
  `docker-compose.yml` (mount `./config.yaml:/etc/notifier/config.yaml:ro`, keep `env_file` optional with a
  comment), Dockerfile `CMD ["serve"]`. Remove godotenv (`go mod tidy`).
- [ ] **Step 4: Run** `go test -race ./...`, `gofmt -l .`, `go vet ./...` — Expected: clean. Manual: on a VM or
  WSL with the 1.1.1 release installed by `install.sh`, run `install.sh update` from this branch's build; the
  service starts, `/status` in the test group shows the same chat settings, a GitHub redelivery arrives.
- [ ] **Step 5: Commit** `feat: add subcommands and migrate 1.x configuration to config.yaml` —
  `Refs NTF-15.` Body lists the breaking changes (`.env` no longer read by `serve` except the Docker fallback).

### Task 8: Panel and health for several messengers

**Files:**
- Modify: `internal/web/web.go`, `internal/web/templates/*.html`, `internal/web/web_test.go`,
  `internal/server/server.go` (checks per messenger), `main.go`

**Interfaces:**
- Consumes: storage model (Task 2), `OutboxCounts`, recent dead rows (Task 6), `messenger.Messenger.Health`.
- Produces: `web.Source` adds `DeadItems(limit int) ([]storage.OutboxItem, error)` and `OutboxCounts()`; health
  checks named `db` and one per enabled messenger (`telegram`, later `discord`, `max`).
- Panel: chats page grouped by messenger with title, chat ID, look, threads and routes, subscriptions; inactive
  destinations marked; overview shows pending/dead counts and warns about repositories that keep sending
  webhooks with no subscribers (deliveries `ignored`/`not connected` in the last 7 days). Still read-only,
  loopback-only, no secrets.

- [ ] **Step 1: Write failing tests**: `TestChatsGroupedByMessenger`, `TestInactiveDestinationMarked`,
  `TestOverviewShowsOutbox`, `TestOverviewWarnsUnconnectedRepo`, `TestPanelNeverShowsSecrets` (extend the
  existing one to the new pages), `TestHealthPerMessenger`.
- [ ] **Step 2: Run** `go test ./internal/web/ ./internal/server/` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test -race ./...` — Expected: PASS; goldens unchanged.
- [ ] **Step 5: Commit** `feat: show destinations per messenger and the outbox in the panel` — `Refs NTF-16.`

---

After Task 8: whole-branch review, CHANGELOG `[Unreleased]` entries (Added / Changed / **Breaking changes**),
PR into `development`.
