// Package quota implements Revision 6 quota tracking, reset forecasting,
// cost analytics support, and multi-account routing primitives.
package quota

import "time"

// MetricSource identifies how a metric value was obtained.
type MetricSource string

const (
	SourceProviderReported    MetricSource = "provider_reported"
	SourceProviderHeader      MetricSource = "provider_header"
	SourceProviderAPI         MetricSource = "provider_api"
	SourceLocalAuthoritative  MetricSource = "local_authoritative"
	SourceLocalEstimated      MetricSource = "local_estimated"
	SourceManualConfiguration MetricSource = "manual_configuration"
	SourceReconciled          MetricSource = "reconciled"
	SourceUnknown             MetricSource = "unknown"
)

// QuotaDimension identifies what a quota measures.
type QuotaDimension string

const (
	DimRequests           QuotaDimension = "requests"
	DimInputTokens        QuotaDimension = "input_tokens"
	DimOutputTokens       QuotaDimension = "output_tokens"
	DimTotalTokens        QuotaDimension = "total_tokens"
	DimCachedInputTokens  QuotaDimension = "cached_input_tokens"
	DimCacheWriteTokens   QuotaDimension = "cache_write_tokens"
	DimProviderCompute    QuotaDimension = "provider_compute_units"
	DimCredits            QuotaDimension = "credits"
	DimEstimatedCost      QuotaDimension = "estimated_cost"
	DimBilledCost         QuotaDimension = "billed_cost"
	DimConcurrentRequests QuotaDimension = "concurrent_requests"
)

// QuotaUnit is the unit of a quota limit/used value.
type QuotaUnit string

const (
	UnitCount          QuotaUnit = "count"
	UnitTokens         QuotaUnit = "tokens"
	UnitUSD            QuotaUnit = "USD"
	UnitProviderCredit QuotaUnit = "provider_credit"
	UnitComputeUnit    QuotaUnit = "compute_unit"
)

// WindowType describes how a quota window advances.
type WindowType string

const (
	WindowRolling          WindowType = "rolling"
	WindowFixedCalendar    WindowType = "fixed_calendar"
	WindowProviderReported WindowType = "provider_reported"
	WindowLifetime         WindowType = "lifetime"
	WindowUnknown          WindowType = "unknown"
)

// EnforcementMode controls local reaction to quota pressure.
type EnforcementMode string

const (
	EnforceObserveOnly     EnforcementMode = "observe_only"
	EnforceWarn            EnforcementMode = "warn"
	EnforceSoftLimit       EnforcementMode = "soft_limit"
	EnforceHardLimit       EnforcementMode = "hard_limit"
	EnforceProviderManaged EnforcementMode = "provider_managed"
)

// QuotaStatus is the operational health of a quota window.
type QuotaStatus string

const (
	StatusHealthy      QuotaStatus = "healthy"
	StatusWatch        QuotaStatus = "watch"
	StatusWarning      QuotaStatus = "warning"
	StatusCritical     QuotaStatus = "critical"
	StatusExhausted    QuotaStatus = "exhausted"
	StatusResetPending QuotaStatus = "reset_pending"
	StatusStale        QuotaStatus = "stale"
	StatusUnknown      QuotaStatus = "unknown"
	StatusUnavailable  QuotaStatus = "unavailable"
)

// ForecastStatus describes burn-rate forecast outcome.
type ForecastStatus string

const (
	ForecastSafeThroughReset ForecastStatus = "safe_through_reset"
	ForecastLikelyExhaust    ForecastStatus = "likely_to_exhaust_before_reset"
	ForecastAlreadyExhausted ForecastStatus = "already_exhausted"
	ForecastInsufficientData ForecastStatus = "insufficient_data"
	ForecastProviderStale    ForecastStatus = "provider_data_stale"
)

// AccountRoutingMode controls multi-account selection strategy.
type AccountRoutingMode string

const (
	RouteFixed              AccountRoutingMode = "fixed"
	RouteRoundRobin         AccountRoutingMode = "round_robin"
	RouteWeightedRoundRobin AccountRoutingMode = "weighted_round_robin"
	RouteLeastUsed          AccountRoutingMode = "least_used"
	RouteMostRemaining      AccountRoutingMode = "most_remaining"
	RouteResetAware         AccountRoutingMode = "reset_aware"
	RouteCostAware          AccountRoutingMode = "cost_aware"
	RouteQuotaBalanced      AccountRoutingMode = "quota_balanced"
	RouteManual             AccountRoutingMode = "manual"
)

