package console

import (
	"net/http"
	"strings"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/execution"
	"github.com/termrouter/termrouter/internal/provider"
	"github.com/termrouter/termrouter/internal/provider/anthropic"
	"github.com/termrouter/termrouter/internal/provider/compatible"
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

	reg := provider.NewRegistry()
	reg.Register(compatible.NewOpenAI())
	reg.Register(compatible.NewCompatible())
	reg.Register(anthropic.New())
	coord := execution.New(reg, s.Creds, s.Store, s.Log)

	return smart.NewModelAssessmentService(s.Store, credCheck, providerCheck, ps, coord, rc.Cfg)
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
	aid := r.PathValue("assessmentID")
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
	aid := r.PathValue("assessmentID")
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
	aid := r.PathValue("assessmentID")
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
	// Find smart routes that include this provider/model as a candidate.
	rc, loadErr := s.loadConfig()
	affected := []string{}
	if loadErr == nil && rc != nil {
		targetKey := smart.ProfileKey(rec.ProviderID, rec.ModelID)
		for name, route := range rc.Cfg.Routes {
			if strings.ToLower(route.Strategy) != "smart" && route.Smart == nil && len(route.Candidates) == 0 {
				continue
			}
			for _, c := range route.Candidates {
				if c.Provider == rec.ProviderID && c.Model == rec.ModelID {
					affected = append(affected, name)
					break
				}
				if smart.ProfileKey(c.Provider, c.Model) == targetKey {
					affected = append(affected, name)
					break
				}
			}
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
	aid := r.PathValue("assessmentID")
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
	// Persist accepted profile fields into config.yaml (same as CLI apply).
	if rec.ProposedProfile != nil {
		key := smart.ProfileKey(rec.ProviderID, rec.ModelID)
		accepted := body.AcceptedFields
		preserve := body.PreserveUserOverrides
		rev, mutErr := s.applyMutation("profile_assessment_apply", key, func(cfg *config.Config) error {
			if cfg.ModelProfiles == nil {
				cfg.ModelProfiles = map[string]config.ModelProfileConfig{}
			}
			mp := cfg.ModelProfiles[key]
			if mp.Capabilities == nil {
				mp.Capabilities = map[string]int{}
			}
			applyAll := len(accepted) == 0
			for k, v := range rec.ProposedProfile.Capabilities {
				if applyAll || containsString(accepted, k) {
					if preserve && mp.Source == smart.SourceUser {
						if _, exists := mp.Capabilities[k]; exists {
							continue
						}
					}
					mp.Capabilities[k] = v
				}
			}
			if rec.ProposedProfile.Properties.ContextWindow > 0 {
				mp.Properties.ContextWindow = rec.ProposedProfile.Properties.ContextWindow
			}
			if rec.ProposedProfile.Properties.MaxOutputTokens > 0 {
				mp.Properties.MaxOutputTokens = rec.ProposedProfile.Properties.MaxOutputTokens
			}
			if rec.ProposedProfile.Properties.CostTier > 0 {
				mp.Properties.CostTier = rec.ProposedProfile.Properties.CostTier
			}
			if rec.ProposedProfile.Properties.LatencyTier > 0 {
				mp.Properties.LatencyTier = rec.ProposedProfile.Properties.LatencyTier
			}
			if rec.ProposedProfile.Properties.Privacy != "" {
				mp.Properties.Privacy = rec.ProposedProfile.Properties.Privacy
			}
			if rec.ProposedProfile.Properties.Vision != nil {
				mp.Properties.Vision = rec.ProposedProfile.Properties.Vision
			}
			if rec.ProposedProfile.Properties.Tools != nil {
				mp.Properties.Tools = rec.ProposedProfile.Properties.Tools
			}
			if rec.ProposedProfile.Properties.ParallelTools != nil {
				mp.Properties.ParallelTools = rec.ProposedProfile.Properties.ParallelTools
			}
			if rec.ProposedProfile.Properties.StructuredOutput != nil {
				mp.Properties.StructuredOutput = rec.ProposedProfile.Properties.StructuredOutput
			}
			if rec.ProposedProfile.Properties.Streaming != nil {
				mp.Properties.Streaming = rec.ProposedProfile.Properties.Streaming
			}
			mp.Source = smart.SourceSelfAssess
			mp.Version = rec.BenchmarkVersion
			cfg.ModelProfiles[key] = mp
			return nil
		})
		if mutErr != nil {
			writeError(w, http.StatusBadRequest, "save_error", mutErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"assessment_id": aid,
			"status":        rec.Status,
			"applied_at":    rec.AppliedAt,
			"revision":      rev,
			"profile":       key,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"assessment_id": aid,
		"status":        rec.Status,
		"applied_at":    rec.AppliedAt,
	})
}
