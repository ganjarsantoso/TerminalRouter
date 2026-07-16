# TermRouter

Terminal-only, multi-provider AI API gateway and protocol router.

One lightweight binary. One local API endpoint. Multiple AI providers — credentials stored securely, requests normalized, routes with fallback, streaming, and structured logs.

## Install

```bash
go build -o bin/termrouter ./cmd/termrouter
# optional
sudo cp bin/termrouter /usr/local/bin/
```

Requires Go 1.22+ to build. The binary has no runtime dependencies.

## Quick start (≤ 5 minutes)

```bash
# 1. Initialize home dir (~/.termrouter) + first client key
termrouter init --backend vault --create-key
# Save the printed tr_live_… key

# 2. Add a provider (OpenAI, Anthropic, or any OpenAI-compatible endpoint)
export OPENAI_API_KEY=sk-...
termrouter provider add --name openai-main --type openai --env OPENAI_API_KEY

# Or a local OpenAI-compatible server (Ollama, LM Studio, vLLM, …):
termrouter provider add --name local --type openai-compatible \
  --base-url http://127.0.0.1:11434/v1 --api-key-stdin <<< ""

# 3. Create an alias (and optional fallback route)
termrouter alias add coding --provider openai-main --model gpt-4o-mini

# Fallback example:
# termrouter route add coding-route --strategy fallback \
#   --target openai-main:gpt-4o-mini --target local:llama3.2
# termrouter alias add coding --route coding-route

# 4. Start the gateway (default 127.0.0.1:8787)
termrouter serve
```

### Point clients at TermRouter

**OpenAI-compatible**

```bash
export OPENAI_BASE_URL=http://127.0.0.1:8787/v1
export OPENAI_API_KEY=tr_live_<your-router-client-key>
export MODEL=coding
```

**Anthropic-compatible**

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:8787
export ANTHROPIC_API_KEY=tr_live_<your-router-client-key>
# model = your alias, e.g. coding
```

**curl**

```bash
curl http://127.0.0.1:8787/v1/chat/completions \
  -H "Authorization: Bearer tr_live_…" \
  -H "Content-Type: application/json" \
  -d '{"model":"coding","messages":[{"role":"user","content":"hi"}]}'
```

## CLI overview

| Command | Purpose |
|---------|---------|
| `termrouter init` | Create config, DB, vault, optional client key |
| `termrouter serve` | Start localhost gateway |
| `termrouter stop` / `status` / `doctor` | Lifecycle & diagnostics |
| `termrouter provider add\|list\|show\|test\|enable\|disable\|remove` | Providers |
| `termrouter alias` / `route` | Public model names & fallback plans |
| `termrouter key create\|list\|rotate\|disable\|remove` | Router client keys |
| `termrouter logs` / `usage today\|summary` | Metadata observability |
| `termrouter config show\|check\|export\|path` | Configuration |
| `termrouter test <alias>` | Live request (needs server + `TERMROUTER_TEST_KEY`) |
| `termrouter version` | Version |

Global flags: `--home`, `--json`. Destructive commands require `--yes`.

Use a custom home for testing:

```bash
export TERMROUTER_HOME=/tmp/tr-demo
termrouter init
```

## API surface

| Method | Path | Notes |
|--------|------|--------|
| GET | `/health` | Liveness (no auth) |
| GET | `/ready` | Readiness (no auth) |
| GET | `/v1/models` | Public aliases |
| POST | `/v1/chat/completions` | OpenAI Chat Completions + SSE |
| POST | `/v1/messages` | Anthropic Messages + SSE |

Auth: `Authorization: Bearer tr_live_…` or `x-api-key: tr_live_…`  
Upstream provider keys are **never** the client key; they are resolved from env / vault / keyring only after routing.

## Security model

- Listens on **127.0.0.1:8787** by default; remote bind requires `server.insecure_remote: true` (MVP has no TLS yet).
- Client keys stored as **Argon2id hashes** only; plaintext shown once at create/rotate.
- Upstream secrets: **env://**, **vault://** (ChaCha20-Poly1305), or **keyring://** — not plaintext in `config.yaml`.
- Logs default to **metadata-only** with secret redaction.
- `termrouter config export` redacts credential references.

## Configuration layout

```
~/.termrouter/
├── config.yaml    # human-editable, no plaintext secrets
├── router.db      # SQLite: key hashes, health, usage
├── vault.db       # encrypted credentials (vault backend)
├── logs/
└── run/           # pid file
```

## Development

```bash
go test ./...
go test -race ./...
go build -o bin/termrouter ./cmd/termrouter
```

PRD: see [`prd/`](prd/README.md).

## License

MIT
