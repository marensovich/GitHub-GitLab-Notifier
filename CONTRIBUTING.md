# Contributing

Thanks for your interest in GitHub-GitLab Notifier! Bug reports, new event handlers, translations and documentation
fixes are all welcome.

## Workflow

1. Fork the repository and create a branch from **`development`** (not `master`).
2. Make your change with tests.
3. Run the checks:
   ```bash
   make lint
   make test
   ```
4. Open a pull request into `development`. `master` only receives releases.

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/):
`feat(github): ...`, `fix(server): ...`, `docs: ...`, `ci: ...`, `chore: ...`.
Write them in English and explain *why* in the body.

## Development setup

- Go **1.26+**
- A Telegram bot token for manual testing (create a separate test bot with [@BotFather](https://t.me/BotFather))
- Optional: `shellcheck`, Docker

```bash
cp .env.example .env
make run
```

To send a test webhook locally:

```bash
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: push" \
  --data-binary @internal/handler/testdata/github/push/branch.json
```

## Adding a new event handler

Example: GitHub `discussion` events.

1. **Register the event type** in `internal/platform/platform.go`, choosing the topic it belongs to:
   ```go
   GitHubDiscussion = EventType{"discussion", GitHub, TopicIssues}
   ```
   and add it to the `registry` list in the same file.

2. **Write the handler** in `internal/handler/` (e.g. `github_discussion.go`):
   ```go
   type GitHubDiscussionHandler struct{}

   func (h *GitHubDiscussionHandler) GetEventType() platform.EventType {
       return platform.GitHubDiscussion
   }

   func (h *GitHubDiscussionHandler) Handle(lang i18n.Lang, payload []byte) (notify.Notification, error) {
       var dto GitHubDiscussionDto
       if err := decode(lang, "discussion", payload, &dto); err != nil {
           return notify.Notification{}, err
       }
       if dto.Action != "created" {
           return notify.Notification{}, ErrSkip // don't notify about noisy actions
       }
       return itemView{
           Heading:  i18n.T(lang, i18n.MsgDiscussionCreated),
           Repo:     dto.Repository.FullName,
           RepoURL:  dto.Repository.HTMLURL,
           Ref:      fmt.Sprintf("#%d", dto.Discussion.Number),
           URL:      dto.Discussion.HTMLURL,
           Title:    dto.Discussion.Title,
           Actor:    dto.Sender.Login,
           ActorURL: dto.Sender.HTMLURL,
           Body:     dto.Discussion.Body,
           Buttons:  []notify.Button{button(lang, "btn_open_discussion", dto.Discussion.HTMLURL, notify.StylePrimary)},
           Time:     dto.Discussion.CreatedAt.Time, // a handler.Timestamp
       }.notification(), nil
   }
   ```
   A handler returns a `notify.Notification` — what to say, not how: the renderers in `internal/notify` turn it
   into a rich message (`notify.Rich`) and a classic HTML message with inline buttons (`notify.Classic`). Build it
   with `itemView`, `ciView` or the other helpers in `internal/handler` so messages look consistent, and keep
   headings and labels as plain text: the renderers escape everything, so never put HTML into a notification.

3. **Register the handler** in `All()` in `internal/handler/handler.go`.

4. **Add translations**: a `Msg...` constant in `internal/i18n/i18n.go` and the key in **every** file in `locales/`.
   The i18n tests fail if a key or a `%s`/`%d` placeholder is missing in one of the languages.

5. **Add a fixture and its golden file** — a new event is not done without them. Put the payload into
   `internal/handler/testdata/<platform>/<event>/<case>.json`, where `<event>` is the event name in lower case with
   spaces replaced by `_` (`github/discussion/created.json`, `gitlab/note_hook/issue.json`). Take a real delivery
   (GitHub: *Settings → Webhooks → Recent Deliveries*, GitLab: *Webhooks → Edit → Recent events*) and clean it:
   ```bash
   scripts/scrub-fixture.sh payload.json > internal/handler/testdata/github/discussion/created.json
   ```
   The script replaces e-mails and drops avatar/node IDs and installation data; check the rest by hand. Add one
   fixture per interesting case (skipped actions too). Then generate the expected messages and **read them**:
   ```bash
   go test ./internal/handler -run TestGolden -update
   git diff internal/handler/testdata
   ```
   `TestGolden` renders every fixture in every language and compares it with `<case>.golden` (the topic or
   `skip: true`, then per language a `--- <lang> rich ---` section and a `--- <lang> classic ---` section ending
   with its `buttons:` line). It fails if a registered handler has no fixtures, and on any change
   of a template, translation or parser — rerun with `-update` and review the diff when the change is intended.
   For specific assertions you can still add a case to `github_test.go` / `gitlab_test.go`. Both runners check that
   the rich and the classic HTML use only tags Telegram accepts.

6. Update the *Supported events* table in `README.md` and `CHANGELOG.md`.

## Adding a language

1. Copy `locales/en.json` to `locales/<code>.json` and translate the values (keep placeholders in the same order).
2. Add the language to `Supported` in `internal/i18n/i18n.go`.
3. Run `make test`.

## Reporting bugs

Use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.md). Include the bot version (`/version`), how you run
it (systemd / Docker / binary) and the relevant log lines. **Never post your bot token or webhook secrets.**

Security issues: see [SECURITY.md](SECURITY.md).
