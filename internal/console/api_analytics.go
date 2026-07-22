package console

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/termrouter/termrouter/internal/storage"
)

// GET /admin/v1/analytics/usage
func (s *Server) handleAnalyticsUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f := storage.UsageFilter{
		ProviderID:  r.URL.Query().Get("provider_id"),
		AccountID:   r.URL.Query().Get("account_id"),
		ModelID:     r.URL.Query().Get("model_id"),
		RouteAlias:  r.URL.Query().Get("route_id"),
		ClientKeyID: r.URL.Query().Get("client_key_id"),
	}
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.From = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.To = t
		}
	}
	if err := f.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	groupBy := r.URL.Query().Get("group_by")
	if groupBy == "" {
		groupBy = "provider"
	}

	usage, err := s.Store.AggregateUsage(ctx, f, groupBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analytics_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"group_by": groupBy,
		"from":     f.From.Format(time.RFC3339),
		"to":       f.To.Format(time.RFC3339),
		"data":     usage,
		"count":    len(usage),
	})
}

// GET /admin/v1/analytics/cost
func (s *Server) handleAnalyticsCost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f := storage.UsageFilter{
		ProviderID: r.URL.Query().Get("provider_id"),
		AccountID:  r.URL.Query().Get("account_id"),
		ModelID:    r.URL.Query().Get("model_id"),
	}
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.From = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.To = t
		}
	}
	if err := f.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	usage, err := s.Store.AggregateUsage(ctx, f, "model")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analytics_error", err.Error())
		return
	}

	var totalCost float64
	for _, u := range usage {
		totalCost += u.EstimatedUSD
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"from":       f.From.Format(time.RFC3339),
		"to":         f.To.Format(time.RFC3339),
		"total_cost": totalCost,
		"currency":   "USD",
		"by_model":   usage,
	})
}

// GET /admin/v1/analytics/models
func (s *Server) handleAnalyticsModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f := storage.UsageFilter{
		ProviderID: r.URL.Query().Get("provider_id"),
	}
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.From = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.To = t
		}
	}
	if err := f.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	usage, err := s.Store.AggregateUsage(ctx, f, "model")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analytics_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"from":   f.From.Format(time.RFC3339),
		"to":     f.To.Format(time.RFC3339),
		"models": usage,
		"count":  len(usage),
	})
}

// GET /admin/v1/analytics/providers
func (s *Server) handleAnalyticsProviders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f := storage.UsageFilter{}
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.From = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.To = t
		}
	}
	if err := f.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	usage, err := s.Store.AggregateUsage(ctx, f, "provider")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analytics_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"from":      f.From.Format(time.RFC3339),
		"to":        f.To.Format(time.RFC3339),
		"providers": usage,
		"count":     len(usage),
	})
}

// GET /admin/v1/analytics/trends
func (s *Server) handleAnalyticsTrends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f := storage.UsageFilter{
		ProviderID: r.URL.Query().Get("provider_id"),
		AccountID:  r.URL.Query().Get("account_id"),
		ModelID:    r.URL.Query().Get("model_id"),
	}
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.From = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.To = t
		}
	}
	interval := r.URL.Query().Get("interval")
	if interval == "" {
		interval = "day"
	}
	if err := f.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	trends, err := s.Store.UsageTrends(ctx, f, interval)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analytics_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"from":     f.From.Format(time.RFC3339),
		"to":       f.To.Format(time.RFC3339),
		"interval": interval,
		"trends":   trends,
		"count":    len(trends),
	})
}

// GET /admin/v1/analytics/export
func (s *Server) handleAnalyticsExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f := storage.UsageFilter{
		ProviderID:  r.URL.Query().Get("provider_id"),
		AccountID:   r.URL.Query().Get("account_id"),
		ModelID:     r.URL.Query().Get("model_id"),
		RouteAlias:  r.URL.Query().Get("route_id"),
		ClientKeyID: r.URL.Query().Get("client_key_id"),
	}
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.From = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.To = t
		}
	}
	if err := f.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}

	usage, err := s.Store.AggregateUsage(ctx, f, "model")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analytics_error", err.Error())
		return
	}

	if format == "json" {
		writeJSON(w, http.StatusOK, map[string]any{
			"from":   f.From.Format(time.RFC3339),
			"to":     f.To.Format(time.RFC3339),
			"export": usage,
		})
		return
	}

	// CSV export.
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=usage_export.csv")
	writer := csv.NewWriter(w)
	defer writer.Flush()

	_ = writer.Write([]string{"Provider", "Account", "Model", "Route", "Client Key", "Requests", "Input Tokens", "Output Tokens", "Estimated Cost USD"})
	for _, u := range usage {
		costStr := ""
		if u.HasCost {
			costStr = fmt.Sprintf("%.6f", u.EstimatedUSD)
		}
		_ = writer.Write([]string{
			u.ProviderID,
			u.AccountID,
			u.ModelID,
			u.RouteAlias,
			sanitizeExportKey(u.ClientKeyID),
			strconv.Itoa(u.Requests),
			strconv.FormatInt(u.InputTokens, 10),
			strconv.FormatInt(u.OutputTokens, 10),
			costStr,
		})
	}
}

func sanitizeExportKey(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if len(id) <= 8 {
		return id
	}
	return id[:4] + "…" + id[len(id)-4:]
}
