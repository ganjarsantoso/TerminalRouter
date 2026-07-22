package cli

import "time"

type QuotaWindowDTO struct {
	ProviderID      string     `json:"provider_id"`
	AccountID       string     `json:"account_id"`
	Dimension       string     `json:"dimension"`
	Used            float64    `json:"used"`
	Limit           *float64   `json:"limit"`
	Utilization     *float64   `json:"utilization"`
	Status          string     `json:"status"`
	Source          string     `json:"source"`
	Confidence      string     `json:"confidence"`
	FreshnessStatus string     `json:"freshness_status"`
	ResetAt         *time.Time `json:"reset_at"`
}
