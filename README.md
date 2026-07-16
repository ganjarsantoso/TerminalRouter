<div align="center">

# TermRouter

**A terminal-only, multi-provider AI API gateway and protocol router.**

One lightweight binary. One local API endpoint. Multiple AI providers.

TermRouter securely resolves provider credentials, normalizes OpenAI and Anthropic requests, routes model aliases with fallback, preserves streaming responses, and records privacy-conscious structured logs.

[![Go 1.22+](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Interface: Terminal](https://img.shields.io/badge/Interface-Terminal-24292f)](#cli-reference)
[![API: OpenAI compatible](https://img.shields.io/badge/API-OpenAI--compatible-412991)](#api-compatibility)
[![API: Anthropic compatible](https://img.shields.io/badge/API-Anthropic--compatible-D97757)](#api-compatibility)

[Quick start](#quick-start) · [How it works](#how-it-works) · [CLI reference](#cli-reference) · [Security](#security-model) · [PRD](prd/README.md)

</div>

---

## Why TermRouter?

AI tools often require different API keys, base URLs, model names, and request formats. Moving between providers—or recovering from a rate limit or outage—usually means reconfiguring every client.

TermRouter puts one small local gateway between your AI clients and providers:

```text
OpenAI-compatible clients ──┐
Anthropic-compatible clients ├──> TermRouter ──> OpenAI
Custom scripts and agents ──┘       │          ├─> Anthropic
                                    │          ├─> OpenAI-compatible APIs
                                    │          └─> Local servers
                                    │
                                    ├─ Protocol normalization
                                    ├─ Model aliases
                                    ├─ Ordered fallback
                                    ├─ Secure credential resolution
                                    └─ Metadata-only observability
```

Configure providers once, expose stable model aliases such as `coding`, and point supported clients at `http://127.0.0.1:8787`.

## Highlights

- **Terminal only** — no browser dashboard or frontend runtime.
- **Single Go binary** — no runtime dependencies after compilation.
- **Two client protocols** — OpenAI Chat Completions and Anthropic Messages.
- **Streaming support** — Server-Sent Events are relayed incrementally.
- **Provider flexibility** — OpenAI, Anthropic, and custom OpenAI-compatible endpoints.
- **Model aliases** — clients use stable names instead of provider-specific model IDs.
- **Ordered fallback** — retry eligible failures against the next configured target.
- **Secure credentials** — environment, encrypted vault, or OS keyring references.
- **Client authentication** — router keys are stored only as Argon2id hashes.
- **Local-first security** — listens on `127.0.0.1:8787` by default.
- **Private-by-default logs** — metadata-only logging with secret redaction.
- **Operational commands** — health, readiness, provider tests, status, doctor, logs, and usage.

## Project status

TermRouter is an MVP intended for local development and trusted self-hosted environments.

### Implemented

- OpenAI-compatible `POST /v1/chat/completions`
- Anthropic-compatible `POST /v1/messages`
- Non-streaming and SSE streaming responses
- OpenAI, Anthropic, and custom OpenAI-compatible providers
- Direct aliases and ordered fallback routes
- Environment, encrypted-vault, and OS-keyring credential references
- Argon2id-hashed router client keys
- SQLite-backed health and usage metadata
- Structured, metadata-only logs with redaction
- Configuration validation, diagnostics, and sanitized export

### Current limitations

- No native TLS termination
- No web interface
- No native Gemini endpoint
- No embeddings, image, audio, or video routing
- No distributed or multi-node deployment

> [!WARNING]
> TermRouter does not provide native TLS in the current MVP. Do not expose it directly to the public internet. For remote access, keep it bound to localhost and use SSH port forwarding, Tailscale, WireGuard, or a TLS-enabled reverse proxy.

## Requirements

- Go **1.22 or newer** to build from source
- At least one supported AI provider, or a local OpenAI-compatible server

The compiled binary has no separate runtime dependency.

## Installation

### Build from source

```bash
git clone https://github.com/ganjarsantoso/TerminalRouter.git
cd TerminalRouter
go build -o bin/termrouter ./cmd/termrouter
```

Run it directly:

```bash
./bin/termrouter version
```

Optionally install it on Linux or macOS:

```bash
sudo install -m 0755 bin/termrouter /usr/local/bin/termrouter
```

On Windows PowerShell:

```powershell
go build -o bin\termrouter.exe .\cmd\termrouter
.\bin\termrouter.exe version
```

## Quick start

The following setup creates an encrypted local vault, adds an OpenAI provider, exposes the model through the alias `coding`, and starts the gateway.

### 1. Initialize TermRouter

```bash
termrouter init --backend vault --create-key
```

This creates the TermRouter home directory and prints a client key beginning with `tr_live_`.

> [!IMPORTANT]
> Save the client key when it is shown. TermRouter stores only its hash and cannot display the plaintext key again.

### 2. Add a provider

Export the provider credential:

```bash
export OPENAI_API_KEY='sk-...'
```

Add the provider using an environment-variable reference:

```bash
termrouter provider add \
  --name openai-main \
  --type openai \
  --env OPENAI_API_KEY
```

Test the connection:

```bash
termrouter provider test openai-main
```

### 3. Create a public model alias

```bash
termrouter alias add coding \
  --provider openai-main \
  --model gpt-4o-mini
```

Clients will now request `coding`; only TermRouter needs to know the upstream provider and model name.

### 4. Start the gateway

```bash
termrouter serve
```

The default listener is:

```text
http://127.0.0.1:8787
```

### 5. Verify the gateway

In another terminal:

```bash
curl http://127.0.0.1:8787/health
curl http://127.0.0.1:8787/ready
```

List public aliases:

```bash
curl http://127.0.0.1:8787/v1/models \
  -H "Authorization: Bearer tr_live_<your-router-client-key>"
```

Send a completion:

```bash
curl http://127.0.0.1:8787/v1/chat/completions \
  -H "Authorization: Bearer tr_live_<your-router-client-key>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "coding",
    "messages": [
      {
        "role": "user",
        "content": "Reply with exactly: TermRouter works"
      }
    ]
  }'
```

## Connect a local OpenAI-compatible server

TermRouter can route to local or self-hosted OpenAI-compatible endpoints such as Ollama, LM Studio, or vLLM.

If your current CLI accepts an empty key through standard input:

```bash
printf '' | termrouter provider add \
  --name local \
  --type openai-compatible \
  --base-url http://127.0.0.1:11434/v1 \
  --api-key-stdin
```

PowerShell equivalent:

```powershell
"" | termrouter provider add `
  --name local `
  --type openai-compatible `
  --base-url http://127.0.0.1:11434/v1 `
  --api-key-stdin
```

Then create an alias using the model name exposed by the local server:

```bash
termrouter alias add local-chat \
  --provider local \
  --model llama3.2
```

## Configure fallback routing

A route can try multiple provider/model targets in order:

```bash
termrouter route add coding-route \
  --strategy fallback \
  --target openai-main:gpt-4o-mini \
  --target local:llama3.2

termrouter alias add coding --route coding-route
```

### Fallback behavior

TermRouter may move to the next eligible target when an attempt fails before visible content is delivered, including supported transport failures, rate limits, provider overload, and transient server errors.

TermRouter does not normally fall back for invalid requests, unsupported required features, client-authentication errors, or provider safety refusals.

> [!IMPORTANT]
> During streaming, fallback is allowed only before the first client-visible content event. Once streaming content has begun, TermRouter never combines output from different providers.

## Point clients at TermRouter

### OpenAI-compatible clients

```bash
export OPENAI_BASE_URL=http://127.0.0.1:8787/v1
export OPENAI_API_KEY=tr_live_<your-router-client-key>
export MODEL=coding
```

PowerShell:

```powershell
$env:OPENAI_BASE_URL = "http://127.0.0.1:8787/v1"
$env:OPENAI_API_KEY = "tr_live_<your-router-client-key>"
$env:MODEL = "coding"
```

### Anthropic-compatible clients

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:8787
export ANTHROPIC_API_KEY=tr_live_<your-router-client-key>
```

Use a TermRouter alias such as `coding` as the requested model.

PowerShell:

```powershell
$env:ANTHROPIC_BASE_URL = "http://127.0.0.1:8787"
$env:ANTHROPIC_API_KEY = "tr_live_<your-router-client-key>"
```

## How it works

```text
1. A client authenticates with a TermRouter client key.
2. The inbound OpenAI or Anthropic request is normalized.
3. The requested model alias is resolved into a route.
4. TermRouter filters and orders eligible provider targets.
5. The provider credential is resolved only after routing.
6. The request is translated and sent upstream.
7. The response or stream is normalized and returned in the client's protocol.
8. Routing and usage metadata are recorded without prompt bodies by default.
```

Upstream provider credentials and TermRouter client keys are separate:

- **Client key:** authenticates an application to TermRouter.
- **Provider credential:** authenticates TermRouter to an upstream AI provider.

Never configure an upstream provider key as the client key used by your applications.

## API compatibility

| Method | Path | Authentication | Purpose |
|---|---|---:|---|
| `GET` | `/health` | No | Process liveness |
| `GET` | `/ready` | No | Configuration and storage readiness |
| `GET` | `/v1/models` | Yes | List public aliases |
| `POST` | `/v1/chat/completions` | Yes | OpenAI Chat Completions, including SSE |
| `POST` | `/v1/messages` | Yes | Anthropic Messages, including SSE |

Supported client authentication headers:

```http
Authorization: Bearer tr_live_<your-router-client-key>
```

or:

```http
x-api-key: tr_live_<your-router-client-key>
```

## CLI reference

### Core commands

| Command | Purpose |
|---|---|
| `termrouter init` | Create the configuration, database, vault, and optional first client key |
| `termrouter serve` | Start the local API gateway |
| `termrouter stop` | Stop the managed TermRouter process |
| `termrouter status` | Show server, provider, and route status |
| `termrouter doctor` | Run configuration and environment diagnostics |
| `termrouter version` | Print version information |

### Providers and routing

| Command | Purpose |
|---|---|
| `termrouter provider add` | Add a provider connection |
| `termrouter provider list` | List configured providers |
| `termrouter provider show` | Inspect one provider without exposing its secret |
| `termrouter provider test` | Validate provider connectivity and credentials |
| `termrouter provider enable` | Enable a provider |
| `termrouter provider disable` | Disable a provider |
| `termrouter provider remove` | Remove a provider |
| `termrouter alias ...` | Manage public model aliases |
| `termrouter route ...` | Manage direct or fallback route plans |

### Keys, diagnostics, and configuration

| Command | Purpose |
|---|---|
| `termrouter key create` | Create a router client key |
| `termrouter key list` | List client-key metadata |
| `termrouter key rotate` | Replace a client key |
| `termrouter key disable` | Disable a client key |
| `termrouter key remove` | Delete a client key |
| `termrouter logs` | Inspect structured request metadata and errors |
| `termrouter usage today` | Show today's usage summary |
| `termrouter usage summary` | Show aggregated usage |
| `termrouter config show` | Display effective configuration with secrets protected |
| `termrouter config check` | Validate configuration |
| `termrouter config export` | Export sanitized configuration |
| `termrouter config path` | Print the active configuration path |
| `termrouter test <alias>` | Send a live test request through the running server |

Global flags:

```text
--home <path>    Use a custom TermRouter home directory
--json           Return machine-readable output where supported
```

Destructive commands require `--yes` when used non-interactively.

For isolated testing:

```bash
export TERMROUTER_HOME=/tmp/termrouter-demo
termrouter init --backend vault --create-key
```

PowerShell:

```powershell
$env:TERMROUTER_HOME = "$env:TEMP\termrouter-demo"
termrouter init --backend vault --create-key
```

## Configuration and data

The default home directory is `~/.termrouter`:

```text
~/.termrouter/
├── config.yaml    # Human-editable configuration; no plaintext provider secrets
├── router.db      # SQLite: client-key hashes, health, and usage metadata
├── vault.db       # Encrypted credentials when the vault backend is used
├── logs/          # Local structured logs
└── run/           # Runtime files such as the PID file
```

Use the CLI to locate and validate the active configuration:

```bash
termrouter config path
termrouter config check
termrouter config show
```

## Security model

- TermRouter listens on **`127.0.0.1:8787`** by default.
- Non-loopback binding requires the explicit `server.insecure_remote: true` setting.
- The MVP does not terminate TLS itself.
- Router client keys are stored only as **Argon2id hashes**.
- Plaintext client keys are shown once during creation or rotation.
- Upstream secrets use **`env://`**, **`vault://`**, or **`keyring://`** references.
- Vault credentials are encrypted using **ChaCha20-Poly1305**.
- Provider credentials are not stored as plaintext in `config.yaml`.
- Logs default to **metadata-only** mode and apply secret redaction.
- `termrouter config export` produces a sanitized configuration export.
- Provider credentials are resolved only after routing selects an upstream target.

### Safe remote access

Prefer one of these patterns instead of exposing TermRouter directly:

```bash
# SSH local forwarding example
ssh -L 8787:127.0.0.1:8787 user@your-server
```

You can then use `http://127.0.0.1:8787` on the local machine.

Other suitable options include Tailscale, WireGuard, or an authenticated TLS reverse proxy.

## Troubleshooting

### Run diagnostics

```bash
termrouter doctor
termrouter config check
termrouter status
```

### Test only the provider

```bash
termrouter provider test openai-main
```

### Check the server

```bash
curl http://127.0.0.1:8787/health
curl http://127.0.0.1:8787/ready
```

### Inspect logs

```bash
termrouter logs
```

### Common problems

**`401` from TermRouter**

The client key is missing, malformed, disabled, or incorrect. Use a `tr_live_...` key issued by `termrouter key create`; do not use the upstream provider key.

**Provider authentication failure**

Confirm that the referenced environment variable, vault entry, or keyring item is available to the process running `termrouter serve`. Then run:

```bash
termrouter provider test <provider-name>
```

**Unknown model or alias**

List aliases and inspect the relevant route:

```bash
termrouter alias list
termrouter route list
```

**Local provider connection refused**

Confirm that the local model server is running and that its base URL includes the correct OpenAI-compatible path, commonly `/v1`.

**Vault works interactively but not as a service**

Ensure the service process receives the vault unlock method supported by your deployment. Do not place the vault password in `config.yaml` or commit it to source control.

## Development

```bash
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
go build -trimpath -o bin/termrouter ./cmd/termrouter
```

Run with an isolated home directory during development:

```bash
export TERMROUTER_HOME=/tmp/termrouter-dev
./bin/termrouter init --backend vault --create-key
./bin/termrouter serve
```

The complete product requirements and implementation specification are available in [`prd/`](prd/README.md).

## Contributing

Contributions are welcome. Before submitting a change:

1. Keep the terminal-only, local-first scope intact.
2. Add tests for protocol translation, routing, streaming, or secret handling as applicable.
3. Run formatting, vetting, normal tests, and race tests.
4. Do not include provider keys, client keys, prompt data, or generated vault files.
5. Document user-facing commands and configuration changes.

When reporting a security-sensitive issue, avoid posting credentials, prompts, or exploitable details in a public issue.

## License

TermRouter is licensed under the [MIT License](LICENSE).
