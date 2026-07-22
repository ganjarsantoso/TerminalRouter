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
    max_concurrent_requests INTEGER,
    daily_request_limit INTEGER,
    daily_input_tokens INTEGER,
    daily_output_tokens INTEGER,
    daily_estimated_cost_usd REAL,
    max_output_tokens INTEGER,
    max_request_body INTEGER,
    allow_direct_models INTEGER NOT NULL DEFAULT 0,
    allowed_direct_models TEXT, -- JSON array; empty/null with allow_direct_models=1 = unrestricted
    expires_at TEXT,
    portable INTEGER NOT NULL DEFAULT 0,
    optimization_mode TEXT, -- per-key maximum optimization mode (off|safe|balanced|aggressive); empty = server default
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
    stream INTEGER NOT NULL DEFAULT 0,
    client_label TEXT
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

CREATE TABLE IF NOT EXISTS config_history (
    revision INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,
    session_id TEXT,
    change_type TEXT NOT NULL,
    affected_resources TEXT,
    config_yaml TEXT NOT NULL,
    sanitized_yaml TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS model_assessments (
    assessment_id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    model_id TEXT NOT NULL,
    connection_fingerprint TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    depth TEXT NOT NULL,
    benchmark_version TEXT NOT NULL,
    scoring_version TEXT NOT NULL,
    categories_json TEXT,
    started_at TEXT,
    completed_at TEXT,
    estimated_tokens INTEGER NOT NULL DEFAULT 0,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_cost REAL NOT NULL DEFAULT 0,
    actual_cost REAL NOT NULL DEFAULT 0,
    confidence REAL NOT NULL DEFAULT 0,
    proposed_profile_json TEXT,
    applied_at TEXT,
    applied_fields TEXT,
    error_text TEXT
);

CREATE INDEX IF NOT EXISTS idx_model_assessments_model ON model_assessments(provider_id, model_id);
CREATE INDEX IF NOT EXISTS idx_model_assessments_status ON model_assessments(status);

CREATE TABLE IF NOT EXISTS model_assessment_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    assessment_id TEXT NOT NULL REFERENCES model_assessments(assessment_id),
    category TEXT NOT NULL,
    test_name TEXT NOT NULL,
    passed INTEGER NOT NULL DEFAULT 0,
    score REAL,
    evidence TEXT,
    latency_ms INTEGER,
    input_tokens INTEGER,
    output_tokens INTEGER,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_assessment_results_cat ON model_assessment_results(assessment_id, category);

CREATE TABLE IF NOT EXISTS external_evidence_records (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    model_identity TEXT NOT NULL,
    benchmark TEXT NOT NULL,
    value REAL NOT NULL,
    scale TEXT NOT NULL,
    capability TEXT NOT NULL,
    reported_at TEXT NOT NULL,
    url TEXT,
    notes TEXT
);

CREATE INDEX IF NOT EXISTS idx_external_evidence_model ON external_evidence_records(model_identity);

CREATE TABLE IF NOT EXISTS external_profile_proposals (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    model_id TEXT NOT NULL,
    model_identity TEXT NOT NULL,
    fields_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    status TEXT NOT NULL,
    registry_version TEXT NOT NULL,
    mandatory_review INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_external_proposals_status ON external_profile_proposals(status);

CREATE TABLE IF NOT EXISTS external_profile_imports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id TEXT NOT NULL,
    proposal_id TEXT NOT NULL,
    applied_at TEXT NOT NULL,
    capabilities_json TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_external_imports_profile ON external_profile_imports(profile_id);

CREATE TABLE IF NOT EXISTS external_registry_versions (
    version TEXT PRIMARY KEY,
    updated_at TEXT NOT NULL,
    source_count INTEGER NOT NULL,
    model_count INTEGER NOT NULL,
    evidence_count INTEGER NOT NULL
);

-- Optimization decision records (no raw prompts). Privacy-conscious: only
-- counts, action names, mode, and provenance are stored. Cache fields use
-- distinct labels: cache_opportunity_tokens_est (estimated prefix stabilization)
-- is separate from cache_read_tokens_actual / cache_write_tokens_actual
-- (provider-reported). The old cached_input_tokens / cache_write_tokens columns
-- are deprecated but retained for databases that have them.
CREATE TABLE IF NOT EXISTS request_optimizations (
    request_id TEXT PRIMARY KEY,
    route_name TEXT,
    client_key_id TEXT,
    provider_id TEXT,
    model_id TEXT,
    mode_requested TEXT,
    mode_applied TEXT,
    lui_version TEXT,
    renderer TEXT,
    estimators_json TEXT,
    optimizers_json TEXT,
    input_tokens_before INTEGER,
    input_tokens_after_estimated INTEGER,
    provider_input_tokens_actual INTEGER,
    provider_output_tokens_actual INTEGER,
    cache_status TEXT,
    cache_opportunity_tokens_est INTEGER,
    cache_read_tokens_actual INTEGER,
    cache_write_tokens_actual INTEGER,
    cache_source TEXT,
    deprecated_cached_input_tokens INTEGER,
    deprecated_cache_write_tokens INTEGER,
    compression_input_tokens INTEGER,
    compression_output_tokens INTEGER,
    gross_saving_usd REAL,
    optimizer_cost_usd REAL,
    net_saving_usd REAL,
    added_latency_ms INTEGER,
    loss_class TEXT,
    bypassed INTEGER NOT NULL DEFAULT 0,
    bypass_reason TEXT,
    quality_status TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_request_optimizations_created ON request_optimizations(created_at);
CREATE INDEX IF NOT EXISTS idx_request_optimizations_provider ON request_optimizations(provider_id, model_id);

-- Revision 6: quota tracking and analytics
CREATE TABLE IF NOT EXISTS quota_snapshots (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    model_id TEXT,
    definition_id TEXT,
    dimension TEXT NOT NULL,
    window_start TEXT,
    window_end TEXT,
    reset_at TEXT,
    limit_value REAL,
    used_value REAL,
    remaining_value REAL,
    reserved_value REAL,
    source TEXT NOT NULL,
    confidence REAL,
    observed_at TEXT NOT NULL,
    stale_after TEXT,
    metadata_json TEXT
);

CREATE INDEX IF NOT EXISTS idx_quota_snapshots_provider ON quota_snapshots(provider_id, account_id, observed_at);
CREATE INDEX IF NOT EXISTS idx_quota_snapshots_definition ON quota_snapshots(definition_id, observed_at);

CREATE TABLE IF NOT EXISTS quota_events (
    id TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    provider_id TEXT,
    account_id TEXT,
    model_id TEXT,
    dimension TEXT,
    window_id TEXT,
    source TEXT,
    status TEXT,
    message TEXT,
    request_id TEXT,
    created_at TEXT NOT NULL,
    metadata_json TEXT
);

CREATE INDEX IF NOT EXISTS idx_quota_events_created ON quota_events(created_at);

CREATE TABLE IF NOT EXISTS account_routing_state (
    provider_id TEXT NOT NULL,
    route_id TEXT NOT NULL,
    last_account_id TEXT,
    sequence INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (provider_id, route_id)
);

CREATE TABLE IF NOT EXISTS provider_account_state (
    provider_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    draining INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL,
    metadata_json TEXT,
    PRIMARY KEY (provider_id, account_id)
);

CREATE TABLE IF NOT EXISTS usage_rollups_hourly (
    bucket_start TEXT NOT NULL,
    provider_id TEXT NOT NULL DEFAULT '',
    account_id TEXT NOT NULL DEFAULT '',
    model_id TEXT NOT NULL DEFAULT '',
    route_alias TEXT NOT NULL DEFAULT '',
    client_key_id TEXT NOT NULL DEFAULT '',
    requests INTEGER NOT NULL DEFAULT 0,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cached_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_cost_usd REAL NOT NULL DEFAULT 0,
    billed_cost_usd REAL NOT NULL DEFAULT 0,
    errors INTEGER NOT NULL DEFAULT 0,
    throttles INTEGER NOT NULL DEFAULT 0,
    opt_savings_usd REAL NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_start, provider_id, account_id, model_id, route_alias, client_key_id)
);

CREATE INDEX IF NOT EXISTS idx_usage_rollups_hourly_bucket ON usage_rollups_hourly(bucket_start, provider_id, account_id);

CREATE TABLE IF NOT EXISTS usage_rollups_daily (
    bucket_start TEXT NOT NULL,
    provider_id TEXT NOT NULL DEFAULT '',
    account_id TEXT NOT NULL DEFAULT '',
    model_id TEXT NOT NULL DEFAULT '',
    route_alias TEXT NOT NULL DEFAULT '',
    client_key_id TEXT NOT NULL DEFAULT '',
    requests INTEGER NOT NULL DEFAULT 0,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cached_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_cost_usd REAL NOT NULL DEFAULT 0,
    billed_cost_usd REAL NOT NULL DEFAULT 0,
    errors INTEGER NOT NULL DEFAULT 0,
    throttles INTEGER NOT NULL DEFAULT 0,
    opt_savings_usd REAL NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_start, provider_id, account_id, model_id, route_alias, client_key_id)
);

CREATE TABLE IF NOT EXISTS subscription_plans (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    monthly_price REAL,
    currency TEXT NOT NULL DEFAULT 'USD',
    allowance_json TEXT,
    billing_cycle_anchor TEXT,
    renewal_rule_json TEXT,
    overage_allowed INTEGER NOT NULL DEFAULT 0,
    overage_pricing_source TEXT,
    source TEXT NOT NULL DEFAULT 'manual_configuration',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS cost_reconciliations (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    period_start TEXT NOT NULL,
    period_end TEXT NOT NULL,
    local_total REAL,
    provider_total REAL,
    delta REAL,
    source TEXT,
    observed_at TEXT NOT NULL,
    notes TEXT
);
`

const currentSchemaVersion = 13

// migrationSQLv6 adds per-key public-hosting policy columns and client labels.
// Applied for databases created before schema version 6 (CREATE IF NOT EXISTS
// does not alter existing tables).
var migrationSQLv6 = []string{
	`ALTER TABLE client_keys ADD COLUMN max_concurrent_requests INTEGER`,
	`ALTER TABLE client_keys ADD COLUMN daily_request_limit INTEGER`,
	`ALTER TABLE client_keys ADD COLUMN daily_input_tokens INTEGER`,
	`ALTER TABLE client_keys ADD COLUMN daily_output_tokens INTEGER`,
	`ALTER TABLE client_keys ADD COLUMN daily_estimated_cost_usd REAL`,
	`ALTER TABLE client_keys ADD COLUMN max_output_tokens INTEGER`,
	`ALTER TABLE client_keys ADD COLUMN expires_at TEXT`,
	`ALTER TABLE client_keys ADD COLUMN portable INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE request_log ADD COLUMN client_label TEXT`,
}

// migrationSQLv7 adds the per-key request body size cap.
var migrationSQLv7 = []string{
	`ALTER TABLE client_keys ADD COLUMN max_request_body INTEGER`,
}

// migrationSQLv8 adds per-key direct-model access controls. Direct-model access
// defaults to disabled so that existing keys cannot bypass alias policy via
// provider/model syntax.
var migrationSQLv8 = []string{
	`ALTER TABLE client_keys ADD COLUMN allow_direct_models INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE client_keys ADD COLUMN allowed_direct_models TEXT`,
}

// migrationSQLv9 persists the external-consensus proposal mandatory-review flag
// (§18) so it survives a load/apply round-trip and cannot be silently cleared.
var migrationSQLv9 = []string{
	`ALTER TABLE external_profile_proposals ADD COLUMN mandatory_review INTEGER NOT NULL DEFAULT 0`,
}

// migrationSQLv10 adds the per-key optimization policy column (the
// request_optimizations table itself is created idempotently by schemaSQL).
var migrationSQLv10 = []string{
	`ALTER TABLE client_keys ADD COLUMN optimization_mode TEXT`,
}

// migrationSQLv11 adds account attribution on request_log. Quota tables are
// created idempotently by schemaSQL (CREATE TABLE IF NOT EXISTS).
var migrationSQLv11 = []string{
	`ALTER TABLE request_log ADD COLUMN account_id TEXT`,
	`CREATE INDEX IF NOT EXISTS idx_request_log_account ON request_log(account_id, timestamp)`,
	`CREATE INDEX IF NOT EXISTS idx_request_log_provider_ts ON request_log(provider_id, timestamp)`,
	`CREATE INDEX IF NOT EXISTS idx_request_log_model_ts ON request_log(upstream_model, timestamp)`,
}

// migrationSQLv12 adds TLS provenance tracking to cached evidence records.
var migrationSQLv12 = []string{
	`ALTER TABLE external_evidence_records ADD COLUMN tls_disabled INTEGER NOT NULL DEFAULT 0`,
}

// migrationSQLv13 adds cache-estimate/actual separation columns to
// request_optimizations. Cache status, source, and separate estimate/actual
// token counts replace the single cached_input_tokens / cache_write_tokens
// columns which are retained as deprecated_cached_input_tokens /
// deprecated_cache_write_tokens.
var migrationSQLv13 = []string{
	`ALTER TABLE request_optimizations ADD COLUMN cache_status TEXT`,
	`ALTER TABLE request_optimizations ADD COLUMN cache_opportunity_tokens_est INTEGER`,
	`ALTER TABLE request_optimizations ADD COLUMN cache_read_tokens_actual INTEGER`,
	`ALTER TABLE request_optimizations ADD COLUMN cache_write_tokens_actual INTEGER`,
	`ALTER TABLE request_optimizations ADD COLUMN cache_source TEXT`,
	`ALTER TABLE request_optimizations ADD COLUMN deprecated_cached_input_tokens INTEGER`,
	`ALTER TABLE request_optimizations ADD COLUMN deprecated_cache_write_tokens INTEGER`,
}
