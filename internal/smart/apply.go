package smart

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/router"
	"github.com/termrouter/termrouter/internal/storage"
)

// ApplyResult is the outcome of applying smart selection to a plan.
type ApplyResult struct {
	Plan     *router.Plan
	Decision *Decision
}

// GatewayEngine builds a smart engine from runtime deps.
func GatewayEngine(cfg *config.Config, store *storage.Store, credsCredentialCheck func(ref string) bool) *Engine {
	strict := true
	userProfiles, assessmentProfiles, extProfiles := SplitProfilesFromConfig(cfg)
	profiles := NewProfileStoreWithAssessments(userProfiles, assessmentProfiles, extProfiles, strict)
	eng := &Engine{
		Profiles:  profiles,
		Providers: map[string]ProviderState{},
		Affinity:  NewStoreAffinity(store),
	}
	if cfg != nil {
		for name, p := range cfg.Providers {
			st := ProviderState{
				Enabled:       p.IsEnabled(),
				HasCredential: true,
			}
			if p.CredentialRef != "" && p.CredentialRef != "none://" && credsCredentialCheck != nil {
				st.HasCredential = credsCredentialCheck(p.CredentialRef)
			}
			if store != nil {
				h, _ := store.GetProviderHealth(context.Background(), name)
				if h != nil && h.CircuitState == storage.CircuitOpen {
					st.CircuitOpen = true
				}
			}
			eng.Providers[name] = st
		}
	}
	return eng
}

// ApplySmart runs smart selection when plan.Strategy == "smart".
// For shadow mode, the returned plan keeps deterministic default attempts
// but decision records the recommendation.
// For live mode, the plan is reordered to the smart attempt plan with strategy fallback.
func ApplySmart(
	ctx context.Context,
	eng *Engine,
	cfg *config.Config,
	store *storage.Store,
	plan *router.Plan,
	req *normalization.NormalizedRequest,
	r *http.Request,
) (*ApplyResult, error) {
	if plan == nil || plan.Strategy != "smart" || plan.Smart == nil {
		return &ApplyResult{Plan: plan}, nil
	}
	if eng == nil {
		eng = GatewayEngine(cfg, store, nil)
	}

	routeCfg := RouteFromConfig(plan.Smart.RouteName, plan.Smart.Route)
	ov := OverridesFromRequest(req, r)

	decision, err := eng.Select(req, routeCfg, ov)
	if err != nil && decision == nil {
		return nil, err
	}
	// Persist even on partial failure if we have a decision
	if decision != nil {
		_ = PersistDecision(ctx, store, decision)
	}
	if err != nil {
		return &ApplyResult{Plan: plan, Decision: decision}, err
	}

	mode := routeCfg.Mode
	newPlan := *plan
	newPlan.RouteName = plan.Smart.RouteName

	switch mode {
	case ModeLive:
		pairs := AttemptPlan(decision)
		attempts, berr := buildAttempts(cfg, pairs)
		if berr != nil {
			return nil, berr
		}
		newPlan.Attempts = attempts
		newPlan.Strategy = "fallback" // allow smart fallback among eligible candidates
	case ModeShadow, ModeOff, "":
		// Deterministic: use the explicitly configured default target only. No
		// implicit fallback to a candidate — the default must be set via
		// route.default or route.smart.low_confidence_target (no hardcoded model).
		defP, defM := routeCfg.DefaultProvider, routeCfg.DefaultModel
		if defP == "" {
			return nil, fmt.Errorf("smart route %q in shadow/off mode requires an explicit default target; set route.default or route.smart.low_confidence_target", plan.RouteName)
		}
		attempts, berr := buildAttempts(cfg, []struct{ Provider, Model string }{{defP, defM}})
		if berr != nil {
			return nil, berr
		}
		newPlan.Attempts = attempts
		newPlan.Strategy = "direct"
	default:
		newPlan.Strategy = "fallback"
	}

	return &ApplyResult{Plan: &newPlan, Decision: decision}, nil
}

func buildAttempts(cfg *config.Config, pairs []struct{ Provider, Model string }) ([]router.Attempt, error) {
	var out []router.Attempt
	for _, pair := range pairs {
		if pair.Provider == "" {
			continue
		}
		p, ok := cfg.Providers[pair.Provider]
		if !ok || !p.IsEnabled() {
			continue
		}
		out = append(out, router.Attempt{ProviderID: pair.Provider, Model: pair.Model, Config: p})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no enabled attempts after smart selection")
	}
	return out, nil
}

// OverridesFromRequest extracts client overrides from metadata and headers.
func OverridesFromRequest(req *normalization.NormalizedRequest, r *http.Request) Override {
	ov := Override{}
	if r != nil {
		if v := r.Header.Get("X-TermRouter-Policy"); v != "" {
			ov.Policy = strings.ToLower(v)
		}
		if v := r.Header.Get("X-TermRouter-Session"); v != "" {
			ov.SessionID = v
		}
		if v := r.Header.Get("X-TermRouter-Reclassify"); v != "" {
			ov.Reclassify, _ = strconv.ParseBool(v)
		}
	}
	if req != nil && req.Metadata != nil {
		if tr, ok := req.Metadata["termrouter"].(map[string]any); ok {
			if p, ok := tr["policy"].(string); ok && p != "" {
				ov.Policy = strings.ToLower(p)
			}
			if s, ok := tr["session_id"].(string); ok {
				ov.SessionID = s
			}
			if b, ok := tr["reclassify"].(bool); ok {
				ov.Reclassify = b
			}
			if n, ok := tr["max_cost_tier"].(float64); ok {
				ov.MaxCostTier = int(n)
			}
			if arr, ok := tr["require"].([]any); ok {
				for _, a := range arr {
					if s, ok := a.(string); ok {
						ov.RequireCaps = append(ov.RequireCaps, s)
					}
				}
			}
		}
	}
	return ov
}

// WriteDecisionHeaders adds optional diagnostic headers.
func WriteDecisionHeaders(w http.ResponseWriter, d *Decision) {
	if w == nil || d == nil {
		return
	}
	if d.RequestID != "" {
		w.Header().Set("X-TermRouter-Request-ID", d.RequestID)
	}
	if d.RouteID != "" {
		w.Header().Set("X-TermRouter-Route", d.RouteID)
	}
	if d.SelectedKey() != "" {
		w.Header().Set("X-TermRouter-Selected-Model", d.SelectedKey())
	}
	if d.Task.PrimaryType != "" {
		w.Header().Set("X-TermRouter-Decision", d.Task.PrimaryType+"_"+d.Task.Complexity)
	}
	w.Header().Set("X-TermRouter-Confidence", fmt.Sprintf("%.2f", d.Task.Confidence))
	if d.Mode != "" {
		w.Header().Set("X-TermRouter-Smart-Mode", d.Mode)
	}
}
