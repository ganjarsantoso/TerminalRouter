package smart

import (
	"fmt"
	"strings"
	"time"

	"github.com/termrouter/termrouter/internal/normalization"
)

// RouteConfig is the smart-route configuration used at decision time.
type RouteConfig struct {
	RouteID             string
	Mode                string // shadow | live | off
	Policy              string
	Candidates          []Candidate
	ConfidenceThreshold float64 // high confidence threshold (default 0.80)
	LowConfidenceFloor  float64 // below this → default (default 0.50)
	MinimumTaskMatch    float64
	DefaultProvider     string
	DefaultModel        string
	StrictProfiles      bool
	SessionAffinity     bool
	SessionTTL          time.Duration
}

// Engine performs smart model selection.
type Engine struct {
	Profiles *ProfileStore
	// Optional reliability scores 0–1 by provider id.
	Reliability map[string]float64
	// Optional provider runtime state.
	Providers map[string]ProviderState
	// Affinity store (optional).
	Affinity AffinityStore
}

// AffinityStore abstracts session pin storage.
type AffinityStore interface {
	Get(sessionID string) (AffinityRecord, bool)
	Put(rec AffinityRecord) error
	Delete(sessionID string) error
}

// AffinityRecord pins a session to a target.
type AffinityRecord struct {
	SessionID  string
	RouteID    string
	Provider   string
	Model      string
	ExpiresAt  time.Time
	TaskType   string
	Complexity string
}

// Select analyzes the request and produces a Decision plus ordered eligible targets.
func (e *Engine) Select(req *normalization.NormalizedRequest, route RouteConfig, ov Override) (*Decision, error) {
	if e.Profiles == nil {
		e.Profiles = NewProfileStore(nil, true)
	}
	mode := route.Mode
	if mode == "" {
		mode = ModeShadow
	}

	task := Classify(req)
	if len(ov.RequireCaps) > 0 {
		ApplyRequireCaps(&task, ov.RequireCaps)
	}

	policyName := route.Policy
	if ov.Policy != "" {
		policyName = ov.Policy
	}
	policy, err := ResolvePolicy(policyName, route.MinimumTaskMatch, ov.MaxCostTier)
	if err != nil {
		return nil, err
	}

	decision := &Decision{
		RequestID:      reqID(req),
		RouteID:        route.RouteID,
		RequestedAlias: req.ResolvedAlias,
		Mode:           mode,
		Policy:         policy.Name,
		Task:           task,
		CatalogVersion: CatalogVersion,
		CreatedAt:      time.Now().UTC(),
	}
	if decision.RequestedAlias == "" && req != nil {
		decision.RequestedAlias = req.RequestedModel
	}

	// Session affinity check
	sessionID := ov.SessionID
	if sessionID == "" && req != nil {
		sessionID = sessionFromMetadata(req)
	}
	if route.SessionAffinity && sessionID != "" && e.Affinity != nil && !ov.Reclassify {
		if rec, ok := e.Affinity.Get(sessionID); ok && rec.RouteID == route.RouteID && time.Now().Before(rec.ExpiresAt) {
			// verify pin still eligible under hard constraints
			pinCand := Candidate{Provider: rec.Provider, Model: rec.Model}
			prof, found := e.Profiles.Resolve(rec.Provider, rec.Model, "")
			fctx := FilterContext{
				Task: task, Policy: policy, StrictProfiles: route.StrictProfiles,
				Providers: e.Providers, MaxCostTier: ov.MaxCostTier,
			}
			ev := FilterCandidate(pinCand, prof, found, fctx)
			if ev.Eligible {
				// check context limit still ok
				if !needsReclassify(task, rec) {
					decision.SessionAffinity = SessionAffinityResult{
						Hit: true, SessionID: sessionID,
						PinnedProvider: rec.Provider, PinnedModel: rec.Model,
						Reason: "session pinned",
					}
					decision.SelectedProvider = rec.Provider
					decision.SelectedModel = rec.Model
					decision.SelectionReasons = []string{"session affinity pin"}
					// still compute evaluations for explainability
					decision.Evaluations = e.evaluateAll(route, task, policy, ov)
					return decision, nil
				}
				decision.SessionAffinity = SessionAffinityResult{
					SessionID: sessionID, Reclassified: true, Reason: "capability or context change",
				}
			} else {
				decision.SessionAffinity = SessionAffinityResult{
					SessionID: sessionID, Reclassified: true, Reason: "pinned target no longer eligible",
				}
			}
		}
	}

	evals := e.evaluateAll(route, task, policy, ov)
	decision.Evaluations = evals

	// Confidence handling thresholds
	high := route.ConfidenceThreshold
	if high <= 0 {
		high = 0.80
	}
	// PRD: >=0.80 use best; >=0.50 and <0.80 prefer balanced generalist unless clear win; <0.50 default
	low := route.LowConfidenceFloor
	if low <= 0 {
		low = 0.50
	}

	var selected *CandidateEvaluation
	switch {
	case task.Confidence < low:
		decision.UsedDefault = true
		decision.DefaultReason = "low classification confidence"
		if route.DefaultProvider != "" {
			decision.SelectedProvider = route.DefaultProvider
			decision.SelectedModel = route.DefaultModel
			decision.SelectionReasons = []string{"low confidence; using configured default"}
		} else if best := firstEligible(evals); best != nil {
			selected = best
		}
	case task.Confidence < high:
		// Prefer default generalist unless another candidate wins clearly (score margin > 0.08)
		best := firstEligible(evals)
		if best != nil {
			if route.DefaultProvider != "" {
				defScore := scoreOf(evals, route.DefaultProvider, route.DefaultModel)
				if best.FinalScore-defScore < 0.08 && isEligible(evals, route.DefaultProvider, route.DefaultModel) {
					decision.SelectedProvider = route.DefaultProvider
					decision.SelectedModel = route.DefaultModel
					decision.SelectionReasons = []string{"medium confidence; preferring configured generalist default"}
					decision.UsedDefault = true
					decision.DefaultReason = "medium confidence generalist preference"
				} else {
					selected = best
				}
			} else {
				selected = best
			}
		}
	default:
		selected = firstEligible(evals)
	}

	if selected != nil {
		decision.SelectedProvider = selected.Provider
		decision.SelectedModel = selected.Model
		decision.SelectionScore = selected.FinalScore
		decision.SelectionReasons = selected.Explanation
	}

	if decision.SelectedProvider == "" {
		// No eligible candidate
		if route.DefaultProvider != "" && isEligible(evals, route.DefaultProvider, route.DefaultModel) {
			decision.SelectedProvider = route.DefaultProvider
			decision.SelectedModel = route.DefaultModel
			decision.UsedDefault = true
			decision.DefaultReason = "no scored winner; using default"
		} else {
			return decision, fmt.Errorf("%s", formatNoEligible(route.RouteID, task, evals))
		}
	}

	// Shadow recommendation is always the smart pick; live selection is the same unless mode is shadow
	// (caller applies mode: for shadow, actual plan uses deterministic targets).
	decision.ShadowRecommendation = decision.SelectedKey()

	// Persist affinity on live selection
	if mode == ModeLive && route.SessionAffinity && sessionID != "" && e.Affinity != nil {
		ttl := route.SessionTTL
		if ttl <= 0 {
			ttl = 60 * time.Minute
		}
		_ = e.Affinity.Put(AffinityRecord{
			SessionID:  sessionID,
			RouteID:    route.RouteID,
			Provider:   decision.SelectedProvider,
			Model:      decision.SelectedModel,
			ExpiresAt:  time.Now().Add(ttl),
			TaskType:   task.PrimaryType,
			Complexity: task.Complexity,
		})
		if !decision.SessionAffinity.Hit {
			decision.SessionAffinity.SessionID = sessionID
		}
	}

	return decision, nil
}

