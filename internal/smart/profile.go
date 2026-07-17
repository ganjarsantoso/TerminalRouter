package smart

import (
	"fmt"
	"strings"
)

// ProfileStore resolves model profiles from user overrides and the built-in catalog.
type ProfileStore struct {
	// User overrides keyed by provider/model (or explicit profile id).
	User map[string]ModelProfile
	// Strict rejects unprofiled candidates from smart routes.
	Strict bool
}

// NewProfileStore builds a store from user profile map.
func NewProfileStore(user map[string]ModelProfile, strict bool) *ProfileStore {
	if user == nil {
		user = map[string]ModelProfile{}
	}
	return &ProfileStore{User: user, Strict: strict}
}

// Resolve returns the effective profile for a candidate.
// Precedence: user override (by profileID or provider/model) > builtin > empty unprofiled.
func (s *ProfileStore) Resolve(providerID, modelID, profileID string) (ModelProfile, bool) {
	keys := []string{}
	if profileID != "" {
		keys = append(keys, profileID)
	}
	keys = append(keys, ProfileKey(providerID, modelID), modelID)

	// User overrides win entirely when present.
	for _, k := range keys {
		if p, ok := s.User[k]; ok {
			p.ID = k
			if p.ProviderID == "" {
				p.ProviderID = providerID
			}
			if p.ModelID == "" {
				p.ModelID = modelID
			}
			if p.Source == "" {
				p.Source = SourceUser
			}
			return mergeWithDefaults(p), true
		}
	}

	// Builtin catalog
	for _, k := range keys {
		if p, ok := LookupBuiltin(k); ok {
			p.ProviderID = providerID
			p.ModelID = modelID
			return p, true
		}
	}
	// Try provider-type family: e.g. anthropic-main/claude-sonnet → anthropic/claude-sonnet
	family := familyKey(providerID, modelID)
	if p, ok := LookupBuiltin(family); ok {
		p.ProviderID = providerID
		p.ModelID = modelID
		p.ID = ProfileKey(providerID, modelID)
		return p, true
	}

	return ModelProfile{
		ID: ProfileKey(providerID, modelID), ProviderID: providerID, ModelID: modelID,
		Source: SourceUnknown, Capabilities: map[string]int{},
	}, false
}

func familyKey(providerID, modelID string) string {
	p := strings.ToLower(providerID)
	m := strings.ToLower(modelID)
	switch {
	case strings.Contains(p, "anthropic") || strings.Contains(m, "claude"):
		return "anthropic/" + modelID
	case strings.Contains(p, "openai") || strings.HasPrefix(m, "gpt-") || strings.HasPrefix(m, "o1"):
		return "openai/" + modelID
	case strings.Contains(p, "deepseek") || strings.Contains(m, "deepseek"):
		return "deepseek/" + modelID
	case strings.Contains(p, "local") || p == "ollama" || p == "lmstudio":
		return "local/" + modelID
	default:
		return providerID + "/" + modelID
	}
}

func mergeWithDefaults(p ModelProfile) ModelProfile {
	if p.Capabilities == nil {
		p.Capabilities = map[string]int{}
	}
	if p.Version == "" {
		p.Version = "user"
	}
	return p
}

// ValidateProfile checks capability ranges and property values.
func ValidateProfile(p ModelProfile) error {
	for k, v := range p.Capabilities {
		if v < 0 || v > 5 {
			return fmt.Errorf("capability %q must be 0–5, got %d", k, v)
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
func (p ModelProfile) Cap(name string) int {
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

// ListBuiltinKeys returns sorted builtin catalog keys.
func ListBuiltinKeys() []string {
	cat := BuiltinCatalog()
	keys := make([]string, 0, len(cat))
	for k := range cat {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		j := i
		for j > 0 && keys[j] < keys[j-1] {
			keys[j], keys[j-1] = keys[j-1], keys[j]
			j--
		}
	}
	return keys
}
