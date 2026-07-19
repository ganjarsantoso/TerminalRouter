package console

import (
	"net/http"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/smart"
	"github.com/termrouter/termrouter/internal/smart/external"
)

// getExternalService returns an ExternalEvidenceService backed by the store.
func (s *Server) getExternalService() *external.Service {
	return external.NewService(s.Store)
}

func (s *Server) handleExternalRegistryInfo(w http.ResponseWriter, r *http.Request) {
	svc := s.getExternalService()
	writeJSON(w, http.StatusOK, svc.RegistryInfo())
}

func (s *Server) handleExternalEvidenceSearch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	providerID, modelID := splitProfileID(id)
	svc := s.getExternalService()
	cp, ok := svc.Search(providerID, modelID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":   "no_external_evidence",
			"message": "No curated independent benchmark evidence found for " + id,
		})
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

	p, ok := svc.BuildProposal(providerID, modelID, current)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":   "no_external_evidence",
			"message": "No curated independent benchmark evidence found for " + id,
		})
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

	caps, err := svc.ApplyProposal(p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "apply_error", err.Error())
		return
	}

	// Persist into config as an external-consensus profile (source precedence
	// below any user override, below assessment; above builtin).
	profileID := p.ProviderID + "/" + p.ModelID
	rev, merr := s.applyMutation("external_profile_apply", profileID, func(cfg *config.Config) error {
		if cfg.ModelProfiles == nil {
			cfg.ModelProfiles = map[string]config.ModelProfileConfig{}
		}
		mp := cfg.ModelProfiles[profileID]
		mp.Source = smart.SourceExternal
		mp.Version = p.RegistryVersion
		if mp.Capabilities == nil {
			mp.Capabilities = map[string]float64{}
		}
		for k, v := range caps {
			mp.Capabilities[k] = v
		}
		cfg.ModelProfiles[profileID] = mp
		return nil
	})
	if merr != nil {
		writeError(w, http.StatusInternalServerError, "config_error", merr.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":            "applied",
		"profile_id":        profileID,
		"revision":          rev,
		"capabilities":      caps,
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