func (e *Engine) evaluateAll(route RouteConfig, task TaskProfile, policy Policy, ov Override) []CandidateEvaluation {
	fctx := FilterContext{
		Task: task, Policy: policy, StrictProfiles: route.StrictProfiles,
		Providers: e.Providers, MaxCostTier: ov.MaxCostTier,
	}
	order := map[string]int{}
	evals := make([]CandidateEvaluation, 0, len(route.Candidates))
	for i, c := range route.Candidates {
		order[ProfileKey(c.Provider, c.Model)] = c.Order
		if c.Order == 0 {
			order[ProfileKey(c.Provider, c.Model)] = i
		}
		prof, found := e.Profiles.Resolve(c.Provider, c.Model, c.ProfileID)
		prof.ProviderID = c.Provider
		prof.ModelID = c.Model
		ev := FilterCandidate(c, prof, found, fctx)
		if !ev.Eligible {
			evals = append(evals, ev)
			continue
		}
		rel := 0.85
		if e.Reliability != nil {
			if r, ok := e.Reliability[c.Provider]; ok {
				rel = r
			}
		}
		// health penalty via provider circuit already filtered; reliability from metrics
		scored := ScoreCandidate(prof, task, policy, i, rel)
		scored.Provider = c.Provider
		scored.Model = c.Model
		if !scored.Eligible {
			evals = append(evals, scored)
			continue
		}
		// mark healthy provider in explanation
		scored.Explanation = append([]string{}, scored.Explanation...)
		if ps, ok := e.Providers[c.Provider]; ok && ps.Enabled && !ps.CircuitOpen {
			scored.Explanation = append(scored.Explanation, "healthy provider")
		}
		evals = append(evals, scored)
	}
	SortEvaluations(evals, order)
	return evals
}