// DefaultAccountID is the implicit account when a provider has no accounts map.
const DefaultAccountID = "default"

// ProviderAccount is a credential-backed account under a provider.
type ProviderAccount struct {
	ID                     string    `json:"id"`
	ProviderID             string    `json:"provider_id"`
	DisplayName            string    `json:"display_name"`
	CredentialRef          string    `json:"-"` // never serialize raw ref to browser without sanitization
	CredentialBackend      string    `json:"credential_backend,omitempty"`
	CredentialAvailable    bool      `json:"credential_available"`
	Enabled                bool      `json:"enabled"`
	Draining               bool      `json:"draining"`
	QuotaTrackingMode      string    `json:"quota_tracking_mode,omitempty"`
	RoutingMode            string    `json:"routing_mode,omitempty"`
	RoutingWeight          int       `json:"routing_weight"`
	SubscriptionPlanID     string    `json:"subscription_plan_id,omitempty"`
	BillingCurrency        string    `json:"billing_currency,omitempty"`
	Timezone               string    `json:"timezone,omitempty"`
	QuotaRoutingAllowed    bool      `json:"quota_routing_allowed"`
	MultiAccountRotationOK bool      `json:"multi_account_rotation_allowed"`
	Tags                   []string  `json:"tags,omitempty"`
	CreatedAt              time.Time `json:"created_at,omitempty"`
	UpdatedAt              time.Time `json:"updated_at,omitempty"`
}

// QuotaDefinition describes a configured or discovered quota window.
type QuotaDefinition struct {
	ID              string          `json:"id"`
	ProviderID      string          `json:"provider_id"`
	AccountID       string          `json:"account_id"`
	ModelPattern    string          `json:"model_pattern,omitempty"`
	Dimension       QuotaDimension  `json:"dimension"`
	Limit           float64         `json:"limit"`
	Unit            QuotaUnit       `json:"unit"`
	WindowType      WindowType      `json:"window_type"`
	WindowDuration  time.Duration   `json:"window_duration_ns,omitempty"`
	ResetRule       ResetRule       `json:"reset_rule"`
	EnforcementMode EnforcementMode `json:"enforcement_mode"`
	Source          MetricSource    `json:"source"`
	Enabled         bool            `json:"enabled"`
	Priority        int             `json:"priority"`
}

// ResetRule describes when a quota window resets.
type ResetRule struct {
	Type            string        `json:"type"` // rolling | daily | weekly | monthly | provider_reported | unknown | fixed
	Duration        time.Duration `json:"duration_ns,omitempty"`
	Hour            int           `json:"hour,omitempty"`
	Minute          int           `json:"minute,omitempty"`
	DayOfWeek       *time.Weekday `json:"day_of_week,omitempty"`
	DayOfMonth      int           `json:"day_of_month,omitempty"`
	Timezone        string        `json:"timezone,omitempty"`
	ProviderManaged bool          `json:"provider_managed,omitempty"`
	// Anchor is used for monthly billing-cycle resets.
	Anchor *time.Time `json:"anchor,omitempty"`
}

