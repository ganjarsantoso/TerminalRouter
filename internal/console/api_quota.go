package console

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/termrouter/termrouter/internal/quota"
	"github.com/termrouter/termrouter/internal/storage"
)

// GET /admin/v1/quota/summary
func (s *Server) handleQuotaSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now().UTC()

	snapshots, err := s.Store.LatestQuotaSnapshots(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "quota_error", err.Error())
		return
	}

	// Build windows from latest snapshots.
	windows := snapshotsToWindows(snapshots, now)

	// Aggregate totals from local usage.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	usage, err := s.Store.LocalUsage(ctx, "", "", today, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "usage_error", err.Error())
		return
	}

	// Count active accounts from stored operational state.
	accounts, _ := s.Store.ListAccountOpStates(ctx)
	activeAccounts := 0
	for _, a := range accounts {
		if a.Enabled && !a.Draining {
			activeAccounts++
		}
	}

	var winWarn, winCrit int
	var nextReset *time.Time
	for i := range windows {
		switch windows[i].Status {
		case quota.StatusWarning:
			winWarn++
		case quota.StatusCritical, quota.StatusExhausted:
			winCrit++
		}
		if windows[i].ResetAt != nil && (nextReset == nil || windows[i].ResetAt.Before(*nextReset)) {
			nextReset = windows[i].ResetAt
		}
	}

	// Estimate monthly cost.
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthUsage, err := s.Store.LocalUsage(ctx, "", "", monthStart, now)
	if err != nil {
		monthUsage = &storage.LocalUsageWindow{}
	}

	// Check freshness.
	fresh := quota.Freshness{Status: "fresh"}
	if len(snapshots) > 0 {
		oldest := now
		for _, snap := range snapshots {
			if snap.ObservedAt.Before(oldest) {
				oldest = snap.ObservedAt
			}
		}
		fresh.LastLocalUpdate = &oldest
		if now.Sub(oldest) > 15*time.Minute {
			fresh.Status = "stale"
		}
	}

	// Build alerts.
	var alerts []quota.Alert
	for _, w := range windows {
		if w.Status == quota.StatusCritical || w.Status == quota.StatusExhausted {
			alerts = append(alerts, quota.Alert{
				ID:         string(w.Dimension) + "-" + w.AccountID,
				Severity:   "critical",
				Code:       "quota_" + string(w.Status),
				Message:    string(w.Dimension) + " quota " + string(w.Status) + " for " + w.AccountID,
				ProviderID: w.ProviderID,
				AccountID:  w.AccountID,
				CreatedAt:  now,
			})
		}
	}

	summary := quota.Summary{
		GeneratedAt:        now,
		ActiveAccounts:     activeAccounts,
		WindowsWarning:     winWarn,
		WindowsCritical:    winCrit,
		TokensToday:        usage.InputTokens + usage.OutputTokens,
		RequestsToday:      usage.Requests,
		InputTokensToday:   usage.InputTokens,
		OutputTokensToday:  usage.OutputTokens,
		EstimatedCostMonth: nil,
		NextResetAt:        nextReset,
		Windows:            windows,
		Alerts:             alerts,
		Freshness:          fresh,
		TotalsSource:       quota.SourceLocalAuthoritative,
	}
	if usage.HasCost {
		summary.EstimatedCostMonth = &usage.EstimatedUSD
	}
	if monthUsage.HasCost {
		billed := monthUsage.EstimatedUSD
		summary.BilledCostMonth = &billed
	}

	writeJSON(w, http.StatusOK, summary)
}

// GET /admin/v1/quota/windows
func (s *Server) handleQuotaWindows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now().UTC()

	snapshots, err := s.Store.LatestQuotaSnapshots(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "quota_error", err.Error())
		return
	}

	windows := snapshotsToWindows(snapshots, now)

	// Optional filter by provider/account.
	if pid := r.URL.Query().Get("provider_id"); pid != "" {
		var filtered []quota.QuotaWindowState
		for _, w := range windows {
			if w.ProviderID == pid {
				filtered = append(filtered, w)
			}
		}
		windows = filtered
	}
	if aid := r.URL.Query().Get("account_id"); aid != "" {
		var filtered []quota.QuotaWindowState
		for _, w := range windows {
			if w.AccountID == aid {
				filtered = append(filtered, w)
			}
		}
		windows = filtered
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"windows": windows,
		"count":   len(windows),
	})
}

// GET /admin/v1/quota/events
func (s *Server) handleQuotaEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := parseIntSafe(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	events, err := s.Store.ListQuotaEvents(ctx, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "quota_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"count":  len(events),
	})
}

// POST /admin/v1/quota/refresh
func (s *Server) handleQuotaRefresh(w http.ResponseWriter, r *http.Request) {
	// Trigger a local re-aggregation from request_log.
	// Provider-specific refresh is async; this just recomputes local windows.
	ctx := r.Context()
	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)

	usage, err := s.Store.LocalUsage(ctx, "", "", today, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "refresh_error", err.Error())
		return
	}

	// Store a snapshot of today's local usage.
	snap := quota.QuotaWindowState{
		DefinitionID:  "local:daily_usage",
		Dimension:     quota.DimTotalTokens,
		Unit:          quota.UnitTokens,
		WindowType:    quota.WindowRolling,
		WindowStart:   today,
		Used:          float64(usage.TotalTokens),
		Source:        quota.SourceLocalAuthoritative,
		Confidence:    1.0,
		LastUpdatedAt: now,
		StaleAfter:    now.Add(5 * time.Minute),
	}

	err = s.Store.InsertQuotaSnapshot(ctx, storage.QuotaSnapshotRecord{
		ProviderID: "local",
		AccountID:  "default",
		Dimension:  string(snap.Dimension),
		UsedValue:  snap.Used,
		Source:     string(snap.Source),
		Confidence: snap.Confidence,
		ObservedAt: now,
		StaleAfter: &snap.StaleAfter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "snapshot_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "refreshed",
		"timestamp": now.Format(time.RFC3339),
		"usage": map[string]any{
			"requests":      usage.Requests,
			"input_tokens":  usage.InputTokens,
			"output_tokens": usage.OutputTokens,
			"total_tokens":  usage.TotalTokens,
		},
	})
}

