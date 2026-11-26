# Personal DMs — design

Task: NTF-30…34 (pulled into 2.0 by the owner). Builds on the 2.0 foundation and the Discord adapter.

## Intent

A team chat shows everything; the one event that is about *you* scrolls away. The bot should also write to a
person directly when an event concerns them: they were mentioned, assigned, asked for a review, or their own
pipeline failed. Any member of a team chat can turn this on for themselves (owner's decision), in Telegram and
Discord now and in MAX when its adapter lands.

## Goals

- `/link github <PAT>` / `/link gitlab <PAT> [host]` ties a GitHub/GitLab account to the messenger user who sent
  it. The token proves the account is theirs (owner's decision: PAT, not OAuth, not trust).
- Personal events go to that user's DM with the bot, with a line saying why: mentioned, assigned, review requested,
  your pipeline failed.
- `/me` shows what is linked and lets the person switch each kind of personal event off and on; `/unlink` removes a
  link.
- Bot administrators (`admins` in `config.yaml`) can remove anyone's link; the web panel lists all links.

## Non-goals

- OAuth sign-in, GitHub Apps, GitLab group hooks.
- Personal events for repositories nobody connected (there is no webhook for them).
- Mentions of teams/groups (`@org/team`), or of everyone (`@all`).
- Digests or quiet hours for DMs.
- MAX (its adapter does not exist yet; the design leaves it a slot).

## Anti-goals

- The token is stored, logged, echoed in an error, or left visible in a chat.
- Someone links an account that is not theirs.
- A person who left the team keeps receiving events of a **private** repository in their DM.
- The people who acted get a DM about their own action ("you assigned yourself").
- Group messages change: the Telegram and Discord goldens stay as they are.

## Approaches considered

1. **People as their own outbox target** — a new queue for DMs next to the chat outbox. Duplicates retries, rate
   limits and per-chat ordering.
2. **A DM is a destination** (chosen) — the private chat with the bot is stored as an ordinary `destinations` row;
   a personal message is an ordinary outbox row for it. Retries, limits, "chat gone" (the user blocked the bot or
   closed DMs) and the web panel's dead-letter view all work unchanged. A Telegram DM is already a destination
   when someone ran `/connect` there.
3. **Fan-out inside handlers** — each handler returns its recipients. Spreads identity logic over 19 handlers and
   mixes "who" with "what the message says".

Choice 2, with recipient extraction in its own package (`internal/personal`), separate from the handlers.

## How it works

### Linking

- `/link <github|gitlab> <token> [host]`.
  - GitHub: `GET https://api.github.com/user` (for a host other than `github.com`: `https://<host>/api/v3/user`).
  - GitLab: `GET https://<host>/api/v4/user`; `host` may be left out when exactly one GitLab host appears among the
    connected repositories, otherwise the bot lists the known hosts and asks for one.
  - The answer gives the account's numeric ID and login. The token is used for this one request and dropped: it
    is never stored, logged, or put in an error text (errors name the host and the HTTP status only).
- Where the command may be typed:
  - Telegram: only in the private chat with the bot. The bot deletes the user's message with the token right after
    reading it. In a group it refuses ("write to me privately"), tries to delete the message, and does not use
    the token.
  - Discord: in a server (the command and its reply are ephemeral, the option is never shown to others) or in the
    DM with the bot; the command is registered for both contexts. The bot opens the DM channel
    (`POST /users/@me/channels`) at link time; that channel is the destination.
- `/link` with no arguments explains which token to create: GitHub fine-grained token with no permissions, or a
  GitLab token with `read_user`.
- An account is linked to at most one person per messenger. A new verified link of the same account in the same
  messenger replaces the old one (the token proved ownership). The same person may link the account in Telegram
  and in Discord; both get the DM.
- `/unlink <github|gitlab> [login]` removes the sender's link(s); an admin may name anyone's login.
- `/me` lists the sender's links and four toggle buttons (mentions, assignments, review requests, failed
  pipelines). Toggles are per person (messenger user), all on by default.

### Storage (migration `0006_people.sql`)

- `people(id, messenger, user_id, destination_id → destinations ON DELETE CASCADE, muted, created_at)`,
  unique `(messenger, user_id)`. `muted` is a comma-separated set of reasons.
- `accounts(id, person_id → people ON DELETE CASCADE, messenger, platform, host, account_id, login, linked_at)`,
  unique `(messenger, platform, host, account_id)`.

### Who gets a DM

`internal/personal` reads the raw webhook payload and returns hits `{Platform, Host, AccountID, Login, Reason}`:

| Event | Reason |
|-------|--------|
| GitHub `pull_request` `review_requested` (user reviewer) | review |
| GitHub `pull_request` / `issues` `assigned` | assigned |
| GitHub `pull_request`/`issues` `opened`, `issue_comment` `created`, `pull_request_review` `submitted`: `@login` in the body | mention |
| GitHub `workflow_run` `completed` with `failure`: `triggering_actor` (else `actor`) | pipeline |
| GitLab `merge_request` open/update: reviewers and assignees added (`changes.reviewers`, `changes.assignees`; on open, all of them) | review / assigned |
| GitLab `issue` open/update: assignees added | assigned |
| GitLab `note`, MR/issue description on open: `@username` | mention |
| GitLab `pipeline` with status `failed`: `user` | pipeline |

- Mentions: `@name` not preceded by a word character, `/` or `.`; text in code blocks and inline code is ignored;
  `@org/team` is ignored. Matched case-insensitively against stored logins (GitLab usernames, GitHub logins).
  Other hits match by account ID.
- The host comes from the repository URL.
- The actor of the event never gets a DM for it, except for the failed-pipeline reason (that is their own run).
- One event gives a person at most one DM; with several reasons the first in the order
  review → assigned → mention → pipeline wins.
- A muted reason gives no DM.

### What the DM says

The same `notify.Notification` the event produces for chats, with one note in front saying why (`🔔 You were
mentioned`, `👤 You were assigned`, `👀 Your review is requested`, `❌ Your pipeline failed`), in the DM
destination's language and look. Events that chats do not show (`assigned`, `review_requested`, GitLab MR/issue
updates that only change people) get a notification built for the DM only: the handler returns it with
`handler.ErrPersonalOnly`, the dispatcher marks it as skipped for chats and keeps it for DMs.

