package console

import (
	"net/http"

	"github.com/termrouter/termrouter/internal/lui"
)

func (s *Server) handleLUIValidate(w http.ResponseWriter, r *http.Request) {
	var env lui.Envelope
	if err := decodeJSON(r, &env); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := lui.Validate(&env); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_envelope", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"valid":   true,
		"version": env.Version,
		"kind":    string(env.Kind),
	})
}

func (s *Server) handleLUIRender(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Envelope lui.Envelope `json:"envelope"`
		Format   string       `json:"format"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := lui.Validate(&body.Envelope); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_envelope", err.Error())
		return
	}
	renderer := body.Format
	if renderer == "" {
		renderer = "compact_json"
	}
	out, _, err := lui.Render(&body.Envelope, renderer)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rendered": out,
		"format":   renderer,
	})
}

func (s *Server) handleLUIInspect(w http.ResponseWriter, r *http.Request) {
	requestID := r.PathValue("requestID")
	if requestID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "requestID is required")
		return
	}
	rec, err := s.Store.GetOptimizationRecord(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query_error", err.Error())
		return
	}
	if rec == nil {
		writeError(w, http.StatusNotFound, "not_found", "no optimization record for this request")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id":                   rec.RequestID,
		"route_name":                   rec.RouteName,
		"client_key_id":                rec.ClientKeyID,
		"provider_id":                  rec.ProviderID,
		"model_id":                     rec.ModelID,
		"mode_requested":               rec.ModeRequested,
		"mode_applied":                 rec.ModeApplied,
		"lui_version":                  rec.LUIVersion,
		"renderer":                     rec.Renderer,
		"input_tokens_before":          rec.InputTokensBefore,
		"input_tokens_after":           rec.InputTokensAfterEstimated,
		"cache_status":                 rec.CacheStatus,
		"cache_opportunity_tokens_est": rec.CacheOpportunityTokensEst,
		"cache_read_tokens_actual":     rec.CacheReadTokensActual,
		"cache_write_tokens_actual":    rec.CacheWriteTokensActual,
		"cache_source":                 rec.CacheSource,
		"net_saving_usd":               rec.NetSavingUSD,
		"loss_class":                   rec.LossClass,
		"bypassed":                     rec.Bypassed,
		"bypass_reason":                rec.BypassReason,
		"quality_status":               rec.QualityStatus,
		"created_at":                   rec.CreatedAt,
	})
}