// AttemptPlan returns ordered attempts for live execution (eligible only, score order).
// Includes default at end if eligible and not already present.
func AttemptPlan(decision *Decision) []struct{ Provider, Model string } {
	if decision == nil {
		return nil
	}
	var out []struct{ Provider, Model string }
	seen := map[string]bool{}
	// Selected first
	if decision.SelectedProvider != "" {
		out = append(out, struct{ Provider, Model string }{decision.SelectedProvider, decision.SelectedModel})
		seen[decision.SelectedKey()] = true
	}
	for _, ev := range decision.Evaluations {
		if !ev.Eligible {
			continue
		}
		k := ProfileKey(ev.Provider, ev.Model)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, struct{ Provider, Model string }{ev.Provider, ev.Model})
	}
	return out
}

func firstEligible(evals []CandidateEvaluation) *CandidateEvaluation {
	for i := range evals {
		if evals[i].Eligible {
			return &evals[i]
		}
	}
	return nil
}

func scoreOf(evals []CandidateEvaluation, provider, model string) float64 {
	for _, e := range evals {
		if e.Provider == provider && e.Model == model {
			return e.FinalScore
		}
	}
	return 0
}

func isEligible(evals []CandidateEvaluation, provider, model string) bool {
	for _, e := range evals {
		if e.Provider == provider && e.Model == model {
			return e.Eligible
		}
	}
	// not in list — unknown
	return false
}

func needsReclassify(task TaskProfile, rec AffinityRecord) bool {
	// Hard capability change vs original pin context
	if task.HardRequirements.Tools || task.HardRequirements.Vision {
		// if task type drifted heavily
		if rec.TaskType != "" && rec.TaskType != task.PrimaryType {
			// only force when complexity escalates significantly
			if task.Complexity == ComplexityComplex && rec.Complexity != ComplexityComplex {
				return true
			}
		}
	}
	if task.HardRequirements.Vision || task.HardRequirements.Tools {
		// reclassify when new hard requirements appear (pin will be re-filtered anyway)
		return false // filter handles eligibility
	}
	return false
}

func reqID(req *normalization.NormalizedRequest) string {
	if req == nil {
		return ""
	}
	return req.ID
}

func sessionFromMetadata(req *normalization.NormalizedRequest) string {
	if req == nil || req.Metadata == nil {
		return ""
	}
	// metadata.termrouter.session_id
	if tr, ok := req.Metadata["termrouter"].(map[string]any); ok {
		if s, ok := tr["session_id"].(string); ok {
			return s
		}
	}
	if s, ok := req.Metadata["session_id"].(string); ok {
		return s
	}
	return ""
}

func formatNoEligible(routeID string, task TaskProfile, evals []CandidateEvaluation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "No eligible candidate for route %q.\n\nRequired:\n", routeID)
	if task.HardRequirements.Tools {
		b.WriteString("  tools=true\n")
	}
	if task.HardRequirements.Vision {
		b.WriteString("  vision=true\n")
	}
	if task.HardRequirements.StructuredOutput {
		b.WriteString("  structured_output=true\n")
	}
	if task.HardRequirements.MinimumContextWindow > 0 {
		fmt.Fprintf(&b, "  context_window>=%d\n", task.HardRequirements.MinimumContextWindow)
	}
	b.WriteString("\nRejected:\n")
	for _, ev := range evals {
		if ev.Eligible {
			continue
		}
		fmt.Fprintf(&b, "  %s/%s: %s\n", ev.Provider, ev.Model, strings.Join(ev.RejectionReasons, "; "))
	}
	return b.String()
}

// MemoryAffinity is an in-memory AffinityStore for tests and single-process use.
type MemoryAffinity struct {
	m map[string]AffinityRecord
}

func NewMemoryAffinity() *MemoryAffinity {
	return &MemoryAffinity{m: map[string]AffinityRecord{}}
}

func (a *MemoryAffinity) Get(sessionID string) (AffinityRecord, bool) {
	r, ok := a.m[sessionID]
	if !ok {
		return AffinityRecord{}, false
	}
	if time.Now().After(r.ExpiresAt) {
		delete(a.m, sessionID)
		return AffinityRecord{}, false
	}
	return r, true
}

func (a *MemoryAffinity) Put(rec AffinityRecord) error {
	a.m[rec.SessionID] = rec
	return nil
}

func (a *MemoryAffinity) Delete(sessionID string) error {
	delete(a.m, sessionID)
	return nil
}