### Private repositories

The DM is sent only while the person is a member of at least one active chat of the same messenger that is
subscribed to the repository, when the repository is private (GitHub `repository.private`; GitLab
`project.visibility_level` below 20). The webhook handler cannot call messenger APIs, so the outbox checks this
before sending: adapters implement `messenger.MemberChecker` (`IsMember(ctx, chatID, userID)`: Telegram
`getChatMember`, status other than `left`/`kicked`; Discord `GET /guilds/<guild>/members/<user>`, the guild taken
from the channel). An answer is cached for 10 minutes. A row whose person is a member nowhere is finished without
sending and logged; an error from the check is retried like a failed send.

### Errors

- Discord `50007` (cannot send messages to this user: DMs closed) marks the DM destination inactive, like a chat
  the bot was removed from. The next command from that user activates it again.
- Telegram "bot was blocked by the user" already maps to "chat gone".

## Decided by the owner

- Any chat member may link themselves, not just the owner.
- Ownership of an account is proved with a personal access token.
- Personal DMs are part of 2.0.
- Command replies are private wherever the messenger supports it (already built).

## Calls I made (correctable)

- The DM is a destination row; personal messages are outbox rows.
- The token is never stored; Telegram deletes the message holding it; `/link` in a Telegram group is refused.
- A new verified link replaces an old link of the same account.
- The membership check only applies to private/internal repositories.
- Per-person toggles for the four reasons; admins unlink by login; the web panel lists links read-only.
- GitLab host is an argument when several GitLab hosts are connected (no host picker buttons).
- GitHub Enterprise hosts are supported through `https://<host>/api/v3`.

## Left to the builder

- Exact locale strings (English and Russian).
- The shape of the web panel table.
- Mention regex details within the rule above.
