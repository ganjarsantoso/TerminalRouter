package console

import (
	"net/http"
	"strings"

	"github.com/termrouter/termrouter/internal/smart"
)

// getAssessmentService creates or returns the shared ModelAssessmentService.
func (s *Server) getAssessmentService() *smart.ModelAssessmentService {
	rc, err := s.loadConfig()
	if err != nil {
		return nil
	}
	ps := smart.NewProfileStoreWithAssessments(
		smart.ProfilesFromConfig(rc.Cfg),
		map[string]smart.ModelProfile{},
		true,
	)
	credCheck := func(providerID string) bool {
		p, ok := rc.Cfg.Providers[providerID]
		if !ok {
			return false
		}
		if p.CredentialRef == "" || p.CredentialRef == "none://" {
			return false
		}
		secret, err := s.resolveCred(p.CredentialRef)
		return err == nil && secret != ""
	}
	providerCheck := func(providerID, modelID string) (bool, bool, bool) {
		p, ok := rc.Cfg.Providers[providerID]
		if !ok {
			return false, false, false
		}
		adapter := adapterFor(p.Type)
		if adapter == nil {
			return false, false, false
		}
		secret, err := s.resolveCred(p.CredentialRef)
		if err != nil || secret == "" {
			return false, false, false
		}
		models, err := adapter.ListModels(s.Ctx, p, secret)
		if err != nil {
			return false, false, false
		}
		for _, m := range models {
			if m.ID == modelID {
				return true, true, p.Type != "anthropic"
			}
		}
		return false, false, false
	}
	return smart.NewModelAssessmentService(s.Store, credCheck, providerCheck, ps)
}

func (s *Server) handleAssessmentPreflight(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	provider, model := splitProfileID(id)
	svc := s.getAssessmentService()
	if svc == nil {
		writeError(w, http.StatusInternalServerError, "service_error", "cannot initialize assessment service")
		return
	}
	result := svc.Preflight(provider, model)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAssessmentEstimate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	provider, model := splitProfileID(id)
	var body struct {
		Depth      smart.AssessmentDepth `json:"depth"`
		Categories []string              `json:"categories"`
	}
	if err := decodeJSON(r, &body); err != nil {
		body.Depth = smart.DepthStandard
	}
	if body.Depth == "" {
		body.Depth = smart.DepthStandard
	}
	svc := s.getAssessmentService()
	if svc == nil {
		writeError(w, http.StatusInternalServerError, "service_error", "cannot initialize assessment service")
		return
	}
	est := svc.Estimate(provider, model, body.Depth, body.Categories)
	writeJSON(w, http.StatusOK, est)
}

func (s *Server) handleStartAssessment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	provider, model := splitProfileID(id)
	var body struct {
		Depth      smart.AssessmentDepth  `json:"depth"`
		Categories []string               `json:"categories"`
		Limits     *smart.AssessmentPlan  `json:"limits,omitempty"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if body.Depth == "" {
		body.Depth = smart.DepthStandard
	}
	svc := s.getAssessmentService()
	if svc == nil {
		writeError(w, http.StatusInternalServerError, "service_error", "cannot initialize assessment service")
		return
	}
	rec, err := svc.Start(provider, model, body.Depth, body.Categories, body.Limits)
	if err != nil {
		writeError(w, http.StatusBadRequest, "start_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

func (s *Server) handleGetAssessment(w http.ResponseWriter, r *http.Request) {
	aid := r.PathValue("assessment-id")
	svc := s.getAssessmentService()
	if svc == nil {
		writeError(w, http.StatusInternalServerError, "service_error", "cannot initialize assessment service")
		return
	}
	rec, err := svc.GetAssessment(aid)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleCancelAssessment(w http.ResponseWriter, r *http.Request) {
	aid := r.PathValue("assessment-id")
	svc := s.getAssessmentService()
	if svc == nil {
		writeError(w, http.StatusInternalServerError, "service_error", "cannot initialize assessment service")
		return
	}
	if err := svc.Cancel(aid); err != nil {
		writeError(w, http.StatusBadRequest, "cancel_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled", "assessment_id": aid})
}

func (s *Server) handleListAssessments(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	provider, model := splitProfileID(id)
	svc := s.getAssessmentService()
	if svc == nil {
		writeError(w, http.StatusInternalServerError, "service_error", "cannot initialize assessment service")
		return
	}
	list, err := svc.ListAssessments(provider, model)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_error", err.Error())
		return
	}
	if list == nil {
		list = []smart.AssessmentSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"assessments": list})
}

func (s *Server) handleGetAssessmentProposal(w http.ResponseWriter, r *http.Request) {
	aid := r.PathValue("assessment-id")
	svc := s.getAssessmentService()
	if svc == nil {
		writeError(w, http.StatusInternalServerError, "service_error", "cannot initialize assessment service")
		return
	}
	// Find affected routes
	rc, err := s.loadConfig()
	affected := []string{}
	if err == nil && rc != nil {
		for name, route := range rc.Cfg.Routes {
			if strings.ToLower(route.Strategy) == "smart" {
				for _, c := range route.Candidates {
					svc := s.getAssessmentService()
					if svc != nil {
						_ = svc
					}
					_ = c
				}
			}
			_ = name
		}
	}
	prop, err := svc.GenerateProposal(aid, affected)
	if err != nil {
		writeError(w, http.StatusBadRequest, "proposal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, prop)
}

func (s *Server) handleApplyAssessmentProposal(w http.ResponseWriter, r *http.Request) {
	aid := r.PathValue("assessment-id")
	var body smart.ApplyProposalRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	body.AssessmentID = aid
	svc := s.getAssessmentService()
	if svc == nil {
		writeError(w, http.StatusInternalServerError, "service_error", "cannot initialize assessment service")
		return
	}
	rec, err := svc.ApplyProposal(aid, body.AcceptedFields, body.PreserveUserOverrides)
	if err != nil {
		writeError(w, http.StatusBadRequest, "apply_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"assessment_id": aid,
		"status":        rec.Status,
		"applied_at":    rec.AppliedAt,
	})
}
