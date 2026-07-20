# TermRouter VPS Deployment Guide

Deploy TermRouter on a public VPS so coding agents on different computers can
use one stable endpoint and one TermRouter client API key over standard HTTPS —
no VPN, tunnel client, or custom certificate required on the work computer.

This guide implements the controls in `prd/revision4.md`. It is **fail-closed**:
missing/invalid keys, disallowed routes, and exceeded quotas all fail before any
provider call.

> **Organizational note:** Use this only where authorized by the device owner
> and employer policy. TermRouter cannot bypass corporate network controls.

---

## 1. Architecture

```text
Coding agent (any authorized computer)
        │ Standard HTTPS on TCP 443
        │ Authorization: Bearer tr_live_...
        ▼
Caddy on VPS (public ports 80/443 only, auto TLS)
        │ Local HTTP only
        ▼
TermRouter inference API   127.0.0.1:8787
TermRouter Web Console     127.0.0.1:8788  (loopback only, SSH tunnel)
```

Only inference paths are public. The Web Console and `/admin/v1/*` are never
proxied.

---

## 2. Prerequisites

- Ubuntu/Debian VPS with `systemd` and a public IPv4 address (configure IPv6
  only if you have tested the firewall for it).
- A domain/subdomain (e.g. `ai.example.com`) you control, resolving to the VPS.
- Approved provider credentials for TermRouter to store server-side.
- `ufw`, and (optionally) Caddy from your distribution's package manager.

Do **not** invent domain names, IPs, credentials, or spending limits.

---

## 3. DNS

```bash
A     ai.example.com    <VPS IPv4>
# AAAA only if IPv6 is deliberately enabled and firewalled
```

Verify before requesting certificates:

```bash
dig +short ai.example.com A
```

---

## 4. Install TermRouter

Use `deploy/setup-vps.sh`, which is idempotent:

```bash
sudo TERMROUTER_BIN_SRC=./termrouter bash deploy/setup-vps.sh
```

It creates the `termrouter` system user, stable paths
(`/opt/termrouter`, `/etc/termrouter`, `/var/lib/termrouter`,
`/var/log/termrouter`), installs the systemd unit, and prints the UFW rules.

Confirm the loopback-only listener after starting:

```bash
sudo systemctl enable --now termrouter
sudo ss -ltnp | grep -E ':8787|:8788'
# expect 127.0.0.1:8787 and 127.0.0.1:8788 only
```

---

## 5. Configure providers and public routes

```bash
termrouter provider add openai --type openai --credential-ref openai:default
termrouter alias add coding   --provider openai --model gpt-4o
termrouter alias add auto     --provider openai --model gpt-4o-mini
termrouter alias add fast     --provider openai --model gpt-4o-mini
```

Edit `/etc/termrouter/config.yaml` so the server binds loopback and requires auth:

```yaml
server:
  host: 127.0.0.1
  port: 8787
  auth_required: true
  trusted_proxies:
    - 127.0.0.1/32
  max_request_size: 20MiB
  max_messages: 200
  max_tools: 64
  request_timeout: 180s

public_hosting:
  enabled: true
  external_url: https://ai.example.com
  expose_health: true
  expose_ready: false
  console_public: false   # must stay false; config validation rejects true
```

`public_hosting.console_public: true` is rejected by config validation.

---

## 6. Create the portable client key

```bash
termrouter key create --name portable-agents --portable \
  --alias coding --alias auto --alias fast \
  --rpm 30 --max-concurrent 4 \
  --daily-requests 500 \
  --daily-input-tokens 3000000 --daily-output-tokens 500000 \
  --daily-cost-usd 10 \
  --max-request-body 10485760
```

The plaintext `tr_live_...` is shown **exactly once**. Store only the hash.
Deliver it through a password manager or secure copy — never chat/email/git.

The `--portable` flag prints the shared-key warning. The `doctor` command warns
if a portable key lacks an alias restriction, a request-body limit, or a spend
budget.

Rotate:

```bash
termrouter key rotate <key_id>     # issues new plaintext, disables old hash
termrouter key disable <key_id>    # immediate emergency revocation
```

---

## 7. Install and configure Caddy

Install Caddy from a supported package, then deploy `deploy/Caddyfile` (replace
`ai.example.com`):

```bash
sudo cp deploy/Caddyfile /etc/caddy/Caddyfile
sudo sed -i 's/ai.example.com/your.domain.here/' /etc/caddy/Caddyfile
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

The Caddyfile forwards only:

```text
/health
/ready
/v1/models
/v1/chat/completions
/v1/messages
```

Everything else returns `404`. Request/response bodies and auth headers are not
logged.

---

## 8. Firewall

Allow SSH **first**, then 80/443. Do not open 8787 or 8788.

```bash
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
```

Match the same set in your VPS provider's security group. Keep a second SSH
session open while enabling UFW.

---

## 9. Verify

From an external network:

```bash
curl -I https://ai.example.com/health              # 200, minimal
curl -i https://ai.example.com/v1/models            # 401 without key
curl -i https://ai.example.com/v1/models \
  -H "Authorization: Bearer $TERMROUTER_API_KEY"    # 200 with key
curl -i https://ai.example.com/admin/v1/status      # 404
curl -i https://ai.example.com/                        # 404
```

Check the certificate is publicly trusted (never `curl -k`):

```bash
openssl s_client -connect ai.example.com:443 -servername ai.example.com </dev/null
```

---

## 10. Remote Console administration

The Console stays on `127.0.0.1:8788`. Reach it via SSH tunnel from a trusted
computer:

```bash
ssh -L 8788:127.0.0.1:8788 user@vps
# then open http://127.0.0.1:8788
```

Do not expose the Console as a workaround for restricted clients.

---

## 11. Monitoring and incident response

- `systemctl is-active termrouter caddy`
- Watch for 401/429 spikes, repeated invalid keys, disk > 80%, cert renewal
  failures.
- Emergency: `termrouter key disable <key_id>` immediately stops new requests.
- Rotate provider credentials if deeper compromise is suspected.

---

## 12. Files

| Path | Purpose |
|------|---------|
| `deploy/termrouter.service` | systemd unit (sandboxed) |
| `deploy/Caddyfile` | public reverse proxy with path allowlist |
| `deploy/setup-vps.sh` | idempotent user/path/unit/firewall setup |
| `/etc/termrouter/config.yaml` | server + public_hosting config |
| `/etc/termrouter/termrouter.env` | provider credentials (root:termrouter 0640) |
| `/var/lib/termrouter/` | state (termrouter:termrouter 0750) |
| `/var/log/termrouter/` | logs (termrouter:termrouter 0750) |
