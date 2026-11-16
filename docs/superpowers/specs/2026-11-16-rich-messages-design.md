# Rich notifications (Telegram Rich Messages) — design

Date: 2026-11-16 · Status: draft for approval

## Why

Notifications are classic `sendMessage` HTML: everything is the same font size, so the header, the title, the
metadata and the quoted text run together, and there is nothing to tap except inline links. The owner wants
messages that are easy to scan in a busy forum topic and that offer actions as buttons.

Telegram Bot API 10.1–10.3 (June–August 2026) added **Rich Messages** (`sendRichMessage`): headings, tables,
lists, `<details>`, expandable quotes, `<footer>`, `<tg-time>` and inline `<tg-button>` buttons with colours,
up to 32 768 characters. A throwaway spike (three hand-written rich messages sent to the test group's PR, CI and
Pushes topics) rendered well on the owner's clients and was approved as the target look.

## Goals

- Every webhook notification (all 19 handlers, plus the "unknown event" message when `NOTIFY_UNKNOWN_EVENTS` is
  on) is sent as a rich message with a clear visual hierarchy:
  1. heading (`<h4>`): emoji + action, e.g. "🔀 New pull request", "❌ CI failed";
  2. the item as a bold link: `#12 Add dark mode`, `!3 MS-Viewer update`, `v1.2.0 — Dark mode`;
  3. a compact two-column table of metadata (repository, branches, changes, commit, duration, …) —
     only rows that have a value;
  4. user text (PR/issue/MR body, comment, release notes) in `<blockquote expandable>`;
  5. long lists (push commits, changed files) in collapsed `<details>` with the count in the summary;
  6. URL buttons (see below);
  7. `<footer>`: actor link · event time as `<tg-time>`.
- URL buttons per event, at most three, most useful first; colour `primary` for the main action, `danger` for
  failures, `success` for a passed CI / merged PR, no colour for secondary ones.
- If Telegram rejects the rich message, the same notification is sent as classic HTML, so nothing is lost
  because of formatting.
- Each chat chooses in `/configure` whether it wants forum topics and whether it gets the new (rich) or the
  classic look. Readers on old or third-party clients, which show rich messages as "unsupported", are served by
  choosing classic for their chat.
- Both renderings are covered by the golden tests.

## Non-goals

- Action buttons that change something on GitHub/GitLab (re-run CI, merge, close) — they need platform tokens
  with write access; that belongs to 2.0.
- Editing earlier messages instead of sending new ones (NTF-20) and the send queue with retries/rate limits
  (NTF-15). The design must not block them: `editMessageText` accepts `rich_message`, so NTF-20 can reuse the
  renderer.
- Rich formatting for bot command replies (`/status`, `/help`, `/connect`, …) — they stay classic.
- Media (avatars, images from release notes), custom emoji (need a Premium owner).
- Discord and MAX adapters (NTF-11, NTF-50, NTF-51). The design keeps them possible: handlers produce a
  messenger-neutral model, and the per-chat "topics yes/no" choice maps to channels/threads in Discord and is
  absent in MAX, which has no topics.

## Anti-goals

- A notification silently lost or turned into raw HTML because the rich render failed.
- User-controlled text (titles, bodies, commit messages, branch names) able to inject tags, buttons or links.
- A regression of the existing content: everything shown today (stats, branches, fork label, 🔒 confidential
  mark, "…and N more commits", skip rules) is still shown.

## Constraints

- telego must be upgraded from v1.9.0 to a version with `SendRichMessage` (v1.13.0 has it). Only telego is
  upgraded; check its changelog for breaking changes in the methods the bot uses.
- Rich limits: 32 768 characters, 500 blocks, 20 table columns. Existing truncation (titles 200, bodies
  `maxBodyLength`, 5 commits, 10 files) keeps us far below; rich lets the commit list grow to 20 inside
  `<details>` (still "…and N more").
- `skip_entity_detection: true`, so `#10`, `@user` and URLs in user text are not turned into entities that
  point somewhere unexpected.
- The forum topic routing (`message_thread_id`), the delivery log, dedupe and metrics keep working unchanged.
- No main-thread concerns (plain Go service); no new dependencies besides the telego upgrade.

## Approaches considered

- **A. Rich Messages with classic fallback — chosen.** Solves both complaints (font hierarchy and buttons);
  fallback removes the risk of clients/servers that reject rich content. Cost: library upgrade and a rewrite of
  every handler's output; all golden files change.
