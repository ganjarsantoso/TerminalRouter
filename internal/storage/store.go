package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/termrouter/termrouter/internal/optimization"

	_ "modernc.org/sqlite"
)

// Store is the SQLite-backed state store.
type Store struct {
	db *sql.DB
	// PriceFunc computes the USD cost for a resolved provider/model and token
	// counts. It is set from configuration at startup and used by
	// UsageForKeySince to price aggregated usage; a nil value means cost
	// budgets cannot be enforced and unpriced routes must be rejected.
	PriceFunc func(provider, model string, inTokens, outTokens int) (float64, bool)
}

// SetPricing wires the cost computation function used when aggregating usage.
func (s *Store) SetPricing(f func(provider, model string, inTokens, outTokens int) (float64, bool)) {
	s.PriceFunc = f
}

// Open opens (or creates) the router database in WAL mode and runs migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: single writer; fine for local gateway
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	var ver int
	err := s.db.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&ver)
	if err == sql.ErrNoRows {
		_, err = s.db.Exec(`INSERT INTO schema_version(version) VALUES(?)`, currentSchemaVersion)
		return err
	}
	if err != nil {
		// table empty or missing row
		var count int
		if e := s.db.QueryRow(`SELECT COUNT(*) FROM schema_version`).Scan(&count); e == nil && count == 0 {
			_, err = s.db.Exec(`INSERT INTO schema_version(version) VALUES(?)`, currentSchemaVersion)
			return err
		}
		return err
	}
	if ver > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than this binary (%d)", ver, currentSchemaVersion)
	}
	if ver < currentSchemaVersion {
		if err := s.applyMigrations(ver); err != nil {
			return err
		}
		if _, err := s.db.Exec(`UPDATE schema_version SET version = ?`, currentSchemaVersion); err != nil {
			return err
		}
	}
	return nil
}

// applyMigrations runs incremental upgrades from fromVersion exclusive to currentSchemaVersion.
func (s *Store) applyMigrations(fromVersion int) error {
	if fromVersion < 6 {
		for _, stmt := range migrationSQLv6 {
			if _, err := s.db.Exec(stmt); err != nil {
				// Ignore "duplicate column" on partially upgraded databases.
				if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
					return fmt.Errorf("migrate to v6: %w", err)
				}
			}
		}
	}
	if fromVersion < 7 {
		for _, stmt := range migrationSQLv7 {
			if _, err := s.db.Exec(stmt); err != nil {
				if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
					return fmt.Errorf("migrate to v7: %w", err)
				}
			}
		}
	}
	if fromVersion < 8 {
		for _, stmt := range migrationSQLv8 {
			if _, err := s.db.Exec(stmt); err != nil {
				if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
					return fmt.Errorf("migrate to v8: %w", err)
				}
			}
		}
	}
	if fromVersion < 9 {
		for _, stmt := range migrationSQLv9 {
			if _, err := s.db.Exec(stmt); err != nil {
				if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
					return fmt.Errorf("migrate to v9: %w", err)
				}
			}
		}
	}
	if fromVersion < 10 {
		for _, stmt := range migrationSQLv10 {
			if _, err := s.db.Exec(stmt); err != nil {
				if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
					return fmt.Errorf("migrate to v10: %w", err)
				}
			}
		}
	}
	if fromVersion < 11 {
		for _, stmt := range migrationSQLv11 {
			if _, err := s.db.Exec(stmt); err != nil {
				low := strings.ToLower(err.Error())
				if !strings.Contains(low, "duplicate column") && !strings.Contains(low, "already exists") {
					return fmt.Errorf("migrate to v11: %w", err)
				}
			}
		}
	}
	if fromVersion < 12 {
		for _, stmt := range migrationSQLv12 {
			if _, err := s.db.Exec(stmt); err != nil {
				low := strings.ToLower(err.Error())
				if !strings.Contains(low, "duplicate column") && !strings.Contains(low, "already exists") {
					return fmt.Errorf("migrate to v12: %w", err)
				}
			}
		}
	}
	if fromVersion < 13 {
		for _, stmt := range migrationSQLv13 {
			if _, err := s.db.Exec(stmt); err != nil {
				low := strings.ToLower(err.Error())
				if !strings.Contains(low, "duplicate column") && !strings.Contains(low, "already exists") {
					return fmt.Errorf("migrate to v13: %w", err)
				}
			}
		}
	}
	return nil
}

// DB exposes the underlying *sql.DB for advanced queries in tests.
func (s *Store) DB() *sql.DB { return s.db }

// --- Client keys ---

type ClientKey struct {
	ID                    string
	Name                  string
	KeyPrefix             string
	KeyHash               string
	Salt                  string
	Enabled               bool
	AllowedAliases        []string
	RateLimitRPM          *int
	MaxConcurrentRequests *int
	DailyRequestLimit     *int
	DailyInputTokens      *int64
	DailyOutputTokens     *int64
	DailyEstimatedCostUSD *float64
	MaxOutputTokens       *int
	MaxRequestBody        *int64   // per-key request body size cap in bytes (nil = unlimited)
	AllowDirectModels     bool     // whether the key may use provider/model direct syntax at all
	AllowedDirectModels   []string // exact direct models allowed; empty with AllowDirectModels=true means unrestricted
	ExpiresAt             *time.Time
	Portable              bool
	OptimizationMode      *string // per-key maximum optimization mode; nil = server default
	CreatedAt             time.Time
	RotatedAt             *time.Time
	DisabledAt            *time.Time
}

