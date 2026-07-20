package smart

import (
	"strings"
	"time"

	"github.com/termrouter/termrouter/internal/config"
)

// ProfilesFromConfig converts config model_profiles to smart profiles.
func ProfilesFromConfig(cfg *config.Config) map[string]ModelProfile {
	user, _, _ := SplitProfilesFromConfig(cfg)
	return user
}

// SplitProfilesFromConfig separates layered config model_profiles into user
// overrides, local assessment baselines, and external-consensus baselines. Each
// returned map is keyed by profile id and contains only the fields present in
// that layer, allowing per-field precedence at resolution time.
func SplitProfilesFromConfig(cfg *config.Config) (user, assessment, external map[string]ModelProfile) {
	user = map[string]ModelProfile{}
	assessment = map[string]ModelProfile{}
	external = map[string]ModelProfile{}
	if cfg == nil {
		return user, assessment, external
	}
	for id, mp := range cfg.ModelProfiles {
		if bl := mp.UserOverrides; bl != nil {
			user[id] = baselineToProfile(id, bl, SourceUser)
		}
		if bl := mp.AssessmentBaseline; bl != nil {
			assessment[id] = baselineToProfile(id, bl, SourceSelfAssess)
		}
		if bl := mp.ExternalBaseline; bl != nil {
			external[id] = baselineToProfile(id, bl, SourceExternal)
		}
	}
	return user, assessment, external
}

func baselineToProfile(id string, bl *config.ProfileBaseline, source string) ModelProfile {
	p := ModelProfile{
		ID:          id,
		Source:      source,
		Version:     bl.Version,
		Capabilities: map[string]float64{},
		Confidence:  map[string]float64{},
	}
	for k, v := range bl.Capabilities {
		p.Capabilities[k] = v
	}
	for k, v := range bl.Confidence {
		p.Confidence[k] = v
	}
	if bl.Properties != nil {
		p.Properties = ModelProperties{
			Vision: bl.Properties.Vision,
			Tools: bl.Properties.Tools,
			ParallelTools: bl.Properties.ParallelTools,
			StructuredOutput: bl.Properties.StructuredOutput,
			Streaming: bl.Properties.Streaming,
			ContextWindow: bl.Properties.ContextWindow,
			MaxOutputTokens: bl.Properties.MaxOutputTokens,
			CostTier: bl.Properties.CostTier,
			LatencyTier: bl.Properties.LatencyTier,
			Privacy: bl.Properties.Privacy,
		}
	}
	if i := strings.IndexByte(id, '/'); i > 0 {
		p.ProviderID = id[:i]
		p.ModelID = id[i+1:]
	}
	return p
}

// NewProfileStoreFromConfig builds a ProfileStore from layered config profiles.
func NewProfileStoreFromConfig(cfg *config.Config, strict bool) *ProfileStore {
	user, assessment, external := SplitProfilesFromConfig(cfg)
	return NewProfileStoreWithAssessments(user, assessment, external, strict)
}

// ProfileToConfig converts a smart profile to layered config form for saving,
// placing it into the baseline that matches its source so fields from other
// layers are preserved.
func ProfileToConfig(p ModelProfile) config.ModelProfileConfig {
	bl := &config.ProfileBaseline{
		Version:      p.Version,
		Capabilities: p.Capabilities,
		Confidence:   p.Confidence,
		Properties: &config.ModelPropertiesConfig{
			Vision: p.Properties.Vision,
			Tools: p.Properties.Tools,
			ParallelTools: p.Properties.ParallelTools,
			StructuredOutput: p.Properties.StructuredOutput,
			Streaming: p.Properties.Streaming,
			ContextWindow: p.Properties.ContextWindow,
			MaxOutputTokens: p.Properties.MaxOutputTokens,
			CostTier: p.Properties.CostTier,
			LatencyTier: p.Properties.LatencyTier,
			Privacy: p.Properties.Privacy,
		},
	}
	out := config.ModelProfileConfig{}
	switch p.Source {
	case SourceSelfAssess:
		out.AssessmentBaseline = bl
	case SourceExternal:
		out.ExternalBaseline = bl
	default:
		out.UserOverrides = bl
	}
	return out
}

// RouteFromConfig builds a smart RouteConfig from config.RouteConfig.
func RouteFromConfig(name string, r config.RouteConfig) RouteConfig {
	rc := RouteConfig{
		RouteID: name,
		Mode:    ModeShadow,
		Policy:  PolicyBalanced,
		StrictProfiles: true,
		SessionAffinity: true,
		SessionTTL: 60 * time.Minute,
	}
	if r.Smart != nil {
		if r.Smart.Mode != "" {
			rc.Mode = strings.ToLower(r.Smart.Mode)
		}
		if r.Smart.Policy != "" {
			rc.Policy = strings.ToLower(r.Smart.Policy)
		}
		if r.Smart.ConfidenceThreshold > 0 {
			rc.ConfidenceThreshold = r.Smart.ConfidenceThreshold
		}
		if r.Smart.MinimumTaskMatch > 0 {
			rc.MinimumTaskMatch = r.Smart.MinimumTaskMatch
		}
		if r.Smart.StrictProfiles != nil {
			rc.StrictProfiles = *r.Smart.StrictProfiles
		}
		if r.Smart.SessionAffinity.Enabled != nil {
			rc.SessionAffinity = *r.Smart.SessionAffinity.Enabled
		}
		if r.Smart.SessionAffinity.TTL > 0 {
			rc.SessionTTL = r.Smart.SessionAffinity.TTL.Duration()
		}
		if r.Smart.LowConfidenceTarget != "" {
			if p, m, err := config.ParseProviderModel(r.Smart.LowConfidenceTarget); err == nil {
				rc.DefaultProvider, rc.DefaultModel = p, m
			}
		}
	}
	if r.Default != "" {
		if p, m, err := config.ParseProviderModel(r.Default); err == nil {
			rc.DefaultProvider, rc.DefaultModel = p, m
		}
	}
	cands := r.Candidates
	if len(cands) == 0 {
		for _, t := range r.Targets {
			cands = append(cands, config.CandidateConfig{Provider: t.Provider, Model: t.Model})
		}
	}
	for i, c := range cands {
		rc.Candidates = append(rc.Candidates, Candidate{
			Provider: c.Provider, Model: c.Model, ProfileID: c.Profile, Order: i,
		})
	}
	// No implicit default: the default model must be set explicitly via
	// route.default or route.smart.low_confidence_target. When neither is set,
	// the engine errors on low-confidence / no-winner decisions instead of
	// silently substituting a model (PRD principle: no hardcoded default model).
	return rc
}
