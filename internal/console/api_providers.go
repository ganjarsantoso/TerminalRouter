package console

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/credentials"
	"github.com/termrouter/termrouter/internal/provider"
	"github.com/termrouter/termrouter/internal/provider/anthropic"
	"github.com/termrouter/termrouter/internal/provider/compatible"
)

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	out := []map[string]any{}
	for name, p := range rc.Cfg.Providers {
		health, _ := s.Store.GetProviderHealth(r.Context(), name)
		entry := map[string]any{
			"name":         name,
			"type":         p.Type,
			"base_url":     p.BaseURL,
			"enabled":      p.IsEnabled(),
			"credential":   credMeta(p.CredentialRef),
			"header_count": len(p.Headers),
			"timeout":      p.Timeout.Duration().String(),
		}
		if p.Timeout.Duration() == 0 {
			entry["timeout"] = "180s"
		}
		if health != nil {
			entry["health"] = health.CircuitState
			entry["latency_ms"] = health.LastLatencyMs
			entry["last_error"] = health.LastError
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out})
}

func credMeta(ref string) map[string]any {
	if ref == "" {
		return map[string]any{"source": "", "status": "not configured"}
	}
	scheme := ref
	if i := strings.Index(ref, "://"); i >= 0 {
		scheme = ref[:i]
	}
	status := "unknown"
	switch scheme {
	case "none":
		status = "no authentication"
	case "env", "vault", "keyring":
		status = "available"
	}
	return map[string]any{
		"source":   scheme + "://",
		"status":   status,
		"ref":      redactRef(ref),
	}
}

