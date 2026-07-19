package console

import (
	"net/http"
	"sort"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/smart"
)

// allProfilesFromConfig returns every config profile (user + external) keyed by id.
func allProfilesFromConfig(cfg *config.Config) map[string]smart.ModelProfile {
	user, external := smart.SplitProfilesFromConfig(cfg)
	out := make(map[string]smart.ModelProfile, len(user)+len(external))
	for k, v := range user {
		out[k] = v
	}
	for k, v := range external {
		out[k] = v
	}
	return out
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	ps := smart.NewProfileStoreFromConfig(rc.Cfg, true)
	out := []map[string]any{}
	for name, p := range rc.Cfg.Providers {
		adapter := adapterFor(p.Type)
		if adapter == nil {
			continue
		}
		secret, err := s.resolveCred(p.CredentialRef)
		if err != nil || secret == "" {
			// We can still list configured models from routes/aliases.
			continue
		}
		models, err := adapter.ListModels(r.Context(), p, secret)
		if err != nil {
			continue
		}
		for _, m := range models {
			prof, _ := ps.Resolve(p.Type, m.ID, "")
			out = append(out, map[string]any{
				"provider":   name,
				"model":      m.ID,
				"id":         smart.ProfileKey(p.Type, m.ID),
				"state":      "discovered",
				"profiled":   prof.Source != smart.SourceUnknown,
				"capabilities": prof.Capabilities,
			})
		}
	}
	// Also include configured targets that may not have been discovered.
	for _, rt := range rc.Cfg.Routes {
		for _, t := range rt.Targets {
			out = appendOrMerge(out, map[string]any{
				"provider": t.Provider, "model": t.Model,
				"id": smart.ProfileKey(t.Provider, t.Model), "state": "configured",
			})
		}
		for _, c := range rt.Candidates {
			out = appendOrMerge(out, map[string]any{
				"provider": c.Provider, "model": c.Model,
				"id": smart.ProfileKey(c.Provider, c.Model), "state": "configured",
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": out})
}

func appendOrMerge(list []map[string]any, item map[string]any) []map[string]any {
	for _, e := range list {
		if e["id"] == item["id"] {
			return list
		}
	}
	return append(list, item)
}

func (s *Server) handleListProfiles(w http.ResponseWriter, r *http.Request) {
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	ps := smart.NewProfileStoreFromConfig(rc.Cfg, true)
	rows := []map[string]any{}
	for k, mp := range rc.Cfg.ModelProfiles {
		src := mp.Source
		if src == "" {
			src = smart.SourceUser
		}
		p, _ := ps.Resolve("", "", k)
		rows = append(rows, profileRow(k, p, src))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i]["id"].(string) < rows[j]["id"].(string) })
	writeJSON(w, http.StatusOK, map[string]any{"profiles": rows})
}

func profileRow(id string, p smart.ModelProfile, source string) map[string]any {
	return map[string]any{
		"id":          id,
		"source":      source,
		"version":     p.Version,
		"capabilities": p.Capabilities,
		"properties":  p.Properties,
	}
}

func (s *Server) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	ps := smart.NewProfileStoreFromConfig(rc.Cfg, true)
	provider, model := splitProfileID(id)
	p, found := ps.Resolve(provider, model, id)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          id,
		"source":      p.Source,
		"found":       found,
		"capabilities": p.Capabilities,
		"properties":  p.Properties,
	})
}

func (s *Server) handlePutProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Capabilities map[string]float64         `json:"capabilities"`
		Properties   config.ModelPropertiesConfig `json:"properties"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	rev, err := s.applyMutation("profile_set", id, func(cfg *config.Config) error {
		if cfg.ModelProfiles == nil {
			cfg.ModelProfiles = map[string]config.ModelProfileConfig{}
		}
		mp := cfg.ModelProfiles[id]
		if mp.Capabilities == nil && body.Capabilities != nil {
			mp.Capabilities = map[string]float64{}
		}
		for k, v := range body.Capabilities {
			mp.Capabilities[k] = v
		}
		mp.Properties = body.Properties
		mp.Source = smart.SourceUser
		cfg.ModelProfiles[id] = mp
		// Validate via smart package.
		prof := allProfilesFromConfig(cfg)[id]
		return smart.ValidateProfile(prof)
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "save_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "revision": rev})
}

func (s *Server) handleResetProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rev, err := s.applyMutation("profile_reset", id, func(cfg *config.Config) error {
		if _, ok := cfg.ModelProfiles[id]; !ok {
			return errNotFound("profile")
		}
		delete(cfg.ModelProfiles, id)
		return nil
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "save_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reset": id, "revision": rev})
}

func (s *Server) handleValidateProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	ps := smart.NewProfileStoreFromConfig(rc.Cfg, true)
	provider, model := splitProfileID(id)
	p, _ := ps.Resolve(provider, model, id)
	if err := smart.ValidateProfile(p); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "errors": []string{err.Error()}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "errors": []string{}})
}

func splitProfileID(id string) (string, string) {
	for i := 0; i < len(id); i++ {
		if id[i] == '/' {
			return id[:i], id[i+1:]
		}
	}
	return "", id
}
