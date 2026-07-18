package console

import (
	"net/http"
	"time"
)

func (s *Server) handleUsageSummary(w http.ResponseWriter, r *http.Request) {
	since := time.Now().UTC().Truncate(24 * time.Hour)
	if v := r.URL.Query().Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			since = t
		}
	}
	sum, err := s.Store.UsageSince(r.Context(), since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "usage_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"since":   since.Format(time.RFC3339),
		"summary": sum,
	})
}

func (s *Server) handleUsageTimeseries(w http.ResponseWriter, r *http.Request) {
	// Lightweight daily series derived from recent request log.
	rows, err := s.Store.RecentRequests(r.Context(), 500, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "usage_error", err.Error())
		return
	}
	// Bucket by day for last 7 days.
	days := map[string]int{}
	now := time.Now().UTC()
	for i := 0; i < 7; i++ {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		days[d] = 0
	}
	for _, rec := range rows {
		d := rec.Timestamp.UTC().Format("2006-01-02")
		if _, ok := days[d]; ok {
			days[d]++
		}
	}
	series := []map[string]any{}
	for i := 6; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		series = append(series, map[string]any{"date": d, "requests": days[d]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"series": series})
}

func (s *Server) handleUsageBreakdown(w http.ResponseWriter, r *http.Request) {
	since := time.Now().UTC().Truncate(24 * time.Hour)
	sum, err := s.Store.UsageSince(r.Context(), since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "usage_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"by_provider": sum.ByProvider,
		"since":       since.Format(time.RFC3339),
	})
}
