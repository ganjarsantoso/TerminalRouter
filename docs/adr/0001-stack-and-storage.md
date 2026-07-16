# ADR 0001: Stack and storage choices

## Status

Accepted

## Context

TermRouter is a local-first single binary AI gateway. Cross-platform builds (Windows/Linux/macOS) and minimal audit surface matter more than framework features.

## Decision

- **Language**: Go
- **CLI**: Cobra
- **HTTP**: `net/http` standard library
- **Config**: YAML (`gopkg.in/yaml.v3`), no secrets in file
- **State DB**: SQLite via `modernc.org/sqlite` (pure Go, no CGO) in WAL mode
- **Credentials**: env references, OS keyring (`zalando/go-keyring`), encrypted vault (Argon2id + XChaCha20-Poly1305)
- **Client keys**: Argon2id salted hashes only

## Consequences

- CGO-free builds simplify cross-compilation.
- SQLite single-writer is fine for a laptop/local gateway.
- Vault file next to config is the reliable headless fallback when keyring is unavailable.
