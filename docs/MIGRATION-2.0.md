# Upgrading from 1.x to 2.0

2.0 moves the bot to one core for several messengers (Telegram, Discord, MAX). Your chats, connected
repositories, secrets, topics and per-chat settings carry over. What changes for you is mostly the
configuration file. This guide covers a systemd installation, Docker, and going back to 1.1.x.

## What changes

| 1.x | 2.0 |
|:----|:----|
| Settings in `.env` / `notifier.env` | Settings in `config.yaml`; `${VAR}` still takes values from the environment |
| `--config notifier.env` | `--config config.yaml`; a `.env` given to `--config` stops with the command that converts it |
| `CHAT_ID` received events of unconnected repositories | Events of a repository no chat connected with `/connect` are dropped (`202 SKIPPED`) and listed on the web panel |
| Webhooks answered after sending (`200`) | Webhooks answered once stored (`202 QUEUED`); a send queue delivers them and retries for up to 10 attempts |
| `TELEGRAM_BOT_USERNAME` | Not needed; the bot reads its name from Telegram |
| `--db`, `--lang` | Still work, with a deprecation warning; use `storage.path` and `defaults.language` |

The database schema is upgraded on the first start of 2.0. Before that, the 1.x database is copied once to
`<db>.1x-backup` (for example `/var/lib/github-gitlab-notifier/notifier.db.1x-backup`). Repositories that were in
the 1.x database without a chat are dropped by the upgrade: they had nowhere to post anyway.

How 1.x variables map to `config.yaml`:

| 1.x variable | `config.yaml` |
|:-------------|:--------------|
| `BOT_TOKEN` | `messengers.telegram.token` |
| `SERVER_ADDRESS`, `SERVER_PORT` | `server.listen` (`"127.0.0.1:8080"`) |
| `DOMAIN`, `USE_HTTPS` | `server.public_url` (`"https://bot.example.com"`) |
| `BOT_LANG` | `defaults.language` |
| `NOTIFY_UNKNOWN_EVENTS` | `defaults.notify_unknown_events` |
| `WEBHOOK_SECRET` | `security.webhook_secret` |
| `DB_PATH`, `DELIVERIES_RETENTION_DAYS` | `storage.path`, `storage.deliveries_retention_days` |
| `PROXY_ENABLED`, `PROXY_TYPE`, `PROXY_HOST`, `PROXY_PORT` | `messengers.telegram.proxy` (`"socks5://host:port"`) |
| `TELEGRAM_API_URL` | `messengers.telegram.api_url` |
| `RICH_MESSAGES` | `messengers.telegram.rich_messages` |
| `PANEL_ADDRESS`, `PANEL_ALLOW_REMOTE` | `panel.listen`, `panel.allow_remote` |
| `METRICS_ENABLED`, `METRICS_ADDR` | `metrics.enabled`, `metrics.listen` |
| `CHAT_ID`, `TELEGRAM_BOT_USERNAME` | no longer used |

`notifier migrate-config` does this conversion for you.

## systemd (installed with `install.sh`)

1. Take a backup you control, in addition to the automatic ones:

   ```bash
   sudo cp -a /etc/github-gitlab-notifier ~/notifier-config-1x
   sudo cp /var/lib/github-gitlab-notifier/notifier.db ~/notifier-1x.db
   ```

2. Update with the 2.0 installer:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/marensovich/GitHub-GitLab-Notifier/master/scripts/install.sh | sudo bash -s -- update
   ```

   It stops the service, copies the database aside, converts `/etc/github-gitlab-notifier/notifier.env` to
   `config.yaml` (the old file is kept as `notifier.env.1x`), rewrites the systemd unit to use `config.yaml` and
   starts 2.0. If 2.0 does not start, it puts the 1.x binary, unit, configuration and database back by itself.

3. Check that it runs:

   ```bash
   sudo systemctl status github-gitlab-notifier
   ```

   ```bash
   curl -s http://127.0.0.1:8080/healthz
   ```

   Then send `/status` in one of your chats. `notifier config check --config /etc/github-gitlab-notifier/config.yaml`
   validates the configuration without starting the bot.

4. Read the warnings `migrate-config` printed, if any (for example about `CHAT_ID`), and look at
   `/etc/github-gitlab-notifier/config.yaml`. To add Discord or MAX, see the README.

## Docker

The image reads `/etc/notifier/config.yaml`. The `docker-compose.yml` from the repository mounts `./config.yaml`
there and keeps reading `.env` for the values `config.yaml` refers to.

A container started with only the 1.x `.env` still runs: without `config.yaml` and with `BOT_TOKEN` in the
environment, 2.0 starts from the 1.x variables and logs a warning. Convert once to stop relying on that:

```bash
docker compose pull
rmdir config.yaml 2>/dev/null   # Docker leaves an empty directory where the file was missing
docker run --rm -u 0 -v "$PWD:/conf" ghcr.io/marensovich/github-gitlab-notifier:latest \
  migrate-config --from /conf/.env --to /conf/config.yaml
sudo chown 65532:65532 config.yaml   # the image runs as uid 65532; the file stays 0600
docker compose up -d
```

Check `docker compose logs -f` and `/status` in a chat. The database stays in the `data` volume; its 1.x copy is
`/data/notifier.db.1x-backup` in the same volume.

If you pinned the image (`:1.1`), change the tag to `:2.0` or `:latest`.

## Going back to 1.1.x

1.x cannot read a 2.0 database, so going back means going back to the database from before the upgrade. Anything
done after the upgrade (newly connected repositories, linked accounts, changed settings) is lost.

### systemd

```bash
sudo systemctl stop github-gitlab-notifier
cd /etc/github-gitlab-notifier
sudo mv notifier.env.1x notifier.env
sudo rm config.yaml
cd /var/lib/github-gitlab-notifier
sudo mv notifier.db notifier.db.2x
sudo rm -f notifier.db-wal notifier.db-shm
sudo cp -p notifier.db.1x-backup notifier.db
sudo chown notifier:notifier notifier.db
curl -fsSL https://raw.githubusercontent.com/marensovich/GitHub-GitLab-Notifier/v1.1.1/scripts/install.sh | sudo bash -s -- update --version v1.1.1
```

The 1.1.1 installer puts back the 1.x binary and a unit that reads `notifier.env`.

### Docker

```bash
docker compose down
docker run --rm -v <project>_data:/data alpine \
  sh -c 'mv /data/notifier.db /data/notifier.db.2x; rm -f /data/notifier.db-wal /data/notifier.db-shm; cp -p /data/notifier.db.1x-backup /data/notifier.db'
```

Then set the image tag to `:1.1` in `docker-compose.yml`, remove the `config.yaml` mount and start it again with
`docker compose up -d`. `<project>_data` is the volume: the compose project name (by default the directory name) plus `_data`;
`docker volume ls` shows it.

## After the upgrade

- `/configure` in a chat sets topics and the look as before; Discord and MAX chats use the same commands.
- The web panel now groups chats by messenger and shows the send queue and repositories that send webhooks but
  are connected to no chat.
- Personal DMs are new: see `/link` in the README.
