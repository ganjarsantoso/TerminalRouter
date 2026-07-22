package quota

import (
	"time"
)

// ReconciliationResult compares provider-reported usage with local observations.
type ReconciliationResult struct {
	ProviderID     string     `json:"provider_id"`
	AccountID      string     `json:"account_id"`
	Dimension      string     `json:"dimension"`
	ProviderTotal  float64    `json:"provider_total"`
	LocalTotal     float64    `json:"local_total"`
	Delta          float64    `json:"delta"`
	DeltaPercent   float64    `json:"delta_percent"`
	DriftThreshold float64    `json:"drift_threshold"`
	MaterialDrift  bool       `json:"material_drift"`
	PossibleCauses []string   `json:"possible_causes,omitempty"`
	ReconciledAt   time.Time  `json:"reconciled_at"`
	ProviderFresh  bool       `json:"provider_fresh"`
	ProviderAt     *time.Time `json:"provider_at,omitempty"`
	LocalAt        time.Time  `json:"local_at"`
}

// ReconcileConfig controls drift thresholds.
type ReconcileConfig struct {
	// MaterialDriftPercent is the threshold above which drift is flagged.
	MaterialDriftPercent float64
	// MaxStaleness is how old provider data can be before it's considered stale.
	MaxStaleness time.Duration
}

// DefaultReconcileConfig returns conservative defaults.
func DefaultReconcileConfig() ReconcileConfig {
	return ReconcileConfig{
		MaterialDriftPercent: 10.0, // 10% drift is material
		MaxStaleness:         15 * time.Minute,
	}
}

// Reconcile compares provider and local totals and produces a reconciliation result.
func Reconcile(providerTotal, localTotal float64, providerAt, localAt time.Time, cfg ReconcileConfig) ReconciliationResult {
	if cfg.MaterialDriftPercent <= 0 {
		cfg = DefaultReconcileConfig()
	}

	now := time.Now().UTC()
	delta := providerTotal - localTotal
	var deltaPct float64
	if providerTotal > 0 {
		deltaPct = (delta / providerTotal) * 100
	}

	materialDrift := deltaPct > cfg.MaterialDriftPercent || deltaPct < -cfg.MaterialDriftPercent
	providerFresh := !providerAt.IsZero() && now.Sub(providerAt) <= cfg.MaxStaleness

	causes := []string{}
	if materialDrift {
		if localTotal < providerTotal {
			causes = append(causes, "requests made outside TermRouter")
		}
		if localTotal > providerTotal {
			causes = append(causes, "provider billing delay or rounding")
		}
		if !providerFresh {
			causes = append(causes, "provider data stale")
		}
		causes = append(causes, "multiple gateways sharing one account")
		causes = append(causes, "clock or timezone differences")
	}

	return ReconciliationResult{
		ProviderTotal:  providerTotal,
		LocalTotal:     localTotal,
		Delta:          delta,
		DeltaPercent:   deltaPct,
		DriftThreshold: cfg.MaterialDriftPercent,
		MaterialDrift:  materialDrift,
		PossibleCauses: causes,
		ReconciledAt:   now,
		ProviderFresh:  providerFresh,
		ProviderAt:     &providerAt,
		LocalAt:        localAt,
	}
}
