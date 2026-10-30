# Security Policy

## Supported versions

Only the latest release receives security fixes.

## Reporting a vulnerability

Please **do not open a public issue** for security problems.

Report them privately via
[GitHub Security Advisories](https://github.com/marensovich/GitHub-GitLab-Notifier/security/advisories/new).
Include a description, steps to reproduce and the affected version. You will get a response within a week.

## Hardening recommendations

- Set `WEBHOOK_SECRET` (or use the per-repository secret from `/connect`) so that only GitHub/GitLab can trigger
  notifications.
- Run the bot behind an HTTPS reverse proxy and bind it to `127.0.0.1` (`SERVER_ADDRESS`).
- Keep the configuration file private: it contains the bot token. The Linux installer stores it with mode `0640`,
  readable only by root and the service user.
- Use the provided systemd unit or Docker image: both run the bot as an unprivileged user.
