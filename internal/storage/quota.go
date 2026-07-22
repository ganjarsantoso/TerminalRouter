package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// --- Quota domain persistence ---

// QuotaSnapshotRecord is an immutable provider/local quota observation.
type QuotaSnapshotRecord struct {
	ID             string
	ProviderID     string
	AccountID      string
	ModelID        string
	DefinitionID   string
	Dimension      string
	WindowStart    *time.Time
	WindowEnd      *time.Time
	ResetAt        *time.Time
	LimitValue     *float64
	UsedValue      float64
	RemainingValue *float64
	ReservedValue  float64
	Source         string
	Confidence     float64
	ObservedAt     time.Time
	StaleAfter     *time.Time
	MetadataJSON   string
}

// InsertQuotaSnapshot stores an immutable snapshot.
func (s *Store) InsertQuotaSnapshot(ctx context.Context, r QuotaSnapshotRecord) error {
	if r.ObservedAt.IsZero() {
		r.ObservedAt = time.Now().UTC()
	}
	if r.ID == "" {
		r.ID = fmt.Sprintf("%s-%s-%s-%d", r.ProviderID, r.AccountID, r.Dimension, r.ObservedAt.UnixNano())
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO quota_snapshots(
			id, provider_id, account_id, model_id, definition_id, dimension,
			window_start, window_end, reset_at, limit_value, used_value, remaining_value,
			reserved_value, source, confidence, observed_at, stale_after, metadata_json)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.ProviderID, r.AccountID, nullStr(r.ModelID), nullStr(r.DefinitionID), r.Dimension,
		nullTime(r.WindowStart), nullTime(r.WindowEnd), nullTime(r.ResetAt),
		nullFloat(r.LimitValue), r.UsedValue, nullFloat(r.RemainingValue),
		r.ReservedValue, r.Source, r.Confidence,
		r.ObservedAt.UTC().Format(time.RFC3339Nano), nullTime(r.StaleAfter), nullStr(r.MetadataJSON),
	)
	return err
}

// LatestQuotaSnapshots returns the newest snapshot per provider/account/dimension.
func (s *Store) LatestQuotaSnapshots(ctx context.Context) ([]QuotaSnapshotRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, provider_id, account_id, model_id, definition_id, dimension,
			window_start, window_end, reset_at, limit_value, used_value, remaining_value,
			reserved_value, source, confidence, observed_at, stale_after, metadata_json
		FROM quota_snapshots s
		WHERE observed_at = (
			SELECT MAX(observed_at) FROM quota_snapshots s2
			WHERE s2.provider_id = s.provider_id
			  AND s2.account_id = s.account_id
			  AND s2.dimension = s.dimension
			  AND IFNULL(s2.definition_id,'') = IFNULL(s.definition_id,'')
		)
		ORDER BY provider_id, account_id, dimension`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanQuotaSnapshots(rows)
}

// LatestQuotaSnapshotsForAccount returns newest snapshots for one account.
func (s *Store) LatestQuotaSnapshotsForAccount(ctx context.Context, providerID, accountID string) ([]QuotaSnapshotRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, provider_id, account_id, model_id, definition_id, dimension,
			window_start, window_end, reset_at, limit_value, used_value, remaining_value,
			reserved_value, source, confidence, observed_at, stale_after, metadata_json
		FROM quota_snapshots s
		WHERE provider_id = ? AND account_id = ?
		  AND observed_at = (
			SELECT MAX(observed_at) FROM quota_snapshots s2
			WHERE s2.provider_id = s.provider_id
			  AND s2.account_id = s.account_id
			  AND s2.dimension = s.dimension
			  AND IFNULL(s2.definition_id,'') = IFNULL(s.definition_id,'')
		)
		ORDER BY dimension`, providerID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanQuotaSnapshots(rows)
}

