package console

import (
	"net/http"

	"github.com/termrouter/termrouter/internal/config"
)

func (s *Server) handleListAliases(w http.ResponseWriter, r *http.Request) {
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	out := []map[string]any{}
	for name, a := range rc.Cfg.Aliases {
		entry := map[string]any{"name": name}
		if a.Route != "" {
			entry["route_type"] = "route"
			entry["resolved"] = a.Route
			if rt, ok := rc.Cfg.Routes[a.Route]; ok {
				entry["route_strategy"] = rt.Strategy
			}
		} else {
			entry["route_type"] = "direct"
			entry["resolved"] = a.Provider + "/" + a.Model
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"aliases": out})
}

func (s *Server) handleGetAlias(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	a, ok := rc.Cfg.Aliases[id]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "alias not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": id, "route": a.Route, "provider": a.Provider, "model": a.Model,
	})
}

func (s *Server) handleCreateAlias(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Route    string `json:"route"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if body.Route == "" && (body.Provider == "" || body.Model == "") {
		writeError(w, http.StatusBadRequest, "invalid_request", "provide route or provider+model")
		return
	}
	rev, err := s.applyMutation("alias_add", body.Name, func(cfg *config.Config) error {
		if _, exists := cfg.Aliases[body.Name]; exists {
			return errConflict("alias exists")
		}
		cfg.Aliases[body.Name] = config.AliasConfig{Route: body.Route, Provider: body.Provider, Model: body.Model}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "save_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": body.Name, "revision": rev})
}

func (s *Server) handleUpdateAlias(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Route    string `json:"route"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	rev, err := s.applyMutation("alias_update", id, func(cfg *config.Config) error {
		a, ok := cfg.Aliases[id]
		if !ok {
			return errNotFound("alias")
		}
		if body.Route != "" {
			a.Route = body.Route
			a.Provider = ""
			a.Model = ""
		}
		if body.Provider != "" {
			a.Provider = body.Provider
			a.Route = ""
		}
		if body.Model != "" {
			a.Model = body.Model
		}
		cfg.Aliases[id] = a
		return nil
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "save_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": id, "revision": rev})
}

func (s *Server) handleDeleteAlias(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rev, err := s.applyMutation("alias_remove", id, func(cfg *config.Config) error {
		if _, ok := cfg.Aliases[id]; !ok {
			return errNotFound("alias")
		}
		delete(cfg.Aliases, id)
		return nil
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "save_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": id, "revision": rev})
}
