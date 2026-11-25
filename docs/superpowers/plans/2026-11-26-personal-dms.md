# Personal DMs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A person links their GitHub/GitLab account with a PAT and gets DMs for mentions, assignments, review
requests and their own failed pipelines.

**Architecture:** A DM is an ordinary `destinations` row; personal messages are ordinary outbox rows for it.
`internal/personal` finds who an event concerns; `storage` maps accounts to people; the server adds personal
outbox rows next to the chat rows; the outbox checks membership for private repositories before sending.

**Tech Stack:** Go 1.27, SQLite (modernc), telego v1.13, Discord REST v10, no new dependencies.

**Spec:** `docs/superpowers/specs/2026-11-26-personal-dms-design.md`

## Global Constraints

- No new dependencies; no Discord/MAX SDKs.
- Telegram and Discord goldens (`internal/handler/testdata/*.golden`, `*.discord.golden`) must not change.
- The token is never stored, logged or included in an error or reply text.
- Commits: Angular subject without a scope, detailed English body, `Refs NTF-30.`, no attribution trailers.
- Every user-facing string is in `locales/en.json` and `locales/ru.json`.
- Go env: `export PATH=/d/GoSDK/go1.27.3/bin:$PATH`; no `-race` (no gcc).

## Review Focus

1. A token in an error: a GitLab host that answers 500 with the request echoed, a URL error that contains the
   Authorization header — the reply and the log must show only host and status.
2. A login renamed on GitHub after linking: ID-based hits still match; mention hits by the old login stop.
3. An event whose actor is the linked person: no DM (except failed pipeline).
4. A person linked in both Telegram and Discord with one DM inactive: the other DM still goes.
5. The membership check erroring (Telegram 429): the row is retried, not dropped and not sent.

---

### Task 1: People and accounts in storage

**Files:**
- Create: `storage/migrations/0006_people.sql`, `storage/people.go`
- Test: `storage/people_test.go`

**Interfaces — Produces:**
- `type Account struct { Platform, Host, AccountID, Login string }` (Platform `"github"`/`"gitlab"`, Host
  lower-case).
- `type Person struct { ID int64; Messenger, UserID string; DestinationID int64; Muted []string }`
- `type Recipient struct { Person; Account }`
- `func (s *Store) Link(messenger, userID string, destID int64, a Account) (replaced bool, err error)` — upserts
  the person (keeps Muted, updates DestinationID); a row of the same account in the same messenger owned by
  another person is deleted (`replaced = true`).
- `func (s *Store) PersonOf(messenger, userID string) (*Person, []Account, error)` — nil person when none.
- `func (s *Store) Unlink(messenger, userID, platform, login string) (int, error)` — login `""` = all of that
  platform.
- `func (s *Store) UnlinkLogin(platform, login string) (int, error)` — admin: every messenger.
- `func (s *Store) SetMuted(personID int64, muted []string) error`
- `func (s *Store) Recipients(platform, host string, ids, logins []string) ([]Recipient, error)` — accounts whose
  ID is in ids or whose login (case-insensitive) is in logins, joined with their person; only persons whose
  destination is active.
- `func (s *Store) Links() ([]Recipient, error)` — all, for the web panel.

- [ ] **Step 1: Write failing tests** `TestLinkReplacesOtherOwner`, `TestLinkKeepsMuted`,
  `TestRecipientsByIDAndLogin` (login `Alice` matches `alice`; inactive destination excluded),
  `TestUnlinkByLoginAndAll`, `TestPersonDeletedWithDestination` (cascade), `TestMigrationFromV5`.
- [ ] **Step 2: Run** `go test ./storage/` — expect FAIL (undefined).
- [ ] **Step 3: Implement** the migration (tables in the spec, `COLLATE NOCASE` on login, index on
  `(platform, host, account_id)` and `(platform, host, login)`) and the methods.
- [ ] **Step 4: Run** `go test ./storage/` — expect PASS.
- [ ] **Step 5: Commit** `feat: store linked GitHub and GitLab accounts`.

### Task 2: Who an event concerns

**Files:**
- Create: `internal/personal/personal.go`, `internal/personal/mentions.go`
- Test: `internal/personal/personal_test.go`, `internal/personal/mentions_test.go`

**Interfaces — Produces:**
- `type Reason string`; consts `Review = "review"`, `Assigned = "assigned"`, `Mention = "mention"`,
  `Pipeline = "pipeline"`; `var Order = []Reason{Review, Assigned, Mention, Pipeline}`.
- `type Hit struct { AccountID, Login string; Reason Reason }`
- `type Event struct { Platform, Host string; Private bool; ActorID, ActorLogin string; Hits []Hit }`
- `func Extract(p platform.Platform, eventName string, payload []byte) Event` — table in the spec; Platform is
  `"github"`/`"gitlab"`; Host from the repository URL.
- `func Mentions(text string) []string` — logins, lower-case, deduplicated, in order.

