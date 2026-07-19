package smart

import (
	"strings"
	"time"

	"github.com/termrouter/termrouter/internal/config"
)

// ProfilesFromConfig converts config model_profiles to smart profiles.
func ProfilesFromConfig(cfg *config.Config) map[string]ModelProfile {
	user, _ := SplitProfilesFromConfig(cfg)
	return user
}

// SplitProfilesFromConfig separates config model_profiles into user overrides and
// external-consensus baselines based on their declared source.
func SplitProfilesFromConfig(cfg *config.Config) (user, external map[string]ModelProfile) {
	user = map[string]ModelProfile{}
	external = map[string]ModelProfile{}
	if cfg == nil {
		return user, external
	}
	for id, mp := range cfg.ModelProfiles {
		p := ModelProfile{
			ID:     id,
			Source: mp.Source,
			Version: mp.Version,
			Capabilities: map[string]float64{},
			Properties: ModelProperties{
				Vision: mp.Properties.Vision,
				Tools: mp.Properties.Tools,
				ParallelTools: mp.Properties.ParallelTools,
				StructuredOutput: mp.Properties.StructuredOutput,
				Streaming: mp.Properties.Streaming,
				ContextWindow: mp.Properties.ContextWindow,
				MaxOutputTokens: mp.Properties.MaxOutputTokens,
				CostTier: mp.Properties.CostTier,
				LatencyTier: mp.Properties.LatencyTier,
				Privacy: mp.Properties.Privacy,
			},
		}
		if p.Source == "" {
			p.Source = SourceUser
		}
		for k, v := range mp.Capabilities {
			p.Capabilities[k] = v
		}
		// split provider/model from id when possible
		if i := strings.IndexByte(id, '/'); i > 0 {
			p.ProviderID = id[:i]
			p.ModelID = id[i+1:]
		}
		if p.Source == SourceExternal {
			external[id] = p
		} else {
			user[id] = p
		}
	}
	return user, external
}

// NewProfileStoreFromConfig builds a ProfileStore separating user overrides and
// external-consensus baselines by their declared source.
func NewProfileStoreFromConfig(cfg *config.Config, strict bool) *ProfileStore {
	user, external := SplitProfilesFromConfig(cfg)
	return NewProfileStoreWithAssessments(user, nil, external, strict)
}

// ProfileToConfig converts a smart profile to config form for saving.
func ProfileToConfig(p ModelProfile) config.ModelProfileConfig {
	return config.ModelProfileConfig{
		Source:       p.Source,
		Version:      p.Version,
		Capabilities: p.Capabilities,
		Properties: config.ModelPropertiesConfig{
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
	// default default = first candidate
	if rc.DefaultProvider == "" && len(rc.Candidates) > 0 {
		rc.DefaultProvider = rc.Candidates[0].Provider
		rc.DefaultModel = rc.Candidates[0].Model
	}
	return rc
}
