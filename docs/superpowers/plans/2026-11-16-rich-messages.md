# Rich Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Send every webhook notification as a Telegram Rich Message (heading, table, collapsed lists, URL buttons,
footer) with a classic fallback, and let each chat choose topics and look in `/configure`.

**Architecture:** Handlers stop writing HTML and return a messenger-neutral `notify.Notification`. Package
`internal/notify` renders it as rich HTML or classic HTML + inline keyboard. The bot sends rich via
`sendRichMessage` and falls back to classic on content 400s; per-chat `format`/`use_topics` live in `chats`
(migration 0003) and are set by a button-driven `/configure`.

**Tech Stack:** Go 1.27, telego v1.13.0 (upgrade from v1.9.0), modernc SQLite, hand-written Prometheus metrics.

**Spec:** `docs/superpowers/specs/2026-11-16-rich-messages-design.md`

## Global Constraints

- Only telego is upgraded (v1.9.0 → v1.13.0); no other new dependencies.
- `skip_entity_detection: true` on every rich message.
- Rich limits: 32 768 characters, 500 blocks, 20 table columns; titles truncated at 200, bodies at `maxBodyLength`.
- Commit list in rich `<details>`: up to 20 entries, then "…and N more"; classic keeps 5 commits / 10 files.
- At most 3 buttons per notification; styles `primary`, `success`, `danger` or none.
- No handler writes HTML; all escaping happens in `internal/notify`.
- Every existing content item stays: stats, branches, fork label `owner:branch`, 🔒 confidential note,
  "…and N more", all skip rules.
- `RICH_MESSAGES` env, default `true`; `false` forces classic for every chat.
- New chats and migrated chats: `format = 'rich'`, `use_topics = 1`.
- Every user-visible string goes through `locales/en.json` and `locales/ru.json` (both files, same placeholders).
- Commits: Angular style, detailed English body, `Refs NTF-xx.` (card created in Task 0), trailer
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Go: `export PATH=/d/GoSDK/go1.27.3/bin:$PATH`; `-race` only runs in CI.

## Review Focus

1. A payload URL that is not `http(s)://` (e.g. `javascript:`, empty) must not become a button or link — the
   button is dropped, the text stays. Test in Task 1.
2. Hostile user text (`</table><tg-button type="url" url="x">`, `<details>`, `&amp;`) in titles, bodies,
   branch names, commit messages and field values comes out escaped in both renders. Test in Task 1.
3. A huge body or 1000-commit push stays under the rich limits (32 768 chars, 500 blocks). Test in Task 1.
4. A button press from a non-admin, from another chat's message, or with stale/unknown callback data changes
   nothing and answers with an alert. Test in Task 7.
5. A chat with `use_topics = 0` whose topics still exist receives everything in the main chat; a deleted topic
   with `use_topics = 1` still falls back to the main chat with at most two send calls per format. Tests in
   Tasks 4 and 6.

---

### Task 0: Tracking card

- [ ] **Step 1:** Create a draft item in GitHub Project 7 (owner `marensovich`) titled
  `NTF-91 · Rich-уведомления и настройка чата в /configure`, status In Progress; use `NTF-91` in every commit.
  If the number is taken, use the next free one and keep it consistent.

### Task 1: `internal/notify` — model and both renderers

**Files:**
- Create: `internal/notify/notify.go`, `internal/notify/rich.go`, `internal/notify/classic.go`,
  `internal/notify/htmlcheck.go` (exported checker used by tests in other packages)
- Test: `internal/notify/rich_test.go`, `internal/notify/classic_test.go`

**Interfaces:**
- Produces:
  ```go
  package notify
  type Style string // "", "primary", "success", "danger"
  type Part struct { Kind PartKind; Text, URL string } // Kind: Text, Code, Link, Bold
  type Link struct { Text, URL string }
  type Field struct { Label string; Value []Part }
  type Section struct { Summary string; Items [][]Part; More int } // More = items not listed
  type Button struct { Label, URL string; Style Style }
  type Notification struct {
      Emoji, Heading string
      Item, Repo, Actor Link
      Fields   []Field
      Quote    string
      Sections []Section
      Notes    []string
      Buttons  []Button
      Time     time.Time
  }
  func T(s string) Part; func C(s string) Part; func L(text, url string) Part; func B(s string) Part
  func Rich(lang i18n.Lang, n Notification) string
  func Classic(lang i18n.Lang, n Notification) (text string, keyboard [][]Button)
  func CheckRichHTML(html string) error    // allowed: h4 p b i code a table tr td blockquote(expandable) details summary ul li footer tg-button tg-time br
  func CheckClassicHTML(html string) error // allowed: b i a code pre blockquote(expandable); balanced; no raw < >
  ```