- [ ] **Step 1: Write failing tests**
  - `TestMentions`: `"hi @Alice and @bob-2, mail a@b.c, see @org/team, `@code`, ```\n@block\n```"` →
    `["alice", "bob-2"]`.
  - One table test `TestExtract` with inline JSON per row of the spec table, asserting Hits, Host, Private,
    Actor; plus `review_requested` for a team (no hit), workflow_run success (no hit), GitLab MR update without
    people changes (no hit), GitLab `visibility_level: 20` (not private), `10` (private).
- [ ] **Step 2: Run** `go test ./internal/personal/` — expect FAIL.
- [ ] **Step 3: Implement** with small DTOs local to the package (do not import `internal/handler`).
- [ ] **Step 4: Run** — expect PASS.
- [ ] **Step 5: Commit** `feat: find the people a webhook event concerns`.

### Task 3: Notifications only for DMs

**Files:**
- Modify: `internal/handler/handler.go`, `github_pull_request.go`, `github_issues.go`, `gitlab_events.go`,
  `internal/dispatcher/dispatcher.go`, `locales/*.json`
- Test: `internal/handler/personal_only_test.go`, `internal/dispatcher/dispatcher_test.go`

**Interfaces — Produces:**
- `var ErrPersonalOnly = errors.New("personal only")` in `handler`; a handler returns `(n, ErrPersonalOnly)`.
- `dispatcher.Result` gains `PersonalOnly bool`; such a result has `Skip: true` and a filled `Notification`.

- [ ] **Step 1: Write failing tests**: GitHub PR `assigned` and `review_requested`, issue `assigned`, GitLab MR
  `update` with `changes.reviewers`, GitLab issue `update` with `changes.assignees` each return ErrPersonalOnly
  with a heading (`👤 Assigned` / `👀 Review requested`) and the item link; GitLab MR `update` without people
  changes still returns ErrSkip; `TestDispatchPersonalOnly` → `Skip && PersonalOnly && Notification.Heading != ""`.
- [ ] **Step 2: Run** `go test ./internal/handler/ ./internal/dispatcher/` — expect FAIL.
- [ ] **Step 3: Implement**; new locale keys `pr_assigned`, `pr_review_requested`, `issue_assigned`,
  `mr_assigned`, `mr_review_requested`.
- [ ] **Step 4: Run** `go test ./...` — expect PASS, goldens unchanged.
- [ ] **Step 5: Commit** `feat: build DM-only notifications for assignments and review requests`.

### Task 4: Personal rows in the outbox

**Files:**
- Modify: `internal/server/server.go`, `internal/outbox/outbox.go` (Payload only), `locales/*.json`
- Test: `internal/server/personal_test.go`

**Interfaces:**
- Consumes: Task 1 `Recipients`, Task 2 `Extract`, Task 3 `Result.PersonalOnly`.
- Produces: `outbox.Payload` gains `Member *MemberCheck` with
  `type MemberCheck struct { RepoID int64; UserID string }` (set only for private repositories).
  Locale keys `personal_review`, `personal_assigned`, `personal_mention`, `personal_pipeline`.

- [ ] **Step 1: Write failing tests** (server env with fake messengers):
  - `TestMentionGoesToLinkedDM`: comment mentioning `@alice`, alice linked → one chat row + one DM row whose
    payload's first note is the mention reason in the DM's language.
  - `TestActorGetsNoDM`; `TestFailedPipelineGoesToActor`.
  - `TestOneDMPerPerson` (assigned and mentioned → assigned reason only).
  - `TestMutedReasonSkipped`.
  - `TestPersonalOnlyEventSkipsChats` (PR assigned → only the DM row, answer 202).
  - `TestPrivateRepoRowCarriesMemberCheck` / public → nil.
  - `TestDMInBothMessengers` → two DM rows.
- [ ] **Step 2: Run** `go test ./internal/server/` — expect FAIL.
- [ ] **Step 3: Implement** `personalItems(p, eventName, body, repo, results)` in `server.go`, dispatching per DM
  language with the existing per-language cache; the DM row has no thread. `handleWebhook` treats "only personal
  rows" as queued.
- [ ] **Step 4: Run** `go test ./...` — expect PASS.
- [ ] **Step 5: Commit** `feat: queue DMs for the people an event concerns`.

### Task 5: Membership check before a DM from a private repository

**Files:**
- Modify: `internal/messenger/messenger.go`, `internal/outbox/outbox.go`, `storage/model.go`,
  `internal/messenger/fake/fake.go`
- Test: `internal/outbox/member_test.go`

**Interfaces — Produces:**
- `type MemberChecker interface { IsMember(ctx context.Context, chatID, userID string) (bool, error) }` in
  `messenger`.
