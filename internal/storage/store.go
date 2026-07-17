package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store is the SQLite-backed state store.
type Store struct {
	db *sql.DB
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
		if _, err := s.db.Exec(`UPDATE schema_version SET version = ?`, currentSchemaVersion); err != nil {
			return err
		}
	}
	return nil
}

// DB exposes the underlying *sql.DB for advanced queries in tests.
func (s *Store) DB() *sql.DB { return s.db }

// --- Client keys ---

type ClientKey struct {
	ID             string
	Name           string
	KeyPrefix      string
	KeyHash        string
	Salt           string
	Enabled        bool
	AllowedAliases []string
	RateLimitRPM   *int
	CreatedAt      time.Time
	RotatedAt      *time.Time
	DisabledAt     *time.Time
}

func (s *Store) InsertClientKey(ctx context.Context, k ClientKey) error {
	aliases, _ := json.Marshal(k.AllowedAliases)
	var rpm any
	if k.RateLimitRPM != nil {
		rpm = *k.RateLimitRPM
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO client_keys(id, name, key_prefix, key_hash, salt, enabled, allowed_aliases, rate_limit_rpm, created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		k.ID, k.Name, k.KeyPrefix, k.KeyHash, k.Salt, boolToInt(k.Enabled),
		string(aliases), rpm, k.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) ListClientKeys(ctx context.Context) ([]ClientKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, key_prefix, key_hash, salt, enabled, allowed_aliases, rate_limit_rpm, created_at, rotated_at, disabled_at
		FROM client_keys ORDER BY created_at`)
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
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, key_prefix, key_hash, salt, enabled, allowed_aliases, rate_limit_rpm, created_at, rotated_at, disabled_at
		FROM client_keys WHERE id=?`, id)
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
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, key_prefix, key_hash, salt, enabled, allowed_aliases, rate_limit_rpm, created_at, rotated_at, disabled_at
		FROM client_keys WHERE enabled=1`)
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
	var enabled int
	var aliases sql.NullString
	var rpm sql.NullInt64
	var created string
	var rotated, disabled sql.NullString
	err := row.Scan(&k.ID, &k.Name, &k.KeyPrefix, &k.KeyHash, &k.Salt, &enabled, &aliases, &rpm, &created, &rotated, &disabled)
	if err != nil {
		return k, err
	}
	k.Enabled = enabled == 1
	if aliases.Valid && aliases.String != "" && aliases.String != "null" {
		_ = json.Unmarshal([]byte(aliases.String), &k.AllowedAliases)
	}
	if rpm.Valid {
		v := int(rpm.Int64)
		k.RateLimitRPM = &v
	}
	k.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
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

// --- Provider health ---

type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half_open"
)

type ProviderHealth struct {
	ProviderID           string
	CircuitState         CircuitState
	ConsecutiveFailures  int
	CooldownUntil        *time.Time
	LastError            string
	LastLatencyMs        int
	LastSuccessAt        *time.Time
	LastFailureAt        *time.Time
	UpdatedAt            time.Time
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
}

func (s *Store) InsertRequest(ctx context.Context, r RequestRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO request_log(id, timestamp, client_key_id, inbound_protocol, requested_model, resolved_alias,
			provider_id, upstream_model, attempt, fallback_reason, status_code, latency_ms, time_to_first_token_ms,
			input_tokens, output_tokens, usage_source, error_class, stream)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Timestamp.UTC().Format(time.RFC3339Nano), nullStr(r.ClientKeyID), r.InboundProtocol,
		r.RequestedModel, nullStr(r.ResolvedAlias), nullStr(r.ProviderID), nullStr(r.UpstreamModel),
		r.Attempt, nullStr(r.FallbackReason), r.StatusCode, r.LatencyMs, r.TimeToFirstTokenMs,
		r.InputTokens, r.OutputTokens, nullStr(r.UsageSource), nullStr(r.ErrorClass), boolToInt(r.Stream),
	)
	return err
}

type UsageSummary struct {
	TotalRequests  int
	SuccessCount   int
	ErrorCount     int
	InputTokens    int64
	OutputTokens   int64
	AvgLatencyMs   float64
	ByProvider     map[string]int
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
		provider_id, upstream_model, attempt, fallback_reason, status_code, latency_ms, time_to_first_token_ms,
		input_tokens, output_tokens, usage_source, error_class, stream
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
		var clientKey, alias, provider, model, fb, usage, errClass sql.NullString
		var stream int
		if err := rows.Scan(&r.ID, &ts, &clientKey, &r.InboundProtocol, &r.RequestedModel, &alias,
			&provider, &model, &r.Attempt, &fb, &r.StatusCode, &r.LatencyMs, &r.TimeToFirstTokenMs,
			&r.InputTokens, &r.OutputTokens, &usage, &errClass, &stream); err != nil {
			return nil, err
		}
		r.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		r.ClientKeyID = clientKey.String
		r.ResolvedAlias = alias.String
		r.ProviderID = provider.String
		r.UpstreamModel = model.String
		r.FallbackReason = fb.String
		r.UsageSource = usage.String
		r.ErrorClass = errClass.String
		r.Stream = stream == 1
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
	Total             int
	ByTaskType        map[string]int
	ByRecommendation  map[string]int
	UncertainCount    int
	MatchActualCount  int // when shadow rec equals actual provider/model (if known)
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
