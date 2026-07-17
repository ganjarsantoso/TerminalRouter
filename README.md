<div align="center">

# TermRouter

### One local AI endpoint. Multiple providers. Intelligent model selection.

**TermRouter is a lightweight, terminal-only AI gateway that securely connects OpenAI, Anthropic, OpenAI-compatible APIs, and local model servers through a single interface.**

TermRouter normalizes requests, protects provider credentials, exposes stable model aliases, supports streaming and fallback, and can automatically select the most suitable configured model for each task with **Smart Routes**.

[!\[Go 1.22+](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[!\[License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[!\[Interface: Terminal](https://img.shields.io/badge/Interface-Terminal-24292f)](#cli-reference)
[!\[OpenAI compatible](https://img.shields.io/badge/API-OpenAI--compatible-412991)](#api-compatibility)
[!\[Anthropic compatible](https://img.shields.io/badge/API-Anthropic--compatible-D97757)](#api-compatibility)
[!\[Smart Routes](https://img.shields.io/badge/Routing-Task--aware-7C3AED)](#smart-routes)

[Quick start](#quick-start) · [Smart Routes](#smart-routes) · [How it works](#how-termrouter-works) · [CLI](#cli-reference) · [Security](#security-model) · [Troubleshooting](#troubleshooting)

</div>

\---

## What is TermRouter?

AI applications often require different API keys, base URLs, request formats, model names, and provider-specific configuration. Switching providers—or recovering from a rate limit or outage—usually means updating every client separately.

TermRouter places one small local gateway between your clients and providers:

```text
OpenAI-compatible clients ─────┐
Anthropic-compatible clients ──┤
Coding agents and IDEs ────────┼──> TermRouter ──> OpenAI
Custom scripts and services ───┤       │          ├─> Anthropic
Local development tools ───────┘       │          ├─> OpenAI-compatible APIs
                                       │          └─> Local model servers
                                       │
                                       ├─ Client authentication
                                       ├─ Protocol normalization
                                       ├─ Secure credential resolution
                                       ├─ Stable model aliases
                                       ├─ Direct and fallback routes
                                       ├─ Task-aware Smart Routes
                                       ├─ SSE streaming
                                       └─ Privacy-conscious observability
```

Configure providers once, expose stable aliases such as `coding` or `auto`, and point compatible clients at:

```text
http://127.0.0.1:8787
```

## Why TermRouter?

### One configuration for every client

Provider credentials, endpoints, aliases, and routing rules live in TermRouter instead of being duplicated across every agent and application.

### One stable model name

Clients can request an alias such as `coding`. The upstream provider and model can change without requiring client reconfiguration.

### Automatic task-aware model selection

Clients can request `auto`. Smart Routes analyzes the task, checks capability and policy constraints, scores eligible configured models, preserves session affinity, and produces an explainable execution plan.

### Safer credentials

Provider credentials are resolved from environment references, an encrypted vault, or the operating-system keyring only after routing chooses an upstream target. Router client keys are stored as Argon2id hashes.

### Lightweight operation

TermRouter is a terminal-first Go application with no browser dashboard and no separate frontend runtime.

## Feature overview

### Gateway and compatibility

* OpenAI-compatible Chat Completions endpoint
* Anthropic-compatible Messages endpoint
* Non-streaming responses
* Server-Sent Events streaming
* OpenAI, Anthropic, and custom OpenAI-compatible upstream providers
* Support for local OpenAI-compatible servers such as Ollama, LM Studio, and vLLM
* Model listing through public aliases

### Routing

* Direct model aliases
* Ordered provider/model fallback
* Provider health and eligibility checks
* Existing retry and fallback execution plans
* Smart Routes for automatic task-aware model selection
* Candidate capability filtering
* Configurable routing policies
* Confidence-aware default selection
* Session affinity
* Shadow evaluation mode
* Historical decision explanation

### Security

* Localhost-only listener by default
* Separate router client keys and upstream provider credentials
* Argon2id-hashed router client keys
* Environment-variable credential references
* ChaCha20-Poly1305 encrypted local vault
* OS-keyring credential references
* Metadata-only logs by default
* Secret redaction
* Sanitized configuration export

### Operations

* Provider validation and connectivity tests
* Health and readiness endpoints
* Status and doctor commands
* Structured logs
* Usage summaries
* SQLite-backed health, usage, smart-decision, and session-affinity state
* Machine-readable JSON output where supported

## Project status

TermRouter is an MVP intended for local development and trusted self-hosted environments.

### Implemented

* OpenAI-compatible `POST /v1/chat/completions`
* Anthropic-compatible `POST /v1/messages`
* Non-streaming and SSE streaming responses
* OpenAI, Anthropic, and custom OpenAI-compatible providers
* Direct aliases and ordered fallback routes
* Smart-route configuration and model profiles
* Heuristic task classification
* Smart candidate selection before execution
* Diagnostic routing headers
* Shadow and live Smart Route modes
* Smart decision persistence
* Session-affinity persistence
* Request and prompt-level route explanation
* Environment, encrypted-vault, and OS-keyring credential references
* Argon2id-hashed router client keys
* SQLite-backed health and usage metadata
* Structured metadata logs with redaction
* Configuration validation, diagnostics, and sanitized export

### Current limitations

* No native TLS termination
* No web dashboard
* No native Gemini endpoint
* No embeddings, image-generation, audio, or video routing
* No distributed or multi-node deployment
* Smart Routes use configured model profiles and local classification; automatic learned routing is not part of the current MVP

> \\\[!WARNING]
> TermRouter does not provide native TLS in the current MVP. Do not expose the TermRouter listener directly to the public internet. For remote use, keep TermRouter bound to localhost and use SSH port forwarding, Tailscale, WireGuard, or an authenticated TLS reverse proxy.

\---

## Requirements

* Go **1.22 or newer** to build from source
* At least one supported cloud provider or local OpenAI-compatible server
* A supported credential backend for provider secrets

The compiled TermRouter binary does not require Go at runtime.

## Installation

### Clone and build

```bash
git clone https://github.com/ganjarsantoso/TerminalRouter.git
cd TerminalRouter
go build -o bin/termrouter ./cmd/termrouter
```

Verify the binary:

```bash
./bin/termrouter version
```

### Linux or macOS installation

```bash
sudo install -m 0755 bin/termrouter /usr/local/bin/termrouter
termrouter version
```

### Windows PowerShell

```powershell
git clone https://github.com/ganjarsantoso/TerminalRouter.git
Set-Location TerminalRouter
go build -o bin\\\\termrouter.exe .\\\\cmd\\\\termrouter
.\\\\bin\\\\termrouter.exe version
```

You can invoke `bin\\\\termrouter.exe` directly or place the binary in a directory included in `PATH`.

\---

## Quick start

This setup creates an encrypted local vault, generates a router client key, adds an OpenAI provider, creates a fixed alias, and starts the gateway.

### 1\. Initialize TermRouter

```bash
termrouter init --backend vault --create-key
```

TermRouter creates its home directory and prints a router client key beginning with `tr\\\_live\\\_`.

> \\\[!IMPORTANT]
> Save the printed client key immediately. TermRouter stores only the Argon2id hash and cannot display the plaintext key again.

### 2\. Add a provider

Set the upstream provider credential:

```bash
export OPENAI\\\_API\\\_KEY='sk-...'
```

Add an OpenAI connection that references the environment variable:

```bash
termrouter provider add \\\\
  --name openai-main \\\\
  --type openai \\\\
  --env OPENAI\\\_API\\\_KEY
```

Test the provider before creating routes:

```bash
termrouter provider test openai-main
```

### 3\. Create a fixed alias

```bash
termrouter alias add coding \\\\
  --provider openai-main \\\\
  --model gpt-4o-mini
```

Applications request `coding`; TermRouter resolves the real provider and model.

### 4\. Start the gateway

```bash
termrouter serve
```

The default listener is:

```text
http://127.0.0.1:8787
```

### 5\. Verify the gateway

Open another terminal:

```bash
curl http://127.0.0.1:8787/health
curl http://127.0.0.1:8787/ready
```

List public aliases:

```bash
curl http://127.0.0.1:8787/v1/models \\\\
  -H "Authorization: Bearer tr\\\_live\\\_<your-router-client-key>"
```

Send a test completion:

```bash
curl http://127.0.0.1:8787/v1/chat/completions \\\\
  -H "Authorization: Bearer tr\\\_live\\\_<your-router-client-key>" \\\\
  -H "Content-Type: application/json" \\\\
  -d '{
    "model": "coding",
    "messages": \\\[
      {
        "role": "user",
        "content": "Reply with exactly: TermRouter works"
      }
    ]
  }'
```

\---

## Connect providers

### OpenAI

```bash
export OPENAI\\\_API\\\_KEY='sk-...'

termrouter provider add \\\\
  --name openai-main \\\\
  --type openai \\\\
  --env OPENAI\\\_API\\\_KEY
```

### Anthropic

```bash
export ANTHROPIC\\\_API\\\_KEY='sk-ant-...'

termrouter provider add \\\\
  --name anthropic-main \\\\
  --type anthropic \\\\
  --env ANTHROPIC\\\_API\\\_KEY
```

### Custom OpenAI-compatible provider

```bash
export PROVIDER\\\_API\\\_KEY='...'

termrouter provider add \\\\
  --name compatible-main \\\\
  --type openai-compatible \\\\
  --base-url https://provider.example.com/v1 \\\\
  --env PROVIDER\\\_API\\\_KEY
```

### Local OpenAI-compatible server

TermRouter can route to local endpoints such as Ollama, LM Studio, or vLLM.

Using standard input for an endpoint that does not require a key:

```bash
printf '' | termrouter provider add \\\\
  --name local \\\\
  --type openai-compatible \\\\
  --base-url http://127.0.0.1:11434/v1 \\\\
  --api-key-stdin
```

PowerShell:

```powershell
"" | termrouter provider add `
  --name local `
  --type openai-compatible `
  --base-url http://127.0.0.1:11434/v1 `
  --api-key-stdin
```

Create a fixed local alias:

```bash
termrouter alias add local-chat \\\\
  --provider local \\\\
  --model llama3.2
```

### Manage provider state

```bash
termrouter provider list
termrouter provider show openai-main
termrouter provider test openai-main
termrouter provider disable openai-main
termrouter provider enable openai-main
termrouter provider remove openai-main --yes
```

\---

## Fixed aliases and fallback routes

### Direct alias

A direct alias always resolves to one configured provider and model:

```bash
termrouter alias add coding \\\\
  --provider openai-main \\\\
  --model gpt-4o-mini
```

### Ordered fallback route

A fallback route tries eligible targets in order:

```bash
termrouter route add coding-route \\\\
  --strategy fallback \\\\
  --target openai-main:gpt-4o-mini \\\\
  --target local:llama3.2

termrouter alias add coding --route coding-route
```

### Fallback behavior

TermRouter may try the next target when a supported transient failure occurs before visible response content reaches the client. Typical eligible failures include transport errors, rate limits, provider overload, and transient server errors.

TermRouter does not normally fall back for:

* Invalid client requests
* Router client-authentication failures
* Mandatory unsupported features
* Provider safety refusals
* Explicit provider or policy restrictions

> \\\[!IMPORTANT]
> During streaming, fallback is permitted only before the first client-visible semantic content event. After streaming content begins, TermRouter never combines output from different models or providers.

\---

# Smart Routes

Smart Routes allow one alias—such as `auto`—to select the most suitable configured model for each request.

Instead of statically mapping `auto` to one model, TermRouter:

1. Normalizes the incoming OpenAI or Anthropic request.
2. Extracts task and capability requirements.
3. Classifies the task using the configured local classifier.
4. Filters candidates that violate hard requirements or policy constraints.
5. Scores eligible candidates against their model profiles.
6. Applies confidence and default-target behavior.
7. Reuses a session-affinity selection when appropriate.
8. Produces an ordered execution plan.
9. Passes that plan to the existing retry and fallback engine.
10. Stores a privacy-conscious decision record for later explanation.

```text
Client requests model="auto"
          │
          ▼
Normalized request
          │
          ▼
Task classification
          │
          ├─ simple / medium / complex
          ├─ coding / reasoning / analysis / general
          └─ tools / context / structured-output requirements
          │
          ▼
Hard capability and policy filtering
          │
          ▼
Candidate scoring
          │
          ├─ capability match
          ├─ policy preference
          ├─ reliability and health
          ├─ cost tier
          └─ latency tier
          │
          ▼
Selected model and ordered fallback plan
          │
          ▼
Existing TermRouter execution engine
```

Smart Routes do not generate the answer, run several models in parallel, judge model responses, or merge outputs. Smart Routes only decide which configured model should answer.

## Smart Route concepts

### Model profile

A model profile describes a specific candidate's capabilities and properties, including reasoning, analysis, coding, tool use, structured output, context, cost, latency, and privacy characteristics.

### Task profile

A task profile describes the inferred request category, complexity, required capabilities, hard constraints, and classification confidence.

### Candidate

A candidate is a configured provider/model pair that the Smart Route may select.

### Policy

A policy controls the trade-off between task fit, quality, reliability, cost, latency, and privacy.

### Shadow mode

Shadow mode generates and stores a Smart Route recommendation but leaves real traffic on the existing route. Use shadow mode to evaluate classification and candidate distribution safely.

### Session affinity

Session affinity keeps related turns on the selected model unless requirements change, context limits are reached, the target becomes unavailable, or the affinity record expires.

## Configure model profiles

List profiles:

```bash
termrouter model profile list
```

Inspect a profile:

```bash
termrouter model profile show local/qwen-coder
```

Set or override a profile:

```bash
termrouter model profile set local/qwen-coder \\\\
  --general 3 \\\\
  --coding 5 \\\\
  --reasoning 4 \\\\
  --analysis 3 \\\\
  --tool-use 4 \\\\
  --cost-tier 1 \\\\
  --latency-tier 1
```

Validate a profile:

```bash
termrouter model profile validate local/qwen-coder
```

Reset user overrides:

```bash
termrouter model profile reset local/qwen-coder --yes
```

The exact profile flags available for a build can be inspected with:

```bash
termrouter model profile set --help
```

## Create a Smart Route

```bash
termrouter route add intelligent \\\\
  --strategy smart \\\\
  --candidate local:qwen-coder \\\\
  --candidate compatible-main:general-model \\\\
  --candidate anthropic-main:analytical-model \\\\
  --candidate openai-main:reasoning-model \\\\
  --policy balanced \\\\
  --default anthropic-main:analytical-model
```

Expose the route through the alias `auto`:

```bash
termrouter alias add auto --route intelligent
```

Clients can now request:

```json
{
  "model": "auto",
  "messages": \\\[
    {
      "role": "user",
      "content": "Review this concurrent Go worker pool and identify the deadlock."
    }
  ]
}
```

## Smart policies

A Smart Route policy determines how the router ranks eligible candidates.

### Balanced

Prioritizes task suitability while considering quality, reliability, cost, and latency.

```bash
--policy balanced
```

### Quality-oriented

Use the quality-oriented policy defined by the current configuration when task quality should dominate cost and latency.

### Economy-oriented

Use the economy-oriented policy when reducing cost is important, while retaining the configured minimum suitability requirements.

### Latency-oriented

Use the latency-oriented policy when fast response is more important than maximum model capability.

### Privacy-oriented

Use a privacy-oriented policy to restrict selection to configured local or private targets.

Run the CLI help or inspect the active configuration to see the exact policy names supported by the current build:

```bash
termrouter route add --help
termrouter config show
```

## Test classification without routing a request

```bash
termrouter smart classify \\\\
  --prompt "Review this concurrent Go function and identify race conditions"
```

This helps inspect the inferred category, complexity, requirements, and confidence before enabling live selection.

## Explain a prompt-level decision

```bash
termrouter explain auto \\\\
  --prompt "Review this concurrent Go function and identify race conditions"
```

The explanation can include:

* Task category and complexity
* Classification confidence
* Required capabilities
* Eligible candidates
* Rejected candidates and reasons
* Candidate scores
* Selected target
* Policy influence

## Enable evaluation in shadow mode

```bash
termrouter route smart enable intelligent --shadow
```

In shadow mode:

* Smart classification runs locally.
* TermRouter records the recommendation.
* Existing real routing remains unchanged.
* No additional candidate model is called merely for comparison.
* Raw prompts are not required for the decision record.

Inspect shadow results:

```bash
termrouter smart report \\\\
  --route intelligent \\\\
  --last 7d
```

## Enable live Smart Route selection

The currently implemented live activation form is:

```bash
termrouter route smart enable intelligent --shadow=false
```

> \\\[!CAUTION]
> Live mode allows Smart Routes to control model selection. Evaluate the route in shadow mode first, verify model profiles, review candidate distribution, and confirm cost and privacy policies before switching to live mode.

## Inspect Smart Route status

```bash
termrouter smart status
```

Use this to inspect whether Smart Routes are disabled, running in shadow mode, or controlling live selection.

## Explain a completed request

```bash
termrouter explain --request req\\\_01JABC123
```

A historical explanation can show:

* Requested alias
* Smart route and policy
* Task classification
* Selected target
* Candidate ranking or rejection reasons
* Session-affinity influence
* Retry and fallback attempts
* Whether fallback happened before stream commitment

## Smart decision diagnostics

OpenAI- and Anthropic-compatible responses may include TermRouter diagnostic headers describing the request ID, route, selected model, decision class, or confidence according to configured diagnostic behavior.

Use the request ID with:

```bash
termrouter explain --request <request-id>
```

## Session affinity

TermRouter persists Smart Route session-affinity state in SQLite. Affinity prevents unnecessary model changes during a multi-turn conversation.

A pinned selection may be reconsidered when:

* Required capabilities change
* Tool, image, or output requirements change
* Context would exceed the selected model's limit
* The selected target becomes unhealthy
* The route or policy changes
* The affinity record expires
* The client explicitly causes a new routing context

Session affinity affects model selection only. Provider retry and fallback continue to use the execution plan produced for the request.

## Recommended Smart Route rollout

### Step 1: Validate profiles

```bash
termrouter model profile list
termrouter model profile validate local/qwen-coder
```

### Step 2: Test representative prompts

```bash
termrouter smart classify --prompt "Summarize this paragraph"
termrouter smart classify --prompt "Debug this concurrent Go service"
termrouter smart classify --prompt "Compare three architecture options"
```

### Step 3: Inspect route explanations

```bash
termrouter explain auto --prompt "Debug this concurrent Go service"
```

### Step 4: Enable shadow mode

```bash
termrouter route smart enable intelligent --shadow
```

### Step 5: Review aggregate behavior

```bash
termrouter smart report --route intelligent --last 7d
```

### Step 6: Enable live mode

```bash
termrouter route smart enable intelligent --shadow=false
```

### Step 7: Monitor decisions

```bash
termrouter smart status
termrouter logs
termrouter explain --request <request-id>
```

\---

## Point clients at TermRouter

### OpenAI-compatible clients

Linux or macOS:

```bash
export OPENAI\\\_BASE\\\_URL=http://127.0.0.1:8787/v1
export OPENAI\\\_API\\\_KEY=tr\\\_live\\\_<your-router-client-key>
export MODEL=auto
```

PowerShell:

```powershell
$env:OPENAI\\\_BASE\\\_URL = "http://127.0.0.1:8787/v1"
$env:OPENAI\\\_API\\\_KEY = "tr\\\_live\\\_<your-router-client-key>"
$env:MODEL = "auto"
```

Use `coding` for a deterministic alias or `auto` for a Smart Route alias.

### Anthropic-compatible clients

Linux or macOS:

```bash
export ANTHROPIC\\\_BASE\\\_URL=http://127.0.0.1:8787
export ANTHROPIC\\\_API\\\_KEY=tr\\\_live\\\_<your-router-client-key>
```

PowerShell:

```powershell
$env:ANTHROPIC\\\_BASE\\\_URL = "http://127.0.0.1:8787"
$env:ANTHROPIC\\\_API\\\_KEY = "tr\\\_live\\\_<your-router-client-key>"
```

Use a configured TermRouter alias as the requested model.

### Router key versus provider key

These credentials are intentionally different:

* **Router client key:** authenticates an application to TermRouter and begins with `tr\\\_live\\\_`.
* **Provider credential:** authenticates TermRouter to OpenAI, Anthropic, or another upstream provider.

Never put a provider credential in the client application when the application is connecting through TermRouter.

\---

## How TermRouter works

### Fixed route request

```text
1. Client authenticates with a TermRouter client key.
2. TermRouter parses and normalizes the inbound protocol.
3. The requested alias resolves to a direct or fallback route.
4. TermRouter checks provider, credential, capability, and health eligibility.
5. The execution engine sends the request to the selected target.
6. Retry or fallback runs when an eligible pre-commit failure occurs.
7. The response is normalized into the client's expected protocol.
8. Metadata is recorded without prompt bodies by default.
```

### Smart route request

```text
1. Client requests a Smart Route alias such as auto.
2. TermRouter normalizes the request.
3. The Smart classifier creates a task profile.
4. Hard capability and policy constraints remove ineligible candidates.
5. Eligible candidates are scored using model profiles and route policy.
6. Confidence and session affinity influence the final selection.
7. Smart Routes produce an immutable ordered execution plan.
8. The existing execution engine performs retry and fallback.
9. The response is returned using the inbound protocol.
10. The Smart Decision is stored for status, reporting, and explanation.
```

### Architecture boundaries

|Area|Responsibility|
|-|-|
|Configuration|Providers, routes, candidates, `smart.\\\*`, aliases, and model profiles|
|Router|Resolve direct, fallback, and Smart Route aliases|
|Smart selection|Classify tasks, filter candidates, score models, and produce a plan|
|Gateway|Apply selection before upstream execution|
|Protocol adapters|Normalize OpenAI and Anthropic requests and responses|
|Execution|Apply existing retry, cooldown, circuit, fallback, and stream behavior|
|Credentials|Resolve upstream secrets only after target selection|
|SQLite|Persist keys, health, usage, Smart Decisions, and session affinity|
|Observability|Logs, usage, diagnostic headers, status, reports, and explanations|

\---

## API compatibility

|Method|Path|Authentication|Purpose|
|-|-|-:|-|
|`GET`|`/health`|No|Process liveness|
|`GET`|`/ready`|No|Configuration and storage readiness|
|`GET`|`/v1/models`|Yes|List public model aliases|
|`POST`|`/v1/chat/completions`|Yes|OpenAI Chat Completions with optional SSE|
|`POST`|`/v1/messages`|Yes|Anthropic Messages with optional SSE|

### Client authentication

OpenAI-style authentication:

```http
Authorization: Bearer tr\\\_live\\\_<your-router-client-key>
```

Anthropic-style authentication:

```http
x-api-key: tr\\\_live\\\_<your-router-client-key>
```

Provider credentials are not forwarded from clients. TermRouter resolves the selected upstream credential internally.

\---

## CLI reference

Use command-level help to see the exact flags supported by the current build:

```bash
termrouter --help
termrouter <command> --help
```

### Initialization and lifecycle

```bash
termrouter init
termrouter serve
termrouter stop
termrouter status
termrouter doctor
termrouter version
```

### Provider management

```bash
termrouter provider add
termrouter provider list
termrouter provider show <name>
termrouter provider test <name>
termrouter provider enable <name>
termrouter provider disable <name>
termrouter provider remove <name>
```

### Alias and route management

```bash
termrouter alias add
termrouter alias list
termrouter alias show <name>
termrouter alias remove <name>

termrouter route add
termrouter route list
termrouter route show <name>
termrouter route remove <name>
```

### Smart Route and profile management

```bash
termrouter model profile list
termrouter model profile show <provider/model>
termrouter model profile set <provider/model>
termrouter model profile reset <provider/model>
termrouter model profile validate <provider/model>

termrouter route add intelligent --strategy smart --candidate ... --policy balanced --default ...
termrouter route smart enable intelligent --shadow
termrouter route smart enable intelligent --shadow=false

termrouter explain auto --prompt "..."
termrouter explain --request req\\\_...
termrouter smart classify --prompt "..."
termrouter smart report --route intelligent --last 7d
termrouter smart status
```

### Router client keys

```bash
termrouter key create
termrouter key list
termrouter key rotate <name>
termrouter key disable <name>
termrouter key remove <name>
```

### Logs and usage

```bash
termrouter logs
termrouter usage today
termrouter usage summary
```

### Configuration

```bash
termrouter config path
termrouter config show
termrouter config check
termrouter config export
```

### Live request testing

```bash
termrouter test <alias>
```

The live test command requires the running server and the configured test-key mechanism, such as `TERMROUTER\\\_TEST\\\_KEY`, according to the active build.

### Global flags

```text
--home <path>    Use a custom TermRouter home directory
--json           Produce machine-readable output where supported
```

Destructive commands require `--yes` when used non-interactively.

### Isolated development home

Linux or macOS:

```bash
export TERMROUTER\\\_HOME=/tmp/termrouter-demo
termrouter init --backend vault --create-key
```

PowerShell:

```powershell
$env:TERMROUTER\\\_HOME = "$env:TEMP\\\\termrouter-demo"
termrouter init --backend vault --create-key
```

\---

## Configuration and storage

The default TermRouter home directory is `\\\~/.termrouter`:

```text
\\\~/.termrouter/
├── config.yaml    # Human-editable configuration; no plaintext provider secrets
├── router.db      # SQLite state, hashes, health, usage, and Smart Route records
├── vault.db       # Encrypted provider credentials when using the vault backend
├── logs/          # Structured local logs
└── run/           # Runtime state, including the PID file
```

### Main configuration concepts

```text
server            Listener, authentication, limits, and remote-binding behavior
providers         Upstream provider connections
aliases           Public model names requested by clients
routes            Direct, fallback, and smart route definitions
model\\\_profiles    Model capabilities and operational properties
smart.\\\*           Classification, policy, confidence, affinity, and decision settings
logging           Log level, payload behavior, and retention
```

Inspect and validate configuration:

```bash
termrouter config path
termrouter config check
termrouter config show
```

Export a sanitized configuration:

```bash
termrouter config export
```

\---

## Security model

### Local-first listener

TermRouter listens on `127.0.0.1:8787` by default. Non-loopback binding requires explicit insecure-remote configuration because native TLS is not currently included.

### Client keys

* Router client keys begin with `tr\\\_live\\\_`.
* Plaintext is shown only during creation or rotation.
* TermRouter stores Argon2id hashes, not plaintext client keys.
* Client keys can be listed, rotated, disabled, and removed independently.

### Provider credentials

Provider secrets are referenced rather than embedded in `config.yaml`:

```text
env://       Environment variable
vault://     Encrypted local vault
keyring://   Operating-system credential store
```

Vault credential material is protected with ChaCha20-Poly1305.

### Routing and secret isolation

Provider credentials are resolved only after TermRouter selects an upstream target. Client applications authenticate to TermRouter with router keys, not provider keys.

### Logging privacy

* Metadata-only logging is the default.
* Authorization headers and configured secret fields are redacted.
* Raw prompt and response bodies are not required for Smart Decisions.
* Configuration export sanitizes credential references.
* Smart classification and decision metadata can be recorded without retaining raw prompts.

### Smart Route policy enforcement

Smart scoring cannot override:

* Client-key permissions
* Provider disablement
* Missing credentials
* Hard capability requirements
* Privacy restrictions
* Cost ceilings
* Provider health and circuit state

### Safe remote access

Prefer an encrypted tunnel instead of exposing TermRouter directly:

```bash
ssh -L 8787:127.0.0.1:8787 user@your-server
```

Then continue using:

```text
http://127.0.0.1:8787
```

Tailscale, WireGuard, or an authenticated TLS reverse proxy are also suitable deployment patterns.

\---

## Observability

### Health

```bash
curl http://127.0.0.1:8787/health
curl http://127.0.0.1:8787/ready
```

### Status

```bash
termrouter status
termrouter smart status
```

### Provider diagnostics

```bash
termrouter provider test <provider-name>
termrouter doctor
```

### Logs

```bash
termrouter logs
```

Logs can include metadata such as:

```text
request ID
requested alias
resolved route
selected provider and model
attempt number
fallback reason
latency
input and output token usage
Smart Route category and confidence
session-affinity result
error classification
```

### Usage

```bash
termrouter usage today
termrouter usage summary
```

### Smart Route reports

```bash
termrouter smart report --route intelligent --last 7d
```

### Decision investigation

```bash
termrouter explain --request req\\\_...
```

\---

## Troubleshooting

### Run the diagnostic sequence

```bash
termrouter config check
termrouter doctor
termrouter status
termrouter smart status
```

### `401` from TermRouter

The router client key is missing, malformed, disabled, or incorrect.

Use a key created by:

```bash
termrouter key create
```

Do not use an OpenAI, Anthropic, or other upstream provider key as the TermRouter client key.

### Provider authentication failure

Confirm that the configured environment variable, vault entry, or keyring item is available to the process running `termrouter serve`.

Then test the provider directly:

```bash
termrouter provider test <provider-name>
```

### Unknown model or alias

Inspect aliases and routes:

```bash
termrouter alias list
termrouter route list
termrouter route show <route-name>
```

### No eligible Smart Route candidate

Inspect classification and route reasoning:

```bash
termrouter explain auto --prompt "<representative prompt>"
termrouter model profile list
termrouter smart status
```

Common reasons include:

* Missing or invalid model profile
* Tool requirement unsupported by every candidate
* Context requirement larger than every candidate limit
* Provider disabled or unhealthy
* Missing credential
* Candidate blocked by privacy or cost policy
* Candidate below the minimum suitability threshold

### Unexpected Smart Route model

Use:

```bash
termrouter explain --request <request-id>
```

Check:

* Task classification
* Confidence
* Session-affinity reuse
* Candidate capability profiles
* Policy weights or constraints
* Provider health
* Rejection reasons

If a model profile does not reflect a local deployment, override and validate the profile:

```bash
termrouter model profile set <provider/model> \\\[flags]
termrouter model profile validate <provider/model>
```

### Smart Route is not changing traffic

The route may be running in shadow mode.

```bash
termrouter smart status
```

Live activation in the current CLI is:

```bash
termrouter route smart enable intelligent --shadow=false
```

### Smart Route is switching models between turns

Check whether the client supplies a stable conversation identity and whether session affinity remains active. Also verify whether capability requirements, context size, provider health, route configuration, or affinity TTL caused reclassification.

### Local provider connection refused

Confirm that the local model server is running and that the base URL includes the correct OpenAI-compatible path, commonly `/v1`.

```bash
termrouter provider test local
```

### Streaming stops after partial output

TermRouter intentionally does not switch models after the first visible stream content. Check the request explanation and logs for an upstream stream failure:

```bash
termrouter explain --request <request-id>
termrouter logs
```

### Vault works interactively but not as a service

Confirm that the service process receives the supported vault unlock mechanism. Do not place the vault password in `config.yaml` or commit vault secrets to version control.

### Port already in use

Inspect the current TermRouter process:

```bash
termrouter status
```

Then either stop it or configure another listener port.

\---

## Development

### Build and test

```bash
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
go build -trimpath -o bin/termrouter ./cmd/termrouter
```

### Make targets

Inspect the repository `Makefile` for available development and release targets:

```bash
make help
```

### Isolated development run

```bash
export TERMROUTER\\\_HOME=/tmp/termrouter-dev
./bin/termrouter init --backend vault --create-key
./bin/termrouter serve
```

### Recommended Smart Route validation

Before changing classifier or scoring behavior:

1. Add or update deterministic classification fixtures.
2. Validate hard-capability filtering.
3. Test tied candidate scores and deterministic tie-breaking.
4. Test low-confidence fallback.
5. Test session-affinity reuse and reclassification.
6. Test shadow mode does not modify live selection.
7. Test fallback before stream commitment.
8. Test no model switch after stream commitment.
9. Run race tests.
10. Confirm logs contain no test canary secrets or prompt bodies by default.

### Architecture documentation

Architecture decisions are stored under:

```text
docs/adr/
```

Keep durable technical decisions documented there, especially changes to:

* Protocol normalization
* Credential storage
* Streaming behavior
* Smart classification
* Candidate scoring
* Session identity
* Persistence schema
* Security defaults

\---

## Repository layout

```text
TerminalRouter/
├── bin/                  # Local build output
├── cmd/termrouter/       # CLI entrypoint and commands
├── docs/adr/             # Architecture decision records
├── internal/             # Gateway, routing, providers, storage, security, and Smart Routes
├── .gitignore
├── LICENSE
├── Makefile
├── README.md
├── go.mod
└── go.sum
```

\---

## Design principles

1. **Terminal first** — every feature is operable without a browser.
2. **Local first** — localhost is the safe default.
3. **Protocol aware** — OpenAI and Anthropic requests are normalized rather than blindly forwarded.
4. **Provider independent** — clients use TermRouter aliases, not provider-specific configuration.
5. **Explainable routing** — automatic decisions can be inspected.
6. **Hard constraints before preferences** — capability and security requirements cannot be outscored.
7. **Safe streaming** — no cross-model stream merging.
8. **Private by default** — credentials and prompt bodies are not ordinary log content.
9. **Backward compatible** — deterministic aliases remain available when Smart Routes are disabled.
10. **One execution engine** — Smart Routes produce plans; existing retry and fallback execute them.

\---

## Contributing

Contributions are welcome.

Before opening a pull request:

1. Keep the terminal-only and local-first scope intact.
2. Add tests for user-visible behavior.
3. Add golden fixtures for protocol or classifier changes.
4. Test streaming cancellation and failure paths where relevant.
5. Run formatting, vetting, normal tests, and race tests.
6. Update CLI help and README examples when commands change.
7. Add a migration for persistent schema changes.
8. Never commit provider keys, router client keys, prompts, vault files, or local databases.
9. Preserve explainability for Smart Route changes.
10. Document durable architectural changes under `docs/adr/`.

When reporting a security issue, do not include real credentials, private prompts, or exploitable secrets in a public issue.

\---

## Roadmap

Potential future work includes:

* Native TLS or documented hardened reverse-proxy deployment
* Additional provider protocols
* Native Gemini compatibility
* Additional service types such as embeddings
* More client integration guides
* Expanded model-profile catalog
* Additional Smart Route policy controls
* Optional classifier backends
* Feedback-based profile calibration using explicit quality signals
* Improved release packaging and signed binaries

Roadmap items are not guarantees and should not be treated as implemented functionality.

\---

## License

TermRouter is licensed under the [MIT License](LICENSE).

\---

<div align="center">

**TermRouter decides who should answer. The selected model produces the answer.**

[Back to top](#termrouter)

</div>
