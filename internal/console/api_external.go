package console

import (
	"net/http"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/smart/external"
)

// getExternalService returns an ExternalEvidenceService backed by the store, a
// live web searcher (configured, e.g. for TLS-intercepting proxies), and an LLM
// summarizer that reads fetched pages.
func (s *Server) getExternalService() *external.Service {
	cfg, err := s.loadConfig()
	if err != nil {
		cfg = &revisionedConfig{Cfg: &config.Config{}}
	}
	searcher, err := external.NewWebSearcher(cfg.Cfg.WebSearch)
	if err != nil {
		// Log the error but continue with a default searcher to avoid breaking the UI.
		if s.Log != nil {
			s.Log.Warn("external web searcher init failed; using defaults", "error", err)
		}
		searcher, _ = external.NewWebSearcher(config.WebSearchConfig{})
	}
	summarizer := NewProviderSummarizer(cfg.Cfg, s.Creds, summarizerTarget{})
	return external.NewService(s.Store, searcher, summarizer)
}

func (s *Server) handleExternalRegistryInfo(w http.ResponseWriter, r *http.Request) {
	svc := s.getExternalService()
	writeJSON(w, http.StatusOK, svc.RegistryInfo())
}

func (s *Server) handleExternalEvidenceSearch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	providerID, modelID := splitProfileID(id)
	svc := s.getExternalService()
	cp, ok, err := svc.Search(r.Context(), providerID, modelID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "search_failed",
			"Could not search the web for benchmark evidence: "+err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_model",
			"Model "+id+" is not in the identity directory; add it or use a known model id.")
		return
	}
	if cp == nil {
		writeError(w, http.StatusNotFound, "no_external_evidence",
			"No independent benchmark evidence found online for "+id)
		return
	}
	writeJSON(w, http.StatusOK, cp)
}

func (s *Server) handleExternalEvidenceProposal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	providerID, modelID := splitProfileID(id)
	svc := s.getExternalService()

	// Current profile values (from config) for comparison.
	current := map[string]float64{}
	if rc, err := s.loadConfig(); err == nil {
		if mp, ok := rc.Cfg.ModelProfiles[id]; ok {
			for k, v := range mp.Capabilities {
				current[k] = v
			}
		}
	}

	p, ok, err := svc.BuildProposal(r.Context(), providerID, modelID, current)
	if err != nil {
		writeError(w, http.StatusBadGateway, "search_failed",
			"Could not search the web for benchmark evidence: "+err.Error())
		return
	}
	if !ok || p == nil {
		writeError(w, http.StatusNotFound, "no_external_evidence",
			"No independent benchmark evidence found online for "+id)
		return
	}
	if err := svc.SaveProposal(*p); err != nil {
		writeError(w, http.StatusInternalServerError, "save_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleListExternalProposals(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	svc := s.getExternalService()
	list, err := svc.ListProposals(status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": list})
}

func (s *Server) handleGetExternalProposal(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("proposalID")
	svc := s.getExternalService()
	p, ok, err := svc.GetProposal(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get_error", err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "proposal not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDismissExternalProposal(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("proposalID")
	svc := s.getExternalService()
	if err := svc.DismissProposal(pid); err != nil {
		writeError(w, http.StatusInternalServerError, "dismiss_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "dismissed"})
}

// handleApplyExternalProposal applies an external consensus proposal to the
// model profile, persisting it as an external-consensus source (never a user
// override) and reloading the runtime so routing uses it.
func (s *Server) handleApplyExternalProposal(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("proposalID")
	svc := s.getExternalService()
	p, ok, err := svc.GetProposal(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get_error", err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "proposal not found")
		return
	}

	// §18: proposals flagged for mandatory human review cannot be applied via
	// the API. They must be reviewed and cleared (or edited) first.
	if p.MandatoryReview {
		writeError(w, http.StatusConflict, "mandatory_review", "proposal requires human sign-off before it can be applied")
		return
	}

	caps, err := svc.ApplyProposal(p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "apply_error", err.Error())
		return
	}

	// Persist into config as an external-consensus baseline. This layer sits
	// below user overrides and local assessment; it never overwrites them, and
	// per-field resolution keeps higher layers' values for any shared field.
	profileID := p.ProviderID + "/" + p.ModelID
	rev, merr := s.applyMutation("external_profile_apply", profileID, func(cfg *config.Config) error {
		if cfg.ModelProfiles == nil {
			cfg.ModelProfiles = map[string]config.ModelProfileConfig{}
		}
		mp := cfg.ModelProfiles[profileID]
		if mp.ExternalBaseline == nil {
			mp.ExternalBaseline = &config.ProfileBaseline{}
		}
		mp.ExternalBaseline.Version = p.RegistryVersion
		if mp.ExternalBaseline.Capabilities == nil {
			mp.ExternalBaseline.Capabilities = map[string]float64{}
		}
		for k, v := range caps {
			mp.ExternalBaseline.Capabilities[k] = v
		}
		cfg.ModelProfiles[profileID] = mp
		return nil
	})
	if merr != nil {
		writeError(w, http.StatusInternalServerError, "config_error", merr.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":                  "applied",
		"profile_id":              profileID,
		"revision":                rev,
		"capabilities":            caps,
		"preserve_user_overrides": false,
	})
}

func (s *Server) handleExternalImportHistory(w http.ResponseWriter, r *http.Request) {
	svc := s.getExternalService()
	hist, err := svc.ImportHistory(50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "history_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imports": hist})
}