func scanQuotaSnapshots(rows *sql.Rows) ([]QuotaSnapshotRecord, error) {
	var out []QuotaSnapshotRecord
	for rows.Next() {
		var r QuotaSnapshotRecord
		var model, def, meta, ws, we, ra, obs, sa sql.NullString
		var lim, rem sql.NullFloat64
		if err := rows.Scan(&r.ID, &r.ProviderID, &r.AccountID, &model, &def, &r.Dimension,
			&ws, &we, &ra, &lim, &r.UsedValue, &rem, &r.ReservedValue, &r.Source, &r.Confidence,
			&obs, &sa, &meta); err != nil {
			return nil, err
		}
		r.ModelID, r.DefinitionID, r.MetadataJSON = model.String, def.String, meta.String
		r.WindowStart = parseNullTime(ws)
		r.WindowEnd = parseNullTime(we)
		r.ResetAt = parseNullTime(ra)
		r.StaleAfter = parseNullTime(sa)
		if lim.Valid {
			r.LimitValue = &lim.Float64
		}
		if rem.Valid {
			r.RemainingValue = &rem.Float64
		}
		if obs.Valid {
			r.ObservedAt, _ = time.Parse(time.RFC3339Nano, obs.String)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func parseNullTime(ns sql.NullString) *time.Time {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, ns.String)
	if err != nil {
		t, err = time.Parse(time.RFC3339, ns.String)
		if err != nil {
			return nil
		}
	}
	return &t
}

// QuotaEventRecord is a sanitized operational event.
type QuotaEventRecord struct {
	ID           string
	EventType    string
	ProviderID   string
	AccountID    string
	ModelID      string
	Dimension    string
	WindowID     string
	Source       string
	Status       string
	Message      string
	RequestID    string
	CreatedAt    time.Time
	MetadataJSON string
}

// InsertQuotaEvent stores a quota event.
func (s *Store) InsertQuotaEvent(ctx context.Context, e QuotaEventRecord) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	if e.ID == "" {
		e.ID = fmt.Sprintf("qe-%d", e.CreatedAt.UnixNano())
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO quota_events(
			id, event_type, provider_id, account_id, model_id, dimension, window_id,
			source, status, message, request_id, created_at, metadata_json)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.EventType, nullStr(e.ProviderID), nullStr(e.AccountID), nullStr(e.ModelID),
		nullStr(e.Dimension), nullStr(e.WindowID), nullStr(e.Source), nullStr(e.Status),
		nullStr(e.Message), nullStr(e.RequestID), e.CreatedAt.UTC().Format(time.RFC3339Nano),
		nullStr(e.MetadataJSON),
	)
	return err
}

// ListQuotaEvents returns recent events newest-first.
func (s *Store) ListQuotaEvents(ctx context.Context, limit int) ([]QuotaEventRecord, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, event_type, provider_id, account_id, model_id, dimension, window_id,
			source, status, message, request_id, created_at, metadata_json
		FROM quota_events ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QuotaEventRecord
	for rows.Next() {
		var e QuotaEventRecord
		var prov, acc, model, dim, win, src, st, msg, req, created, meta sql.NullString
		if err := rows.Scan(&e.ID, &e.EventType, &prov, &acc, &model, &dim, &win,
			&src, &st, &msg, &req, &created, &meta); err != nil {
			return nil, err
		}
		e.ProviderID, e.AccountID, e.ModelID = prov.String, acc.String, model.String
		e.Dimension, e.WindowID, e.Source = dim.String, win.String, src.String
		e.Status, e.Message, e.RequestID = st.String, msg.String, req.String
		e.MetadataJSON = meta.String
		if created.Valid {
			e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created.String)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AccountRoutingStateRecord persists multi-account selection state.
type AccountRoutingStateRecord struct {
	ProviderID    string
	RouteID       string
	LastAccountID string
	Sequence      int64
	UpdatedAt     time.Time
}

// UpsertAccountRoutingState saves selection progress.
func (s *Store) UpsertAccountRoutingState(ctx context.Context, r AccountRoutingStateRecord) error {
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO account_routing_state(provider_id, route_id, last_account_id, sequence, updated_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(provider_id, route_id) DO UPDATE SET
			last_account_id=excluded.last_account_id,
			sequence=excluded.sequence,
			updated_at=excluded.updated_at`,
		r.ProviderID, r.RouteID, r.LastAccountID, r.Sequence,
		r.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

// GetAccountRoutingState loads selection progress.
func (s *Store) GetAccountRoutingState(ctx context.Context, providerID, routeID string) (*AccountRoutingStateRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT provider_id, route_id, last_account_id, sequence, updated_at
		FROM account_routing_state WHERE provider_id=? AND route_id=?`, providerID, routeID)
	var r AccountRoutingStateRecord
	var updated string
	err := row.Scan(&r.ProviderID, &r.RouteID, &r.LastAccountID, &r.Sequence, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &r, nil
}

// --- Analytics aggregation from request_log ---

// UsageFilter bounds analytics queries.
type UsageFilter struct {
	From        time.Time
	To          time.Time
	ProviderID  string
	AccountID   string
	ModelID     string
	RouteAlias  string
	ClientKeyID string
	// MaxRange caps the allowed span; default 90 days.
	MaxRange time.Duration
}

// Validate enforces max range and swaps inverted bounds.
func (f *UsageFilter) Validate() error {
	if f.MaxRange <= 0 {
		f.MaxRange = 90 * 24 * time.Hour
	}
	if f.From.IsZero() {
		f.From = time.Now().UTC().Add(-7 * 24 * time.Hour)
	}
	if f.To.IsZero() {
		f.To = time.Now().UTC()
	}
	if f.To.Before(f.From) {
		f.From, f.To = f.To, f.From
	}
	if f.To.Sub(f.From) > f.MaxRange {
		return fmt.Errorf("date range exceeds maximum of %s", f.MaxRange)
	}
	return nil
}

// AggregatedUsage is a single group bucket.
type AggregatedUsage struct {
	Key          string
	ProviderID   string
	AccountID    string
	ModelID      string
	RouteAlias   string
	ClientKeyID  string
	Requests     int
	InputTokens  int64
	OutputTokens int64
	EstimatedUSD float64
	HasCost      bool
}

// AggregateUsage groups request_log rows by groupBy field.
// groupBy: provider | account | model | route | client_key
func (s *Store) AggregateUsage(ctx context.Context, f UsageFilter, groupBy string) ([]AggregatedUsage, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	col := "provider_id"
	switch groupBy {
	case "provider", "":
		col = "provider_id"
	case "account":
		col = "account_id"
	case "model":
		col = "upstream_model"
	case "route", "alias":
		col = "resolved_alias"
	case "client_key":
		col = "client_key_id"
	default:
		return nil, fmt.Errorf("unsupported group_by %q", groupBy)
	}

	q := fmt.Sprintf(`
		SELECT IFNULL(%s,''), IFNULL(provider_id,''), IFNULL(account_id,''), IFNULL(upstream_model,''),
			IFNULL(resolved_alias,''), IFNULL(client_key_id,''),
			COUNT(*), IFNULL(SUM(input_tokens),0), IFNULL(SUM(output_tokens),0)
		FROM request_log
		WHERE timestamp >= ? AND timestamp <= ?`, col)
	args := []any{f.From.UTC().Format(time.RFC3339Nano), f.To.UTC().Format(time.RFC3339Nano)}
	if f.ProviderID != "" {
		q += ` AND provider_id = ?`
		args = append(args, f.ProviderID)
	}
	if f.AccountID != "" {
		q += ` AND account_id = ?`
		args = append(args, f.AccountID)
	}
	if f.ModelID != "" {
		q += ` AND upstream_model = ?`
		args = append(args, f.ModelID)
	}
	if f.RouteAlias != "" {
		q += ` AND resolved_alias = ?`
		args = append(args, f.RouteAlias)
	}
	if f.ClientKeyID != "" {
		q += ` AND client_key_id = ?`
		args = append(args, f.ClientKeyID)
	}
	q += fmt.Sprintf(` GROUP BY %s ORDER BY COUNT(*) DESC LIMIT 500`, col)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AggregatedUsage
	for rows.Next() {
		var u AggregatedUsage
		if err := rows.Scan(&u.Key, &u.ProviderID, &u.AccountID, &u.ModelID, &u.RouteAlias, &u.ClientKeyID,
			&u.Requests, &u.InputTokens, &u.OutputTokens); err != nil {
			return nil, err
		}
		if u.Key == "" {
			u.Key = "unknown"
		}
		// Price when possible.
		if s.PriceFunc != nil && u.ProviderID != "" && u.ModelID != "" {
			if cost, ok := s.PriceFunc(u.ProviderID, u.ModelID, int(u.InputTokens), int(u.OutputTokens)); ok {
				u.EstimatedUSD = cost
				u.HasCost = true
			}
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UsageTrendBucket is one time bucket for trend charts.
type UsageTrendBucket struct {
	BucketStart  time.Time
	Requests     int
	InputTokens  int64
	OutputTokens int64
	EstimatedUSD float64
	HasCost      bool
}

// UsageTrends returns request_log aggregates bucketed by interval.
// interval: hour | day
func (s *Store) UsageTrends(ctx context.Context, f UsageFilter, interval string) ([]UsageTrendBucket, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	// Bound row explosion: max 90 days hourly = 2160, daily = 90.
	if interval == "" {
		interval = "day"
	}
	if interval != "hour" && interval != "day" {
		return nil, fmt.Errorf("interval must be hour or day")
	}
	if interval == "hour" && f.To.Sub(f.From) > 31*24*time.Hour {
		return nil, fmt.Errorf("hourly interval limited to 31 days")
	}

	// Pull matching rows and bucket in Go (SQLite strftime works but pricing needs per-model).
	q := `
		SELECT timestamp, IFNULL(provider_id,''), IFNULL(upstream_model,''),
			IFNULL(input_tokens,0), IFNULL(output_tokens,0)
		FROM request_log
		WHERE timestamp >= ? AND timestamp <= ?`
	args := []any{f.From.UTC().Format(time.RFC3339Nano), f.To.UTC().Format(time.RFC3339Nano)}
	if f.ProviderID != "" {
		q += ` AND provider_id = ?`
		args = append(args, f.ProviderID)
	}
	if f.AccountID != "" {
		q += ` AND account_id = ?`
		args = append(args, f.AccountID)
	}
	if f.ModelID != "" {
		q += ` AND upstream_model = ?`
		args = append(args, f.ModelID)
	}
	if f.ClientKeyID != "" {
		q += ` AND client_key_id = ?`
		args = append(args, f.ClientKeyID)
	}
	q += ` ORDER BY timestamp ASC LIMIT 100000`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	buckets := map[string]*UsageTrendBucket{}
	var order []string
	for rows.Next() {
		var ts, prov, model string
		var inTok, outTok int64
		if err := rows.Scan(&ts, &prov, &model, &inTok, &outTok); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			t, _ = time.Parse(time.RFC3339, ts)
		}
		var key string
		var start time.Time
		if interval == "hour" {
			start = t.UTC().Truncate(time.Hour)
			key = start.Format(time.RFC3339)
		} else {
			start = time.Date(t.UTC().Year(), t.UTC().Month(), t.UTC().Day(), 0, 0, 0, 0, time.UTC)
			key = start.Format("2006-01-02")
		}
		b := buckets[key]
		if b == nil {
			b = &UsageTrendBucket{BucketStart: start}
			buckets[key] = b
			order = append(order, key)
		}
		b.Requests++
		b.InputTokens += inTok
		b.OutputTokens += outTok
		if s.PriceFunc != nil && prov != "" && model != "" {
			if cost, ok := s.PriceFunc(prov, model, int(inTok), int(outTok)); ok {
				b.EstimatedUSD += cost
				b.HasCost = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]UsageTrendBucket, 0, len(order))
	for _, k := range order {
		out = append(out, *buckets[k])
	}
	return out, nil
}

// LocalUsageWindow aggregates usage for a provider/account in a time window.
type LocalUsageWindow struct {
	Requests     int
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
	EstimatedUSD float64
	HasCost      bool
}

// LocalUsage aggregates request_log for provider/account between from and to.
func (s *Store) LocalUsage(ctx context.Context, providerID, accountID string, from, to time.Time) (*LocalUsageWindow, error) {
	q := `
		SELECT COUNT(*), IFNULL(SUM(input_tokens),0), IFNULL(SUM(output_tokens),0),
			IFNULL(provider_id,''), IFNULL(upstream_model,''), IFNULL(input_tokens,0), IFNULL(output_tokens,0)
		FROM request_log
		WHERE timestamp >= ? AND timestamp < ?`
	// We need per-row pricing, so select rows.
	q = `
		SELECT IFNULL(provider_id,''), IFNULL(upstream_model,''), IFNULL(input_tokens,0), IFNULL(output_tokens,0)
		FROM request_log
		WHERE timestamp >= ? AND timestamp < ?`
	args := []any{from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano)}
	if providerID != "" {
		q += ` AND provider_id = ?`
		args = append(args, providerID)
	}
	if accountID != "" {
		// Treat empty account_id in log as "default"
		if accountID == "default" {
			q += ` AND (account_id = ? OR account_id IS NULL OR account_id = '')`
			args = append(args, accountID)
		} else {
			q += ` AND account_id = ?`
			args = append(args, accountID)
		}
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &LocalUsageWindow{}
	for rows.Next() {
		var prov, model string
		var inTok, outTok int64
		if err := rows.Scan(&prov, &model, &inTok, &outTok); err != nil {
			return nil, err
		}
		out.Requests++
		out.InputTokens += inTok
		out.OutputTokens += outTok
		out.TotalTokens += inTok + outTok
		if s.PriceFunc != nil && prov != "" && model != "" {
			if cost, ok := s.PriceFunc(prov, model, int(inTok), int(outTok)); ok {
				out.EstimatedUSD += cost
				out.HasCost = true
			}
		}
	}
	return out, rows.Err()
}

// UsageSinceDetailed extends UsageSince with cost and account breakdown.
type UsageSinceDetailed struct {
	TotalRequests int
	SuccessCount  int
	ErrorCount    int
	InputTokens   int64
	OutputTokens  int64
	EstimatedUSD  float64
	HasCost       bool
	ByProvider    map[string]int
	ByAccount     map[string]int
	ByModel       map[string]int
	AvgLatencyMs  float64
}

// UsageSinceDetailed aggregates richer usage since a timestamp.
func (s *Store) UsageSinceDetailed(ctx context.Context, since time.Time) (*UsageSinceDetailed, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT IFNULL(provider_id,''), IFNULL(account_id,''), IFNULL(upstream_model,''),
			status_code, latency_ms, IFNULL(input_tokens,0), IFNULL(output_tokens,0)
		FROM request_log WHERE timestamp >= ?`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sum := &UsageSinceDetailed{
		ByProvider: map[string]int{},
		ByAccount:  map[string]int{},
		ByModel:    map[string]int{},
	}
	var latSum int64
	for rows.Next() {
		var prov, acc, model string
		var status, latency, inTok, outTok sql.NullInt64
		if err := rows.Scan(&prov, &acc, &model, &status, &latency, &inTok, &outTok); err != nil {
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
		in, outN := inTok.Int64, outTok.Int64
		sum.InputTokens += in
		sum.OutputTokens += outN
		if prov == "" {
			prov = "unknown"
		}
		if acc == "" {
			acc = "default"
		}
		if model == "" {
			model = "unknown"
		}
		sum.ByProvider[prov]++
		sum.ByAccount[prov+"/"+acc]++
		sum.ByModel[prov+"/"+model]++
		if s.PriceFunc != nil && prov != "unknown" && model != "unknown" {
			if cost, ok := s.PriceFunc(prov, model, int(in), int(outN)); ok {
				sum.EstimatedUSD += cost
				sum.HasCost = true
			}
		}
	}
	if sum.TotalRequests > 0 {
		sum.AvgLatencyMs = float64(latSum) / float64(sum.TotalRequests)
	}
	return sum, rows.Err()
}

// UpsertUsageRollupHourly upserts an hourly rollup row (idempotent recompute).
type UsageRollupHourly struct {
	BucketStart      time.Time
	ProviderID       string
	AccountID        string
	ModelID          string
	RouteAlias       string
	ClientKeyID      string
	Requests         int
	InputTokens      int64
	OutputTokens     int64
	CachedTokens     int64
	EstimatedCostUSD float64
	BilledCostUSD    float64
	Errors           int
	Throttles        int
	OptSavingsUSD    float64
}

// UpsertUsageRollupHourly writes or replaces a rollup bucket.
func (s *Store) UpsertUsageRollupHourly(ctx context.Context, r UsageRollupHourly) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO usage_rollups_hourly(
			bucket_start, provider_id, account_id, model_id, route_alias, client_key_id,
			requests, input_tokens, output_tokens, cached_tokens, estimated_cost_usd,
			billed_cost_usd, errors, throttles, opt_savings_usd)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(bucket_start, provider_id, account_id, model_id, route_alias, client_key_id) DO UPDATE SET
			requests=excluded.requests,
			input_tokens=excluded.input_tokens,
			output_tokens=excluded.output_tokens,
			cached_tokens=excluded.cached_tokens,
			estimated_cost_usd=excluded.estimated_cost_usd,
			billed_cost_usd=excluded.billed_cost_usd,
			errors=excluded.errors,
			throttles=excluded.throttles,
			opt_savings_usd=excluded.opt_savings_usd`,
		r.BucketStart.UTC().Format(time.RFC3339),
		nullStr(r.ProviderID), nullStr(r.AccountID), nullStr(r.ModelID),
		nullStr(r.RouteAlias), nullStr(r.ClientKeyID),
		r.Requests, r.InputTokens, r.OutputTokens, r.CachedTokens,
		r.EstimatedCostUSD, r.BilledCostUSD, r.Errors, r.Throttles, r.OptSavingsUSD,
	)
	return err
}

// SetAccountDraining marks an account drain flag in a JSON-ish side table.
// We store lightweight account operational state separately from config YAML.
type AccountOpState struct {
	ProviderID string
	AccountID  string
	Draining   bool
	Enabled    bool
	UpdatedAt  time.Time
	Metadata   map[string]any
}

// UpsertAccountOpState persists drain/enable operational overrides.
func (s *Store) UpsertAccountOpState(ctx context.Context, st AccountOpState) error {
	if st.UpdatedAt.IsZero() {
		st.UpdatedAt = time.Now().UTC()
	}
	meta, _ := json.Marshal(st.Metadata)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_account_state(provider_id, account_id, draining, enabled, updated_at, metadata_json)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(provider_id, account_id) DO UPDATE SET
			draining=excluded.draining,
			enabled=excluded.enabled,
			updated_at=excluded.updated_at,
			metadata_json=excluded.metadata_json`,
		st.ProviderID, st.AccountID, boolToInt(st.Draining), boolToInt(st.Enabled),
		st.UpdatedAt.UTC().Format(time.RFC3339Nano), string(meta),
	)
	return err
}

// GetAccountOpState loads operational account state.
func (s *Store) GetAccountOpState(ctx context.Context, providerID, accountID string) (*AccountOpState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT provider_id, account_id, draining, enabled, updated_at, metadata_json
		FROM provider_account_state WHERE provider_id=? AND account_id=?`, providerID, accountID)
	var st AccountOpState
	var drain, en int
	var updated, meta sql.NullString
	err := row.Scan(&st.ProviderID, &st.AccountID, &drain, &en, &updated, &meta)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	st.Draining = drain == 1
	st.Enabled = en == 1
	if updated.Valid {
		st.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated.String)
	}
	if meta.Valid && meta.String != "" {
		_ = json.Unmarshal([]byte(meta.String), &st.Metadata)
	}
	return &st, nil
}

// ListAccountOpStates returns all operational account states.
func (s *Store) ListAccountOpStates(ctx context.Context) ([]AccountOpState, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider_id, account_id, draining, enabled, updated_at, metadata_json
		FROM provider_account_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountOpState
	for rows.Next() {
		var st AccountOpState
		var drain, en int
		var updated, meta sql.NullString
		if err := rows.Scan(&st.ProviderID, &st.AccountID, &drain, &en, &updated, &meta); err != nil {
			return nil, err
		}
		st.Draining = drain == 1
		st.Enabled = en == 1
		if updated.Valid {
			st.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated.String)
		}
		if meta.Valid && meta.String != "" {
			_ = json.Unmarshal([]byte(meta.String), &st.Metadata)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// SanitizeExportKeyLabel returns a non-secret label for exports.
func SanitizeExportKeyLabel(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if len(id) <= 8 {
		return id
	}
	return id[:4] + "…" + id[len(id)-4:]
}
