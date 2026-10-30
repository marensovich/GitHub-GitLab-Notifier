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
  --data-binary @internal/handler/testdata/github/push.json
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

   func (h *GitHubDiscussionHandler) Handle(lang i18n.Lang, payload []byte) (string, error) {
       var dto GitHubDiscussionDto
       if err := decode(lang, "discussion", payload, &dto); err != nil {
           return "", err
       }
       if dto.Action != "created" {
           return "", ErrSkip // don't notify about noisy actions
       }
       return formatItem(lang, itemView{
           Header:   i18n.T(lang, i18n.MsgDiscussionCreated),
           Repo:     dto.Repository.FullName,
           RepoURL:  dto.Repository.HTMLURL,
           Ref:      fmt.Sprintf("#%d", dto.Discussion.Number),
           URL:      dto.Discussion.HTMLURL,
           Title:    dto.Discussion.Title,
           Actor:    dto.Sender.Login,
           ActorURL: dto.Sender.HTMLURL,
           Body:     dto.Discussion.Body,
       }), nil
   }
   ```
   Use `formatItem` / `formatCI` / `formatTag` where possible so messages look consistent.
   **Always escape user-provided text** (`esc`, `link`, `excerpt` already do it).

3. **Register the handler** in `All()` in `internal/handler/handler.go`.

4. **Add translations**: a `Msg...` constant in `internal/i18n/i18n.go` and the key in **every** file in `locales/`.
   The i18n tests fail if a key or a `%s`/`%d` placeholder is missing in one of the languages.

5. **Add a test**: put a real payload into `internal/handler/testdata/<platform>/` (GitHub shows recent deliveries
   under *Settings → Webhooks → Recent Deliveries*; remove private data) and add a case to `github_test.go` /
   `gitlab_test.go`. The shared test runner also checks that the message is valid Telegram HTML.

6. Update the *Supported events* table in `README.md` and `CHANGELOG.md`.

## Adding a language

1. Copy `locales/en.json` to `locales/<code>.json` and translate the values (keep placeholders in the same order).
2. Add the language to `Supported` in `internal/i18n/i18n.go`.
3. Run `make test`.

## Reporting bugs

Use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.md). Include the bot version (`/version`), how you run
it (systemd / Docker / binary) and the relevant log lines. **Never post your bot token or webhook secrets.**

Security issues: see [SECURITY.md](SECURITY.md).
