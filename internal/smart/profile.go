package smart

import (
	"fmt"
)

// ProfileStore resolves model profiles from user overrides and the built-in catalog.
// It is assessment-aware: a resolved profile can wrap an assessment baseline + user overrides.
type ProfileStore struct {
	// User overrides keyed by provider/model (or explicit profile id).
	User map[string]ModelProfile
	// Assessment baselines keyed by provider/model.
	Assessments map[string]ModelProfile
	// External consensus baselines keyed by provider/model.
	External map[string]ModelProfile
	// Strict rejects unprofiled candidates from smart routes.
	Strict bool
}

// NewProfileStore builds a store from user profile map.
func NewProfileStore(user map[string]ModelProfile, strict bool) *ProfileStore {
	if user == nil {
		user = map[string]ModelProfile{}
	}
	return &ProfileStore{User: user, Assessments: map[string]ModelProfile{}, External: map[string]ModelProfile{}, Strict: strict}
}

// NewProfileStoreWithAssessments builds a store with user profiles and assessment baselines.
func NewProfileStoreWithAssessments(user, assessments, external map[string]ModelProfile, strict bool) *ProfileStore {
	if user == nil {
		user = map[string]ModelProfile{}
	}
	if assessments == nil {
		assessments = map[string]ModelProfile{}
	}
	if external == nil {
		external = map[string]ModelProfile{}
	}
	return &ProfileStore{User: user, Assessments: assessments, External: external, Strict: strict}
}

// SetExternalBaselines replaces the external consensus baselines.
func (s *ProfileStore) SetExternalBaselines(external map[string]ModelProfile) {
	if external == nil {
		external = map[string]ModelProfile{}
	}
	s.External = external
}

// Layers in ascending precedence order (lowest first).
func (s *ProfileStore) layerOrder(keys []string) [][2]string {
	// returns list of (source, key) to inspect; we iterate per-field instead.
	return nil
}

// Resolve returns the effective profile for a candidate using per-field
// layered precedence: user override > local assessment > external consensus >
// built-in (runtime). Each field is taken from the highest-precedence layer
// that defines it.
func (s *ProfileStore) Resolve(providerID, modelID, profileID string) (ModelProfile, bool) {
	r := s.ResolveDetailed(providerID, modelID, profileID)
	return r.Effective, r.Found
}

// ResolvedField documents one capability after layered resolution.
type ResolvedField struct {
	Value         float64 `json:"value"`
	Source        string  `json:"source"`
	BaselineValue float64 `json:"baseline_value"`
	BaselineSource string `json:"baseline_source"`
	Confidence    float64 `json:"confidence"`
}

// ResolvedProfile is the effective profile with per-field provenance.
type ResolvedProfile struct {
	ProviderID string                  `json:"provider_id"`
	ModelID    string                  `json:"model_id"`
	ProfileID  string                  `json:"profile_id,omitempty"`
	Found      bool                    `json:"found"`
	Effective  ModelProfile            `json:"effective"`
	Capabilities map[string]ResolvedField `json:"capabilities"`
	PropertySources map[string]string  `json:"property_sources,omitempty"`
}

func defaultConfidence(source string) float64 {
	switch source {
	case SourceUser:
		return 1.0
	case SourceSelfAssess:
		return 0.92
	case SourceExternal:
		return 0.86
	case SourceObserved:
		return 0.8
	default:
		return 0.0
	}
}

// profileLayer is one (source, profile) to consider, in ascending precedence.
func (s *ProfileStore) candidateLayers(key string) []struct {
	source  string
	profile *ModelProfile
} {
	var layers []struct {
		source  string
		profile *ModelProfile
	}
	if p, ok := s.External[key]; ok {
		layers = append(layers, struct {
			source  string
			profile *ModelProfile
		}{SourceExternal, &p})
	}
	if p, ok := s.Assessments[key]; ok {
		layers = append(layers, struct {
			source  string
			profile *ModelProfile
		}{SourceSelfAssess, &p})
	}
	if p, ok := s.User[key]; ok {
		layers = append(layers, struct {
			source  string
			profile *ModelProfile
		}{SourceUser, &p})
	}
	return layers
}

