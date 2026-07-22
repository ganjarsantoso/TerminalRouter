<div align="center">

# TermRouter

### One local AI endpoint. Multiple providers. Intelligent model selection.

**TermRouter is a lightweight, terminal-first AI gateway written in Go. It gives coding agents, IDEs, scripts, and applications one stable OpenAI-compatible or Anthropic-compatible endpoint while routing requests to cloud providers, OpenAI-compatible services, or local model servers.**

TermRouter centralizes provider credentials, normalizes protocols, exposes stable public model names, supports ordered fallback and safe streaming, and can automatically choose the most suitable configured model through **Smart Routes**.

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Interface: Terminal-first](https://img.shields.io/badge/Interface-Terminal--first-24292f)](#cli-reference)
[![Optional Console](https://img.shields.io/badge/Console-Local--only-24292f)](#termrouter-console)
[![OpenAI Compatible](https://img.shields.io/badge/API-OpenAI--compatible-412991)](#api-compatibility)
[![Anthropic Compatible](https://img.shields.io/badge/API-Anthropic--compatible-D97757)](#api-compatibility)
[![Smart Routes](https://img.shields.io/badge/Routing-Task--aware-7C3AED)](#smart-routes)

[Quick start](#quick-start) · [Smart Routes](#smart-routes) · [How it works](#how-termrouter-works) · [CLI](#cli-reference) · [Security](#security-model) · [Troubleshooting](#troubleshooting)

</div>

\---

## What is TermRouter?

AI applications often require different API keys, base URLs, request formats, model names, and provider-specific configuration. Switching providers—or recovering from a rate limit or outage—usually means updating every client separately.

TermRouter places one small local gateway between your clients and providers:

```text
Clients and coding agents
          │
          │ OpenAI or Anthropic-compatible request
          ▼
┌───────────────────────────────────────────────────────────┐
│                       TermRouter                          │
│                                                           │
│  Client authentication     Protocol normalization         │
│  Alias resolution          Direct / fallback / smart      │
│  Capability filtering      Cost and privacy policy        │
│  Session affinity          Retry and circuit handling     │
│  Usage enforcement         Metadata-only observability    │
└───────────────┬──────────────────────┬────────────────────┘
                │                      │
        Cloud providers        Local model servers
     OpenAI · Anthropic ·       Ollama · LM Studio ·
      compatible services             vLLM
```

---

## Table of contents

- [Why TermRouter](#why-termrouter)
- [Core capabilities](#core-capabilities)
- [How routing works](#how-routing-works)
- [Architecture](#architecture)
- [Requirements](#requirements)
- [Installation](#installation)
- [Five-minute quick start](#five-minute-quick-start)
- [Provider configuration](#provider-configuration)
- [Unified routing workflow](#unified-routing-workflow)
- [Smart Routes](#smart-routes)
- [Model profiles](#model-profiles)
- [Independent benchmark consensus](#independent-benchmark-consensus)
- [Local model assessment](#local-model-assessment)
- [Client keys and policy controls](#client-keys-and-policy-controls)
- [Token optimization](#token-optimization)
- [LUI semantic packets](#lui-semantic-packets)
- [Quota tracking](#quota-tracking)
- [Connect applications](#connect-applications)
- [API compatibility](#api-compatibility)
- [Streaming and fallback guarantees](#streaming-and-fallback-guarantees)
- [TermRouter Console](#termrouter-console)
- [Public VPS deployment](#public-vps-deployment)
- [Security model](#security-model)
- [Configuration](#configuration)
- [Observability and diagnostics](#observability-and-diagnostics)
- [CLI reference](#cli-reference)
- [Development and testing](#development-and-testing)
- [Project layout](#project-layout)
- [Current limits](#current-limits)
- [Troubleshooting](#troubleshooting)
- [Roadmap](#roadmap)
- [Contributing](#contributing)
- [License](#license)

---

## Why TermRouter?

AI clients usually couple four concerns that should be managed independently:

1. the client-facing API protocol;
2. provider credentials and upstream endpoints;
3. model names and routing decisions;
4. operational controls such as quotas, retries, cost, and privacy.

TermRouter separates those concerns. Configure providers once, expose stable aliases such as `coding`, `fast`, or `auto`, and point every compatible client to TermRouter.

### One endpoint for many clients

```text
http://127.0.0.1:8787/v1
```

The caller uses a TermRouter client key. Upstream provider credentials remain inside TermRouter and are resolved only after routing selects a target.

### Stable public model names

Clients can request `coding` even when the actual upstream target changes from one provider or model to another. Client configuration remains stable while infrastructure evolves.

### Three routing modes

```text
Single Model  → one deterministic provider/model target
Fallback      → ordered targets with safe pre-commit failover
Smart         → task-aware model selection plus ordered execution
```

### Security and cost controls at the gateway

TermRouter can enforce alias restrictions, direct-model permissions, requests per minute, per-key concurrency, body-size limits, daily request and token quotas, output-token limits, expiration, and daily estimated-spend budgets before an upstream provider is called.

### Terminal first, browser optional

Every essential operation is available from the CLI. The optional Web Console is embedded in the Go application and serves as a local management interface, not as a public administration dashboard.

---

## Core capabilities

### Gateway and protocol compatibility

- OpenAI-compatible `POST /v1/chat/completions`
- Anthropic-compatible `POST /v1/messages`
- OpenAI-style and Anthropic-style client authentication
- Non-streaming and Server-Sent Events streaming responses
- Request and response normalization through a provider-neutral internal model
- Tool calls and tool results
- Structured-output and request capability signals
- OpenAI, Anthropic, and custom OpenAI-compatible upstream providers
- Local endpoints such as Ollama, LM Studio, and vLLM
- Public model discovery through aliases

### Routing

- Direct aliases
- Ordered fallback routes
- Task-aware Smart Routes
- Hard capability filtering before preference scoring
- Provider health and circuit-state checks
- Configurable balanced, quality, economy, fast, and private policies
- Low-confidence target handling
- Deterministic tie-breaking
- Session affinity for related turns
- Shadow evaluation before live activation
- Historical routing explanations

### Model intelligence

- Layered model profiles
- Independent external benchmark consensus
- Source trust and provenance controls
- Exact and probable model-variant matching
- Mandatory human review for uncertain variant matches
- Local model self-assessment
- Per-field confidence and provenance
- User overrides that remain higher priority than imported baselines

### Security and operations

- Loopback binding by default
- Argon2id-hashed client keys
- Credential references through environment variables, encrypted vault, or OS keyring
- XChaCha20-Poly1305 vault encryption
- Portable-key restrictions for public deployments
- Fail-closed quota enforcement
- Per-key and global concurrency controls
- Direct-model authorization controls
- Request-size and message/tool limits
- Request ID validation
- Invalid-auth throttling
- Metadata-only logs by default
- Secret redaction and sanitized configuration exports
- SQLite-backed usage, health, decision, assessment, and affinity state

---

## How routing works

### Fixed route request

```text
Client request
    │
    ├─ authenticate TermRouter client key
    ├─ enforce body, concurrency, quota, and authorization policy
    ├─ parse OpenAI or Anthropic request
    ├─ normalize into TermRouter's provider-neutral representation
    ├─ resolve alias into direct or fallback execution plan
    ├─ resolve credential for the selected provider
    ├─ execute, retry, or safely fall back
    ├─ normalize the provider response
    └─ record privacy-conscious request metadata
```

### Smart route request

```text
Request model="auto"
        │
        ▼
Normalized request
        │
        ▼
Local task classification
  category · complexity · requirements · confidence
        │
        ▼
Hard filtering
  tools · vision · context · privacy · credentials · health
        │
        ▼
Policy-aware candidate scoring
  task fit · quality · reliability · cost · latency · privacy
        │
        ▼
Session-affinity and confidence handling
        │
        ▼
Immutable ordered execution plan
        │
        ▼
Existing execution, retry, circuit, and streaming engine
```

Smart Routes select who should answer. They do not run multiple candidate models in parallel, judge competing answers, merge outputs, or alter provider responses.

---

## Architecture

TermRouter intentionally separates protocol handling, routing decisions, execution, security, and persistence.

```text
┌────────────────────────────────────────────────────────────┐
│ Client interfaces                                          │
│ OpenAI-compatible · Anthropic-compatible · CLI · Console   │
└──────────────────────────┬─────────────────────────────────┘
                           ▼
┌────────────────────────────────────────────────────────────┐
│ HTTP middleware                                            │
│ Request ID → recovery → trusted proxy → global body limit  │
│ → authentication → per-key body limit → global concurrency │
│ → per-key policy/quota → request tracking                  │
└──────────────────────────┬─────────────────────────────────┘
                           ▼
┌────────────────────────────────────────────────────────────┐
│ Protocol normalization                                     │
│ Messages · content blocks · tools · usage · stream events  │
└──────────────────────────┬─────────────────────────────────┘
                           ▼
┌────────────────────────────────────────────────────────────┐
│ Routing                                                    │
│ Alias resolver · fallback plan · Smart Route engine        │
│ Task classifier · profile resolver · session affinity      │
└──────────────────────────┬─────────────────────────────────┘
                           ▼
┌────────────────────────────────────────────────────────────┐
│ Execution                                                  │
│ Credential resolution · provider registry · retry/fallback │
│ Circuit state · stream commitment · usage accounting       │
└──────────────┬──────────────────────────────┬──────────────┘
               ▼                              ▼
       Provider adapters                 SQLite state
  OpenAI · Anthropic · compatible     keys · usage · health
                                     decisions · profiles
                                     assessments · history
```

### Architectural boundaries

- **Configuration:** providers, aliases, routes, Smart Route options, pricing, model profiles, hosting, and logging.
- **Normalization:** provider-neutral requests, responses, content blocks, tool calls, usage, errors, and stream events.
- **Router:** turns a public alias into an immutable attempt plan.
- **Smart engine:** classifies tasks, filters candidates, scores eligible models, and selects an ordered plan.
- **Execution coordinator:** resolves credentials and executes retry/fallback without duplicating routing logic.
- **Provider adapters:** translate normalized data to and from provider-specific APIs.
- **Storage:** persists authentication data, usage, provider health, Smart Decisions, session affinity, profile evidence, assessments, and configuration history.
- **Console:** presents a local browser interface over administrative APIs while preserving terminal-first operation.

---

## Requirements

- Go **1.26 or newer** to build the current source tree
- At least one supported cloud provider or local OpenAI-compatible server
- A credential backend: encrypted vault, OS keyring, or environment references
- Node.js tooling only when developing the embedded Web Console

The compiled Go binary does not require Go at runtime.

---

## Installation

### Build from source

```bash
git clone https://github.com/ganjarsantoso/TerminalRouter.git
cd TerminalRouter
go build -trimpath -o bin/termrouter ./cmd/termrouter
./bin/termrouter version
```

### Install on Linux or macOS

```bash
sudo install -m 0755 bin/termrouter /usr/local/bin/termrouter
termrouter version
```

### Build on Windows PowerShell

```powershell
git clone https://github.com/ganjarsantoso/TerminalRouter.git
Set-Location TerminalRouter
go build -o bin\termrouter.exe .\cmd\termrouter
.\bin\termrouter.exe version
```

### Useful Make targets

```bash
make build
make test
make race
make fmt
make vet
make clean
```

---

## Five-minute quick start

### 1. Initialize TermRouter

```bash
termrouter init --backend vault --create-key
```

TermRouter creates its configuration directory and prints a client key beginning with `tr_live_`.

> [!IMPORTANT]
> Save the plaintext key immediately. TermRouter stores only its Argon2id hash and cannot display the same plaintext key later.

If the vault backend is used in a headless environment, provide its passphrase through the supported runtime mechanism:

```bash
export TERMROUTER_VAULT_PASSPHRASE='use-a-strong-secret'
```

### 2. Add a provider

```bash
export OPENAI_API_KEY='sk-...'

termrouter provider add \
  --name openai-main \
  --type openai \
  --env OPENAI_API_KEY
```

Validate connectivity:

```bash
termrouter provider test openai-main
```

### 3. Create a Single Model alias

```bash
termrouter alias add coding \
  --provider openai-main \
  --model gpt-4o-mini
```

### 4. Start the gateway

```bash
termrouter serve
```

Default endpoints:

```text
Gateway: http://127.0.0.1:8787
OpenAI:  http://127.0.0.1:8787/v1
```

### 5. Verify the service

```bash
curl http://127.0.0.1:8787/health
curl http://127.0.0.1:8787/ready
```

List public aliases:

```bash
curl http://127.0.0.1:8787/v1/models \
  -H "Authorization: Bearer $TERMROUTER_API_KEY"
```

Send a completion:

```bash
curl http://127.0.0.1:8787/v1/chat/completions \
  -H "Authorization: Bearer $TERMROUTER_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "coding",
    "messages": [
      {"role": "user", "content": "Reply with exactly: TermRouter works"}
    ]
  }'
```

---

## Provider configuration

### OpenAI

```bash
export OPENAI_API_KEY='sk-...'

termrouter provider add \
  --name openai-main \
  --type openai \
  --env OPENAI_API_KEY
```

### Anthropic

```bash
export ANTHROPIC_API_KEY='sk-ant-...'

termrouter provider add \
  --name anthropic-main \
  --type anthropic \
  --env ANTHROPIC_API_KEY
```

### Custom OpenAI-compatible provider

```bash
export PROVIDER_API_KEY='...'

termrouter provider add \
  --name compatible-main \
  --type openai-compatible \
  --base-url https://provider.example.com/v1 \
  --env PROVIDER_API_KEY
```

### Local OpenAI-compatible server

For a local endpoint that does not require authentication:

```bash
printf '' | termrouter provider add \
  --name local \
  --type openai-compatible \
  --base-url http://127.0.0.1:11434/v1 \
  --api-key-stdin
```

Then expose a local model:

```bash
termrouter alias add local-chat \
  --provider local \
  --model llama3.2
```

### Provider operations

```bash
termrouter provider list
termrouter provider show openai-main
termrouter provider test openai-main
termrouter provider disable openai-main
termrouter provider enable openai-main
termrouter provider remove openai-main --yes
```

Provider secrets are referenced, not stored in plaintext inside `config.yaml`.

---

## Unified routing workflow

TermRouter supports three routing modes. All are exposed to clients through aliases.

### Mode 1: Single Model

Use one deterministic target:

```bash
termrouter alias add coding \
  --provider openai-main \
  --model gpt-4o-mini
```

Best for:

- predictable workloads;
- provider-specific evaluation;
- strict operational control;
- debugging without routing variability.

### Mode 2: Fallback

Create an ordered route:

```bash
termrouter route add coding-fallback \
  --strategy fallback \
  --target openai-main:gpt-4o-mini \
  --target local:qwen-coder

termrouter alias add coding --route coding-fallback
```

Fallback is considered only for eligible transient failures before visible streaming content is committed.

### Mode 3: Smart

Create a task-aware candidate pool:

```bash
termrouter route add intelligent \
  --strategy smart \
  --candidate local:qwen-coder \
  --candidate compatible-main:general-model \
  --candidate anthropic-main:analytical-model \
  --candidate openai-main:reasoning-model \
  --policy balanced \
  --default anthropic-main:analytical-model

termrouter alias add auto --route intelligent
```

Smart routes are created in shadow mode by default. Evaluate them before activating live control.

---

## Smart Routes

Smart Routes analyze the request locally and choose among configured candidates.

### Task profile

The deterministic classifier extracts signals such as:

- task category;
- simple, medium, or complex workload;
- coding, reasoning, analysis, writing, extraction, translation, or tool-operation requirements;
- tool-use, vision, structured-output, and context requirements;
- estimated context size;
- classification confidence.

### Hard filtering

A high score cannot override a hard requirement. A candidate can be rejected for reasons such as:

- provider disabled or unhealthy;
- missing credential;
- tool or vision support unavailable;
- insufficient context or output capacity;
- privacy policy mismatch;
- strict profile unavailable;
- direct policy restriction;
- candidate below the configured minimum task match.

### Policy presets

- `balanced` — task fit with quality, reliability, cost, and latency considered.
- `quality` — prioritizes capability and output quality.
- `economy` — favors lower-cost suitable candidates.
- `fast` — emphasizes response latency.
- `private` — favors or restricts selection to local/private targets.

Inspect supported values in the running build:

```bash
termrouter route add --help
termrouter config show
```

### Shadow mode

```bash
termrouter route smart enable intelligent --shadow
```

Shadow mode classifies requests and stores recommendations without changing live traffic. It does not call extra candidate models simply to compare responses.

Review behavior:

```bash
termrouter smart status
termrouter smart report --route intelligent --last 7d
termrouter explain auto --prompt "Review this concurrent Go worker pool"
```

### Live mode

```bash
termrouter route smart enable intelligent --shadow=false
```

> [!CAUTION]
> Validate model profiles, candidate distribution, privacy settings, pricing, and default behavior before enabling live Smart Route selection.

### Session affinity

Session affinity keeps related requests on the chosen provider/model and avoids unnecessary model switching. TermRouter may reconsider affinity when:

- required capabilities change;
- tool, image, or output requirements change;
- context would exceed the selected model's capacity;
- the provider becomes unhealthy;
- route or policy configuration changes;
- the affinity record expires.

Session affinity influences selection only. Retry and fallback still operate on the immutable plan created for the current request.

### Explainability

Explain a hypothetical request:

```bash
termrouter explain auto \
  --prompt "Compare three designs for a concurrent Go job scheduler"
```

Explain a completed request:

```bash
termrouter explain --request req_01JABC123
```

An explanation can include classification, confidence, candidate scores, rejection reasons, selected target, default use, session-affinity influence, and shadow recommendation.

---

## Model profiles

A model profile describes capabilities and operational properties used by Smart Routes.

### Layered resolution

Profiles are resolved per field using this precedence:

```text
User override
    ↓
Local assessment baseline
    ↓
Independent external-consensus baseline
    ↓
Built-in catalog
```

Higher layers override only the fields they define. Resetting a user override reveals the lower baseline rather than destroying it.

### Capability dimensions

Profiles can describe dimensions such as:

- general capability;
- coding;
- reasoning;
- analysis;
- writing;
- tool use;
- instruction following;
- structured output;
- mathematics;
- long context;
- summarization;
- information extraction;
- multilingual behavior.

### Operational properties

- vision;
- tool support;
- parallel tools;
- structured output;
- streaming;
- context window;
- maximum output tokens;
- cost tier;
- latency tier;
- privacy classification.

### Profile commands

```bash
termrouter model profile list
termrouter model profile show local/qwen-coder

termrouter model profile set local/qwen-coder \
  --general 7 \
  --coding 9 \
  --reasoning 8 \
  --analysis 7 \
  --tool-use 8 \
  --cost-tier 1 \
  --latency-tier 1 \
  --privacy local

termrouter model profile validate local/qwen-coder
termrouter model profile reset local/qwen-coder --yes
```

---

## Independent benchmark consensus

TermRouter can build reviewable profile proposals from independent benchmark evidence before relying on local self-assessment or manual configuration.

### Evidence principles

The pipeline is designed around:

- approved and curated source identities;
- deterministic extraction where possible;
- bounded live web retrieval;
- caching and deduplication;
- benchmark-family and per-source contribution caps;
- model identity and variant matching;
- provenance retention;
- confidence bands;
- review before application.

### Variant matching

Evidence is classified by identity compatibility:

- **Exact:** same creator, family, version, and applicable release identity. Full contribution.
- **Strong probable:** likely the same model with a preview/stable, quantization, context-mode, or harness difference. Reduced contribution and mandatory review.
- **Family only:** same family but insufficient version identity. Excluded from scoring.
- **Incompatible:** different creator/family or materially different model variant. Excluded.

A proposal marked `mandatory_review` cannot be applied programmatically until reviewed and cleared through the supported review workflow.

### CLI workflow

Inspect the registry:

```bash
termrouter model profile external registry
```

Search for evidence:

```bash
termrouter model profile external search openai/example-model
```

Create a reviewable proposal:

```bash
termrouter model profile external proposal openai/example-model
```

List proposals:

```bash
termrouter model profile external list-proposals --status pending
```

Apply an eligible proposal:

```bash
termrouter model profile external apply <proposal-id>
```

Inspect import history:

```bash
termrouter model profile external history
```

> [!NOTE]
> External evidence supplies a baseline, not an unquestionable truth. User overrides and accepted local assessments retain higher precedence.

---

## Local model assessment

When independent evidence is unavailable or deployment-specific behavior must be measured, TermRouter can run a local assessment against a configured provider/model.

### Assessment depths

- `quick` — core categories and lower budget;
- `standard` — broader capability coverage;
- `comprehensive` — widest configured benchmark set and highest budget.

TermRouter performs provider/model preflight checks, estimates request and token usage, uses configured pricing when available, persists progress, and generates a reviewable profile proposal.

Run an assessment:

```bash
termrouter model profile assess run local/qwen-coder \
  --depth standard
```

Limit categories when needed:

```bash
termrouter model profile assess run local/qwen-coder \
  --depth standard \
  --categories coding,reasoning,structured_output
```

Inspect, apply, cancel, or view history:

```bash
termrouter model profile assess show <assessment-id>
termrouter model profile assess apply <assessment-id>
termrouter model profile assess cancel <assessment-id>
termrouter model profile assess history local/qwen-coder
```

Assessment results are written to the assessment baseline. Existing user overrides are preserved by layered resolution.

---

## Client keys and policy controls

TermRouter client keys authenticate applications to the router. They are different from provider credentials.

```text
Router client key   → client authenticates to TermRouter
Provider credential → TermRouter authenticates to an upstream provider
```

### Create a local key

```bash
termrouter key create --name local-agent
```

### Create a restricted portable key

```bash
termrouter key create \
  --name portable-agents \
  --portable \
  --alias coding \
  --alias auto \
  --rpm 30 \
  --max-concurrent 4 \
  --daily-requests 500 \
  --daily-input-tokens 3000000 \
  --daily-output-tokens 500000 \
  --daily-cost-usd 10 \
  --max-output-tokens 8192 \
  --max-request-body 10485760
```

### Available policy controls

- allowed aliases;
- direct-model enablement and exact model allowlists;
- requests per minute;
- maximum concurrent requests;
- daily request quota;
- daily input-token quota;
- daily output-token quota;
- daily estimated-spend budget;
- per-request output-token cap;
- per-key request-body cap;
- optional expiration;
- portable/shared-key marker.

In public-hosting mode, portable keys are rejected unless mandatory restrictions (alias, request-body limit, and daily spend budget) are present. The break-glass flag `--unsafe-unrestricted-portable` allows writing a portable key that lacks one or more of those controls; it weakens security and financial protection and should only be used for deliberate recovery. Each use emits a structured `security_event` audit log entry (`portable_key_unsafe_override`).

### Manage keys

```bash
termrouter key list
termrouter key rotate <key-id>
termrouter key set-policy <key-id> --rpm 20 --daily-cost-usd 5
termrouter key disable <key-id>
termrouter key remove <key-id> --yes
```

> [!WARNING]
> A shared portable key creates shared impact: compromise affects all devices, attribution becomes weaker, and rotation must update every client. Prefer separate device or application keys where practical.

---

## Token optimization

TermRouter can optionally reduce token usage before a request reaches an upstream provider. Optimization is **opt-in** (`optimization.enabled: false` by default) and is inspected offline with the `termrouter optimize` command family.

### Modes: safe, balanced, aggressive

| Mode | Intent | Typical transforms |
|------|--------|--------------------|
| `off` | Measure only; do not transform | None |
| `safe` | Lossless / near-lossless compaction | Strip ANSI, compact JSON, deduplicate, stabilize prompt-cache prefixes |
| `balanced` | Selective reduction while preserving operational state | Safe transforms plus conversation-window trimming and tool-result compaction |
| `aggressive` | Maximum local reduction | Balanced transforms plus more aggressive conversation reduction (requires `aggressive_allowed: true`) |

Policy precedence is fail-closed:

```text
server maximum (default_mode + aggressive_allowed)
  > client-key maximum
  > client request preference
  > server default
```

If aggressive mode is requested but not allowed, TermRouter clamps to the highest permitted mode rather than failing open.

### Semantic compression is shadow-only by default

Optional external semantic compressors are **disabled** by default. When evaluation shadow mode is enabled (`optimization.evaluation.shadow_mode`), a compressor may be invoked for quality comparison only: the live request is **not** replaced by the compressed payload. Shadow evaluations record a distinct action (`semantic_compression_shadow_evaluated`) and never set live compression token counters.

### Compressor trust boundary

- External compressors are a separate trust domain from TermRouter itself.
- `optimization.privacy.allow_external_compressors` must be explicitly enabled before remote adapters run.
- Compressor endpoints should stay on loopback or another tightly controlled path; non-loopback destinations require `allow_non_loopback`.
- Remote compressors can see request content that reaches them. Treat them like any other third-party processor: TLS, network isolation, and failure modes (`bypass` vs `reject`) matter.
- Prefer local deterministic optimizers for production; enable remote semantic compression only after deliberate review.

### Estimated versus provider-reported values

Optimization analysis and reports primarily use **local token estimates** (tokenizer or chars-per-token fallback with a safety multiplier). These are not a substitute for provider-reported usage. Spend budgets and post-request accounting may still reconcile against provider-reported tokens when available. Always treat pre-flight savings as estimates.

### CLI

```bash
termrouter optimize status
termrouter optimize analyze --file request.json --model openai/gpt-4o --mode safe
termrouter optimize dry-run --file request.json --mode balanced
termrouter optimize compare --file request.json --modes off,safe,balanced,aggressive
termrouter optimize report --last 7d
termrouter optimize plugins list
termrouter optimize plugins test <name>
```

`dry-run` never calls a provider. `analyze` / `compare` estimate savings only.

---

## LUI semantic packets

**LUI (Lightweight Universal Instruction) v0.1** is an experimental structured packet format used inside TermRouter for optimization and inspection. It is **not** a provider-native protocol.

### Status and assumptions

- LUI is **experimental**. Schema, renderers, and integrity rules may evolve.
- Do **not** assume any upstream provider understands LUI natively. Before a provider call, TermRouter renders or otherwise maps semantics back into ordinary chat/tool messages.
- Integrity hashing treats multi-value collections (goals, constraints, context, state, tools, evidence, output fields, dictionary) as **order-insensitive multisets**: reordering entries does not change the content hash. Scalar fields keep their natural values.

### CLI

```bash
termrouter lui validate packet.json
termrouter lui render packet.json --format compact-json
termrouter lui inspect --request <request-id>
```

Render formats include `compact-json`, `human`, `tagged-text`, and `native-prompt`. `inspect` shows stored optimization metadata for a request ID without replaying raw prompts (subject to privacy settings).

---

## Quota tracking

The `termrouter quota` commands surface multi-account usage windows, freshness, and routing recommendations. Quota data is assembled from several sources and must be interpreted carefully.

### Data sources and limitations

| Source | Meaning |
|--------|---------|
| `local_authoritative` | Derived from TermRouter's own request log / reservations |
| `local_estimated` | Local estimate when authoritative counts are incomplete |
| `provider_header` / `provider_reported` | Values observed from provider response headers or reports |
| `provider_api` | Polled from a provider usage API when configured |
| `manual_configuration` | Operator-supplied limits |
| `reconciled` | Combined after comparing local and provider figures |

Limitations:

- Not every provider exposes reliable remaining-quota headers.
- Provider-reported windows can lag, reset on provider calendars, or omit dimensions TermRouter tracks locally.
- Estimated cost depends on configured pricing; unpriced routes fail closed for portable/public spend enforcement.
- `quota refresh` re-aggregates from the local request log; it does not invent provider-side remaining balances.

### Estimated versus provider-reported

- **Estimated** values are useful for early warning and local budgets.
- **Provider-reported** values are authoritative for the provider's own limits when present, but may not match local token accounting (different tokenizers, cached tokens, tool overhead).
- Recommendations (`termrouter quota recommendations`) bias routing using the best available snapshot; they are advisory unless enforcement mode is hard-limit.

### CLI

```bash
termrouter quota status
termrouter quota windows
termrouter quota report
termrouter quota refresh
termrouter quota events
termrouter quota recommendations
```

---

## Connect applications

### OpenAI-compatible clients

Linux or macOS:

```bash
export OPENAI_BASE_URL=http://127.0.0.1:8787/v1
export OPENAI_API_KEY="$TERMROUTER_API_KEY"
export MODEL=auto
```

PowerShell:

```powershell
$env:OPENAI_BASE_URL = "http://127.0.0.1:8787/v1"
$env:OPENAI_API_KEY = $env:TERMROUTER_API_KEY
$env:MODEL = "auto"
```

### Anthropic-compatible clients

Linux or macOS:

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:8787
export ANTHROPIC_API_KEY="$TERMROUTER_API_KEY"
```

PowerShell:

```powershell
$env:ANTHROPIC_BASE_URL = "http://127.0.0.1:8787"
$env:ANTHROPIC_API_KEY = $env:TERMROUTER_API_KEY
```

Use an alias such as `coding` for deterministic routing or `auto` for Smart Routes.

---

## API compatibility

| Method | Path | Authentication | Purpose |
|---|---|---:|---|
| `GET` | `/health` | No | Minimal liveness response |
| `GET` | `/ready` | No/local policy | Storage and runtime readiness |
| `GET` | `/v1/models` | Yes | List public aliases |
| `POST` | `/v1/chat/completions` | Yes | OpenAI Chat Completions |
| `POST` | `/v1/messages` | Yes | Anthropic Messages |

OpenAI-style authentication:

```http
Authorization: Bearer tr_live_...
```

Anthropic-style authentication:

```http
x-api-key: tr_live_...
```

TermRouter intentionally focuses on a practical compatibility subset. Provider-specific fields may not be portable unless they are explicitly represented by the internal normalization layer.

---

## Streaming and fallback guarantees

Streaming safety is a core invariant.

### Before commitment

TermRouter may try the next eligible target when a retryable failure occurs before client-visible semantic content is sent. Examples can include:

- transport failure;
- rate limit;
- provider overload;
- transient upstream server error;
- timeout before committed content.

### After commitment

Once text or a tool-call start becomes visible to the client:

- TermRouter does not switch providers;
- TermRouter does not splice a second model's output into the stream;
- TermRouter does not merge responses from multiple models;
- a later upstream error is returned as an error for the current stream.

TermRouter does not normally fall back for invalid client requests, authentication failures, unsupported mandatory features, explicit provider restrictions, or non-retryable policy refusal.

---

## TermRouter Console

The optional Console provides browser-based configuration and diagnostics while preserving the terminal-first architecture.

```bash
termrouter console
```

Default address:

```text
http://127.0.0.1:8788
```

The CLI prints a one-time bootstrap login URL. The Console can start the gateway in the same process when it is not already running.

### Console capabilities

- guided initial setup;
- provider and model configuration;
- unified Single, Fallback, and Smart routing workflow;
- model profile inspection and editing;
- independent benchmark proposal review;
- local assessment workflow;
- client-key management and policy limits;
- activity and token-usage views;
- Smart Route shadow reporting;
- request playground and route explanation;
- diagnostics and provider-health views;
- configuration history, diff, and rollback.

### Console security boundary

- loopback only;
- separate port from inference traffic;
- not exposed by the included public reverse-proxy configuration;
- provider secrets are not returned to the browser;
- bootstrap access is one-time and local;
- public configuration is rejected by validation.

Manage the process:

```bash
termrouter console status
termrouter console stop
```

For remote administration, use an SSH tunnel from a trusted device:

```bash
ssh -L 8788:127.0.0.1:8788 user@your-server
```

Then open `http://127.0.0.1:8788` locally.

---

## Public VPS deployment

TermRouter itself has no native TLS termination in the current implementation. A public deployment must keep TermRouter on loopback and place an authenticated TLS reverse proxy at the network edge.

```text
Remote client
    │ HTTPS :443
    ▼
Caddy or equivalent
    │ HTTP over loopback only
    ▼
TermRouter 127.0.0.1:8787

Console 127.0.0.1:8788 → never publicly proxied
```

Included deployment assets:

```text
deploy/Caddyfile
deploy/setup-vps.sh
deploy/termrouter.service
docs/vps-deployment.md
```

### VPS setup

Build or copy the binary, then run:

```bash
sudo TERMROUTER_BIN_SRC=./termrouter bash deploy/setup-vps.sh
```

The helper creates a dedicated service user, stable directories, a sandboxed systemd unit, and recommended UFW rules. It does not automatically enable the firewall or invent domains, credentials, or budgets.

Start the service:

```bash
sudo systemctl enable --now termrouter
sudo systemctl status termrouter
```

Confirm that backend ports remain loopback-only:

```bash
sudo ss -ltnp | grep -E ':8787|:8788'
```

### Reverse proxy boundary

The supplied Caddy design should expose only approved inference paths. Do not proxy `/admin/v1/*`, the Console, arbitrary root paths, or internal diagnostics not intended for external use.

### Firewall boundary

Allow SSH first, then HTTP/HTTPS for certificate issuance and inference:

```bash
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
```

Do not open ports `8787` or `8788` publicly.

> [!IMPORTANT]
> Use TermRouter only on devices and networks where it is authorized. It is not intended to bypass organizational controls or security policy.

---

## Security model

### Local-first network boundary

The safe default is `127.0.0.1:8787`. Non-loopback operation requires explicit configuration, and public-hosting mode validates that the backend remains on loopback behind a TLS reverse proxy.

### Client-key storage

- plaintext shown once;
- non-secret prefix stored for candidate lookup;
- Argon2id hash and salt stored for verification;
- disabled and expired keys are rejected;
- invalid-auth attempts are throttled before expensive verification;
- key rotation replaces the stored verifier.

### Provider credential isolation

Supported references:

```text
env://NAME       environment variable
vault://NAME     encrypted local vault
keyring://NAME   operating-system credential store
none://          explicitly no credential for local endpoints
```

Provider credentials are resolved only after routing chooses an upstream target. They are never supplied by or returned to the client application.

### Request admission

The effective middleware order is designed to reject unsafe or unauthorized requests before provider execution:

```text
Request ID
→ panic recovery
→ trusted-proxy processing
→ global body limit
→ client authentication
→ per-key body limit
→ global concurrency
→ per-key rate/concurrency/quota policy
→ endpoint handler
```

### Financial protection

For spend-enforced keys:

- pricing must be configured for the resolved provider/model;
- unpriced routes fail closed;
- quota-store failure fails closed for portable/public usage;
- usage is attributed to the client key and resolved provider/model;
- reservation logic prevents concurrent requests from silently overspending a shared budget.

### Direct-model protection

Alias authorization and direct provider/model authorization are separate. A key allowed to use alias `coding` cannot automatically bypass that restriction by requesting `provider/model` directly.

### Logging and redaction

- metadata-only logging is the default;
- authorization and API-key patterns are redacted;
- raw prompts are not needed for Smart Decision persistence;
- sanitized configuration export removes credential targets;
- evidence retrieval and profile proposals retain provenance without exposing provider credentials.

### External evidence security

The external benchmark pipeline uses URL validation, restricted-address checks, approved hosts, redirect limits, content-type and size limits, cache controls, and mandatory review for uncertain model identity.

---

## Configuration

Default home directory:

```text
~/.termrouter/
├── config.yaml    # human-editable configuration, no plaintext provider secrets
├── router.db      # keys, usage, health, decisions, assessments, history
├── vault.db       # encrypted credentials when vault backend is selected
├── logs/
└── run/
    ├── termrouter.pid
    └── console.pid
```

Select a different home:

```bash
export TERMROUTER_HOME=/tmp/termrouter-demo
termrouter init --backend vault --create-key
```

Or per command:

```bash
termrouter --home /custom/path status
```

### Main configuration sections

```yaml
server:
  host: 127.0.0.1
  port: 8787
  auth_required: true
  request_timeout: 180s
  max_request_size: 20MiB
  max_concurrency: 64
  max_messages: 200
  max_tools: 64

credentials:
  backend: vault

providers: {}
routes: {}
aliases: {}
model_profiles: {}
pricing: {}

logging:
  level: info
  payloads: metadata-only
  retention_days: 14
```

### Pricing example

Pricing is expressed in USD per one million tokens and can be defined per exact provider/model or provider fallback.

```yaml
pricing:
  openai/gpt-example:
    input_usd_per_million: 1.00
    output_usd_per_million: 4.00
    currency: usd

  local/qwen-coder:
    input_usd_per_million: 0
    output_usd_per_million: 0
    currency: usd
```

Do not copy example rates as current provider pricing. Configure values from your own provider agreement.

### Validate and export

```bash
termrouter config path
termrouter config check
termrouter config show
termrouter config export
```

Configuration writes are atomic and use restrictive permissions.

---

## Observability and diagnostics

### Health and readiness

```bash
curl http://127.0.0.1:8787/health
curl http://127.0.0.1:8787/ready
```

In public-hosting mode, readiness exposure can be disabled to avoid publishing storage or dependency status.

### Runtime status

```bash
termrouter status
termrouter smart status
```

### Doctor

```bash
termrouter doctor
```

Doctor checks configuration, database availability, authentication posture, public-hosting constraints, portable-key controls, and required directories.

### Request logs

```bash
termrouter logs
termrouter logs --errors
termrouter logs --request req_01JABC123
```

Recorded metadata can include:

- request ID;
- client key label;
- inbound protocol;
- requested alias;
- selected provider and upstream model;
- attempt and fallback reason;
- latency and time to first token;
- input and output tokens;
- status and error class;
- stream indicator.

### Usage

```bash
termrouter usage today
termrouter usage summary
```

### Smart analysis

```bash
termrouter smart classify \
  --prompt "Review this concurrent Go function"

termrouter smart report \
  --route intelligent \
  --last 7d

termrouter explain --request req_01JABC123
```

---

## CLI reference

Use command help as the final authority for flags supported by the current build:

```bash
termrouter --help
termrouter <command> --help
```

### Lifecycle

```bash
termrouter init
termrouter serve
termrouter stop
termrouter status
termrouter doctor
termrouter version
```

### Providers

```bash
termrouter provider add
termrouter provider list
termrouter provider show <name>
termrouter provider test <name>
termrouter provider enable <name>
termrouter provider disable <name>
termrouter provider remove <name> --yes
```

### Aliases and routes

```bash
termrouter alias add
termrouter alias list
termrouter alias show <name>
termrouter alias remove <name> --yes

termrouter route add
termrouter route list
termrouter route show <name>
termrouter route remove <name> --yes
termrouter route smart enable <name> --shadow
termrouter route smart enable <name> --shadow=false
termrouter route smart disable <name>
termrouter route smart validate <name>
```

### Smart routing

```bash
termrouter smart classify --prompt "..."
termrouter smart report --route <route> --last 7d
termrouter smart status
termrouter explain <alias> --prompt "..."
termrouter explain --request <request-id>
```

### Model profiles

```bash
termrouter model profile list
termrouter model profile show <provider/model>
termrouter model profile set <provider/model> [flags]
termrouter model profile validate <provider/model>
termrouter model profile reset <provider/model> --yes
```

### External consensus

```bash
termrouter model profile external registry
termrouter model profile external search <provider/model>
termrouter model profile external proposal <provider/model>
termrouter model profile external list-proposals
termrouter model profile external apply <proposal-id>
termrouter model profile external history
```

### Assessment

```bash
termrouter model profile assess run <provider/model>
termrouter model profile assess show <assessment-id>
termrouter model profile assess apply <assessment-id>
termrouter model profile assess cancel <assessment-id>
termrouter model profile assess history <provider/model>
```

### Keys

```bash
termrouter key create
termrouter key list
termrouter key set-policy <key-id>
termrouter key rotate <key-id>
termrouter key disable <key-id>
termrouter key remove <key-id> --yes
```

### Token optimization

```bash
termrouter optimize status
termrouter optimize analyze --file request.json --model <provider/model>
termrouter optimize dry-run --file request.json --mode safe
termrouter optimize compare --file request.json --modes off,safe,balanced,aggressive
termrouter optimize report --last 7d
termrouter optimize plugins list
termrouter optimize plugins test <name>
```

### LUI

```bash
termrouter lui validate <packet.json>
termrouter lui render <packet.json> --format compact-json
termrouter lui inspect --request <request-id>
```

### Quota

```bash
termrouter quota status
termrouter quota windows
termrouter quota report
termrouter quota refresh
termrouter quota events
termrouter quota recommendations
```

### Console, logs, usage, and config

```bash
termrouter console
termrouter console status
termrouter console stop
termrouter logs
termrouter usage today
termrouter usage summary
termrouter config path
termrouter config show
termrouter config check
termrouter config export
```

### Live request test

```bash
export TERMROUTER_TEST_KEY='tr_live_...'
termrouter test coding --prompt "Reply with pong"
termrouter test coding --stream
```

### Global flags

```text
--home <path>  use a custom TermRouter home directory
--json         produce machine-readable output where supported
```

---

## Development and testing

### Standard checks

```bash
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
go build -trimpath -o bin/termrouter ./cmd/termrouter
```

### Important test areas

The repository includes tests for:

- configuration validation and migration;
- direct, fallback, and Smart Route resolution;
- provider adapters and normalization;
- authentication, key expiration, and prefix lookup;
- portable-key restrictions;
- direct-model authorization bypass prevention;
- global and per-key concurrency;
- token, request, and spend quotas;
- fail-closed quota-store behavior;
- request-body limits with known and unknown lengths;
- request ID validation;
- streaming commitment and fallback behavior;
- secret redaction and sanitized exports;
- profile-layer precedence;
- assessment persistence and application;
- external proposal mandatory-review persistence;
- schema migrations;
- SSRF and evidence-pipeline restrictions;
- Console/API policy parity.

### Development home

Keep local experiments isolated:

```bash
export TERMROUTER_HOME=/tmp/termrouter-dev
export TERMROUTER_VAULT_PASSPHRASE='development-only-secret'

./bin/termrouter init --backend vault --create-key
./bin/termrouter serve
```

### Architecture decisions

Durable decisions belong under:

```text
docs/adr/
```

Add ADRs for changes to protocol normalization, credentials, streaming commitment, routing classification, profile precedence, persistence, security defaults, or public deployment behavior.

---

## Project layout

```text
TerminalRouter/
├── cmd/termrouter/           CLI entrypoint
├── internal/
│   ├── api/                  OpenAI, Anthropic, middleware, admin APIs
│   ├── app/                  server composition and runtime lifecycle
│   ├── cli/                  Cobra commands
│   ├── config/               YAML schema, validation, paths, pricing
│   ├── console/              local Web Console backend and embedded assets
│   ├── credentials/          env, keyring, and encrypted vault
│   ├── execution/            retry, fallback, policy and accounting
│   ├── normalization/        provider-neutral messages and stream events
│   ├── observability/        structured logging and redaction
│   ├── provider/             provider contracts and adapters
│   ├── router/               alias and execution-plan resolution
│   ├── smart/                classification, profiles, selection, assessment
│   └── storage/              SQLite schema, migrations, and persistence
├── web/                      React/TypeScript/Tailwind/Vite Console source
├── deploy/                   Caddy, systemd, VPS setup
├── docs/
│   ├── adr/                  architecture decision records
│   └── vps-deployment.md
├── Makefile
├── go.mod
├── go.sum
├── LICENSE
└── README.md
```

---

## Current limits

TermRouter is designed for local development and trusted self-hosted environments. Current limitations include:

- no native TLS termination;
- no native Gemini-compatible endpoint;
- no embeddings, image-generation, audio, or video routing;
- no distributed or multi-node control plane;
- SQLite-oriented single-instance state;
- Smart Routes rely on configured profiles and local classification rather than learned end-to-end routing;
- direct latent/vector communication between models is not part of the current gateway;
- compatibility focuses on supported OpenAI and Anthropic request subsets rather than every provider-specific extension.

---

## Troubleshooting

### Recommended diagnostic sequence

```bash
termrouter config check
termrouter doctor
termrouter status
termrouter smart status
```

### `401` from TermRouter

The client key is missing, malformed, expired, disabled, or incorrect.

```bash
termrouter key list
```

Use a `tr_live_...` TermRouter key, not a provider API key.

### Provider authentication failure

Verify that the environment variable, vault entry, or keyring item is visible to the TermRouter process:

```bash
termrouter provider test <provider>
```

For systemd, confirm the service receives the expected environment or supported vault unlock mechanism.

### Unknown model or alias

```bash
termrouter alias list
termrouter route list
termrouter route show <route>
```

### No eligible Smart Route candidate

```bash
termrouter explain auto --prompt "your representative request"
termrouter model profile list
termrouter smart status
```

Typical causes:

- missing or invalid profile;
- every candidate lacks a mandatory capability;
- insufficient context capacity;
- provider disabled, unhealthy, or missing credentials;
- privacy or cost policy exclusion;
- candidate below the minimum match;
- low-confidence decision without an explicit default.

### Unexpected Smart Route selection

```bash
termrouter explain --request <request-id>
```

Inspect classification, confidence, profile values, policy weights, health, affinity, and rejection reasons.

### Smart Route does not affect traffic

It may still be in shadow mode:

```bash
termrouter smart status
termrouter route smart enable <route> --shadow=false
```

### Models switch between turns

Check whether the client supplies a stable conversation identity and whether affinity remains valid. Capability changes, context growth, provider health, route edits, or TTL expiry can trigger reconsideration.

### Local provider connection refused

Confirm that the local service is running and the base URL includes the correct compatibility prefix, commonly `/v1`.

```bash
termrouter provider test local
```

### Streaming stops after partial output

TermRouter intentionally refuses to switch models after content commitment. Inspect the request explanation and logs:

```bash
termrouter explain --request <request-id>
termrouter logs --request <request-id>
```

### `402 unpriced_route`

A spend-enforced request resolved to a provider/model without configured pricing. Add an exact or provider-level pricing entry and validate the configuration.

### `503 quota_policy_unavailable`

TermRouter could not read authoritative usage state for a key requiring fail-closed quota enforcement. Restore database availability before retrying.

### `503 server_concurrency_limit`

The process-wide request limit has been reached. Retry after active requests complete or adjust `server.max_concurrency` after capacity testing.

### Vault works interactively but not under systemd

Ensure the service receives the supported passphrase/unlock mechanism. Never place vault plaintext secrets inside `config.yaml` or commit them to source control.

### Port already in use

```bash
termrouter status
sudo ss -ltnp | grep -E ':8787|:8788'
```

Stop the existing process or select another configured port.

---

## Roadmap

Potential future work includes:

- native TLS or additional hardened deployment tooling;
- native Gemini compatibility;
- embeddings and additional multimodal endpoints;
- broader provider-specific compatibility;
- expanded independent benchmark registry;
- stronger tokenizer-aware cost estimation;
- feedback-based profile calibration;
- learned or pluggable task classifiers;
- semantic context caching and delta communication;
- shared semantic memory for agent workflows;
- signed release artifacts and expanded packaging;
- distributed or multi-node state where justified.

Roadmap items are directional and are not guarantees of implementation.

---

## Design principles

1. **Terminal first** — essential functionality never requires a browser.
2. **Local first** — loopback is the safe default.
3. **Protocol aware** — requests are normalized rather than blindly forwarded.
4. **Provider independent** — clients depend on aliases, not upstream configuration.
5. **Explainable routing** — automatic decisions remain inspectable.
6. **Hard constraints before preferences** — security and capabilities cannot be outscored.
7. **Safe streaming** — fallback never splices multiple models into one response.
8. **Private by default** — secrets and prompt bodies are not ordinary log content.
9. **Fail closed where financial or security policy requires it.**
10. **One execution engine** — Smart Routes select plans; existing execution logic runs them.
11. **Evidence before assumption** — model profiles prefer traceable evidence and review.
12. **Backward compatibility** — deterministic aliases remain available without Smart Routes.

---

## Contributing

Contributions are welcome.

Before opening a pull request:

1. Preserve terminal-first and local-first behavior.
2. Keep the Console loopback-only and out of the public inference surface.
3. Add tests for user-visible and security-sensitive behavior.
4. Add deterministic fixtures for protocol, classifier, or evidence changes.
5. Test cancellation, streaming, timeout, and fallback boundaries.
6. Run formatting, vetting, normal tests, and race tests.
7. Add a migration for persistent schema changes.
8. Update CLI help and documentation when commands or defaults change.
9. Never commit credentials, client keys, prompts, vaults, databases, or private logs.
10. Preserve Smart Route explainability and profile provenance.
11. Document durable architectural changes with an ADR.
12. Include a concise remediation or validation report for security-sensitive changes.

Report security issues privately and do not include real credentials, private prompts, or exploitable deployment details in a public issue.

---

## License

TermRouter is licensed under the [MIT License](LICENSE).

---

<div align="center">

**TermRouter decides who should answer. The selected model produces the answer.**

One endpoint · Stable aliases · Intelligent routing · Local control

</div>