- B. Classic messages improved (reply-markup buttons, expandable quotes, `<tg-time>`). Cheap and universal, but
  everything stays one font size — fixes only half of the problem.
- C. A with a per-chat `/format rich|classic` command. Rejected as a separate command; the per-chat choice is
  instead part of the `/configure` setup (decided by the owner after the builder review raised the old-client
  problem).

## Design

### Handlers return a model, not HTML

Today `EventHandler.Handle(lang, payload) (string, error)` returns finished HTML built by `formatItem`,
`formatCI`, `formatTag`, the push and star formatters. It changes to return a structured notification:

```go
type Notification struct {
    Emoji, Heading string        // "🔀", localized "New pull request"
    Item           Link          // "#12 Add dark mode" → PR URL; may be empty (ping, star)
    Repo           Link
    Fields         []Field       // ordered table rows: localized label + value parts (text/code/link)
    Quote          string        // raw user text, escaped and truncated by the renderer
    Sections       []Section     // collapsed lists: summary + items (commits, files)
    Notes          []string      // short standalone lines, e.g. 🔒 confidential, ⚠️ force push
    Buttons        []Button      // label, URL, style (primary/success/danger/"")
    Actor          Link
    Time           time.Time     // event time from the payload; zero → no time in the footer
}
```

Values are plain text plus a small set of typed parts (text, code, link, bold); **no handler writes HTML any
more**, which makes escaping a renderer concern and closes the injection anti-goal in one place.

The dispatcher result carries the `Notification` instead of the string. Translations stay in `locales/*.json`;
keys that contained HTML layout (`push_header`, `item_title`, `item_actor`, …) are replaced by plain labels
(`field_repo`, `field_branches`, `button_open_pr`, …).

### Two renderers

`internal/handler/render` (or `internal/render`): `Rich(lang, n) string` produces the rich HTML in the order
listed under Goals; `Classic(lang, n) string` produces today-style Telegram HTML from the same model (heading in
bold, fields as `emoji label: value` lines, quote as `<blockquote expandable>`, sections as plain lists with the
current limits, buttons dropped from the text and sent as an inline keyboard instead, actor line, no
`<tg-time>` or a plain formatted time).

### Per-chat settings and `/configure`

Migration `0003_chat_settings.sql` adds to `chats`:

- `format TEXT NOT NULL DEFAULT 'rich'` — `rich` or `classic`;
- `use_topics INTEGER NOT NULL DEFAULT 1` — whether notifications are routed to forum topics.

Existing chats get `rich` and keep their topics (behaviour of an upgraded install: new look, same routing).

`/configure` becomes a short setup with inline buttons under one message, edited in place as the user answers
(this needs callback-query handling, which the bot does not have yet):

1. **Topics** — "Create topics" / "No topics, post to the main chat". Asked only in a forum chat; in a regular
   group the step is skipped and `use_topics = 0` (today `/configure` refuses non-forum chats; it no longer
   does). Choosing topics runs the existing topic creation; choosing none keeps existing topics but stops
   routing to them.
2. **Look** — "New (recommended)" / "Classic (for old or third-party clients)".
3. A summary of what was set, with the language, as the final edit of the message.

Only chat administrators (or the user who sent `/configure`, if the bot cannot read admins) may press the
buttons; others get a short callback alert. Callback data is short and versioned, e.g. `cfg1:topics:yes`.
Running `/configure` again shows the same steps with the current values marked, so it also serves as the way to
change them. Re-running without pressing anything changes nothing.

The server reads `format` and `use_topics` per target chat: `use_topics = 0` means topic id 0 for every event.

### Sending

`Sender` gets `SendNotification(chatID, topicID, format, lang, n)`. The bot implementation:

1. if the chat format is `classic` or the kill switch is off → `sendMessage` with `Classic(...)` and the
   inline keyboard built from `Buttons`;
2. otherwise `sendRichMessage` with `html = Rich(...)`, `skip_entity_detection = true`, thread id as today;
3. if that returns `400 Bad Request`, the description is classified:
   - chat/topic problems (topic deleted or closed, thread not found, chat not found, not enough rights) are
     returned as errors unchanged, so the server's existing "retry in the main chat" path handles them and a
     dead topic costs two calls, not four;
   - any other 400 is treated as rejected rich content: the error is logged once per process per description
     and `Classic(...)` is sent to the same chat and topic;
4. any other error (429, network, 403) is returned as today — retries are NTF-15's job.

The exact description substrings for step 3 are taken from the Bot API / real responses during implementation
and covered by sender tests.