- `func (s *Store) SubscribersIn(repoID int64, messenger string) ([]Destination, error)` — active, non-DM
  destinations (exclude destinations that are a person's DM).
- Fake messenger: `SetMembers(map[string][]string)` and `IsMember`.

- [ ] **Step 1: Write failing tests**: member of one subscribed chat → sent; member of none → row done, not
  sent; check error → retried (attempts 1), not sent; adapter without MemberChecker → sent; second row within
  10 min does not call IsMember again.
- [ ] **Step 2: Run** `go test ./internal/outbox/` — expect FAIL.
- [ ] **Step 3: Implement** in `send` before `m.Send`; cache `map[[2]string]memberAnswer` with the outbox clock.
- [ ] **Step 4: Run** `go test ./...` — expect PASS.
- [ ] **Step 5: Commit** `feat: check membership before a DM from a private repository`.

### Task 6: Adapter support

**Files:**
- Modify: `internal/messenger/messenger.go`, `internal/messenger/telegram/*.go`, `internal/messenger/discord/*.go`
- Test: `internal/messenger/telegram/personal_test.go`, `internal/messenger/discord/personal_test.go`

**Interfaces — Produces:**
- `type DirectMessenger interface { DirectChat(ctx context.Context, userID string) (chatID string, err error);
  DeleteCommand(ctx context.Context, c Command) error }` in `messenger`.
- Telegram: `DirectChat` returns userID; `DeleteCommand` calls `deleteMessage` on the command message;
  `IsMember` via `getChatMember` (`left`, `kicked` → false).
- Discord: `DirectChat` → `POST /users/@me/channels {recipient_id}` → channel ID; `DeleteCommand` is a no-op;
  `IsMember`: guild from the cached channel, `GET /guilds/<g>/members/<u>`, 404/10007 → false; error code
  `50007` → ChatGone; `link`/`unlink`/`me` registered with `contexts [0, 1]` and their options (`link`: platform,
  token, host; `unlink`: platform, login).
- [ ] **Step 1: Write failing tests** for each method above against the fake APIs, and
  `TestPersonalCommandsInDMContext`.
- [ ] **Step 2: Run** `go test ./internal/messenger/...` — expect FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./...` — expect PASS.
- [ ] **Step 5: Commit** `feat: open DMs and check members in Telegram and Discord`.

### Task 7: /link, /unlink, /me

**Files:**
- Create: `internal/personal/verify.go`, `internal/commands/people.go`
- Modify: `internal/commands/commands.go`, `internal/commands/configure.go` (press routing), `locales/*.json`
- Test: `internal/personal/verify_test.go`, `internal/commands/people_test.go`

**Interfaces — Produces:**
- `type Verifier struct { HTTP *http.Client; BaseURL func(platform, host string) string }`;
  `func (v Verifier) WhoAmI(ctx, platform, host, token string) (storage.Account, error)` — GitHub
  `Authorization: Bearer`, GitLab `PRIVATE-TOKEN`; errors are `fmt.Errorf("%s answered %d", host, status)` or
  `"%s: unreachable"`; 401/403 → `ErrBadToken`.
- `Core` gains a `Verifier`; menu adds `link`, `unlink`, `me`. Press data `me:<reason>` toggles.

- [ ] **Step 1: Write failing tests**:
  - verify: GitHub/GitLab success, 401 → ErrBadToken, 500 with the token echoed in the body → error without the
    token, unreachable host → error without the token.
  - commands: Telegram `/link` in a group refused, message deleted, no request made; in DM → linked, message
    deleted, reply has the login and no token; GitLab without host and two hosts → lists hosts; one host → used;
    `/link` with no args → help text; `/unlink github` removes; admin `/unlink github alice` removes someone
    else's; non-admin naming another login → refused; `/me` lists and toggles (press flips Muted, edit shows
    state); a press by someone other than the person → refused.
- [ ] **Step 2: Run** `go test ./internal/personal/ ./internal/commands/` — expect FAIL.
- [ ] **Step 3: Implement.** Hosts for GitLab come from `store.Repos()` (platform GitLab, host of URL).
- [ ] **Step 4: Run** `go test ./...` — expect PASS.
- [ ] **Step 5: Commit** `feat: link accounts and manage personal DMs from the chat`.

### Task 8: Web panel, wiring and docs

**Files:**
- Modify: `internal/web/web.go` (+ template), `main.go`, `README.md`, `CHANGELOG.md`,
  `docs/superpowers/specs/2026-11-17-v2-foundation-design.md` (non-goals line)
- Test: `internal/web/web_test.go`

- [ ] **Step 1: Write failing test** `TestPeopleTable`: the panel shows messenger, user, platform, host, login;
  no column could hold a token.
- [ ] **Step 2: Run** — expect FAIL.
- [ ] **Step 3: Implement**; wire `Verifier{HTTP: http.DefaultClient}` in `main.go`; README section
  "🔔 Personal DMs" (token scopes, commands, private-repo rule); CHANGELOG entry.
- [ ] **Step 4: Run** `go test ./... && go vet ./...` — expect PASS.
- [ ] **Step 5: Commit** `feat: show linked accounts in the panel and document personal DMs`.