// QuotaWindowState is the live state of one quota window.
type QuotaWindowState struct {
	DefinitionID      string         `json:"definition_id"`
	ProviderID        string         `json:"provider_id"`
	AccountID         string         `json:"account_id"`
	ModelID           string         `json:"model_id,omitempty"`
	Dimension         QuotaDimension `json:"dimension"`
	Unit              QuotaUnit      `json:"unit"`
	WindowType        WindowType     `json:"window_type"`
	WindowStart       time.Time      `json:"window_start"`
	WindowEnd         *time.Time     `json:"window_end"`
	ResetAt           *time.Time     `json:"reset_at"`
	Limit             *float64       `json:"limit"` // null when unknown
	Used              float64        `json:"used"`
	Reserved          float64        `json:"reserved"`
	Remaining         *float64       `json:"remaining"` // null when limit unknown
	Utilization       *float64       `json:"utilization"`
	BurnRate          float64        `json:"burn_rate"` // units per hour
	BurnRateWindow    string         `json:"burn_rate_window,omitempty"`
	ForecastExhaustAt *time.Time     `json:"forecast_exhaust_at"`
	ForecastStatus    ForecastStatus `json:"forecast_status"`
	Status            QuotaStatus    `json:"status"`
	Source            MetricSource   `json:"source"`
	Confidence        float64        `json:"confidence"`
	LastUpdatedAt     time.Time      `json:"last_updated_at"`
	StaleAfter        time.Time      `json:"stale_after"`
	ReconciledDelta   float64        `json:"reconciled_delta"`
	// LocalUsed is TermRouter-observed usage for the window (always local).
	LocalUsed float64 `json:"local_used"`
	// ProviderUsed is provider-reported usage when available.
	ProviderUsed *float64 `json:"provider_used"`
	PaceDelta    *float64 `json:"pace_delta,omitempty"`
	Label        string   `json:"label,omitempty"`
}

// SubscriptionPlan is a manually configured subscription allowance.
type SubscriptionPlan struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	ProviderID           string            `json:"provider_id"`
	AccountID            string            `json:"account_id"`
	MonthlyPrice         *float64          `json:"monthly_price"` // null when unknown
	Currency             string            `json:"currency"`
	IncludedAllowance    []QuotaDefinition `json:"included_allowance,omitempty"`
	BillingCycleAnchor   time.Time         `json:"billing_cycle_anchor"`
	RenewalRule          ResetRule         `json:"renewal_rule"`
	OverageAllowed       bool              `json:"overage_allowed"`
	OveragePricingSource string            `json:"overage_pricing_source,omitempty"`
	Source               MetricSource      `json:"source"`
}

// SubscriptionUtilization is computed pace guidance for a plan.
type SubscriptionUtilization struct {
	PlanID                 string       `json:"plan_id"`
	Name                   string       `json:"name"`
	ProviderID             string       `json:"provider_id"`
	AccountID              string       `json:"account_id"`
	MonthlyPrice           *float64     `json:"monthly_price"`
	Currency               string       `json:"currency"`
	CycleStart             time.Time    `json:"cycle_start"`
	CycleEnd               time.Time    `json:"cycle_end"`
	AllowanceUsed          float64      `json:"allowance_used"`
	AllowanceLimit         *float64     `json:"allowance_limit"`
	AllowanceRemaining     *float64     `json:"allowance_remaining"`
	IdealUtilization       float64      `json:"ideal_utilization"`
	ActualUtilization      *float64     `json:"actual_utilization"`
	PaceDelta              *float64     `json:"pace_delta"`
	PaceLabel              string       `json:"pace_label"` // underutilized | on_pace | ahead_of_pace | likely_to_exhaust_early
	ExpectedUnused         *float64     `json:"expected_unused"`
	ExpectedExhaustAt      *time.Time   `json:"expected_exhaust_at"`
	EffectiveCostPerMToken *float64     `json:"effective_cost_per_million_tokens"`
	ObservedTokens         int64        `json:"observed_tokens"`
	Source                 MetricSource `json:"source"`
	Guidance               string       `json:"guidance"`
}

