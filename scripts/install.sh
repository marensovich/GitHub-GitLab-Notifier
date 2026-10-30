#!/usr/bin/env bash
#
# GitHub-GitLab Notifier — installer for Linux (systemd).
#
# Installs the bot as a systemd service that starts on boot and restarts on
# failure. Works on any systemd-based distribution (Debian, Ubuntu, Fedora,
# RHEL/Rocky/Alma, Arch, openSUSE, Raspberry Pi OS, ...), amd64 and arm64.
#
# Quick install (latest release):
#   curl -fsSL https://raw.githubusercontent.com/marensovich/GitHub-GitLab-Notifier/master/scripts/install.sh | sudo bash
#
# From a cloned repository (builds from source if Go is installed):
#   sudo ./scripts/install.sh install --from-source
#
# Commands:
#   install     Install or reinstall the service (default)
#   update      Replace the binary with a new version and restart the service
#   uninstall   Stop and remove the service (add --purge to delete config and data)
#   status      Show service status and recent logs
#
# Options:
#   --version <tag>      Release to install, e.g. v1.0.0 (default: latest)
#   --from-source        Build from the local checkout with Go instead of downloading
#   --binary <path>      Install an already built binary
#   --non-interactive    Never prompt; take settings from environment variables
#   --purge              With uninstall: also remove configuration and database
#   -h, --help           Show this help
#
# Non-interactive install example:
#   sudo BOT_TOKEN=123:abc TELEGRAM_BOT_USERNAME=MyGitBot DOMAIN=bot.example.com \
#        ./scripts/install.sh install --non-interactive
#
set -euo pipefail

# ─── Settings ────────────────────────────────────────────────────────────────

REPO="marensovich/GitHub-GitLab-Notifier"
SERVICE_NAME="github-gitlab-notifier"
SERVICE_USER="notifier"
BINARY_NAME="github-gitlab-notifier"

INSTALL_DIR="/opt/${SERVICE_NAME}"
CONFIG_DIR="/etc/${SERVICE_NAME}"
CONFIG_FILE="${CONFIG_DIR}/notifier.env"
DATA_DIR="/var/lib/${SERVICE_NAME}"
UNIT_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

COMMAND="install"
VERSION="latest"
FROM_SOURCE=0
BINARY_PATH=""
INTERACTIVE=1
PURGE=0

SCRIPT_DIR=""
SELF="curl -fsSL https://raw.githubusercontent.com/${REPO}/master/scripts/install.sh | sudo bash -s --"
if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    SELF="sudo ${SCRIPT_DIR}/$(basename "${BASH_SOURCE[0]}")"
fi
TMP_DIR=""

# ─── Output helpers ──────────────────────────────────────────────────────────

if [[ -t 1 ]]; then
    C_RESET=$'\033[0m'; C_BOLD=$'\033[1m'; C_RED=$'\033[31m'
    C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'; C_BLUE=$'\033[34m'
else
    C_RESET=""; C_BOLD=""; C_RED=""; C_GREEN=""; C_YELLOW=""; C_BLUE=""
fi

info()    { printf '%s==>%s %s\n' "${C_BLUE}${C_BOLD}" "${C_RESET}" "$*"; }
success() { printf '%s✔%s  %s\n' "${C_GREEN}" "${C_RESET}" "$*"; }
warn()    { printf '%s!%s  %s\n' "${C_YELLOW}" "${C_RESET}" "$*" >&2; }
die()     { printf '%s✖  %s%s\n' "${C_RED}" "$*" "${C_RESET}" >&2; exit 1; }

usage() {
    if [[ -n "${SCRIPT_DIR}" ]]; then
        sed -n '2,/^set -euo/p' "${BASH_SOURCE[0]}" | sed -e '$d' -e 's/^# \{0,1\}//'
    else
        echo "Usage: install.sh [install|update|uninstall|status] [--version TAG] [--from-source]"
        echo "                  [--binary PATH] [--non-interactive] [--purge]"
        echo "Docs: https://github.com/${REPO}#-installation-on-linux-systemd"
    fi
    exit 0
}

cleanup() {
    if [[ -n "${TMP_DIR}" && -d "${TMP_DIR}" ]]; then
        rm -rf "${TMP_DIR}"
    fi
}
trap cleanup EXIT

# ─── Argument parsing ────────────────────────────────────────────────────────

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            install|update|uninstall|status) COMMAND="$1" ;;
            --version)         VERSION="${2:?--version requires a value}"; shift ;;
            --from-source)     FROM_SOURCE=1 ;;
            --binary)          BINARY_PATH="${2:?--binary requires a path}"; shift ;;
            --non-interactive) INTERACTIVE=0 ;;
            --purge)           PURGE=1 ;;
            -h|--help)         usage ;;
            *) die "Unknown argument: $1 (see --help)" ;;
        esac
        shift
    done

    # When piped through "curl | bash" stdin is the script itself:
    # read answers from the terminal, or fall back to non-interactive mode.
    # Only "install" asks questions.
    if [[ ${INTERACTIVE} -eq 1 && "${COMMAND}" == "install" ]]; then
        if ( exec </dev/tty ) 2>/dev/null; then
            exec 3</dev/tty
        else
            warn "No terminal available, switching to --non-interactive mode"
            INTERACTIVE=0
        fi
    fi
}

