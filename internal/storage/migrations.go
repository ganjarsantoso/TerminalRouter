package storage

const schemaSQL = `
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS client_keys (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    key_prefix TEXT NOT NULL,
    key_hash TEXT NOT NULL,
    salt TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    allowed_aliases TEXT, -- JSON array; empty/null = all
    rate_limit_rpm INTEGER,
    created_at TEXT NOT NULL,
    rotated_at TEXT,
    disabled_at TEXT
);

CREATE TABLE IF NOT EXISTS provider_health (
    provider_id TEXT PRIMARY KEY,
    circuit_state TEXT NOT NULL DEFAULT 'closed', -- closed | open | half_open
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    cooldown_until TEXT,
    last_error TEXT,
    last_latency_ms INTEGER,
    last_success_at TEXT,
    last_failure_at TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS request_log (
    id TEXT PRIMARY KEY,
    timestamp TEXT NOT NULL,
    client_key_id TEXT,
    inbound_protocol TEXT,
    requested_model TEXT,
    resolved_alias TEXT,
    provider_id TEXT,
    upstream_model TEXT,
    attempt INTEGER,
    fallback_reason TEXT,
    status_code INTEGER,
    latency_ms INTEGER,
    time_to_first_token_ms INTEGER,
    input_tokens INTEGER,
    output_tokens INTEGER,
    usage_source TEXT,
    error_class TEXT,
    stream INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_request_log_ts ON request_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_request_log_provider ON request_log(provider_id);

CREATE TABLE IF NOT EXISTS model_cache (
    provider_id TEXT NOT NULL,
    model_id TEXT NOT NULL,
    display_name TEXT,
    capabilities TEXT,
    refreshed_at TEXT NOT NULL,
    PRIMARY KEY (provider_id, model_id)
);
`

const currentSchemaVersion = 1
