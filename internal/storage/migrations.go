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

CREATE TABLE IF NOT EXISTS smart_decisions (
    request_id TEXT PRIMARY KEY,
    route_id TEXT NOT NULL,
    requested_alias TEXT,
    mode TEXT NOT NULL,
    policy TEXT,
    task_primary_type TEXT,
    task_complexity TEXT,
    confidence REAL,
    classifier_version TEXT,
    selected_provider TEXT,
    selected_model TEXT,
    selection_score REAL,
    selection_reasons TEXT,
    shadow_recommendation TEXT,
    used_default INTEGER NOT NULL DEFAULT 0,
    default_reason TEXT,
    session_id TEXT,
    session_affinity_hit INTEGER NOT NULL DEFAULT 0,
    evaluations_json TEXT,
    task_json TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_smart_decisions_route ON smart_decisions(route_id);
CREATE INDEX IF NOT EXISTS idx_smart_decisions_created ON smart_decisions(created_at);

CREATE TABLE IF NOT EXISTS smart_session_affinity (
    session_id TEXT PRIMARY KEY,
    route_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    task_type TEXT,
    complexity TEXT,
    expires_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_smart_affinity_expires ON smart_session_affinity(expires_at);
`

const currentSchemaVersion = 2
