package console

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	errorsOnly := r.URL.Query().Get("errors") == "1" || r.URL.Query().Get("errors") == "true"
	rows, err := s.Store.RecentRequests(r.Context(), limit, errorsOnly)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "activity_error", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, rec := range rows {
		status := "success"
		if rec.StatusCode >= 400 || rec.ErrorClass != "" {
			status = "error"
		} else if rec.FallbackReason != "" || rec.Attempt > 1 {
			status = "fallback"
		}
		// Rough cost estimate ($/1M tokens heuristic).
		cost := float64(rec.InputTokens)*0.000003 + float64(rec.OutputTokens)*0.000015
		out = append(out, map[string]any{
			"id":              rec.ID,
			"timestamp":       rec.Timestamp.UTC().Format(time.RFC3339Nano),
			"client_key_id":   rec.ClientKeyID,
			"client_label":    rec.ClientLabel,
			"protocol":        rec.InboundProtocol,
			"requested_model": rec.RequestedModel,
			"alias":           rec.ResolvedAlias,
			"provider":        rec.ProviderID,
			"upstream_model":  rec.UpstreamModel,
			"attempt":         rec.Attempt,
			"fallback_reason": rec.FallbackReason,
			"status_code":     rec.StatusCode,
			"status":          status,
			"latency_ms":      rec.LatencyMs,
			"ttft_ms":         rec.TimeToFirstTokenMs,
			"input_tokens":    rec.InputTokens,
			"output_tokens":   rec.OutputTokens,
			"error_class":     rec.ErrorClass,
			"stream":          rec.Stream,
			"estimated_cost":  cost,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out, "count": len(out)})
}

func (s *Server) handleActivityByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("requestID")
	rows, err := s.Store.RecentRequests(r.Context(), 500, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "activity_error", err.Error())
		return
	}
	for _, rec := range rows {
		if rec.ID != id {
			continue
		}
		decision, _ := s.Store.GetSmartDecision(r.Context(), id)
		writeJSON(w, http.StatusOK, map[string]any{
			"request":  rec,
			"decision": decision,
		})
		return
	}
	// Also allow looking up smart decisions alone.
	if d, _ := s.Store.GetSmartDecision(r.Context(), id); d != nil {
		writeJSON(w, http.StatusOK, map[string]any{"request": nil, "decision": d})
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "request not found")
}

func (s *Server) handleDecisionByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("requestID")
	d, err := s.Store.GetSmartDecision(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "decision_error", err.Error())
		return
	}
	if d == nil {
		writeError(w, http.StatusNotFound, "not_found", "decision not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"decision": d})
}

// filter helper kept for future query expansion
var _ = strings.Contains