# ─── Checks ──────────────────────────────────────────────────────────────────

require_root() {
    if [[ ${EUID} -ne 0 ]]; then
        die "This script must be run as root (use sudo)"
    fi
}

require_systemd() {
    [[ "$(uname -s)" == "Linux" ]] || die "Only Linux is supported by this installer"
    command -v systemctl >/dev/null 2>&1 || die "systemd (systemctl) is required"
    [[ -d /run/systemd/system ]] || die "systemd is not running (containers/WSL without systemd are not supported)"
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)   echo "amd64" ;;
        aarch64|arm64)  echo "arm64" ;;
        *) die "Unsupported architecture: $(uname -m). Build from source with --from-source" ;;
    esac
}

download() { # url dest
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --retry 3 -o "$2" "$1"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "$2" "$1"
    else
        die "curl or wget is required"
    fi
}

# ─── Obtaining the binary ────────────────────────────────────────────────────

# Prints the path of a ready-to-install binary.
# Runs in a $(...) subshell: TMP_DIR must be created by the caller.
obtain_binary() {
    if [[ -n "${BINARY_PATH}" ]]; then
        [[ -f "${BINARY_PATH}" ]] || die "Binary not found: ${BINARY_PATH}"
        echo "${BINARY_PATH}"
        return
    fi

    local repo_root=""
    if [[ -n "${SCRIPT_DIR}" && -f "${SCRIPT_DIR}/../go.mod" ]]; then
        repo_root="$(cd "${SCRIPT_DIR}/.." && pwd)"
    fi

    if [[ ${FROM_SOURCE} -eq 1 ]]; then
        [[ -n "${repo_root}" ]] || die "--from-source must be run from a cloned repository"
        build_from_source "${repo_root}"
    else
        download_release
    fi
}

build_from_source() {
    local root="$1" go_bin
    go_bin="$(command -v go || true)"
    [[ -z "${go_bin}" && -x /usr/local/go/bin/go ]] && go_bin=/usr/local/go/bin/go
    [[ -n "${go_bin}" ]] || die "Go is not installed. Install it from https://go.dev/dl/ or omit --from-source"

    local version build_time
    version="$(git -C "${root}" describe --tags --always --dirty 2>/dev/null || echo dev)"
    build_time="$(date -u '+%Y-%m-%d_%H:%M:%S')"

    info "Building ${version} from source with $("${go_bin}" version | awk '{print $3}')" >&2
    (cd "${root}" && CGO_ENABLED=0 "${go_bin}" build -trimpath \
        -ldflags "-s -w -X main.Version=${version} -X main.BuildTime=${build_time}" \
        -o "${TMP_DIR}/${BINARY_NAME}" .) >&2 || die "Build failed"

    echo "${TMP_DIR}/${BINARY_NAME}"
}

download_release() {
    local arch tag url archive
    arch="$(detect_arch)"
    tag="${VERSION}"

    if [[ "${tag}" == "latest" ]]; then
        info "Looking up the latest release of ${REPO}" >&2
        download "https://api.github.com/repos/${REPO}/releases/latest" "${TMP_DIR}/release.json" \
            || die "Cannot reach the GitHub API. Use --from-source or --binary instead"
        tag="$(grep -m1 '"tag_name"' "${TMP_DIR}/release.json" | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
        [[ -n "${tag}" ]] || die "No published releases found. Use --from-source or --binary instead"
    fi

    archive="GitHub-GitLab-Notifier-linux-${arch}.tar.gz"
    url="https://github.com/${REPO}/releases/download/${tag}/${archive}"

    info "Downloading ${tag} (linux/${arch})" >&2
    download "${url}" "${TMP_DIR}/${archive}" || die "Download failed: ${url}"

    tar -xzf "${TMP_DIR}/${archive}" -C "${TMP_DIR}" || die "Cannot extract ${archive}"

    local bin
    bin="$(find "${TMP_DIR}" -type f -name "GitHub-GitLab-Notifier-linux-${arch}" | head -n1)"
    [[ -n "${bin}" ]] || die "Binary not found in ${archive}"
    echo "${bin}"
}

