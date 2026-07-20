package console

import (
	"net/http"
	"sort"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/smart"
)

// allProfilesFromConfig returns the effective resolved profile for every
// configured profile id (layered resolution applied).
func allProfilesFromConfig(cfg *config.Config) map[string]smart.ModelProfile {
	ps := smart.NewProfileStoreFromConfig(cfg, false)
	out := make(map[string]smart.ModelProfile, len(cfg.ModelProfiles))
	for id := range cfg.ModelProfiles {
		provider, model := splitProfileID(id)
		p, _ := ps.Resolve(provider, model, id)
		out[id] = p
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
	for id := range rc.Cfg.ModelProfiles {
		provider, model := splitProfileID(id)
		res := ps.ResolveDetailed(provider, model, id)
		row := profileRow(id, res.Effective, res.Effective.Source)
		row["capabilities_provenance"] = res.Capabilities
		rows = append(rows, row)
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
	res := ps.ResolveDetailed(provider, model, id)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          id,
		"source":      res.Effective.Source,
		"found":       res.Found,
		"capabilities": res.Effective.Capabilities,
		"capabilities_provenance": res.Capabilities,
		"properties":  res.Effective.Properties,
		"property_sources": res.PropertySources,
	})
}

func (s *Server) handlePutProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Capabilities map[string]float64          `json:"capabilities"`
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
		// Manual edit updates only the user-override layer, merging with any
		// existing override fields (never copying lower-layer values up).
		if mp.UserOverrides == nil {
			mp.UserOverrides = &config.ProfileBaseline{}
		}
		if mp.UserOverrides.Capabilities == nil {
			mp.UserOverrides.Capabilities = map[string]float64{}
		}
		for k, v := range body.Capabilities {
			mp.UserOverrides.Capabilities[k] = v
		}
		if mp.UserOverrides.Properties == nil {
			mp.UserOverrides.Properties = &config.ModelPropertiesConfig{}
		}
		mergeProperties(mp.UserOverrides.Properties, body.Properties)
		cfg.ModelProfiles[id] = mp
		prof := allProfilesFromConfig(cfg)[id]
		return smart.ValidateProfile(prof)
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "save_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "revision": rev})
}

// mergeProperties copies only the fields explicitly set in src into dst. A src
// value is "set" when bools are non-nil, privacy is non-empty, or ints are > 0.
func mergeProperties(dst *config.ModelPropertiesConfig, src config.ModelPropertiesConfig) {
	if src.Vision != nil {
		dst.Vision = src.Vision
	}
	if src.Tools != nil {
		dst.Tools = src.Tools
	}
	if src.ParallelTools != nil {
		dst.ParallelTools = src.ParallelTools
	}
	if src.StructuredOutput != nil {
		dst.StructuredOutput = src.StructuredOutput
	}
	if src.Streaming != nil {
		dst.Streaming = src.Streaming
	}
	if src.ContextWindow > 0 {
		dst.ContextWindow = src.ContextWindow
	}
	if src.MaxOutputTokens > 0 {
		dst.MaxOutputTokens = src.MaxOutputTokens
	}
	if src.CostTier > 0 {
		dst.CostTier = src.CostTier
	}
	if src.LatencyTier > 0 {
		dst.LatencyTier = src.LatencyTier
	}
	if src.Privacy != "" {
		dst.Privacy = src.Privacy
	}
}

func (s *Server) handleResetProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rev, err := s.applyMutation("profile_reset", id, func(cfg *config.Config) error {
		mp, ok := cfg.ModelProfiles[id]
		if !ok {
			return errNotFound("profile")
		}
		// Reset removes user overrides, revealing assessment/external/builtin
		// baselines underneath. Remove the whole entry only if nothing remains.
		mp.UserOverrides = nil
		if mp.ExternalBaseline == nil && mp.AssessmentBaseline == nil {
			delete(cfg.ModelProfiles, id)
		} else {
			cfg.ModelProfiles[id] = mp
		}
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
