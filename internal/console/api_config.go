package console

import (
	"net/http"
	"strconv"
	"time"

	"github.com/termrouter/termrouter/internal/config"
	"gopkg.in/yaml.v3"
)

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	// Never expose secret values — ExportSanitized keeps credential scheme only.
	writeJSON(w, http.StatusOK, map[string]any{
		"config":   rc.Cfg.ExportSanitized(),
		"revision": rc.Revision,
	})
}

func (s *Server) handleValidateConfig(w http.ResponseWriter, r *http.Request) {
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	if err := rc.Cfg.Validate(); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "errors": []string{err.Error()}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "errors": []string{}})
}

func (s *Server) handleRollbackConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Revision int64 `json:"revision"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Revision <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "revision is required")
		return
	}
	hist, err := s.Store.GetConfigHistory(r.Context(), body.Revision)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "history_error", err.Error())
		return
	}
	if hist == nil || hist.ConfigYAML == "" {
		writeError(w, http.StatusNotFound, "not_found", "revision not found or has no full config snapshot")
		return
	}
	cfg := config.Default()
	if err := yaml.Unmarshal([]byte(hist.ConfigYAML), cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_snapshot", err.Error())
		return
	}
	if err := cfg.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_config", err.Error())
		return
	}
	if err := config.Save(s.Paths.Config, cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "save_error", err.Error())
		return
	}
	raw, _ := yaml.Marshal(cfg)
	san, _ := yaml.Marshal(cfg.ExportSanitized())
	rev, err := s.Store.InsertConfigHistory(r.Context(), s.sessionID(), "rollback", "revision:"+strconv.FormatInt(body.Revision, 10), string(raw), string(san))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "history_error", err.Error())
		return
	}
	if s.App != nil && s.ReloadRuntime {
		_ = s.App.Reload(cfg)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rolled_back_to": body.Revision,
		"revision":       rev,
	})
}

func (s *Server) handleConfigHistory(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListConfigHistory(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "history_error", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, h := range rows {
		out = append(out, map[string]any{
			"revision":           h.Revision,
			"timestamp":          h.Timestamp.UTC().Format(time.RFC3339),
			"session_id":         h.SessionID,
			"change_type":        h.ChangeType,
			"affected_resources": h.AffectedResources,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": out})
}

func (s *Server) handleConfigHistoryByRevision(w http.ResponseWriter, r *http.Request) {
	revStr := r.PathValue("revision")
	rev, err := strconv.ParseInt(revStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid revision")
		return
	}
	h, err := s.Store.GetConfigHistory(r.Context(), rev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "history_error", err.Error())
		return
	}
	if h == nil {
		writeError(w, http.StatusNotFound, "not_found", "revision not found")
		return
	}
	// Prefer sanitized YAML for browser display.
	writeJSON(w, http.StatusOK, map[string]any{
		"revision":           h.Revision,
		"timestamp":          h.Timestamp.UTC().Format(time.RFC3339),
		"session_id":         h.SessionID,
		"change_type":        h.ChangeType,
		"affected_resources": h.AffectedResources,
		"sanitized_yaml":     h.SanitizedYAML,
	})
}

func (s *Server) handleConfigExport(w http.ResponseWriter, r *http.Request) {
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	b, err := yaml.Marshal(rc.Cfg.ExportSanitized())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("Content-Disposition", "attachment; filename=termrouter-config.yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}