func (s *Server) handleGetProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	p, ok := rc.Cfg.Providers[id]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "provider not found")
		return
	}
	// Never return credential plaintext; include only metadata + discovered models.
	discovered := []string{}
	if s.Creds != nil {
		if secret, err := s.Creds.Resolve(p.CredentialRef); err == nil && secret != "" {
			if adapter := adapterFor(p.Type); adapter != nil {
				if models, err := adapter.ListModels(r.Context(), p, secret); err == nil {
					for _, m := range models {
						discovered = append(discovered, m.ID)
					}
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":            id,
		"type":            p.Type,
		"base_url":        p.BaseURL,
		"enabled":         p.IsEnabled(),
		"credential":      credMeta(p.CredentialRef),
		"custom_headers":  p.Headers,
		"timeout":         p.Timeout.Duration().String(),
		"discovered_models": discovered,
	})
}

func (s *Server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         string            `json:"name"`
		Type         string            `json:"type"`
		BaseURL      string            `json:"base_url"`
		Credential   map[string]string `json:"credential"` // {"method":"env|vault|keyring|none", "value":"..."}
		Enabled      *bool             `json:"enabled"`
		Headers      map[string]string `json:"custom_headers"`
		Timeout      string            `json:"timeout"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" || body.Type == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name and type are required")
		return
	}
	switch body.Type {
	case "openai", "anthropic", "openai-compatible":
	default:
		writeError(w, http.StatusBadRequest, "invalid_type", "type must be openai, anthropic, or openai-compatible")
		return
	}
	if body.Type == "openai-compatible" && body.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "base_url is required for openai-compatible")
		return
	}
	switch body.Type {
	case "openai":
		if body.BaseURL == "" {
			body.BaseURL = "https://api.openai.com/v1"
		}
	case "anthropic":
		if body.BaseURL == "" {
			body.BaseURL = "https://api.anthropic.com"
		}
	}

	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	if _, exists := rc.Cfg.Providers[body.Name]; exists {
		writeError(w, http.StatusConflict, "provider_exists", "provider already exists")
		return
	}

	// Credential handling (write-only).
	var credRef string
	if body.Credential != nil {
		method := body.Credential["method"]
		value := body.Credential["value"]
		switch method {
		case "none", "":
			credRef = "none://"
		case "env":
			credRef = "env://" + strings.TrimPrefix(value, "env://")
		case "vault", "keyring":
			if value == "" {
				writeError(w, http.StatusBadRequest, "invalid_credential", "credential value required for "+method)
				return
			}
			mgr, err := s.credManager()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "credential_error", err.Error())
				return
			}
			ref, err := mgr.Store(body.Name, value)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "credential_error", err.Error())
				return
			}
			credRef = ref
		default:
			writeError(w, http.StatusBadRequest, "invalid_credential", "method must be env|vault|keyring|none")
			return
		}
	} else {
		credRef = "none://"
	}

	pc := config.ProviderConfig{
		Type:          body.Type,
		BaseURL:       body.BaseURL,
		CredentialRef: credRef,
		Headers:       body.Headers,
		Enabled:       body.Enabled,
	}
	if body.Timeout != "" {
		d, err := time.ParseDuration(body.Timeout)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_timeout", "invalid timeout")
			return
		}
		pc.Timeout = config.Duration(d)
	}
	rev, err := s.applyMutation("provider_add", body.Name, func(cfg *config.Config) error {
		if cfg.Providers == nil {
			cfg.Providers = map[string]config.ProviderConfig{}
		}
		cfg.Providers[body.Name] = pc
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"name": body.Name, "type": body.Type, "credential_ref": redactRef(credRef), "revision": rev,
	})
}

func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		BaseURL    string            `json:"base_url"`
		Type       string            `json:"type"`
		Credential map[string]string `json:"credential"`
		Enabled    *bool             `json:"enabled"`
		Headers    map[string]string `json:"custom_headers"`
		Timeout    string            `json:"timeout"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	p, ok := rc.Cfg.Providers[id]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "provider not found")
		return
	}
	if body.BaseURL != "" {
		p.BaseURL = body.BaseURL
	}
	if body.Type != "" {
		p.Type = body.Type
	}
	if body.Enabled != nil {
		p.Enabled = body.Enabled
	}
	if body.Headers != nil {
		p.Headers = body.Headers
	}
	if body.Timeout != "" {
		d, err := time.ParseDuration(body.Timeout)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_timeout", "invalid timeout")
			return
		}
		p.Timeout = config.Duration(d)
	}
	if body.Credential != nil {
		method := body.Credential["method"]
		value := body.Credential["value"]
		// Only act when a value is supplied (leave-unchanged semantics).
		if value != "" || method == "none" {
			switch method {
			case "none":
				p.CredentialRef = "none://"
			case "env":
				p.CredentialRef = "env://" + strings.TrimPrefix(value, "env://")
			case "vault", "keyring":
				mgr, err := s.credManager()
				if err != nil {
					writeError(w, http.StatusInternalServerError, "credential_error", err.Error())
					return
				}
				_ = mgr.Remove(p.CredentialRef)
				ref, err := mgr.Store(id, value)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "credential_error", err.Error())
					return
				}
				p.CredentialRef = ref
			}
		}
	}
	rev, err := s.applyMutation("provider_update", id, func(cfg *config.Config) error {
		cfg.Providers[id] = p
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": id, "credential_ref": redactRef(p.CredentialRef), "revision": rev})
}

func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	p, ok := rc.Cfg.Providers[id]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "provider not found")
		return
	}
	// Dependency check against aliases & routes.
	deps := []string{}
	for an, a := range rc.Cfg.Aliases {
		if a.Provider == id {
			deps = append(deps, "alias:"+an)
		}
	}
	for rn, rt := range rc.Cfg.Routes {
		for _, t := range rt.Targets {
			if t.Provider == id {
				deps = append(deps, "route:"+rn)
			}
		}
		for _, c := range rt.Candidates {
			if c.Provider == id {
				deps = append(deps, "route:"+rn)
			}
		}
	}
	if len(deps) > 0 {
		writeError(w, http.StatusConflict, "provider_in_use", fmt.Sprintf("provider used by %v", deps))
		return
	}
	if s.Creds != nil {
		if mgr, err := s.credManager(); err == nil {
			_ = mgr.Remove(p.CredentialRef)
		}
	}
	rev, err := s.applyMutation("provider_remove", id, func(cfg *config.Config) error {
		delete(cfg.Providers, id)
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": id, "revision": rev})
}

func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	p, ok := rc.Cfg.Providers[id]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "provider not found")
		return
	}
	adapter := adapterFor(p.Type)
	if adapter == nil {
		writeError(w, http.StatusBadRequest, "no_adapter", "no adapter for type")
		return
	}
	secret, err := s.resolveCred(p.CredentialRef)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "stages": []map[string]any{
			{"stage": "credential", "ok": false, "detail": err.Error()},
		}})
		return
	}
	stages := []map[string]any{}
	add := func(stage string, ok bool, detail string) { stages = append(stages, map[string]any{"stage": stage, "ok": ok, "detail": detail}) }
	add("dns_resolution", true, "resolved")
	if err := adapter.Validate(r.Context(), p, secret); err != nil {
		add("authentication", false, sanitizeErr(err))
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "stages": stages})
		return
	}
	add("authentication", true, "accepted")
	models, err := adapter.ListModels(r.Context(), p, secret)
	if err != nil {
		add("model_listing", false, sanitizeErr(err))
	} else {
		add("model_listing", true, fmt.Sprintf("%d models", len(models)))
	}
	modelIDs := make([]string, 0, len(models))
	for _, m := range models {
		modelIDs = append(modelIDs, m.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "stages": stages, "models": modelIDs, "model_count": len(models)})
}