const clientKeySelectCols = `id, name, key_prefix, key_hash, salt, enabled, allowed_aliases, rate_limit_rpm,
	max_concurrent_requests, daily_request_limit, daily_input_tokens, daily_output_tokens,
	daily_estimated_cost_usd, max_output_tokens, max_request_body, allow_direct_models, allowed_direct_models,
	expires_at, portable, optimization_mode, created_at, rotated_at, disabled_at`

func (s *Store) InsertClientKey(ctx context.Context, k ClientKey) error {
	aliases, _ := json.Marshal(k.AllowedAliases)
	directModels, _ := json.Marshal(k.AllowedDirectModels)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO client_keys(
			id, name, key_prefix, key_hash, salt, enabled, allowed_aliases, rate_limit_rpm,
			max_concurrent_requests, daily_request_limit, daily_input_tokens, daily_output_tokens,
			daily_estimated_cost_usd, max_output_tokens, max_request_body, allow_direct_models,
			allowed_direct_models, expires_at, portable, optimization_mode, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		k.ID, k.Name, k.KeyPrefix, k.KeyHash, k.Salt, boolToInt(k.Enabled),
		string(aliases), nullInt(k.RateLimitRPM), nullInt(k.MaxConcurrentRequests),
		nullInt(k.DailyRequestLimit), nullInt64(k.DailyInputTokens), nullInt64(k.DailyOutputTokens),
		nullFloat(k.DailyEstimatedCostUSD), nullInt(k.MaxOutputTokens), nullInt64(k.MaxRequestBody),
		boolToInt(k.AllowDirectModels), string(directModels),
		nullTime(k.ExpiresAt),
		boolToInt(k.Portable), nullStrPtr(k.OptimizationMode),
		k.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

// UpdateClientKeyPolicy updates non-secret policy fields on a client key.
func (s *Store) UpdateClientKeyPolicy(ctx context.Context, k ClientKey) error {
	aliases, _ := json.Marshal(k.AllowedAliases)
	directModels, _ := json.Marshal(k.AllowedDirectModels)
	res, err := s.db.ExecContext(ctx, `
		UPDATE client_keys SET
			name=?,
			allowed_aliases=?,
			rate_limit_rpm=?,
			max_concurrent_requests=?,
			daily_request_limit=?,
			daily_input_tokens=?,
			daily_output_tokens=?,
			daily_estimated_cost_usd=?,
			max_output_tokens=?,
			max_request_body=?,
			allow_direct_models=?,
			allowed_direct_models=?,
			expires_at=?,
			portable=?,
			optimization_mode=?
		WHERE id=?`,
		k.Name, string(aliases), nullInt(k.RateLimitRPM), nullInt(k.MaxConcurrentRequests),
		nullInt(k.DailyRequestLimit), nullInt64(k.DailyInputTokens), nullInt64(k.DailyOutputTokens),
		nullFloat(k.DailyEstimatedCostUSD), nullInt(k.MaxOutputTokens), nullInt64(k.MaxRequestBody),
		boolToInt(k.AllowDirectModels), string(directModels), nullTime(k.ExpiresAt),
		boolToInt(k.Portable), nullStrPtr(k.OptimizationMode), k.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("client key %q not found", k.ID)
	}
	return nil
}

func (s *Store) ListClientKeys(ctx context.Context) ([]ClientKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+clientKeySelectCols+` FROM client_keys ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClientKey
	for rows.Next() {
		k, err := scanClientKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) GetClientKeyByID(ctx context.Context, id string) (*ClientKey, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+clientKeySelectCols+` FROM client_keys WHERE id=?`, id)
	k, err := scanClientKey(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (s *Store) FindEnabledKeys(ctx context.Context) ([]ClientKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+clientKeySelectCols+` FROM client_keys WHERE enabled=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClientKey
	for rows.Next() {
		k, err := scanClientKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// FindEnabledKeysByPrefix returns enabled keys whose stored non-secret prefix
// matches. This narrows the candidate set before expensive Argon2 verification.
// The prefix is not a secret and prefix equality alone never authenticates.
func (s *Store) FindEnabledKeysByPrefix(ctx context.Context, prefix string) ([]ClientKey, error) {
	if prefix == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+clientKeySelectCols+` FROM client_keys WHERE enabled=1 AND key_prefix=?`, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClientKey
	for rows.Next() {
		k, err := scanClientKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) DisableClientKey(ctx context.Context, id string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `UPDATE client_keys SET enabled=0, disabled_at=? WHERE id=?`, now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("client key %q not found", id)
	}
	return nil
}

func (s *Store) RemoveClientKey(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM client_keys WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("client key %q not found", id)
	}
	return nil
}

func (s *Store) UpdateClientKeyHash(ctx context.Context, id, keyPrefix, keyHash, salt string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
		UPDATE client_keys SET key_prefix=?, key_hash=?, salt=?, rotated_at=? WHERE id=?`,
		keyPrefix, keyHash, salt, now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("client key %q not found", id)
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanClientKey(row scannable) (ClientKey, error) {
	var k ClientKey
	var enabled, portable, allowDirect int
	var aliases, directModels, optMode sql.NullString
	var rpm, maxConc, dailyReq, maxOut, maxBody sql.NullInt64
	var dailyIn, dailyOut sql.NullInt64
	var dailyCost sql.NullFloat64
	var created string
	var expires, rotated, disabled sql.NullString
	err := row.Scan(
		&k.ID, &k.Name, &k.KeyPrefix, &k.KeyHash, &k.Salt, &enabled, &aliases, &rpm,
		&maxConc, &dailyReq, &dailyIn, &dailyOut, &dailyCost, &maxOut, &maxBody,
		&allowDirect, &directModels, &expires, &portable, &optMode,
		&created, &rotated, &disabled,
	)
	if err != nil {
		return k, err
	}
	k.Enabled = enabled == 1
	k.Portable = portable == 1
	k.AllowDirectModels = allowDirect == 1
	if optMode.Valid && optMode.String != "" {
		v := optMode.String
		k.OptimizationMode = &v
	}
	if aliases.Valid && aliases.String != "" && aliases.String != "null" {
		_ = json.Unmarshal([]byte(aliases.String), &k.AllowedAliases)
	}
	if directModels.Valid && directModels.String != "" && directModels.String != "null" {
		_ = json.Unmarshal([]byte(directModels.String), &k.AllowedDirectModels)
	}
	k.RateLimitRPM = intPtrFromNull(rpm)
	k.MaxConcurrentRequests = intPtrFromNull(maxConc)
	k.DailyRequestLimit = intPtrFromNull(dailyReq)
	k.DailyInputTokens = int64PtrFromNull(dailyIn)
	k.DailyOutputTokens = int64PtrFromNull(dailyOut)
	if dailyCost.Valid {
		v := dailyCost.Float64
		k.DailyEstimatedCostUSD = &v
	}
	k.MaxOutputTokens = intPtrFromNull(maxOut)
	k.MaxRequestBody = int64PtrFromNull(maxBody)
	k.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if expires.Valid && expires.String != "" {
		t, e := time.Parse(time.RFC3339Nano, expires.String)
		if e != nil {
			t, e = time.Parse(time.RFC3339, expires.String)
		}
		if e == nil {
			k.ExpiresAt = &t
		}
	}
	if rotated.Valid {
		t, _ := time.Parse(time.RFC3339Nano, rotated.String)
		k.RotatedAt = &t
	}
	if disabled.Valid {
		t, _ := time.Parse(time.RFC3339Nano, disabled.String)
		k.DisabledAt = &t
	}
	return k, nil
}

// Expired reports whether the key is past its optional expiration.
func (k ClientKey) Expired(now time.Time) bool {
	return k.ExpiresAt != nil && !k.ExpiresAt.IsZero() && now.After(*k.ExpiresAt)
}

// KeyUsageToday is aggregate usage for a client key since midnight UTC.
type KeyUsageToday struct {
	Requests     int
	InputTokens  int64
	OutputTokens int64
	EstimatedUSD float64
}

// UsageForKeySince aggregates request_log rows for a client key. Estimated cost
// is computed from the configured pricing source using each row's resolved
// provider/model; rows whose provider/model cannot be priced contribute zero to
// the estimated cost (the unpriced route would already have been rejected at
// admission for portable/public keys).
func (s *Store) UsageForKeySince(ctx context.Context, keyID string, since time.Time) (*KeyUsageToday, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT status_code, input_tokens, output_tokens, provider_id, upstream_model
		FROM request_log
		WHERE client_key_id = ? AND timestamp >= ?`,
		keyID, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &KeyUsageToday{}
	for rows.Next() {
		var status, inTok, outTok sql.NullInt64
		var provider, model sql.NullString
		if err := rows.Scan(&status, &inTok, &outTok, &provider, &model); err != nil {
			return nil, err
		}
		out.Requests++
		var in, outN int64
		if inTok.Valid {
			in = inTok.Int64
			out.InputTokens += in
		}
		if outTok.Valid {
			outN = outTok.Int64
			out.OutputTokens += outN
		}
		if s.PriceFunc != nil && provider.Valid && model.Valid {
			if cost, ok := s.PriceFunc(provider.String, model.String, int(in), int(outN)); ok {
				out.EstimatedUSD += cost
			}
		}
	}
	return out, rows.Err()
}

// CountRequestsForKeySince counts request_log rows for a key in a time window (for RPM checks).
func (s *Store) CountRequestsForKeySince(ctx context.Context, keyID string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM request_log
		WHERE client_key_id = ? AND timestamp >= ?`,
		keyID, since.UTC().Format(time.RFC3339Nano)).Scan(&n)
	return n, err
}

func intPtrFromNull(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

func int64PtrFromNull(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func nullInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// --- Provider health ---

type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half_open"
)

type ProviderHealth struct {
	ProviderID          string
	CircuitState        CircuitState
	ConsecutiveFailures int
	CooldownUntil       *time.Time
	LastError           string
	LastLatencyMs       int
	LastSuccessAt       *time.Time
	LastFailureAt       *time.Time
	UpdatedAt           time.Time
}

func (s *Store) GetProviderHealth(ctx context.Context, providerID string) (*ProviderHealth, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT provider_id, circuit_state, consecutive_failures, cooldown_until, last_error,
		       last_latency_ms, last_success_at, last_failure_at, updated_at
		FROM provider_health WHERE provider_id=?`, providerID)
	h, err := scanHealth(row)
	if err == sql.ErrNoRows {
		return &ProviderHealth{ProviderID: providerID, CircuitState: CircuitClosed, UpdatedAt: time.Now().UTC()}, nil
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func (s *Store) ListProviderHealth(ctx context.Context) ([]ProviderHealth, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider_id, circuit_state, consecutive_failures, cooldown_until, last_error,
		       last_latency_ms, last_success_at, last_failure_at, updated_at
		FROM provider_health`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderHealth
	for rows.Next() {
		h, err := scanHealth(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) UpsertProviderHealth(ctx context.Context, h ProviderHealth) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var cooldown, lastSuccess, lastFailure any
	if h.CooldownUntil != nil {
		cooldown = h.CooldownUntil.UTC().Format(time.RFC3339Nano)
	}
	if h.LastSuccessAt != nil {
		lastSuccess = h.LastSuccessAt.UTC().Format(time.RFC3339Nano)
	}
	if h.LastFailureAt != nil {
		lastFailure = h.LastFailureAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_health(provider_id, circuit_state, consecutive_failures, cooldown_until, last_error,
			last_latency_ms, last_success_at, last_failure_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(provider_id) DO UPDATE SET
			circuit_state=excluded.circuit_state,
			consecutive_failures=excluded.consecutive_failures,
			cooldown_until=excluded.cooldown_until,
			last_error=excluded.last_error,
			last_latency_ms=excluded.last_latency_ms,
			last_success_at=excluded.last_success_at,
			last_failure_at=excluded.last_failure_at,
			updated_at=excluded.updated_at`,
		h.ProviderID, string(h.CircuitState), h.ConsecutiveFailures, cooldown, h.LastError,
		h.LastLatencyMs, lastSuccess, lastFailure, now,
	)
	return err
}

func scanHealth(row scannable) (ProviderHealth, error) {
	var h ProviderHealth
	var state string
	var cooldown, lastSuccess, lastFailure, updated sql.NullString
	var lastErr sql.NullString
	var latency sql.NullInt64
	err := row.Scan(&h.ProviderID, &state, &h.ConsecutiveFailures, &cooldown, &lastErr,
		&latency, &lastSuccess, &lastFailure, &updated)
	if err != nil {
		return h, err
	}
	h.CircuitState = CircuitState(state)
	if lastErr.Valid {
		h.LastError = lastErr.String
	}
	if latency.Valid {
		h.LastLatencyMs = int(latency.Int64)
	}
	if cooldown.Valid {
		t, _ := time.Parse(time.RFC3339Nano, cooldown.String)
		h.CooldownUntil = &t
	}
	if lastSuccess.Valid {
		t, _ := time.Parse(time.RFC3339Nano, lastSuccess.String)
		h.LastSuccessAt = &t
	}
	if lastFailure.Valid {
		t, _ := time.Parse(time.RFC3339Nano, lastFailure.String)
		h.LastFailureAt = &t
	}
	if updated.Valid {
		h.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated.String)
	}
	return h, nil
}

// --- Request log / usage ---

type RequestRecord struct {
	ID                 string
	Timestamp          time.Time
	ClientKeyID        string
	InboundProtocol    string
	RequestedModel     string
	ResolvedAlias      string
	ProviderID         string
	AccountID          string // provider account attribution; empty = default
	UpstreamModel      string
	Attempt            int
	FallbackReason     string
	StatusCode         int
	LatencyMs          int
	TimeToFirstTokenMs int
	InputTokens        int
	OutputTokens       int
	UsageSource        string
	ErrorClass         string
	Stream             bool
	ClientLabel        string
}

func (s *Store) InsertRequest(ctx context.Context, r RequestRecord) error {
	accountID := r.AccountID
	if accountID == "" {
		accountID = "default"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO request_log(id, timestamp, client_key_id, inbound_protocol, requested_model, resolved_alias,
			provider_id, account_id, upstream_model, attempt, fallback_reason, status_code, latency_ms, time_to_first_token_ms,
			input_tokens, output_tokens, usage_source, error_class, stream, client_label)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Timestamp.UTC().Format(time.RFC3339Nano), nullStr(r.ClientKeyID), r.InboundProtocol,
		r.RequestedModel, nullStr(r.ResolvedAlias), nullStr(r.ProviderID), nullStr(accountID), nullStr(r.UpstreamModel),
		r.Attempt, nullStr(r.FallbackReason), r.StatusCode, r.LatencyMs, r.TimeToFirstTokenMs,
		r.InputTokens, r.OutputTokens, nullStr(r.UsageSource), nullStr(r.ErrorClass), boolToInt(r.Stream),
		nullStr(r.ClientLabel),
	)
	return err
}

type UsageSummary struct {
	TotalRequests int
	SuccessCount  int
	ErrorCount    int
	InputTokens   int64
	OutputTokens  int64
	AvgLatencyMs  float64
	ByProvider    map[string]int
}

func (s *Store) UsageSince(ctx context.Context, since time.Time) (*UsageSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider_id, status_code, latency_ms, input_tokens, output_tokens
		FROM request_log WHERE timestamp >= ?`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sum := &UsageSummary{ByProvider: map[string]int{}}
	var latSum int64
	for rows.Next() {
		var provider sql.NullString
		var status, latency, inTok, outTok sql.NullInt64
		if err := rows.Scan(&provider, &status, &latency, &inTok, &outTok); err != nil {
			return nil, err
		}
		sum.TotalRequests++
		if status.Valid && status.Int64 >= 200 && status.Int64 < 400 {
			sum.SuccessCount++
		} else {
			sum.ErrorCount++
		}
		if latency.Valid {
			latSum += latency.Int64
		}
		if inTok.Valid {
			sum.InputTokens += inTok.Int64
		}
		if outTok.Valid {
			sum.OutputTokens += outTok.Int64
		}
		p := "unknown"
		if provider.Valid && provider.String != "" {
			p = provider.String
		}
		sum.ByProvider[p]++
	}
	if sum.TotalRequests > 0 {
		sum.AvgLatencyMs = float64(latSum) / float64(sum.TotalRequests)
	}
	return sum, rows.Err()
}

func (s *Store) RecentRequests(ctx context.Context, limit int, errorsOnly bool) ([]RequestRecord, error) {
	q := `SELECT id, timestamp, client_key_id, inbound_protocol, requested_model, resolved_alias,
		provider_id, account_id, upstream_model, attempt, fallback_reason, status_code, latency_ms, time_to_first_token_ms,
		input_tokens, output_tokens, usage_source, error_class, stream, client_label
		FROM request_log`
	if errorsOnly {
		q += ` WHERE status_code >= 400 OR error_class IS NOT NULL AND error_class != ''`
	}
	q += ` ORDER BY timestamp DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RequestRecord
	for rows.Next() {
		var r RequestRecord
		var ts string
		var clientKey, alias, provider, account, model, fb, usage, errClass, clientLabel sql.NullString
		var stream int
		if err := rows.Scan(&r.ID, &ts, &clientKey, &r.InboundProtocol, &r.RequestedModel, &alias,
			&provider, &account, &model, &r.Attempt, &fb, &r.StatusCode, &r.LatencyMs, &r.TimeToFirstTokenMs,
			&r.InputTokens, &r.OutputTokens, &usage, &errClass, &stream, &clientLabel); err != nil {
			return nil, err
		}
		r.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		r.ClientKeyID = clientKey.String
		r.ResolvedAlias = alias.String
		r.ProviderID = provider.String
		r.AccountID = account.String
		if r.AccountID == "" {
			r.AccountID = "default"
		}
		r.UpstreamModel = model.String
		r.FallbackReason = fb.String
		r.UsageSource = usage.String
		r.ErrorClass = errClass.String
		r.Stream = stream == 1
		r.ClientLabel = clientLabel.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// PurgeOldRequests deletes request logs older than retention.
func (s *Store) PurgeOldRequests(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM request_log WHERE timestamp < ?`, olderThan.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// --- Optimization records ---

// InsertOptimizationRecord persists a privacy-conscious optimization decision
// record. It satisfies the optimization.Store interface.
func (s *Store) InsertOptimizationRecord(ctx context.Context, r optimization.Record) error {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO request_optimizations(
				request_id, route_name, client_key_id, provider_id, model_id,
				mode_requested, mode_applied, lui_version, renderer,
				estimators_json, optimizers_json,
				input_tokens_before, input_tokens_after_estimated,
				provider_input_tokens_actual, provider_output_tokens_actual,
				cache_status, cache_opportunity_tokens_est,
				cache_read_tokens_actual, cache_write_tokens_actual, cache_source,
				compression_input_tokens, compression_output_tokens,
				gross_saving_usd, optimizer_cost_usd, net_saving_usd,
				added_latency_ms, loss_class, bypassed, bypass_reason, quality_status,
				created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.RequestID, nullStr(r.RouteName), nullStr(r.ClientKeyID), nullStr(r.ProviderID), nullStr(r.ModelID),
		nullStr(r.ModeRequested), nullStr(r.ModeApplied), nullStr(r.LUIVersion), nullStr(r.Renderer),
		nullStr(r.EstimatorsJSON), nullStr(r.OptimizersJSON),
		r.InputTokensBefore, r.InputTokensAfterEstimated,
		r.ProviderInputTokensActual, r.ProviderOutputTokensActual,
		nullStr(r.CacheStatus), r.CacheOpportunityTokensEst,
		r.CacheReadTokensActual, r.CacheWriteTokensActual, nullStr(r.CacheSource),
		r.CompressionInputTokens, r.CompressionOutputTokens,
		r.GrossSavingUSD, r.OptimizerCostUSD, r.NetSavingUSD,
		r.AddedLatencyMs, nullStr(r.LossClass), boolToInt(r.Bypassed), nullStr(r.BypassReason), nullStr(r.QualityStatus),
		r.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

// GetOptimizationRecord returns the optimization record for a request, or nil.
func (s *Store) GetOptimizationRecord(ctx context.Context, requestID string) (*optimization.Record, error) {
	row := s.db.QueryRowContext(ctx, `
			SELECT request_id, route_name, client_key_id, provider_id, model_id,
				mode_requested, mode_applied, lui_version, renderer,
				estimators_json, optimizers_json,
				input_tokens_before, input_tokens_after_estimated,
				provider_input_tokens_actual, provider_output_tokens_actual,
				cache_status, cache_opportunity_tokens_est,
				cache_read_tokens_actual, cache_write_tokens_actual, cache_source,
				compression_input_tokens, compression_output_tokens,
				gross_saving_usd, optimizer_cost_usd, net_saving_usd,
				added_latency_ms, loss_class, bypassed, bypass_reason, quality_status, created_at
			FROM request_optimizations WHERE request_id = ?`, requestID)
	var r optimization.Record
	var route, keyID, prov, model, modeReq, modeApp, lui, renderer, est, opt, loss, bypass, qual sql.NullString
	var cacheStat, cacheSrc sql.NullString
	var created string
	err := row.Scan(&r.RequestID, &route, &keyID, &prov, &model,
		&modeReq, &modeApp, &lui, &renderer, &est, &opt,
		&r.InputTokensBefore, &r.InputTokensAfterEstimated,
		&r.ProviderInputTokensActual, &r.ProviderOutputTokensActual,
		&cacheStat, &r.CacheOpportunityTokensEst,
		&r.CacheReadTokensActual, &r.CacheWriteTokensActual, &cacheSrc,
		&r.CompressionInputTokens, &r.CompressionOutputTokens,
		&r.GrossSavingUSD, &r.OptimizerCostUSD, &r.NetSavingUSD,
		&r.AddedLatencyMs, &loss, &bypass, &qual, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.RouteName, r.ClientKeyID, r.ProviderID, r.ModelID = route.String, keyID.String, prov.String, model.String
	r.ModeRequested, r.ModeApplied, r.LUIVersion, r.Renderer = modeReq.String, modeApp.String, lui.String, renderer.String
	r.EstimatorsJSON, r.OptimizersJSON = est.String, opt.String
	r.LossClass, r.BypassReason, r.QualityStatus = loss.String, bypass.String, qual.String
	r.Bypassed = bypass.Valid && bypass.String == "1"
	r.CacheStatus = cacheStat.String
	r.CacheSource = cacheSrc.String
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &r, nil
}

// OptimizationSummary aggregates optimization records since a time window.
type OptimizationSummary struct {
	RequestsOptimized   int
	TokensBefore        int64
	TokensAfter         int64
	CachedTokens        int64
	GrossSavingUSD      float64
	OptimizerCostUSD    float64
	NetSavingUSD        float64
	BypassCount         int
	AddedLatencyMsTotal int64
	ByMode              map[string]int
	ByProvider          map[string]int
}

// OptimizationSummarySince aggregates optimization records since since.
func (s *Store) OptimizationSummarySince(ctx context.Context, since time.Time) (*OptimizationSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
			SELECT mode_applied, provider_id, bypassed,
				input_tokens_before, input_tokens_after_estimated, cache_opportunity_tokens_est,
				gross_saving_usd, optimizer_cost_usd, net_saving_usd, added_latency_ms
			FROM request_optimizations WHERE created_at >= ?`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sum := &OptimizationSummary{ByMode: map[string]int{}, ByProvider: map[string]int{}}
	for rows.Next() {
		var mode, prov, bypass sql.NullString
		var before, after, cached, added sql.NullInt64
		var gross, optCost, net sql.NullFloat64
		if err := rows.Scan(&mode, &prov, &bypass, &before, &after, &cached, &gross, &optCost, &net, &added); err != nil {
			return nil, err
		}
		sum.RequestsOptimized++
		sum.TokensBefore += nullInt64Val(before)
		sum.TokensAfter += nullInt64Val(after)
		sum.CachedTokens += nullInt64Val(cached)
		sum.GrossSavingUSD += nullFloatVal(gross)
		sum.OptimizerCostUSD += nullFloatVal(optCost)
		sum.NetSavingUSD += nullFloatVal(net)
		sum.AddedLatencyMsTotal += nullInt64Val(added)
		if bypass.Valid && bypass.String == "1" {
			sum.BypassCount++
		}
		m := mode.String
		if m == "" {
			m = "unknown"
		}
		sum.ByMode[m]++
		p := prov.String
		if p == "" {
			p = "unknown"
		}
		sum.ByProvider[p]++
	}
	return sum, rows.Err()
}

func nullInt64Val(v sql.NullInt64) int64 {
	if v.Valid {
		return v.Int64
	}
	return 0
}

func nullFloatVal(v sql.NullFloat64) float64 {
	if v.Valid {
		return v.Float64
	}
	return 0
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullStrPtr(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}

// AliasAllowed returns true if the key may use the alias (empty allowlist = all).
func (k ClientKey) AliasAllowed(alias string) bool {
	if len(k.AllowedAliases) == 0 {
		return true
	}
	alias = strings.ToLower(alias)
	for _, a := range k.AllowedAliases {
		if strings.ToLower(a) == alias {
			return true
		}
	}
	return false
}

// DirectModelAllowed returns true if the key may use a specific direct
// provider/model string. It requires AllowDirectModels to be enabled. When the
// allowlist is empty (and direct access is enabled) any direct model is allowed;
// otherwise the model must match an entry exactly (case-insensitive, trimmed).
func (k ClientKey) DirectModelAllowed(model string) bool {
	if !k.AllowDirectModels {
		return false
	}
	if len(k.AllowedDirectModels) == 0 {
		return true
	}
	model = strings.ToLower(strings.TrimSpace(model))
	for _, m := range k.AllowedDirectModels {
		if strings.ToLower(strings.TrimSpace(m)) == model {
			return true
		}
	}
	return false
}

// --- Smart Routes persistence ---

// SmartDecisionRecord is a stored smart routing decision (no raw prompts).
type SmartDecisionRecord struct {
	RequestID            string
	RouteID              string
	RequestedAlias       string
	Mode                 string
	Policy               string
	TaskPrimaryType      string
	TaskComplexity       string
	Confidence           float64
	ClassifierVersion    string
	SelectedProvider     string
	SelectedModel        string
	SelectionScore       float64
	SelectionReasons     string // JSON array
	ShadowRecommendation string
	UsedDefault          bool
	DefaultReason        string
	SessionID            string
	SessionAffinityHit   bool
	EvaluationsJSON      string
	TaskJSON             string
	CreatedAt            time.Time
}

func (s *Store) InsertSmartDecision(ctx context.Context, r SmartDecisionRecord) error {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO smart_decisions(
			request_id, route_id, requested_alias, mode, policy,
			task_primary_type, task_complexity, confidence, classifier_version,
			selected_provider, selected_model, selection_score, selection_reasons,
			shadow_recommendation, used_default, default_reason, session_id,
			session_affinity_hit, evaluations_json, task_json, created_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.RequestID, r.RouteID, nullStr(r.RequestedAlias), r.Mode, nullStr(r.Policy),
		nullStr(r.TaskPrimaryType), nullStr(r.TaskComplexity), r.Confidence, nullStr(r.ClassifierVersion),
		nullStr(r.SelectedProvider), nullStr(r.SelectedModel), r.SelectionScore, nullStr(r.SelectionReasons),
		nullStr(r.ShadowRecommendation), boolToInt(r.UsedDefault), nullStr(r.DefaultReason), nullStr(r.SessionID),
		boolToInt(r.SessionAffinityHit), nullStr(r.EvaluationsJSON), nullStr(r.TaskJSON),
		r.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) GetSmartDecision(ctx context.Context, requestID string) (*SmartDecisionRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT request_id, route_id, requested_alias, mode, policy,
			task_primary_type, task_complexity, confidence, classifier_version,
			selected_provider, selected_model, selection_score, selection_reasons,
			shadow_recommendation, used_default, default_reason, session_id,
			session_affinity_hit, evaluations_json, task_json, created_at
		FROM smart_decisions WHERE request_id = ?`, requestID)
	var r SmartDecisionRecord
	var alias, policy, ttype, tcomp, cver, sp, sm, reasons, shadow, dreason, sid, evals, task sql.NullString
	var usedDef, affHit int
	var conf, score sql.NullFloat64
	var created string
	err := row.Scan(&r.RequestID, &r.RouteID, &alias, &r.Mode, &policy,
		&ttype, &tcomp, &conf, &cver, &sp, &sm, &score, &reasons,
		&shadow, &usedDef, &dreason, &sid, &affHit, &evals, &task, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.RequestedAlias = alias.String
	r.Policy = policy.String
	r.TaskPrimaryType = ttype.String
	r.TaskComplexity = tcomp.String
	r.Confidence = conf.Float64
	r.ClassifierVersion = cver.String
	r.SelectedProvider = sp.String
	r.SelectedModel = sm.String
	r.SelectionScore = score.Float64
	r.SelectionReasons = reasons.String
	r.ShadowRecommendation = shadow.String
	r.UsedDefault = usedDef == 1
	r.DefaultReason = dreason.String
	r.SessionID = sid.String
	r.SessionAffinityHit = affHit == 1
	r.EvaluationsJSON = evals.String
	r.TaskJSON = task.String
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &r, nil
}

// SmartShadowAggregate is a simple aggregate for shadow reports.
type SmartShadowAggregate struct {
	Total            int
	ByTaskType       map[string]int
	ByRecommendation map[string]int
	UncertainCount   int
	MatchActualCount int // when shadow rec equals actual provider/model (if known)
}

func (s *Store) SmartShadowStats(ctx context.Context, routeID string, since time.Time) (*SmartShadowAggregate, error) {
	q := `SELECT task_primary_type, shadow_recommendation, confidence, selected_provider, selected_model
		FROM smart_decisions WHERE created_at >= ?`
	args := []any{since.UTC().Format(time.RFC3339Nano)}
	if routeID != "" {
		q += ` AND route_id = ?`
		args = append(args, routeID)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	agg := &SmartShadowAggregate{
		ByTaskType:       map[string]int{},
		ByRecommendation: map[string]int{},
	}
	for rows.Next() {
		var ttype, shadow, sp, sm sql.NullString
		var conf sql.NullFloat64
		if err := rows.Scan(&ttype, &shadow, &conf, &sp, &sm); err != nil {
			return nil, err
		}
		agg.Total++
		tt := "unknown"
		if ttype.Valid && ttype.String != "" {
			tt = ttype.String
		}
		agg.ByTaskType[tt]++
		rec := "none"
		if shadow.Valid && shadow.String != "" {
			rec = shadow.String
		}
		agg.ByRecommendation[rec]++
		if conf.Valid && conf.Float64 < 0.50 {
			agg.UncertainCount++
		}
		actual := ""
		if sp.Valid && sm.Valid && sp.String != "" {
			actual = sp.String + "/" + sm.String
		}
		if actual != "" && actual == rec {
			agg.MatchActualCount++
		}
	}
	return agg, rows.Err()
}

// SessionAffinityRecord pins a conversation to a target.
type SessionAffinityRecord struct {
	SessionID  string
	RouteID    string
	Provider   string
	Model      string
	TaskType   string
	Complexity string
	ExpiresAt  time.Time
	UpdatedAt  time.Time
}

func (s *Store) GetSessionAffinity(ctx context.Context, sessionID string) (*SessionAffinityRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT session_id, route_id, provider, model, task_type, complexity, expires_at, updated_at
		FROM smart_session_affinity WHERE session_id = ?`, sessionID)
	var r SessionAffinityRecord
	var ttype, comp sql.NullString
	var exp, upd string
	err := row.Scan(&r.SessionID, &r.RouteID, &r.Provider, &r.Model, &ttype, &comp, &exp, &upd)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.TaskType = ttype.String
	r.Complexity = comp.String
	r.ExpiresAt, _ = time.Parse(time.RFC3339Nano, exp)
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, upd)
	if time.Now().After(r.ExpiresAt) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM smart_session_affinity WHERE session_id = ?`, sessionID)
		return nil, nil
	}
	return &r, nil
}

func (s *Store) UpsertSessionAffinity(ctx context.Context, r SessionAffinityRecord) error {
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO smart_session_affinity(session_id, route_id, provider, model, task_type, complexity, expires_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(session_id) DO UPDATE SET
			route_id=excluded.route_id, provider=excluded.provider, model=excluded.model,
			task_type=excluded.task_type, complexity=excluded.complexity,
			expires_at=excluded.expires_at, updated_at=excluded.updated_at`,
		r.SessionID, r.RouteID, r.Provider, r.Model, nullStr(r.TaskType), nullStr(r.Complexity),
		r.ExpiresAt.UTC().Format(time.RFC3339Nano), r.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) DeleteSessionAffinity(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM smart_session_affinity WHERE session_id = ?`, sessionID)
	return err
}

func (s *Store) PurgeExpiredAffinity(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM smart_session_affinity WHERE expires_at < ?`,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// --- Config History ---

type ConfigHistoryRecord struct {
	Revision          int64     `json:"revision"`
	Timestamp         time.Time `json:"timestamp"`
	SessionID         string    `json:"session_id"`
	ChangeType        string    `json:"change_type"`
	AffectedResources string    `json:"affected_resources"`
	ConfigYAML        string    `json:"config_yaml,omitempty"`
	SanitizedYAML     string    `json:"sanitized_yaml,omitempty"`
}

func (s *Store) InsertConfigHistory(ctx context.Context, sessionID, changeType, affectedResources, configYAML, sanitizedYAML string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO config_history(timestamp, session_id, change_type, affected_resources, config_yaml, sanitized_yaml)
		VALUES(?,?,?,?,?,?)`,
		now, nullStr(sessionID), changeType, affectedResources, configYAML, sanitizedYAML,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListConfigHistory(ctx context.Context) ([]ConfigHistoryRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT revision, timestamp, session_id, change_type, affected_resources, sanitized_yaml
		FROM config_history ORDER BY revision DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConfigHistoryRecord
	for rows.Next() {
		var r ConfigHistoryRecord
		var ts string
		var sid, aff sql.NullString
		if err := rows.Scan(&r.Revision, &ts, &sid, &r.ChangeType, &aff, &r.SanitizedYAML); err != nil {
			return nil, err
		}
		r.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		r.SessionID = sid.String
		r.AffectedResources = aff.String
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetConfigHistory(ctx context.Context, revision int64) (*ConfigHistoryRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT revision, timestamp, session_id, change_type, affected_resources, config_yaml, sanitized_yaml
		FROM config_history WHERE revision = ?`, revision)
	var r ConfigHistoryRecord
	var ts string
	var sid, aff sql.NullString
	err := row.Scan(&r.Revision, &ts, &sid, &r.ChangeType, &aff, &r.ConfigYAML, &r.SanitizedYAML)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
	r.SessionID = sid.String
	r.AffectedResources = aff.String
	return &r, nil
}

func (s *Store) GetLatestConfigRevision(ctx context.Context) (int64, error) {
	var rev int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision), 0) FROM config_history`).Scan(&rev)
	return rev, err
}
