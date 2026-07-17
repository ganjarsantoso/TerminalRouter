package smart

import "fmt"

// BuiltinPolicies returns the five MVP routing policies.
func BuiltinPolicies() map[string]Policy {
	return map[string]Policy{
		PolicyBalanced: {
			Name: PolicyBalanced,
			Weights: PolicyWeights{
				TaskMatch: 0.40, SpecializedMatch: 0.15, Quality: 0.15,
				Reliability: 0.10, Cost: 0.10, Latency: 0.10,
			},
			MinimumTaskMatch: 0.60,
		},
		PolicyQuality: {
			Name: PolicyQuality,
			Weights: PolicyWeights{
				TaskMatch: 0.45, SpecializedMatch: 0.20, Quality: 0.25,
				Reliability: 0.10, Cost: 0.00, Latency: 0.00,
			},
			MinimumTaskMatch: 0.50,
		},
		PolicyEconomy: {
			Name: PolicyEconomy,
			Weights: PolicyWeights{
				TaskMatch: 0.30, SpecializedMatch: 0.10, Quality: 0.10,
				Reliability: 0.10, Cost: 0.35, Latency: 0.05,
			},
			MaxCostTier:      3,
			MinimumTaskMatch: 0.60,
		},
		PolicyFast: {
			Name: PolicyFast,
			Weights: PolicyWeights{
				TaskMatch: 0.30, SpecializedMatch: 0.10, Quality: 0.10,
				Reliability: 0.10, Cost: 0.05, Latency: 0.35,
			},
			MinimumTaskMatch: 0.55,
		},
		PolicyPrivate: {
			Name: PolicyPrivate,
			Weights: PolicyWeights{
				TaskMatch: 0.45, SpecializedMatch: 0.20, Quality: 0.15,
				Reliability: 0.10, Cost: 0.05, Latency: 0.05,
			},
			AllowedPrivacy:   []string{PrivacyLocal, PrivacyPrivateCloud},
			MinimumTaskMatch: 0.50,
		},
	}
}

// ResolvePolicy returns a named policy, optionally merged with route overrides.
func ResolvePolicy(name string, minTaskMatch float64, maxCostTier int) (Policy, error) {
	if name == "" {
		name = PolicyBalanced
	}
	policies := BuiltinPolicies()
	p, ok := policies[name]
	if !ok {
		return Policy{}, fmt.Errorf("unknown policy %q (want balanced, quality, economy, fast, private)", name)
	}
	if minTaskMatch > 0 {
		p.MinimumTaskMatch = minTaskMatch
	}
	if maxCostTier > 0 {
		if p.MaxCostTier == 0 || maxCostTier < p.MaxCostTier {
			p.MaxCostTier = maxCostTier
		}
	}
	return NormalizePolicy(p), nil
}

// NormalizePolicy ensures weights sum to 1.0 when total > 0.
func NormalizePolicy(p Policy) Policy {
	w := p.Weights
	sum := w.TaskMatch + w.SpecializedMatch + w.Quality + w.Reliability + w.Cost + w.Latency
	if sum <= 0 {
		return p
	}
	p.Weights = PolicyWeights{
		TaskMatch:        w.TaskMatch / sum,
		SpecializedMatch: w.SpecializedMatch / sum,
		Quality:          w.Quality / sum,
		Reliability:      w.Reliability / sum,
		Cost:             w.Cost / sum,
		Latency:          w.Latency / sum,
	}
	return p
}

// ValidatePolicyWeights checks custom weights are non-negative.
func ValidatePolicyWeights(w PolicyWeights) error {
	vals := []float64{w.TaskMatch, w.SpecializedMatch, w.Quality, w.Reliability, w.Cost, w.Latency}
	for _, v := range vals {
		if v < 0 {
			return fmt.Errorf("policy weights must be non-negative")
		}
	}
	return nil
}