- Rich order: `<h4>{Emoji} {Heading}</h4>`, `<p><b>Item link</b></p>` (or repo link when Item empty),
  `<table compact>` with a Repo row first then Fields (skip empty), Notes as `<p>` each, `<blockquote expandable>`
  Quote, each Section as `<details><summary>Summary (count)</summary><ul>…</ul></details>`, buttons in one `<p>`
  as `<tg-button type="url" style=… url=…>`, `<footer>Actor link · <tg-time unix=… format="r">fallback</tg-time></footer>`.
- Classic: today's look (bold heading line, repo link, `label: value` lines, notes, expandable quote, sections as
  plain lines with classic limits) and buttons returned as keyboard rows (one row, ≤3).
- Labels "Repository" etc. come from i18n keys `field_repo`, `section_more` ("…and %d more") added here.

- [ ] **Step 1: Write failing tests** — `TestRichLayout` (a PR-like notification renders the tags in the order
  above and passes `CheckRichHTML`), `TestRichEscapesHostileText` (every string field set to
  `</table><tg-button type="url" url="x">&amp;<details>` → output contains `&lt;/table&gt;` and
  `CheckRichHTML` passes, no second `<tg-button`), `TestUnsafeURLDropped` (`javascript:alert(1)` and `""`
  URLs → no button, link text kept unlinked), `TestRichLimits` (Quote of 100 000 chars and a Section with 1 000
  items → `len([]rune(out)) < 32768`, block count < 500), `TestClassicKeyboard` (3 buttons → one row of 3, text
  passes `CheckClassicHTML`), `TestEmptyOptionalParts` (no Quote/Fields/Sections/Time → no empty tags).
- [ ] **Step 2:** `go test ./internal/notify` → FAIL (package missing).
- [ ] **Step 3:** Implement the files above. Allow only `http://` and `https://` URLs.
- [ ] **Step 4:** `go test ./internal/notify` → PASS; `gofmt -l internal/notify` empty.
- [ ] **Step 5:** Commit `feat: add notification model with rich and classic renderers`.

### Task 2: Handlers return `notify.Notification`

**Files:**
- Modify: `internal/handler/handler.go` (interface), every `github_*.go` / `gitlab_*.go`, `item.go` (replace
  `itemView`/`formatItem` with builders producing `notify.Notification`), `format.go` (keep `truncate`,
  `firstLine`, `shortSHA`, `branchName`, `isZeroSHA`; drop `esc`/`link`/`excerpt` once unused)
- Modify: `locales/en.json`, `locales/ru.json`, `internal/i18n/i18n.go` (layout keys → plain labels/headings)
- Modify: `internal/handler/golden_test.go`, `handler_test.go`, `github_test.go`, `gitlab_test.go`
- Regenerate: every `internal/handler/testdata/**/*.golden`

**Interfaces:**
- Consumes: Task 1 `notify` API.
- Produces: `Handle(lang i18n.Lang, payload []byte) (notify.Notification, error)` on `EventHandler`.
- Per-event content follows the spec table ("Per-event content"); event time from `head_commit.timestamp`,
  `pull_request.updated_at`, `issue.updated_at`, `comment.created_at`, `release.published_at`,
  `workflow_run.updated_at`, `object_attributes.updated_at` / `created_at` (zero when absent).
- Golden file format per case:
  ```
  topic: <topic>            (or "skip: true" and nothing else)

  --- en rich ---
  <Rich output>

  --- en classic ---
  <Classic text>
  buttons: [Label](url) style | …   (or "buttons: none")

  --- ru rich ---
  …
  ```

- [ ] **Step 1:** Update `TestGolden`/`renderGolden` to the format above (rich via `notify.Rich` +
  `CheckRichHTML`, classic via `notify.Classic` + `CheckClassicHTML`); change `runHandlerCases` to check
  `contains/absent` against the classic text. Run → compile FAIL (handlers still return strings).
- [ ] **Step 2:** Convert the interface and all 19 handlers; keep every skip rule; GitHub push commits list
  becomes a Section (20 in rich via the renderer limit, 5 in classic), files a second Section.
