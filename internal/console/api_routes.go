package console

import (
	"net/http"

	"github.com/termrouter/termrouter/internal/config"
)

func (s *Server) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	out := []map[string]any{}
	for name, rt := range rc.Cfg.Routes {
		cands := rt.Candidates
		if len(cands) == 0 {
			cands = nil
			for _, t := range rt.Targets {
				cands = append(cands, config.CandidateConfig{Provider: t.Provider, Model: t.Model})
			}
		}
		entry := map[string]any{
			"name":        name,
			"strategy":    rt.Strategy,
			"targets":     rt.Targets,
			"candidates":  cands,
			"default":     rt.Default,
		}
		if rt.Smart != nil {
			entry["smart"] = rt.Smart
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"routes": out})
}

func (s *Server) handleGetRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	rt, ok := rc.Cfg.Routes[id]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": id, "strategy": rt.Strategy, "targets": rt.Targets,
		"candidates": rt.Candidates, "smart": rt.Smart, "default": rt.Default,
	})
}

type routeInput struct {
	Name        string                     `json:"name"`
	Strategy    string                     `json:"strategy"`
	Targets     []config.TargetConfig      `json:"targets"`
	Candidates  []config.CandidateConfig   `json:"candidates"`
	Smart       *config.SmartConfig        `json:"smart"`
	Default     string                     `json:"default"`
}

func (s *Server) handleCreateRoute(w http.ResponseWriter, r *http.Request) {
	var body routeInput
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	rev, err := s.applyMutation("route_add", body.Name, func(cfg *config.Config) error {
		if _, exists := cfg.Routes[body.Name]; exists {
			return errConflict("route exists")
		}
		rc := config.RouteConfig{
			Strategy:   body.Strategy,
			Targets:    body.Targets,
			Candidates: body.Candidates,
			Smart:      body.Smart,
			Default:    body.Default,
		}
		if rc.Strategy == "" {
			if len(rc.Candidates) > 0 {
				rc.Strategy = "smart"
			} else if len(rc.Targets) > 1 {
				rc.Strategy = "fallback"
			} else {
				rc.Strategy = "direct"
			}
		}
		if rc.Strategy == "smart" && rc.Smart == nil {
			mode := "shadow"
			rc.Smart = &config.SmartConfig{Mode: mode, Policy: "balanced"}
		}
		cfg.Routes[body.Name] = rc
		return nil
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "save_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": body.Name, "revision": rev})
}

func (s *Server) handleUpdateRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body routeInput
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	rev, err := s.applyMutation("route_update", id, func(cfg *config.Config) error {
		rt, ok := cfg.Routes[id]
		if !ok {
			return errNotFound("route")
		}
		if body.Strategy != "" {
			rt.Strategy = body.Strategy
		}
		if body.Targets != nil {
			rt.Targets = body.Targets
		}
		if body.Candidates != nil {
			rt.Candidates = body.Candidates
		}
		if body.Smart != nil {
			rt.Smart = body.Smart
		}
		if body.Default != "" {
			rt.Default = body.Default
		}
		cfg.Routes[id] = rt
		return nil
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "save_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": id, "revision": rev})
}

func (s *Server) handleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Dependency check: aliases referencing this route.
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	deps := []string{}
	for an, a := range rc.Cfg.Aliases {
		if a.Route == id {
			deps = append(deps, "alias:"+an)
		}
	}
	if len(deps) > 0 {
		writeError(w, http.StatusConflict, "route_in_use", "route used by aliases")
		return
	}
	rev, err := s.applyMutation("route_remove", id, func(cfg *config.Config) error {
		delete(cfg.Routes, id)
		return nil
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "save_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": id, "revision": rev})
}

func (s *Server) handleValidateRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	rt, ok := rc.Cfg.Routes[id]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}
	clone := *rc.Cfg
	clone.Routes = map[string]config.RouteConfig{id: rt}
	if err := clone.Validate(); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "errors": []string{err.Error()}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "errors": []string{}})
}

func (s *Server) handleTestRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	rt, ok := rc.Cfg.Routes[id]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}
	// Validate each target provider credentials + reachability.
	results := []map[string]any{}
	targets := rt.Targets
	if len(targets) == 0 {
		for _, c := range rt.Candidates {
			targets = append(targets, config.TargetConfig{Provider: c.Provider, Model: c.Model})
		}
	}
	for _, t := range targets {
		p, ok := rc.Cfg.Providers[t.Provider]
		if !ok {
			results = append(results, map[string]any{"provider": t.Provider, "model": t.Model, "ok": false, "detail": "unknown provider"})
			continue
		}
		adapter := adapterFor(p.Type)
		if adapter == nil {
			results = append(results, map[string]any{"provider": t.Provider, "model": t.Model, "ok": false, "detail": "no adapter"})
			continue
		}
		secret, err := s.resolveCred(p.CredentialRef)
		if err != nil {
			results = append(results, map[string]any{"provider": t.Provider, "model": t.Model, "ok": false, "detail": "credential: " + sanitizeErr(err)})
			continue
		}
		err = adapter.Validate(r.Context(), p, secret)
		results = append(results, map[string]any{"provider": t.Provider, "model": t.Model, "ok": err == nil, "detail": sanitizeErr(err)})
	}
	allOK := true
	for _, res := range results {
		if !res["ok"].(bool) {
			allOK = false
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": allOK, "results": results})
}