Metrics: `notifier_messages_sent_total` gains a `format` label (`rich`, `classic`) and counts every attempt, so
a rejected rich attempt is a visible `format="rich", result="error"` and the classic retry a separate
`format="classic", result="ok"`. A new counter `notifier_rich_fallbacks_total` counts fallbacks; the Grafana
dashboard (`docs/grafana-dashboard.json`) gets a panel for it and its send panels split by format.

### Kill switch

`RICH_MESSAGES` env (default `true`). `false` forces classic for every chat regardless of its setting — for a
Telegram server or a Bot API proxy (`TELEGRAM_API_URL`) that does not support 10.x yet. Documented in README and
`.env.example`.

### Per-event content

| Event | Heading examples | Fields (in order) | Sections | Buttons |
|---|---|---|---|---|
| push / Push Hook | 🚀 Push to `main`, ⚠️ Force push, 🆕 New branch, 🗑️ Deleted branch | repository, commits count, pusher e-mail | commits, files | Compare (primary) |
| tag push / Tag Push Hook | 🏷 New tag / Tag deleted | repository | — | Open tag |
| pull_request / MR | 🔀 New PR, 📝 draft, 🟣 merged, 🚫 closed, 🔄 reopened | repository, branches (`owner:branch` for forks), changes | — | Open PR (primary), Files changed |
| pull_request_review | ✅ approved / ❗ changes requested / 💬 commented | repository | — | Open review |
| issues / Issue Hook (+ confidential) | 🐛 New / ✅ closed / 🔄 reopened | repository, labels if present | — | Open issue |
| issue_comment / Note Hook (+ confidential) | 💬 New comment on … | repository | — | Open comment |
| release / Release Hook | 📦 New release / 🧪 pre-release | repository, tag | — | Open release (primary) |
| workflow_run / Pipeline Hook | ✅ CI passed / ❌ CI failed / ⛔ cancelled | repository, branch · commit, commit message, duration | — | Open run (danger on failure, success on pass) |
| star / fork / ping | ⭐ / 🍴 / 🏓 | repository, stars count / fork link | — | Open repository / fork |

Confidential issues and notes keep the 🔒 line as a `Note`. Skip rules do not change.

### Tests

- Golden files keep the layout `testdata/<platform>/<event>/<case>.golden`; each now has `--- en rich ---`,
  `--- en classic ---`, `--- ru rich ---`, `--- ru classic ---` sections plus the buttons list for classic.
- A rich-HTML checker (like `assertTelegramHTML`) allows only the rich tag set used by the renderer and checks
  nesting; a test feeds hostile text (`</table><tg-button …>`, `<details>`) through every field and asserts it
  comes out escaped.
- Sender tests with a fake Bot API: rich OK → one call; 400 rich-content → rich + classic with keyboard; 400
  "thread not found" → error returned, no classic attempt; 429 → error returned, no fallback; chat format
  `classic` or `RICH_MESSAGES=false` → classic only.
- Migration test for `0003` on a version-2 database; storage tests for the new settings.
- `/configure` tests: non-forum chat skips the topics step; button presses by a non-admin are refused; the
  chosen values are stored and shown on re-run; server routes to topic 0 when `use_topics = 0`.
- Manual check on the local bot: send all fixtures to the test group (paced under the 20/min group limit) and
  compare on desktop and phone.

## Decided by the owner

- Approach A (rich messages) for all notifications; URL buttons only.
- Per-chat choice in `/configure` of topics (optional) and look (new by default, classic for old clients),
  instead of a single installation-wide default.
- The look of the spike: heading, bold item link, compact table, expandable quote, collapsed lists, coloured
  buttons, footer with actor and `<tg-time>`.

## Builder's calls (marked as proposals)

- Classic fallback only for 400s that are not chat/topic errors; `RICH_MESSAGES` as an installation-wide
  kill switch (default on).
- Metrics per attempt with a `format` label plus a fallback counter (builder review).
- Existing chats migrate to `format = rich`, `use_topics = 1`; only admins may press the setup buttons.
- Event time in the footer comes from the payload (`head_commit.timestamp`, `updated_at`, `created_at`,
  `object_attributes.updated_at`); `<tg-time format="r">` (relative, rendered by the client in the reader's
  language and time zone).
- Commit list in `<details>` grows from 5 to 20 entries.
- Button labels, colours and the per-event table above can be adjusted during implementation if a field turns
  out to be missing in real payloads; such changes are reflected in the golden files.
- Package name and exact struct shapes are left to the builder.