- [ ] **Step 3:** `go test ./internal/handler -run TestGolden -update`, then read the diff of at least: push
  branch/force/many_commits/branch_deleted, PR opened/from_fork/merged, confidential issue + note, pipeline
  failed/success, release prerelease, star. Fix anything that lost content compared to the previous golden.
- [ ] **Step 4:** `go vet ./... && go test ./...` → PASS (dispatcher/server compile after Task 3; until then run
  `go test ./internal/handler ./internal/notify ./internal/i18n`).
- [ ] **Step 5:** Commit `feat: build notifications as a model in every handler` (together with Task 3 if the
  tree does not compile without it).

### Task 3: Dispatcher carries the model

**Files:**
- Modify: `internal/dispatcher/dispatcher.go`; Test: `internal/dispatcher/dispatcher_test.go` (new)

**Interfaces:**
- Produces: `Result{ Notification notify.Notification; Topic platform.Topic; Skip bool }`. Unknown-event and
  handler-error results are Notifications with Emoji "❓"/"⚠️" and the existing texts as Heading/Notes.

- [ ] **Step 1:** Tests `TestDispatchSkip`, `TestDispatchUnknownNotifies` (notifyUnknown=true → Heading
  contains event name), `TestDispatchUnknownIgnored`. Run → FAIL.
- [ ] **Step 2:** Implement; server keeps compiling by calling `notify.Classic` temporarily in `deliver`.
- [ ] **Step 3:** `go test ./...` → PASS. Commit `refactor: pass notification models through the dispatcher`.

### Task 4: telego upgrade and `Bot.SendNotification`

**Files:**
- Modify: `go.mod`, `go.sum`, `internal/bot/bot.go` (send code moves to new `internal/bot/send.go`)
- Test: `internal/bot/send_test.go` (httptest fake Bot API via `telego.WithAPIServer`)

**Interfaces:**
- Produces:
  ```go
  type Attempt struct { Format string /* "rich"|"classic" */; Err error }
  func (b *Bot) SendNotification(chatID int64, topicID int, format string, lang i18n.Lang, n notify.Notification) ([]Attempt, error)
  func isTopicOrChatError(err error) bool // 400 descriptions: thread/topic not found, TOPIC_CLOSED, TOPIC_DELETED, chat not found, not enough rights
  ```
- `Bot` gets `richEnabled bool` from `NewBot(..., rich bool)`; `format == "classic"` or `!richEnabled` →
  classic only. Fallback logs once per process per description (`sync.Map`).

- [ ] **Step 1:** `go get github.com/mymmrac/telego@v1.13.0`, `go mod tidy`, `go build ./...`; fix any API
  breakage in existing calls. Commit separately: `build: update telego to v1.13.0`.
