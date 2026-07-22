package console

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/optimization"
)

func (s *Server) handleOptimizationStatus(w http.ResponseWriter, r *http.Request) {
	if s.Opt == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": false,
		})
		return
	}
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	o := rc.Cfg.Optimization
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":            s.Opt.Enabled(),
		"default_mode":       string(o.DefaultMode),
		"aggressive_allowed": o.AggressiveAllowed,
		"prompt_cache":       o.PromptCache.Enabled,
		"compressors":        s.Opt.Compressors().List(),
	})
}

func (s *Server) handleOptimizationAnalyze(w http.ResponseWriter, r *http.Request) {
	if s.Opt == nil {
		writeError(w, http.StatusServiceUnavailable, "disabled", "optimization is not enabled")
		return
	}
	var req normalization.NormalizedRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.ID == "" {
		req.ID = "console-analyze"
	}
	oc := optimization.OptimizationContext{
		RequestID:        req.ID,
		ProviderID:       r.URL.Query().Get("provider"),
		ModelID:          r.URL.Query().Get("model"),
		ClientPreference: r.URL.Query().Get("mode"),
	}
	_, res, _, err := s.Opt.Process(r.Context(), &req, oc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "process_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mode_requested": string(res.ModeRequested),
		"mode_applied":   string(res.ModeApplied),
		"tokens_before":  res.InputTokensBefore,
		"tokens_after":   res.InputTokensEstimated,
		"removed_tokens": res.RemovedTokensEstimated,
		"cached_tokens":  res.ExpectedCachedTokens,
		"loss_class":     string(res.LossClass),
		"net_saving_usd": res.EstimatedNetSavingUSD,
		"bypassed":       res.Bypassed,
		"bypass_reason":  res.BypassReason,
		"warnings":       res.Warnings,
		"actions":        res.Actions,
	})
}

func (s *Server) handleOptimizationDryRun(w http.ResponseWriter, r *http.Request) {
	if s.Opt == nil {
		writeError(w, http.StatusServiceUnavailable, "disabled", "optimization is not enabled")
		return
	}
	var req normalization.NormalizedRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.ID == "" {
		req.ID = "console-dryrun"
	}
	oc := optimization.OptimizationContext{
		RequestID:        req.ID,
		ProviderID:       r.URL.Query().Get("provider"),
		ModelID:          r.URL.Query().Get("model"),
		ClientPreference: r.URL.Query().Get("mode"),
	}
	optReq, res, _, err := s.Opt.Process(r.Context(), &req, oc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "process_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":              string(res.ModeApplied),
		"tokens_before":     res.InputTokensBefore,
		"tokens_after":      res.InputTokensEstimated,
		"removed_tokens":    res.RemovedTokensEstimated,
		"loss_class":        string(res.LossClass),
		"net_saving_usd":    res.EstimatedNetSavingUSD,
		"optimized_request": optReq,
	})
}

func (s *Server) handleOptimizationCompare(w http.ResponseWriter, r *http.Request) {
	if s.Opt == nil {
		writeError(w, http.StatusServiceUnavailable, "disabled", "optimization is not enabled")
		return
	}
	var req normalization.NormalizedRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.ID == "" {
		req.ID = "console-compare"
	}
	modes := []string{"off", "safe", "balanced", "aggressive"}
	if m := r.URL.Query().Get("modes"); m != "" {
		modes = splitCommaStr(m)
	}
	provider := r.URL.Query().Get("provider")
	model := r.URL.Query().Get("model")

	type row struct {
		Mode    string  `json:"mode"`
		Before  int     `json:"before"`
		After   int     `json:"after"`
		Removed int     `json:"removed"`
		Loss    string  `json:"loss_class"`
		NetUSD  float64 `json:"net_saving_usd"`
	}
	var rows []row
	for _, mode := range modes {
		b, _ := json.Marshal(&req)
		var r2 normalization.NormalizedRequest
		_ = json.Unmarshal(b, &r2)
		oc := optimization.OptimizationContext{
			RequestID:        req.ID,
			ProviderID:       provider,
			ModelID:          model,
			ClientPreference: mode,
		}
		_, res, _, err := s.Opt.Process(r.Context(), &r2, oc)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "process_error", err.Error())
			return
		}
		rows = append(rows, row{
			Mode:    string(res.ModeApplied),
			Before:  res.InputTokensBefore,
			After:   res.InputTokensEstimated,
			Removed: res.RemovedTokensEstimated,
			Loss:    string(res.LossClass),
			NetUSD:  res.EstimatedNetSavingUSD,
		})
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleOptimizationReport(w http.ResponseWriter, r *http.Request) {
	last := r.URL.Query().Get("last")
	if last == "" {
		last = "24h"
	}
	d, err := time.ParseDuration(last)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	since := time.Now().UTC().Add(-d)
	sum, err := s.Store.OptimizationSummarySince(r.Context(), since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"since":          last,
		"requests":       sum.RequestsOptimized,
		"tokens_before":  sum.TokensBefore,
		"tokens_after":   sum.TokensAfter,
		"cached_tokens":  sum.CachedTokens,
		"net_saving_usd": sum.NetSavingUSD,
		"bypass_count":   sum.BypassCount,
	})
}

func (s *Server) handleOptimizationPlugins(w http.ResponseWriter, r *http.Request) {
	if s.Opt == nil {
		writeJSON(w, http.StatusOK, map[string]any{"plugins": []string{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": s.Opt.Compressors().List()})
}

func (s *Server) handleOptimizationPluginsTest(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.Opt == nil {
		writeError(w, http.StatusServiceUnavailable, "disabled", "optimization is not enabled")
		return
	}
	ok := s.Opt.Compressors().Healthy(r.Context(), name)
	status := "healthy"
	if !ok {
		status = "unhealthy-or-disabled"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plugin":  name,
		"status":  status,
		"healthy": ok,
	})
}

func splitCommaStr(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