// ResolveDetailed returns per-field layered resolution with provenance.
func (s *ProfileStore) ResolveDetailed(providerID, modelID, profileID string) ResolvedProfile {
	keys := []string{}
	if profileID != "" {
		keys = append(keys, profileID)
	}
	keys = append(keys, ProfileKey(providerID, modelID), modelID)

	res := ResolvedProfile{
		ProviderID:     providerID,
		ModelID:        modelID,
		ProfileID:      profileID,
		Capabilities:   map[string]ResolvedField{},
		PropertySources: map[string]string{},
		Effective: ModelProfile{
			ID:          ProfileKey(providerID, modelID),
			ProviderID:  providerID,
			ModelID:     modelID,
			Capabilities: map[string]float64{},
			Confidence:  map[string]float64{},
			Source:      SourceUnknown,
		},
	}

	found := false
	highestSource := ""

	// Capabilities: per-field across layers in ascending precedence.
	for _, cap := range AllCapabilities {
		var value float64
		var source string
		var baselineValue float64
		var baselineSource string
		for _, key := range keys {
			layers := s.candidateLayers(key)
			for _, l := range layers {
				v, ok := l.profile.Capabilities[cap]
				if !ok {
					continue
				}
				// lower layer becomes the baseline for the next higher winner
				baselineValue, baselineSource = value, source
				value, source = v, l.source
				found = true
				if l.source > highestSource {
					highestSource = l.source
				}
			}
		}
		if source == "" {
			continue
		}
		conf := defaultConfidence(source)
		if c, ok := layerConfidence(keys, s, cap); ok {
			conf = c
		}
		res.Capabilities[cap] = ResolvedField{
			Value:         value,
			Source:        source,
			BaselineValue: baselineValue,
			BaselineSource: baselineSource,
			Confidence:    conf,
		}
		res.Effective.Capabilities[cap] = value
		res.Effective.Confidence[cap] = conf
	}

	// Properties: per-field across layers in ascending precedence.
	resolvedProps := ModelProperties{}
	applyProp := func(get func(*ModelProperties) (any, bool), set func(*ModelProperties, any), name string) {
		var value any
		var source string
		var has bool
		for _, key := range keys {
			for _, l := range s.candidateLayers(key) {
				v, defined := get(&l.profile.Properties)
				if !defined {
					continue
				}
				source = l.source
				value = v
				has = true
				if l.source > highestSource {
					highestSource = l.source
				}
			}
		}
		if has {
			set(&resolvedProps, value)
			res.PropertySources[name] = source
			found = true
		}
	}
	applyProp(
		func(p *ModelProperties) (any, bool) { if p.Vision == nil { return nil, false }; return *p.Vision, true },
		func(p *ModelProperties, v any) { b := v.(bool); p.Vision = &b }, "vision")
	applyParamBool := func(name string, get func(*ModelProperties) *bool, set func(*ModelProperties, *bool)) {
		applyProp(
			func(p *ModelProperties) (any, bool) { if get(p) == nil { return nil, false }; return *get(p), true },
			func(p *ModelProperties, v any) { b := v.(bool); set(p, &b) }, name)
	}
	applyParamBool("tools", func(p *ModelProperties) *bool { return p.Tools }, func(p *ModelProperties, b *bool) { p.Tools = b })
	applyParamBool("parallel_tools", func(p *ModelProperties) *bool { return p.ParallelTools }, func(p *ModelProperties, b *bool) { p.ParallelTools = b })
	applyParamBool("structured_output", func(p *ModelProperties) *bool { return p.StructuredOutput }, func(p *ModelProperties, b *bool) { p.StructuredOutput = b })
	applyParamBool("streaming", func(p *ModelProperties) *bool { return p.Streaming }, func(p *ModelProperties, b *bool) { p.Streaming = b })
	applyProp(
		func(p *ModelProperties) (any, bool) { if p.ContextWindow == 0 { return nil, false }; return p.ContextWindow, true },
		func(p *ModelProperties, v any) { p.ContextWindow = v.(int) }, "context_window")
	applyProp(
		func(p *ModelProperties) (any, bool) { if p.MaxOutputTokens == 0 { return nil, false }; return p.MaxOutputTokens, true },
		func(p *ModelProperties, v any) { p.MaxOutputTokens = v.(int) }, "max_output_tokens")
	applyProp(
		func(p *ModelProperties) (any, bool) { if p.CostTier == 0 { return nil, false }; return p.CostTier, true },
		func(p *ModelProperties, v any) { p.CostTier = v.(int) }, "cost_tier")
	applyProp(
		func(p *ModelProperties) (any, bool) { if p.LatencyTier == 0 { return nil, false }; return p.LatencyTier, true },
		func(p *ModelProperties, v any) { p.LatencyTier = v.(int) }, "latency_tier")
	applyProp(
		func(p *ModelProperties) (any, bool) { if p.Privacy == "" { return nil, false }; return p.Privacy, true },
		func(p *ModelProperties, v any) { p.Privacy = v.(string) }, "privacy")

	res.Effective.Properties = resolvedProps
	if found {
		res.Found = true
		res.Effective.Source = highestSource
		if res.Effective.Source == "" {
			res.Effective.Source = SourceUnknown
		}
	}
	return res
}