- [ ] **Step 2: Failing tests** — `TestSendRichOK` (1 call to `sendRichMessage`, `skip_entity_detection`
  true, `message_thread_id` set), `TestSendRichRejectedFallsBack` (400 "Bad Request: can't parse rich
  message" → second call `sendMessage` with `reply_markup.inline_keyboard`, attempts `[rich err, classic ok]`),
  `TestSendTopicErrorNoFallback` (400 "Bad Request: message thread not found" → 1 call, error returned),
  `TestSend429NoFallback`, `TestSendClassicFormat` and `TestSendRichDisabled` (only `sendMessage`).
- [ ] **Step 3:** Implement. Keep `SendMessageToTopic` for command replies.
- [ ] **Step 4:** `go test ./internal/bot` → PASS. Commit `feat: send notifications as rich messages with classic fallback`.

### Task 5: Chat settings in storage

**Files:**
- Create: `storage/migrations/0003_chat_settings.sql`
- Modify: `storage/storage.go`; Test: `storage/migrate_test.go`, `storage/storage_test.go`

**Interfaces:**
- Produces: `ChatConfig{ …; Format string; UseTopics bool }`;
  `func (s *Store) SetChatSettings(chatID int64, format string, useTopics bool) error`;
  constants `FormatRich = "rich"`, `FormatClassic = "classic"`. `GetChat`/`GetAllChats` fill the new fields.

- [ ] **Step 1:** Tests `TestMigration0003Defaults` (a version-2 DB with one chat → after open: version 3,
  format `rich`, use_topics true), `TestSetChatSettings` (round trip, invalid format → error).
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement (`ALTER TABLE chats ADD COLUMN …` ×2).
- [ ] **Step 4:** `go test ./storage` → PASS. Commit `feat: store per-chat format and topic routing`.

### Task 6: Server uses settings; metrics by format

**Files:**
- Modify: `internal/server/server.go` (`Sender` interface, `targets` carries `format`/`useTopics`, `deliver`),
  `internal/metrics/metrics.go`, `main.go` (`RICH_MESSAGES` → `bot.NewBot`), `docs/grafana-dashboard.json`
- Test: `internal/server/server_test.go` (fakeSender implements new interface), `internal/metrics/metrics_test.go`

**Interfaces:**
- Consumes: Task 4 `SendNotification`/`Attempt`, Task 5 `ChatConfig`.
- Produces: `type Sender interface { SendNotification(chatID int64, topicID int, format string, lang i18n.Lang, n notify.Notification) ([]bot.Attempt, error) }`
  (define `Attempt` in `server` or a tiny shared package to avoid an import cycle — builder's call);
  `Metrics.MessageSent(messenger, format string, err error)`; `Metrics.RichFallback()` →
  `notifier_rich_fallbacks_total`.

- [ ] **Step 1:** Tests `TestUseTopicsOffRoutesToMain` (chat with topics mapped, `use_topics=0` → topicID 0),
  `TestDeletedTopicFallsBackToMain` (topic send fails → second send to 0; ≤2 SendNotification calls),
  `TestMetricsCountAttemptsByFormat` (attempts `[rich err, classic ok]` → both counters + fallback 1).
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement; dashboard: split "Messages sent" by `format`, add a
  "Rich fallbacks" stat panel. Add `RICH_MESSAGES` to `.env.example`.
- [ ] **Step 4:** `go test ./...` → PASS. Commit `feat: route by chat settings and count sends by format`.

### Task 7: `/configure` setup with buttons

**Files:**
- Modify: `internal/bot/configure.go`, `internal/bot/bot.go` (`consume` handles `update.CallbackQuery`;
  `GetUpdatesParams.AllowedUpdates` includes `callback_query`), locales + i18n keys (`cfg_*`)
- Test: `internal/bot/configure_test.go`

**Interfaces:**
- Consumes: Task 5 `SetChatSettings`, existing topic creation (extract to `createTopics(chatID) (created, skipped []string, err error)`).
- Callback data: `cfg1:topics:yes|no`, `cfg1:format:rich|classic`; pure parser
  `parseConfigureCallback(data string) (step, value string, ok bool)`.
- Flow: `/configure` → (forum only) topics step → format step → summary, one message edited in place; current
  values marked with "✓"; non-forum chat skips topics and stores `use_topics=false`.
- Authorization: presser must be creator/administrator (`getChatMember`); otherwise `answerCallbackQuery`
  with alert `cfg_not_admin`, nothing stored. Callback whose message chat differs from stored chat → ignored.

- [ ] **Step 1:** Tests `TestParseConfigureCallback` (valid, unknown prefix, `cfg0:…`, missing value),
  `TestConfigureNonForumSkipsTopics`, `TestConfigureCallbackNonAdminRefused`,
  `TestConfigureStoresChoices` (fake API: press topics yes + format classic → store has classic/true and topic
  creation ran once), `TestConfigureRerunMarksCurrent`.
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement. **Step 4:** `go test ./internal/bot` → PASS.
- [ ] **Step 5:** Commit `feat: set up topics and message look with buttons in /configure`.

### Task 8: Docs, panel, manual check, PR

**Files:**
- Modify: `README.md` (notification look, `/configure` setup, `RICH_MESSAGES`, old clients note),
  `CHANGELOG.md` [Unreleased] (Added/Changed), `CONTRIBUTING.md` (handlers return `notify.Notification`;
  new golden format), `internal/web` chats page shows format and topics on/off.

- [ ] **Step 1:** Panel test `TestChatsPageShowsSettings`; implement; `go test ./internal/web` → PASS.
- [ ] **Step 2:** Docs updates; `make lint` (gofmt + vet) clean.
- [ ] **Step 3:** Manual: rebuild `data/notifier.exe`, restart the local bot, run `/configure` in the test group
  (choose topics + new look), send all fixtures with the scratchpad sender paced at one per 4 s, then switch to
  classic and send three. Owner checks desktop + phone.
- [ ] **Step 4:** Push `feat/rich-messages`, PR into `development` with the template, wait for CI, merge after
  the owner's go-ahead, mark the card Done.
