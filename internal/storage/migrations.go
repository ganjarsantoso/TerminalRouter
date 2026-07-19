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
    registry_version TEXT NOT NULL
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
`

const currentSchemaVersion = 5