func layerConfidence(keys []string, s *ProfileStore, cap string) (float64, bool) {
	// Confidence is read from the winning (highest) layer that defines the cap.
	var conf float64
	var ok bool
	var bestSource string
	for _, key := range keys {
		for _, l := range s.candidateLayers(key) {
			if c, has := l.profile.Confidence[cap]; has {
				if l.source >= bestSource {
					conf, ok = c, true
					bestSource = l.source
				}
			}
		}
	}
	return conf, ok
}

func mergeWithDefaults(p ModelProfile) ModelProfile {
	if p.Capabilities == nil {
		p.Capabilities = map[string]float64{}
	}
	if p.Confidence == nil {
		p.Confidence = map[string]float64{}
	}
	if p.Version == "" {
		p.Version = "user"
	}
	return p
}

// ValidateProfile checks capability ranges and property values.
func ValidateProfile(p ModelProfile) error {
	for k, v := range p.Capabilities {
		if v < 0 || v > 10 {
			return fmt.Errorf("capability %q must be 0–10, got %g", k, v)
		}
	}
	props := p.Properties
	if props.CostTier < 0 || props.CostTier > 5 {
		return fmt.Errorf("cost_tier must be 0–5")
	}
	if props.LatencyTier < 0 || props.LatencyTier > 5 {
		return fmt.Errorf("latency_tier must be 0–5")
	}
	if props.Privacy != "" {
		switch props.Privacy {
		case PrivacyLocal, PrivacyPrivateCloud, PrivacyCloud:
		default:
			return fmt.Errorf("privacy must be local, private-cloud, or cloud")
		}
	}
	return nil
}

// Cap returns a capability level (0 if missing).
func (p ModelProfile) Cap(name string) float64 {
	if p.Capabilities == nil {
		return 0
	}
	return p.Capabilities[name]
}

// Supports returns whether a boolean property is true.
// Unknown (nil) is not supported for mandatory checks.
func (p ModelProfile) Supports(prop string) (supported bool, known bool) {
	switch prop {
	case "vision":
		if p.Properties.Vision == nil {
			return false, false
		}
		return *p.Properties.Vision, true
	case "tools":
		if p.Properties.Tools == nil {
			return false, false
		}
		return *p.Properties.Tools, true
	case "parallel_tools":
		if p.Properties.ParallelTools == nil {
			return false, false
		}
		return *p.Properties.ParallelTools, true
	case "structured_output":
		if p.Properties.StructuredOutput == nil {
			return false, false
		}
		return *p.Properties.StructuredOutput, true
	case "streaming":
		if p.Properties.Streaming == nil {
			return false, false
		}
		return *p.Properties.Streaming, true
	default:
		return false, false
	}
}

// ListUserKeys returns sorted user profile keys.
func (s *ProfileStore) ListUserKeys() []string {
	keys := make([]string, 0, len(s.User))
	for k := range s.User {
		keys = append(keys, k)
	}
	// simple insertion sort for determinism without extra import complexity
	for i := 1; i < len(keys); i++ {
		j := i
		for j > 0 && keys[j] < keys[j-1] {
			keys[j], keys[j-1] = keys[j-1], keys[j]
			j--
		}
	}
	return keys
}

// ListBuiltinKeys returns no keys: there is no built-in model catalog.
func ListBuiltinKeys() []string {
	return nil
}