install_binary() {
    local src="$1"
    install -d -m 0755 "${INSTALL_DIR}"
    install -m 0755 "${src}" "${INSTALL_DIR}/${BINARY_NAME}.new"
    mv -f "${INSTALL_DIR}/${BINARY_NAME}.new" "${INSTALL_DIR}/${BINARY_NAME}"
    success "Installed $("${INSTALL_DIR}/${BINARY_NAME}" --version 2>/dev/null || echo "${BINARY_NAME}") to ${INSTALL_DIR}"
}

# ─── User and directories ────────────────────────────────────────────────────

create_user() {
    if id -u "${SERVICE_USER}" >/dev/null 2>&1; then
        return
    fi
    local nologin
    nologin="$(command -v nologin || echo /usr/sbin/nologin)"
    useradd --system --home-dir "${DATA_DIR}" --no-create-home \
        --shell "${nologin}" --comment "GitHub-GitLab Notifier" "${SERVICE_USER}"
    success "Created system user '${SERVICE_USER}'"
}

create_dirs() {
    install -d -m 0750 -o root -g "${SERVICE_USER}" "${CONFIG_DIR}"
    install -d -m 0750 -o "${SERVICE_USER}" -g "${SERVICE_USER}" "${DATA_DIR}"
}

# ─── Configuration ───────────────────────────────────────────────────────────

# ask VAR "Question" "default" [secret]
# Uses the existing environment value if set, otherwise prompts (interactive)
# or falls back to the default.
ask() {
    local var="$1" question="$2" default="${3:-}" secret="${4:-}" answer=""
    local current="${!var:-}"

    if [[ -n "${current}" ]]; then
        return
    fi
    if [[ ${INTERACTIVE} -eq 0 ]]; then
        printf -v "${var}" '%s' "${default}"
        return
    fi

    local hint=""
    [[ -n "${default}" ]] && hint=" [${default}]"
    while :; do
        if [[ -n "${secret}" ]]; then
            read -r -s -u 3 -p "  ${question}${hint}: " answer; echo
        else
            read -r -u 3 -p "  ${question}${hint}: " answer
        fi
        answer="${answer:-${default}}"
        if [[ -n "${answer}" || "${secret}" == "optional" ]]; then
            break
        fi
        warn "A value is required"
    done
    printf -v "${var}" '%s' "${answer}"
}

detect_public_ip() {
    local ip=""
    if command -v curl >/dev/null 2>&1; then
        ip="$(curl -fsS --max-time 5 https://api.ipify.org 2>/dev/null || true)"
    fi
    echo "${ip}"
}

write_config() {
    if [[ -f "${CONFIG_FILE}" ]]; then
        success "Keeping existing configuration ${CONFIG_FILE}"
        return
    fi

    info "Configuring the bot"
    [[ ${INTERACTIVE} -eq 1 ]] && echo "  Press Enter to accept the value in [brackets]."

    ask BOT_TOKEN "Telegram bot token from @BotFather" "" secret
    ask TELEGRAM_BOT_USERNAME "Bot username (without @)" ""
    ask SERVER_ADDRESS "Listen address" "0.0.0.0"
    ask SERVER_PORT "Listen port" "8080"
    ask USE_HTTPS "Is the bot behind an HTTPS reverse proxy (nginx, Caddy, ...)? true/false" "false"

    # Without a reverse proxy GitHub/GitLab must call the port directly.
    local ip default_domain
    ip="$(detect_public_ip)"
    default_domain="${ip}"
    if [[ -n "${ip}" && "${USE_HTTPS}" != "true" ]]; then
        default_domain="${ip}:${SERVER_PORT}"
    fi
    ask DOMAIN "Public domain or IP[:port] used in webhook URLs" "${default_domain}"
    ask BOT_LANG "Default language (en/ru)" "en"
    ask CHAT_ID "Fallback chat ID (optional, Enter to skip)" "" optional
    ask WEBHOOK_SECRET "Global webhook secret (optional, Enter to skip)" "" optional

    if [[ -z "${BOT_TOKEN:-}" ]]; then
        warn "BOT_TOKEN is empty — edit ${CONFIG_FILE} before starting the service"
    fi

    local tmp
    tmp="$(mktemp)"
    cat > "${tmp}" <<EOF
# GitHub-GitLab Notifier configuration.
# Generated by install.sh on $(date -u '+%Y-%m-%d %H:%M:%S UTC').
# After editing run: sudo systemctl restart ${SERVICE_NAME}
# All options: https://github.com/${REPO}#environment-variables

# --- Telegram ---
BOT_TOKEN=${BOT_TOKEN:-}
TELEGRAM_BOT_USERNAME=${TELEGRAM_BOT_USERNAME:-}
CHAT_ID=${CHAT_ID:-}
BOT_LANG=${BOT_LANG:-en}

# --- HTTP server ---
SERVER_ADDRESS=${SERVER_ADDRESS:-0.0.0.0}
SERVER_PORT=${SERVER_PORT:-8080}

# --- Webhook URL shown by /connect ---
DOMAIN=${DOMAIN:-}
USE_HTTPS=${USE_HTTPS:-true}

# --- Security ---
WEBHOOK_SECRET=${WEBHOOK_SECRET:-}

# --- Proxy for Telegram API (optional) ---
PROXY_ENABLED=${PROXY_ENABLED:-false}
PROXY_HOST=${PROXY_HOST:-}
PROXY_PORT=${PROXY_PORT:-}
PROXY_TYPE=${PROXY_TYPE:-HTTP}

# Self-hosted Telegram Bot API server (optional, default api.telegram.org)
TELEGRAM_API_URL=${TELEGRAM_API_URL:-}

# --- Storage ---
DB_PATH=${DATA_DIR}/notifier.db

# --- Advanced ---
NOTIFY_UNKNOWN_EVENTS=${NOTIFY_UNKNOWN_EVENTS:-false}
EOF
    install -m 0640 -o root -g "${SERVICE_USER}" "${tmp}" "${CONFIG_FILE}"
    rm -f "${tmp}"
    success "Configuration written to ${CONFIG_FILE}"
}

