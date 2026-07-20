#!/usr/bin/env bash
#
# TermRouter VPS setup helper (PRD revision4 §8, §15, §17).
#
# Creates the dedicated service user, stable paths, permissions, a systemd
# unit, and a UFW firewall matching the narrow public-host exposure. It does
# NOT invent domain names, IPs, provider credentials, or spending limits.
#
# Idempotent: re-running is safe.
set -euo pipefail

TERMROUTER_USER="termrouter"
BIN_SRC="${TERMROUTER_BIN_SRC:-./termrouter}"
BIN_DST="/opt/termrouter/termrouter"
ETC_DIR="/etc/termrouter"
VAR_DIR="/var/lib/termrouter"
LOG_DIR="/var/log/termrouter"
SSH_PORT="${SSH_PORT:-22}"
# Validate SSH_PORT is a numeric port; fall back to 22 if misconfigured so we
# never emit a malformed ufw rule.
case "$SSH_PORT" in
  ''|*[!0-9]*) log "SSH_PORT must be numeric; falling back to 22"; SSH_PORT=22 ;;
esac

log() { printf '[setup] %s\n' "$*"; }

# --- Stage 4: service user and paths -----------------------------------------
if ! id -u "$TERMROUTER_USER" >/dev/null 2>&1; then
  log "creating system user $TERMROUTER_USER"
  sudo useradd --system --home "$VAR_DIR" --shell /usr/sbin/nologin "$TERMROUTER_USER"
fi

sudo install -d -o "$TERMROUTER_USER" -g "$TERMROUTER_USER" -m 0750 "$VAR_DIR"
sudo install -d -o "$TERMROUTER_USER" -g "$TERMROUTER_USER" -m 0750 "$LOG_DIR"
sudo install -d -o root -g "$TERMROUTER_USER" -m 0750 "$ETC_DIR"
sudo install -d -o root -g root -m 0755 /opt/termrouter

# --- Stage 5: install binary ------------------------------------------------
if [ -f "$BIN_SRC" ]; then
  log "installing binary to $BIN_DST"
  sudo install -o root -g root -m 0755 "$BIN_SRC" "$BIN_DST"
  "$BIN_DST" version || log "warning: version check failed"
else
  log "binary not found at $BIN_SRC; skipping install (set TERMROUTER_BIN_SRC)"
fi

# --- Stage 16: install systemd unit -----------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "$SCRIPT_DIR/termrouter.service" ]; then
  sudo install -o root -g root -m 0644 "$SCRIPT_DIR/termrouter.service" \
    /etc/systemd/system/termrouter.service
  sudo systemctl daemon-reload
  log "installed systemd unit (not enabled/started automatically)"
fi

# --- Stage 10: firewall ------------------------------------------------------
if command -v ufw >/dev/null 2>&1; then
  log "configuring UFW (allow SSH on port $SSH_PORT first)"
  sudo ufw default deny incoming
  sudo ufw default allow outgoing
  sudo ufw allow "$SSH_PORT"/tcp comment "OpenSSH"
  sudo ufw allow 80/tcp comment "HTTP/ACME"
  sudo ufw allow 443/tcp comment "HTTPS inference"
  # Enable only after confirming SSH works in a second session.
  log "review the rules, then run: sudo ufw enable"
  sudo ufw status verbose || true
else
  log "ufw not found; configure the host and provider firewall manually (SSH, 80, 443 only)"
fi

log "setup complete. Next: configure providers, routes, a portable key, Caddy, then enable."