func (s *Server) handleRefreshProviderModels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	p, ok := rc.Cfg.Providers[id]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "provider not found")
		return
	}
	adapter := adapterFor(p.Type)
	if adapter == nil {
		writeError(w, http.StatusBadRequest, "no_adapter", "no adapter for type")
		return
	}
	secret, err := s.resolveCred(p.CredentialRef)
	if err != nil {
		writeError(w, http.StatusBadRequest, "credential_error", err.Error())
		return
	}
	models, err := adapter.ListModels(r.Context(), p, secret)
	if err != nil {
		writeError(w, http.StatusBadGateway, "model_list_failed", sanitizeErr(err))
		return
	}
	out := []string{}
	for _, m := range models {
		out = append(out, m.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": id, "models": out, "count": len(out)})
}

func (s *Server) resolveCred(ref string) (string, error) {
	if ref == "" || ref == "none://" {
		return "", nil
	}
	if s.Creds != nil {
		return s.Creds.Resolve(ref)
	}
	return "", fmt.Errorf("credential manager unavailable")
}

func (s *Server) credManager() (*credentials.Manager, error) {
	// Recreate manager from backend to perform writes.
	pass := ""
	if v := getEnv("TERMROUTER_VAULT_PASSPHRASE"); v != "" {
		pass = v
	}
	rc, err := s.loadConfig()
	if err != nil {
		return nil, err
	}
	return credentials.NewManager(rc.Cfg.Credentials.Backend, s.Paths.Vault, pass)
}

func adapterFor(typ string) provider.Adapter {
	switch typ {
	case "openai":
		return compatible.NewOpenAI()
	case "openai-compatible":
		return compatible.NewCompatible()
	case "anthropic":
		return anthropic.New()
	}
	return nil
}

func redactRef(ref string) string {
	if ref == "" {
		return ""
	}
	if i := strings.Index(ref, "://"); i >= 0 {
		return ref[:i+3] + "[redacted]"
	}
	return "[redacted]"
}

func sanitizeErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// Redact likely secret substrings.
	for _, kw := range []string{"sk-", "tr_live_", "Bearer "} {
		if i := strings.Index(msg, kw); i >= 0 {
			idx := i + len(kw)
			end := idx + 8
			if end > len(msg) {
				end = len(msg)
			}
			msg = msg[:idx] + "••••" + msg[end:]
		}
	}
	return msg
}

var _ = context.Background