config_value() { # key
    grep -E "^$1=" "${CONFIG_FILE}" 2>/dev/null | tail -n1 | cut -d= -f2- | tr -d '"' || true
}

# ─── systemd unit ────────────────────────────────────────────────────────────

write_unit() {
    local port caps=""
    port="$(config_value SERVER_PORT)"
    if [[ "${port}" =~ ^[0-9]+$ && "${port}" -lt 1024 ]]; then
        # Allow binding to privileged ports (80/443) without running as root.
        caps=$'AmbientCapabilities=CAP_NET_BIND_SERVICE\nCapabilityBoundingSet=CAP_NET_BIND_SERVICE'
    else
        caps="CapabilityBoundingSet="
    fi

    cat > "${UNIT_FILE}" <<EOF
[Unit]
Description=GitHub-GitLab Notifier (Telegram bot for repository webhooks)
Documentation=https://github.com/${REPO}
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=300
StartLimitBurst=10

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
WorkingDirectory=${DATA_DIR}
ExecStart=${INSTALL_DIR}/${BINARY_NAME} --config ${CONFIG_FILE}
Restart=on-failure
RestartSec=5
TimeoutStopSec=20

# Hardening
${caps}
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${DATA_DIR}
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectControlGroups=true
ProtectClock=true
ProtectHostname=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
RestrictNamespaces=true
RestrictRealtime=true
RestrictSUIDSGID=true
LockPersonality=true
SystemCallArchitectures=native
UMask=0027

[Install]
WantedBy=multi-user.target
EOF
    chmod 0644 "${UNIT_FILE}"
    systemctl daemon-reload
    success "systemd unit written to ${UNIT_FILE}"
}

# ─── Service helpers ─────────────────────────────────────────────────────────

health_check() {
    local port addr
    port="$(config_value SERVER_PORT)"; port="${port:-8080}"
    addr="$(config_value SERVER_ADDRESS)"
    [[ -z "${addr}" || "${addr}" == "0.0.0.0" ]] && addr="127.0.0.1"
    command -v curl >/dev/null 2>&1 || return 0

    for _ in $(seq 1 10); do
        if curl -fsS --max-time 2 "http://${addr}:${port}/health" >/dev/null 2>&1; then
            success "Health check passed: http://${addr}:${port}/health"
            return 0
        fi
        sleep 1
    done
    warn "Health check failed. Inspect the logs: journalctl -u ${SERVICE_NAME} -n 50"
    return 1
}