// GET /admin/v1/quota/recommendations
func (s *Server) handleQuotaRecommendations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now().UTC()

	snapshots, err := s.Store.LatestQuotaSnapshots(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "quota_error", err.Error())
		return
	}

	windows := snapshotsToWindows(snapshots, now)
	var recs []quota.Recommendation

	for _, w := range windows {
		if w.Utilization == nil {
			continue
		}
		util := *w.Utilization

		if util >= 0.9 && w.ForecastStatus == quota.ForecastLikelyExhaust {
			recs = append(recs, quota.Recommendation{
				Code:       "approaching_exhaustion",
				Severity:   "warning",
				Message:    string(w.Dimension) + " quota at " + fmtPct(util) + " and forecast to exhaust before reset",
				ProviderID: w.ProviderID,
				AccountID:  w.AccountID,
			})
		}

		if w.PaceDelta != nil && *w.PaceDelta < -0.2 {
			recs = append(recs, quota.Recommendation{
				Code:       "underutilized",
				Severity:   "info",
				Message:    string(w.Dimension) + " quota is underutilized relative to time elapsed in window",
				ProviderID: w.ProviderID,
				AccountID:  w.AccountID,
			})
		}

		if w.Status == quota.StatusStale {
			recs = append(recs, quota.Recommendation{
				Code:       "stale_data",
				Severity:   "warning",
				Message:    string(w.Dimension) + " quota data is stale; refresh recommended",
				ProviderID: w.ProviderID,
				AccountID:  w.AccountID,
			})
		}
	}

	if len(recs) == 0 {
		recs = []quota.Recommendation{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"recommendations": recs,
		"count":           len(recs),
	})
}

// snapshotsToWindows converts stored snapshots into live window states.
func snapshotsToWindows(snaps []storage.QuotaSnapshotRecord, now time.Time) []quota.QuotaWindowState {
	byKey := map[string]*quota.QuotaWindowState{}
	var order []string

	for _, snap := range snaps {
		key := snap.ProviderID + "|" + snap.AccountID + "|" + snap.Dimension
		w, ok := byKey[key]
		if !ok {
			w = &quota.QuotaWindowState{
				ProviderID: snap.ProviderID,
				AccountID:  snap.AccountID,
				ModelID:    snap.ModelID,
				Dimension:  quota.QuotaDimension(snap.Dimension),
				Unit:       unitForDim(quota.QuotaDimension(snap.Dimension)),
				Source:     quota.MetricSource(snap.Source),
				Confidence: snap.Confidence,
			}
			byKey[key] = w
			order = append(order, key)
		}
		if snap.LimitValue != nil {
			w.Limit = snap.LimitValue
		}
		w.Used = snap.UsedValue
		if snap.RemainingValue != nil {
			w.Remaining = snap.RemainingValue
		}
		w.ResetAt = snap.ResetAt
		w.WindowStart = now // approximate
		w.LastUpdatedAt = snap.ObservedAt
		if snap.StaleAfter != nil {
			w.StaleAfter = *snap.StaleAfter
		}

		// Compute derived fields.
		w.Utilization = quota.Utilization(w.Limit, w.Used)
		w.Remaining = quota.Remaining(w.Limit, w.Used, w.Reserved)

		fc := quota.ForecastExhaustion(quota.ForecastInput{
			Limit:      w.Limit,
			Used:       w.Used,
			Reserved:   w.Reserved,
			BurnRate:   w.BurnRate,
			Now:        now,
			ResetAt:    w.ResetAt,
			Stale:      quota.IsStale(now, w.StaleAfter),
			Confidence: w.Confidence,
		})
		w.ForecastExhaustAt = fc.ForecastExhaustAt
		w.ForecastStatus = fc.Status
		w.Status = quota.DeriveStatus(w.Utilization, fc.Status, now, w.StaleAfter, w.Limit != nil)
	}

	windows := make([]quota.QuotaWindowState, 0, len(order))
	for _, k := range order {
		windows = append(windows, *byKey[k])
	}
	return windows
}

func unitForDim(d quota.QuotaDimension) quota.QuotaUnit {
	switch d {
	case quota.DimEstimatedCost, quota.DimBilledCost:
		return quota.UnitUSD
	case quota.DimRequests, quota.DimConcurrentRequests:
		return quota.UnitCount
	case quota.DimCredits:
		return quota.UnitProviderCredit
	case quota.DimProviderCompute:
		return quota.UnitComputeUnit
	default:
		return quota.UnitTokens
	}
}

func fmtPct(v float64) string {
	return fmt.Sprintf("%.0f%%", v*100)
}

func parseIntSafe(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, &strconv.NumError{Num: string(s), Err: strconv.ErrSyntax}
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