// AccountRoutingState persists multi-account selection progress.
type AccountRoutingState struct {
	ProviderID    string    `json:"provider_id"`
	RouteID       string    `json:"route_id"`
	LastAccountID string    `json:"last_account_id"`
	Sequence      int64     `json:"sequence"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// QuotaHeaderObservation is a sanitized provider response header observation.
type QuotaHeaderObservation struct {
	ProviderID    string         `json:"provider_id"`
	AccountID     string         `json:"account_id"`
	ModelID       string         `json:"model_id,omitempty"`
	Dimension     QuotaDimension `json:"dimension"`
	Limit         *float64       `json:"limit"`
	Remaining     *float64       `json:"remaining"`
	ResetAt       *time.Time     `json:"reset_at"`
	ObservedAt    time.Time      `json:"observed_at"`
	RawHeaderName string         `json:"raw_header_name"`
	Source        MetricSource   `json:"source"`
}

// ProviderQuotaSnapshot is a collector output for one account.
type ProviderQuotaSnapshot struct {
	ProviderID string                   `json:"provider_id"`
	AccountID  string                   `json:"account_id"`
	ObservedAt time.Time                `json:"observed_at"`
	Windows    []QuotaWindowState       `json:"windows"`
	Headers    []QuotaHeaderObservation `json:"headers,omitempty"`
	Source     MetricSource             `json:"source"`
	Error      string                   `json:"error,omitempty"`
}

// Summary is the dashboard aggregate response.
type Summary struct {
	GeneratedAt         time.Time                 `json:"generated_at"`
	Timezone            string                    `json:"timezone"`
	ActiveAccounts      int                       `json:"active_accounts"`
	WindowsWarning      int                       `json:"windows_warning"`
	WindowsCritical     int                       `json:"windows_critical"`
	TokensToday         int64                     `json:"tokens_today"`
	RequestsToday       int                       `json:"requests_today"`
	InputTokensToday    int64                     `json:"input_tokens_today"`
	OutputTokensToday   int64                     `json:"output_tokens_today"`
	EstimatedCostMonth  *float64                  `json:"estimated_cost_month"`
	BilledCostMonth     *float64                  `json:"billed_cost_month"`
	ForecastMonthEnd    *float64                  `json:"forecast_month_end"`
	Currency            string                    `json:"currency"`
	NextResetAt         *time.Time                `json:"next_reset_at"`
	SubscriptionUtil    []SubscriptionUtilization `json:"subscription_utilization,omitempty"`
	OptimizationSavings *float64                  `json:"optimization_savings_month"`
	Windows             []QuotaWindowState        `json:"windows"`
	Alerts              []Alert                   `json:"alerts"`
	Freshness           Freshness                 `json:"freshness"`
	TotalsSource        MetricSource              `json:"totals_source"`
}

// Freshness describes data staleness.
type Freshness struct {
	Status              string     `json:"status"` // fresh | stale | unknown
	LastProviderRefresh *time.Time `json:"last_provider_refresh"`
	LastLocalUpdate     *time.Time `json:"last_local_update"`
}

// Alert is a deduplicated operational alert.
type Alert struct {
	ID         string    `json:"id"`
	Severity   string    `json:"severity"` // info | warning | critical
	Code       string    `json:"code"`
	Message    string    `json:"message"`
	ProviderID string    `json:"provider_id,omitempty"`
	AccountID  string    `json:"account_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// UsageBreakdown is an attribution slice for analytics.
type UsageBreakdown struct {
	Key              string       `json:"key"`
	Requests         int          `json:"requests"`
	InputTokens      int64        `json:"input_tokens"`
	OutputTokens     int64        `json:"output_tokens"`
	CachedTokens     int64        `json:"cached_tokens,omitempty"`
	EstimatedCostUSD *float64     `json:"estimated_cost_usd"`
	BilledCostUSD    *float64     `json:"billed_cost_usd"`
	Source           MetricSource `json:"source"`
}

// TrendPoint is one interval bucket in a timeseries.
type TrendPoint struct {
	BucketStart      time.Time    `json:"bucket_start"`
	Requests         int          `json:"requests"`
	InputTokens      int64        `json:"input_tokens"`
	OutputTokens     int64        `json:"output_tokens"`
	EstimatedCostUSD *float64     `json:"estimated_cost_usd"`
	Source           MetricSource `json:"source"`
}

// Recommendation is safe workload allocation guidance.
type Recommendation struct {
	Code       string `json:"code"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	ProviderID string `json:"provider_id,omitempty"`
	AccountID  string `json:"account_id,omitempty"`
}

// SelectionDecision explains multi-account routing.
type SelectionDecision struct {
	ProviderID      string             `json:"provider_id"`
	SelectedAccount string             `json:"selected_account"`
	Mode            AccountRoutingMode `json:"mode"`
	Eligible        []string           `json:"eligible"`
	Rejected        map[string]string  `json:"rejected"`
	ScoreComponents map[string]float64 `json:"score_components,omitempty"`
	FallbackOrder   []string           `json:"fallback_order,omitempty"`
	ReservationEst  float64            `json:"reservation_estimate"`
	Reason          string             `json:"reason"`
}

// Ptr helpers for optional numeric fields.
func Float64(v float64) *float64 { return &v }
func TimePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	tt := t.UTC()
	return &tt
}