start_service() {
    systemctl enable "${SERVICE_NAME}" >/dev/null 2>&1
    if [[ -z "$(config_value BOT_TOKEN)" ]]; then
        warn "BOT_TOKEN is not set: the service is enabled but not started."
        warn "Edit ${CONFIG_FILE}, then run: sudo systemctl start ${SERVICE_NAME}"
        return
    fi
    systemctl restart "${SERVICE_NAME}"
    sleep 2
    if systemctl is-active --quiet "${SERVICE_NAME}"; then
        success "Service ${SERVICE_NAME} is running and enabled on boot"
        health_check || true
    else
        warn "Service failed to start. Recent logs:"
        journalctl -u "${SERVICE_NAME}" -n 20 --no-pager >&2 || true
        exit 1
    fi
}

print_summary() {
    local domain https proto
    domain="$(config_value DOMAIN)"
    https="$(config_value USE_HTTPS)"
    proto="https"; [[ "${https}" == "false" ]] && proto="http"

    cat <<EOF

${C_BOLD}GitHub-GitLab Notifier is installed.${C_RESET}

  Binary:   ${INSTALL_DIR}/${BINARY_NAME}
  Config:   ${CONFIG_FILE}
  Data:     ${DATA_DIR}
  Webhook:  ${proto}://${domain:-<DOMAIN>}/webhook

${C_BOLD}Next steps${C_RESET}
  1. Add the bot to a Telegram group with Topics enabled and make it an admin.
  2. Send /configure in the group, then /connect <repository_url>.
  3. Create the webhook on GitHub/GitLab with the URL and secret from /connect.

${C_BOLD}Manage the service${C_RESET}
  sudo systemctl status ${SERVICE_NAME}
  sudo systemctl restart ${SERVICE_NAME}
  sudo journalctl -u ${SERVICE_NAME} -f
  ${SELF} update        # upgrade to the latest release
  ${SELF} uninstall     # remove (add --purge to delete data)
EOF
}

# ─── Commands ────────────────────────────────────────────────────────────────

cmd_install() {
    require_root
    require_systemd

    info "Installing ${SERVICE_NAME}"
    TMP_DIR="$(mktemp -d)"
    local bin
    bin="$(obtain_binary)"

    create_user
    create_dirs
    if systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null; then
        systemctl stop "${SERVICE_NAME}"
    fi
    install_binary "${bin}"
    write_config
    write_unit
    start_service
    print_summary
}

cmd_update() {
    require_root
    require_systemd
    [[ -x "${INSTALL_DIR}/${BINARY_NAME}" ]] || die "${SERVICE_NAME} is not installed. Run the install command first"

    info "Updating ${SERVICE_NAME} (current: $("${INSTALL_DIR}/${BINARY_NAME}" --version 2>/dev/null || echo unknown))"
    TMP_DIR="$(mktemp -d)"
    local bin
    bin="$(obtain_binary)"

    cp -f "${INSTALL_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}.bak"
    install_binary "${bin}"
    write_unit
    systemctl restart "${SERVICE_NAME}"
    sleep 2
    if systemctl is-active --quiet "${SERVICE_NAME}"; then
        success "Service restarted with the new version (previous binary: ${BINARY_NAME}.bak)"
        health_check || true
    else
        warn "New version failed to start, rolling back"
        mv -f "${INSTALL_DIR}/${BINARY_NAME}.bak" "${INSTALL_DIR}/${BINARY_NAME}"
        systemctl restart "${SERVICE_NAME}"
        die "Update failed. Logs: journalctl -u ${SERVICE_NAME} -n 50"
    fi
}

cmd_uninstall() {
    require_root
    require_systemd

    info "Removing ${SERVICE_NAME}"
    systemctl disable --now "${SERVICE_NAME}" >/dev/null 2>&1 || true
    rm -f "${UNIT_FILE}"
    systemctl daemon-reload
    rm -rf "${INSTALL_DIR}"
    success "Service and binary removed"

    if [[ ${PURGE} -eq 1 ]]; then
        rm -rf "${CONFIG_DIR}" "${DATA_DIR}"
        if id -u "${SERVICE_USER}" >/dev/null 2>&1; then
            userdel "${SERVICE_USER}" 2>/dev/null || true
        fi
        success "Configuration, database and user '${SERVICE_USER}' removed"
    else
        echo "  Configuration (${CONFIG_DIR}) and database (${DATA_DIR}) were kept."
        echo "  Run with --purge to delete them as well."
    fi
}

cmd_status() {
    require_systemd
    systemctl status "${SERVICE_NAME}" --no-pager || true
    echo
    journalctl -u "${SERVICE_NAME}" -n 20 --no-pager 2>/dev/null || true
}

main() {
    parse_args "$@"
    case "${COMMAND}" in
        install)   cmd_install ;;
        update)    cmd_update ;;
        uninstall) cmd_uninstall ;;
        status)    cmd_status ;;
    esac
}

main "$@"
