package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var slugRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

// Config is the human-editable TermRouter configuration (no plaintext secrets).
type Config struct {
	Server        ServerConfig                  `yaml:"server" json:"server"`
	Credentials   CredentialsConfig             `yaml:"credentials" json:"credentials"`
	Providers     map[string]ProviderConfig     `yaml:"providers" json:"providers"`
	Routes        map[string]RouteConfig        `yaml:"routes" json:"routes"`
	Aliases       map[string]AliasConfig        `yaml:"aliases" json:"aliases"`
	ModelProfiles map[string]ModelProfileConfig `yaml:"model_profiles,omitempty" json:"model_profiles,omitempty"`
	Logging       LoggingConfig                 `yaml:"logging" json:"logging"`
	Summarizer    SummarizerConfig              `yaml:"summarizer,omitempty" json:"summarizer,omitempty"`
	WebSearch     WebSearchConfig               `yaml:"web_search,omitempty" json:"web_search,omitempty"`
	PublicHosting PublicHostingConfig           `yaml:"public_hosting,omitempty" json:"public_hosting,omitempty"`
	Pricing       map[string]PriceConfig        `yaml:"pricing,omitempty" json:"pricing,omitempty"`
	Optimization  OptimizationConfig            `yaml:"optimization,omitempty" json:"optimization,omitempty"`
}

// OptimizationMode identifies a token-optimization aggressiveness tier.
type OptimizationMode string

const (
	OptModeOff        OptimizationMode = "off"
	OptModeSafe       OptimizationMode = "safe"
	OptModeBalanced   OptimizationMode = "balanced"
	OptModeAggressive OptimizationMode = "aggressive"
)

// ParseOptimizationMode normalizes and validates a mode string. Empty maps to
// the safe default. Unknown values return an error.
func ParseOptimizationMode(s string) (OptimizationMode, error) {
	switch OptimizationMode(strings.ToLower(strings.TrimSpace(s))) {
	case "", OptModeSafe:
		return OptModeSafe, nil
	case OptModeOff:
		return OptModeOff, nil
	case OptModeBalanced:
		return OptModeBalanced, nil
	case OptModeAggressive:
		return OptModeAggressive, nil
	default:
		return "", fmt.Errorf("invalid optimization mode %q (want off|safe|balanced|aggressive)", s)
	}
}

// Less reports whether mode a is strictly less aggressive than mode b.
func (m OptimizationMode) Less(b OptimizationMode) bool {
	order := map[OptimizationMode]int{OptModeOff: 0, OptModeSafe: 1, OptModeBalanced: 2, OptModeAggressive: 3}
	return order[m] < order[b]
}

// OptimizationConfig is the top-level token-optimization configuration.
type OptimizationConfig struct {
	Enabled     bool             `yaml:"enabled" json:"enabled"`
	DefaultMode OptimizationMode `yaml:"default_mode" json:"default_mode"`
	// AggressiveAllowed permits the aggressive mode at all (server maximum
	// safety policy). When false, aggressive is clamped to balanced.
	AggressiveAllowed   bool                        `yaml:"aggressive_allowed" json:"aggressive_allowed"`
	TokenEstimation     TokenEstimationConfig       `yaml:"token_estimation" json:"token_estimation"`
	PromptCache         PromptCacheConfig           `yaml:"prompt_cache" json:"prompt_cache"`
	Deterministic       DeterministicConfig         `yaml:"deterministic" json:"deterministic"`
	Conversation        ConversationConfig          `yaml:"conversation" json:"conversation"`
	SemanticCompression SemanticCompressionConfig   `yaml:"semantic_compression" json:"semantic_compression"`
	Output              OutputBudgetConfig          `yaml:"output" json:"output"`
	Privacy             OptimizationPrivacyConfig   `yaml:"privacy" json:"privacy"`
	Evaluation          OptimizationEvalConfig      `yaml:"evaluation" json:"evaluation"`
	Compressors         map[string]CompressorConfig `yaml:"compressors,omitempty" json:"compressors,omitempty"`
	ToolGroups          map[string][]string         `yaml:"tool_groups,omitempty" json:"tool_groups,omitempty"`
}

// TokenEstimationConfig controls the fallback token estimator.
type TokenEstimationConfig struct {
	FallbackCharsPerToken float64 `yaml:"fallback_chars_per_token" json:"fallback_chars_per_token"`
	SafetyMultiplier      float64 `yaml:"safety_multiplier" json:"safety_multiplier"`
}

// PromptCacheConfig controls request-prefix stabilization for cache opportunity
// estimation. This does NOT send native cache-control headers to providers;
// actual cache-hit savings require provider-native cache support which is not
// yet wired. The cache_opportunity_tokens_est field records the estimated
// cacheable prefix, separate from any cache_reported_by_provider actuals.
type PromptCacheConfig struct {
	Enabled                bool `yaml:"enabled" json:"enabled"`
	MinimumPrefixTokens    int  `yaml:"minimum_prefix_tokens" json:"minimum_prefix_tokens"`
	StabilizeSystem        bool `yaml:"stabilize_system" json:"stabilize_system"`
	StabilizeTools         bool `yaml:"stabilize_tools" json:"stabilize_tools"`
	StabilizeStaticContext bool `yaml:"stabilize_static_context" json:"stabilize_static_context"`
}

// DeterministicConfig toggles safe deterministic compactors.
type DeterministicConfig struct {
	Deduplicate        bool `yaml:"deduplicate" json:"deduplicate"`
	CompactJSON        bool `yaml:"compact_json" json:"compact_json"`
	CompactLogs        bool `yaml:"compact_logs" json:"compact_logs"`
	StripANSI          bool `yaml:"strip_ansi" json:"strip_ansi"`
	CompactToolResults bool `yaml:"compact_tool_results" json:"compact_tool_results"`
}

// ConversationConfig manages conversation-window trimming.
type ConversationConfig struct {
	Enabled         bool `yaml:"enabled" json:"enabled"`
	RecentTurnsFull int  `yaml:"recent_turns_full" json:"recent_turns_full"`
	TriggerTokens   int  `yaml:"trigger_tokens" json:"trigger_tokens"`
	TargetTokens    int  `yaml:"target_tokens" json:"target_tokens"`
}

// SemanticCompressionConfig gates optional external semantic compressors.
type SemanticCompressionConfig struct {
	Enabled                      bool     `yaml:"enabled" json:"enabled"`
	Adapter                      string   `yaml:"adapter" json:"adapter"`
	MinimumInputTokens           int      `yaml:"minimum_input_tokens" json:"minimum_input_tokens"`
	MinimumExpectedSavingsTokens int      `yaml:"minimum_expected_savings_tokens" json:"minimum_expected_savings_tokens"`
	Timeout                      Duration `yaml:"timeout" json:"timeout"`
	FailureMode                  string   `yaml:"failure_mode" json:"failure_mode"`
}

// OutputBudgetConfig drives the adaptive output-token planner.
type OutputBudgetConfig struct {
	Mode             string `yaml:"mode" json:"mode"` // off | adaptive
	DefaultMaxTokens int    `yaml:"default_max_tokens" json:"default_max_tokens"`
	SimpleMaxTokens  int    `yaml:"simple_max_tokens" json:"simple_max_tokens"`
	MediumMaxTokens  int    `yaml:"medium_max_tokens" json:"medium_max_tokens"`
	ComplexMaxTokens int    `yaml:"complex_max_tokens" json:"complex_max_tokens"`
}

// OptimizationPrivacyConfig controls payload retention and external plug-ins.
type OptimizationPrivacyConfig struct {
	AllowExternalCompressors bool `yaml:"allow_external_compressors" json:"allow_external_compressors"`
	StoreRawPayloads         bool `yaml:"store_raw_payloads" json:"store_raw_payloads"`
	StoreCompressedPayloads  bool `yaml:"store_compressed_payloads" json:"store_compressed_payloads"`
	StoreLUIPayloads         bool `yaml:"store_lui_payloads" json:"store_lui_payloads"`
	StoreMetrics             bool `yaml:"store_metrics" json:"store_metrics"`
}

// OptimizationEvalConfig controls shadow / quality evaluation.
type OptimizationEvalConfig struct {
	ShadowMode bool    `yaml:"shadow_mode" json:"shadow_mode"`
	SampleRate float64 `yaml:"sample_rate" json:"sample_rate"`
}

// CompressorConfig configures a single external compressor plug-in.
type CompressorConfig struct {
	Enabled          bool     `yaml:"enabled" json:"enabled"`
	Transport        string   `yaml:"transport" json:"transport"` // http | unix
	Endpoint         string   `yaml:"endpoint" json:"endpoint"`
	Timeout          Duration `yaml:"timeout" json:"timeout"`
	FailureMode      string   `yaml:"failure_mode" json:"failure_mode"` // bypass | reject
	AllowedContent   []string `yaml:"allowed_content" json:"allowed_content"`
	TargetRatio      float64  `yaml:"target_ratio" json:"target_ratio"`
	MaxRequestBytes  int      `yaml:"max_request_bytes,omitempty" json:"max_request_bytes,omitempty"`
	MaxResponseBytes int      `yaml:"max_response_bytes,omitempty" json:"max_response_bytes,omitempty"`
	// AllowNonLoopback permits redirection/expansion to non-loopback hosts.
	AllowNonLoopback bool `yaml:"allow_non_loopback" json:"allow_non_loopback"`
}

// ProviderCapabilities describes provider-native feature support used by the
// optimization layer (e.g. prompt caching).
type ProviderCapabilities struct {
	PromptCache string `yaml:"prompt_cache,omitempty" json:"prompt_cache,omitempty"` // none | automatic | openai | anthropic | custom
}

// DefaultOptimization returns safe defaults. Optimization is opt-in (disabled).
func DefaultOptimization() OptimizationConfig {
	return OptimizationConfig{
		Enabled:           false,
		DefaultMode:       OptModeSafe,
		AggressiveAllowed: false,
		TokenEstimation: TokenEstimationConfig{
			FallbackCharsPerToken: 3.5,
			SafetyMultiplier:      1.15,
		},
		PromptCache: PromptCacheConfig{
			Enabled:                true,
			MinimumPrefixTokens:    1024,
			StabilizeSystem:        true,
			StabilizeTools:         true,
			StabilizeStaticContext: true,
		},
		Deterministic: DeterministicConfig{
			Deduplicate:        true,
			CompactJSON:        true,
			CompactLogs:        true,
			StripANSI:          true,
			CompactToolResults: true,
		},
		Conversation: ConversationConfig{
			Enabled:         true,
			RecentTurnsFull: 8,
			TriggerTokens:   24000,
			TargetTokens:    16000,
		},
		SemanticCompression: SemanticCompressionConfig{
			Enabled:                      false,
			MinimumInputTokens:           12000,
			MinimumExpectedSavingsTokens: 2000,
			Timeout:                      5 * 1000 * 1000 * 1000, // 5s as Duration
			FailureMode:                  "bypass",
		},
		Output: OutputBudgetConfig{
			Mode:             "adaptive",
			DefaultMaxTokens: 2048,
			SimpleMaxTokens:  512,
			MediumMaxTokens:  2048,
			ComplexMaxTokens: 4096,
		},
		Privacy: OptimizationPrivacyConfig{
			AllowExternalCompressors: false,
			StoreRawPayloads:         false,
			StoreCompressedPayloads:  false,
			StoreLUIPayloads:         false,
			StoreMetrics:             true,
		},
		Evaluation: OptimizationEvalConfig{
			ShadowMode: true,
			SampleRate: 0.05,
		},
	}
}

// PriceConfig describes the per-token cost for a specific provider/model (or a
// whole provider when keyed by the provider id alone). Costs are expressed per
// one million tokens. There is no built-in global default: enforcement of cost
// budgets requires an explicit price for the resolved provider/model, otherwise
// the request is treated as unpriced and rejected for portable/public keys.
type PriceConfig struct {
	InputUSDPerMillion       float64 `yaml:"input_usd_per_million" json:"input_usd_per_million"`
	OutputUSDPerMillion      float64 `yaml:"output_usd_per_million" json:"output_usd_per_million"`
	CachedInputUSDPerMillion float64 `yaml:"cached_input_usd_per_million,omitempty" json:"cached_input_usd_per_million,omitempty"`
	// Currency is the billing currency for the rates. Only "usd" is supported.
	Currency string `yaml:"currency,omitempty" json:"currency,omitempty"`
}

// Price is a resolved, validated pricing entry returned by LookupPrice.
type Price struct {
	InputUSDPerMillion  float64
	OutputUSDPerMillion float64
	// Source records which key matched: "provider/model", "provider", or "*".
	Source string
}

// supportedPriceCurrency is the only currency accepted for rate entries.
const supportedPriceCurrency = "usd"

// LookupPrice returns the most specific valid pricing for the resolved
// provider/model. ok is false when no entry matches, signalling that the route
// is unpriced. An entry with explicit zero rates is still returned (ok=true):
// a zero price is valid (e.g. a local model). Stored entries are guaranteed
// valid by ValidatePricing, so any returned entry is trustworthy.
func (c *Config) LookupPrice(provider, model string) (Price, bool) {
	key := provider + "/" + model
	if p, ok := c.Pricing[key]; ok {
		return Price{InputUSDPerMillion: p.InputUSDPerMillion, OutputUSDPerMillion: p.OutputUSDPerMillion, Source: key}, true
	}
	if p, ok := c.Pricing[provider]; ok {
		return Price{InputUSDPerMillion: p.InputUSDPerMillion, OutputUSDPerMillion: p.OutputUSDPerMillion, Source: provider}, true
	}
	if p, ok := c.Pricing["*"]; ok {
		return Price{InputUSDPerMillion: p.InputUSDPerMillion, OutputUSDPerMillion: p.OutputUSDPerMillion, Source: "*"}, true
	}
	return Price{}, false
}

// ComputeCost returns the estimated USD cost for the given token counts at the
// resolved provider/model. It delegates rate lookup to LookupPrice and is
// responsible only for the arithmetic. The second return value is false when
// no price entry matches (unpriced).
func (c *Config) ComputeCost(provider, model string, inTokens, outTokens int) (float64, bool) {
	pr, ok := c.LookupPrice(provider, model)
	if !ok {
		return 0, false
	}
	in := float64(inTokens) / 1_000_000 * pr.InputUSDPerMillion
	out := float64(outTokens) / 1_000_000 * pr.OutputUSDPerMillion
	return in + out, true
}

// ValidatePricing enforces that every pricing entry is well-formed: rates must
// be present and non-negative, the currency must be supported (usd), and the
// record must not be malformed. Explicit zero rates are valid.
func (c *Config) ValidatePricing() error {
	for key, p := range c.Pricing {
		cur := strings.ToLower(strings.TrimSpace(p.Currency))
		if cur == "" {
			cur = supportedPriceCurrency
		}
		if cur != supportedPriceCurrency {
			return fmt.Errorf("pricing %q: unsupported currency %q (only %q supported)", key, p.Currency, supportedPriceCurrency)
		}
		if p.InputUSDPerMillion < 0 {
			return fmt.Errorf("pricing %q: input_usd_per_million must be >= 0", key)
		}
		if p.OutputUSDPerMillion < 0 {
			return fmt.Errorf("pricing %q: output_usd_per_million must be >= 0", key)
		}
	}
	return nil
}

// SummarizerConfig selects the model used to summarize fetched benchmark pages
// into structured capability scores. There is no built-in default; it must be
// configured explicitly so the app never relies on a hardcoded model id.
type SummarizerConfig struct {
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`
	Model    string `yaml:"model,omitempty" json:"model,omitempty"`
}

// WebSearchConfig configures the live web-search backend used to gather
// independent benchmark evidence. The defaults use DuckDuckGo's HTML endpoint,
// which needs no API key. In environments where DuckDuckGo is blocked (e.g.
// country-level bans), set Endpoint to an alternative search URL and Method
// accordingly. In environments behind a TLS-intercepting proxy, set
// insecure_skip_verify (or a proxy) so the search can complete.
//
// The effective insecure state is the OR of the config InsecureSkipVerify
// and the TERMROUTER_WEBSEARCH_INSECURE environment variable. Validation
// checks the effective state, not just the config field, to prevent env-var
// bypass of public-hosting safeguards.
type WebSearchConfig struct {
	// Endpoint overrides the search URL (e.g. an alternative engine or proxy).
	Endpoint string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	// Method is the HTTP method: "POST" (default, DuckDuckGo HTML native) or
	// "GET". Some proxies reject one or the other.
	Method string `yaml:"method,omitempty" json:"method,omitempty"`
	// InsecureSkipVerify disables TLS certificate verification (use only behind
	// a trusted TLS-intercepting proxy).
	InsecureSkipVerify bool `yaml:"insecure_skip_verify,omitempty" json:"insecure_skip_verify,omitempty"`
	// CABundle is an optional path to a CA certificate bundle (PEM) for
	// verifying custom TLS certificates (e.g. a corporate TLS-intercepting
	// proxy). When set, the system cert pool is supplemented with these CAs.
	CABundle string `yaml:"ca_bundle,omitempty" json:"ca_bundle,omitempty"`
	// UnsafeTLSOverride permits InsecureSkipVerify when public_hosting is
	// enabled. Must be explicitly set to true; a warning is emitted at startup.
	UnsafeTLSOverride bool `yaml:"unsafe_tls_override,omitempty" json:"unsafe_tls_override,omitempty"`
	// Proxy is an optional HTTP(S) proxy URL for all web-search traffic.
	Proxy string `yaml:"proxy,omitempty" json:"proxy,omitempty"`
	// TimeoutSeconds bounds each request (default 30).
	TimeoutSeconds int `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	// FallbackEndpoints are alternative search URLs tried (in order) if the
	// primary Endpoint fails. Used when the default engine is blocked (e.g.
	// country-level bans). These are infrastructure endpoints, not model data.
	FallbackEndpoints []string `yaml:"fallback_endpoints,omitempty" json:"fallback_endpoints,omitempty"`
}

// EffectiveInsecure returns true if either the config InsecureSkipVerify flag
// is set or the TERMROUTER_WEBSEARCH_INSECURE environment variable is "1".
// This ensures that an env-var-only bypass cannot circumvent public-hosting
// safeguards that are validated before runtime construction.
func (w WebSearchConfig) EffectiveInsecure() bool {
	if w.InsecureSkipVerify {
		return true
	}
	return os.Getenv("TERMROUTER_WEBSEARCH_INSECURE") == "1"
}

type ServerConfig struct {
	Host             string   `yaml:"host" json:"host"`
	Port             int      `yaml:"port" json:"port"`
	AuthRequired     bool     `yaml:"auth_required" json:"auth_required"`
	RequestTimeout   Duration `yaml:"request_timeout" json:"request_timeout"`
	MaxRequestSize   string   `yaml:"max_request_size" json:"max_request_size"`
	MaxConcurrency   int      `yaml:"max_concurrency" json:"max_concurrency"`
	MaxMessages      int      `yaml:"max_messages,omitempty" json:"max_messages,omitempty"`
	MaxTools         int      `yaml:"max_tools,omitempty" json:"max_tools,omitempty"`
	StrictMode       bool     `yaml:"strict_mode" json:"strict_mode"`
	InsecureRemote   bool     `yaml:"insecure_remote" json:"insecure_remote"`
	AllowDirectModel bool     `yaml:"allow_direct_model" json:"allow_direct_model"`
	// TrustedProxies are CIDRs whose X-Forwarded-For / X-Real-IP headers are trusted.
	// Only configure when TermRouter sits behind a reverse proxy on a private path
	// (e.g. 127.0.0.1/32 for local Caddy). Never leave empty and trust arbitrary headers.
	TrustedProxies []string `yaml:"trusted_proxies,omitempty" json:"trusted_proxies,omitempty"`
}

// PublicHostingConfig documents and validates a public VPS reverse-proxy deployment.
// TermRouter itself still binds to loopback; Caddy (or equivalent) is the public edge.
type PublicHostingConfig struct {
	Enabled      bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	ExternalURL  string `yaml:"external_url,omitempty" json:"external_url,omitempty"`
	ExposeHealth bool   `yaml:"expose_health,omitempty" json:"expose_health,omitempty"`
	ExposeReady  bool   `yaml:"expose_ready,omitempty" json:"expose_ready,omitempty"`
	// ConsolePublic must remain false. The management Console is loopback-only.
	ConsolePublic bool `yaml:"console_public,omitempty" json:"console_public,omitempty"`
}

type CredentialsConfig struct {
	Backend string `yaml:"backend" json:"backend"` // keyring | vault | env
}

type ProviderConfig struct {
	Type          string            `yaml:"type" json:"type"` // openai | anthropic | openai-compatible
	BaseURL       string            `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	CredentialRef string            `yaml:"credential_ref,omitempty" json:"credential_ref,omitempty"`
	Headers       map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Enabled       *bool             `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Timeout       Duration          `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	// Capabilities declares provider-native feature support used by the
	// optimization layer (currently prompt caching).
	Capabilities *ProviderCapabilities `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
}

func (p ProviderConfig) IsEnabled() bool {
	if p.Enabled == nil {
		return true
	}
	return *p.Enabled
}

// SanitizedProviderConfig is a safe-for-export copy of ProviderConfig
// with all secret values redacted.
type SanitizedProviderConfig struct {
	Type          string            `json:"type"`
	BaseURL       string            `json:"base_url,omitempty"`
	CredentialRef string            `json:"credential_ref,omitempty"`
	Enabled       *bool             `json:"enabled,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
}

// SanitizeProviderConfig returns a copy safe for external display.
// Credential target is redacted after the scheme and all header values
// are replaced with "[redacted]".
func SanitizeProviderConfig(p ProviderConfig) SanitizedProviderConfig {
	s := SanitizedProviderConfig{
		Type:    p.Type,
		BaseURL: p.BaseURL,
		Enabled: p.Enabled,
	}
	if p.CredentialRef != "" {
		if i := strings.Index(p.CredentialRef, "://"); i >= 0 {
			s.CredentialRef = p.CredentialRef[:i+3] + "[redacted]"
		} else {
			s.CredentialRef = "[redacted]"
		}
	}
	if len(p.Headers) > 0 {
		s.Headers = make(map[string]string, len(p.Headers))
		for k := range p.Headers {
			s.Headers[k] = "[redacted]"
		}
	}
	return s
}

type RouteConfig struct {
	Strategy   string            `yaml:"strategy" json:"strategy"` // direct | fallback | smart
	Targets    []TargetConfig    `yaml:"targets,omitempty" json:"targets,omitempty"`
	Candidates []CandidateConfig `yaml:"candidates,omitempty" json:"candidates,omitempty"` // smart routes
	Smart      *SmartConfig      `yaml:"smart,omitempty" json:"smart,omitempty"`
	// Default target for smart routes (provider:model also accepted via Default string).
	Default string `yaml:"default,omitempty" json:"default,omitempty"`
}

type TargetConfig struct {
	Provider string   `yaml:"provider" json:"provider"`
	Model    string   `yaml:"model" json:"model"`
	Timeout  Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Weight   int      `yaml:"weight,omitempty" json:"weight,omitempty"`
}

// CandidateConfig is a smart-route candidate target.
type CandidateConfig struct {
	Provider string `yaml:"provider" json:"provider"`
	Model    string `yaml:"model" json:"model"`
	Profile  string `yaml:"profile,omitempty" json:"profile,omitempty"`
}

// SmartConfig configures task-aware selection for strategy: smart.
type SmartConfig struct {
	Mode                string                `yaml:"mode,omitempty" json:"mode,omitempty"` // off | shadow | live
	Policy              string                `yaml:"policy,omitempty" json:"policy,omitempty"`
	Classifier          SmartClassifierConfig `yaml:"classifier,omitempty" json:"classifier,omitempty"`
	ConfidenceThreshold float64               `yaml:"confidence_threshold,omitempty" json:"confidence_threshold,omitempty"`
	LowConfidenceTarget string                `yaml:"low_confidence_target,omitempty" json:"low_confidence_target,omitempty"` // provider:model
	MinimumTaskMatch    float64               `yaml:"minimum_task_match,omitempty" json:"minimum_task_match,omitempty"`
	StrictProfiles      *bool                 `yaml:"strict_profiles,omitempty" json:"strict_profiles,omitempty"`
	SessionAffinity     SessionAffinityConfig `yaml:"session_affinity,omitempty" json:"session_affinity,omitempty"`
	Logging             SmartLoggingConfig    `yaml:"logging,omitempty" json:"logging,omitempty"`
}

type SmartClassifierConfig struct {
	Type    string `yaml:"type,omitempty" json:"type,omitempty"` // heuristic | llm
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
}

type SessionAffinityConfig struct {
	Enabled *bool    `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	TTL     Duration `yaml:"ttl,omitempty" json:"ttl,omitempty"`
}

type SmartLoggingConfig struct {
	StoreTaskProfile *bool `yaml:"store_task_profile,omitempty" json:"store_task_profile,omitempty"`
	StorePrompt      *bool `yaml:"store_prompt,omitempty" json:"store_prompt,omitempty"`
}

// ModelProfileConfig stores layered, field-level capability baselines for a
// single provider/model. Resolution merges layers per field with precedence:
// user override > local assessment > external consensus > built-in (runtime).
// Legacy flat profiles (top-level source/capabilities/properties) are migrated
// into the correct baseline on Load via NormalizeProfiles.
type ModelProfileConfig struct {
	ExternalBaseline   *ProfileBaseline `yaml:"external_baseline,omitempty" json:"external_baseline,omitempty"`
	AssessmentBaseline *ProfileBaseline `yaml:"assessment_baseline,omitempty" json:"assessment_baseline,omitempty"`
	UserOverrides      *ProfileBaseline `yaml:"user_overrides,omitempty" json:"user_overrides,omitempty"`

	// Legacy flat fields (transient; migrated on Normalize). Not authoritative.
	Source       string                `yaml:"source,omitempty" json:"-"`
	Version      string                `yaml:"version,omitempty" json:"-"`
	Capabilities map[string]float64    `yaml:"capabilities,omitempty" json:"-"`
	Properties   ModelPropertiesConfig `yaml:"properties,omitempty" json:"-"`
}

// ProfileBaseline is one provenance layer of a model profile.
type ProfileBaseline struct {
	Version      string                 `yaml:"version,omitempty" json:"version,omitempty"`
	Capabilities map[string]float64     `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Confidence   map[string]float64     `yaml:"confidence,omitempty" json:"confidence,omitempty"`
	Properties   *ModelPropertiesConfig `yaml:"properties,omitempty" json:"properties,omitempty"`
}

type ModelPropertiesConfig struct {
	Vision           *bool  `yaml:"vision,omitempty" json:"vision,omitempty"`
	Tools            *bool  `yaml:"tools,omitempty" json:"tools,omitempty"`
	ParallelTools    *bool  `yaml:"parallel_tools,omitempty" json:"parallel_tools,omitempty"`
	StructuredOutput *bool  `yaml:"structured_output,omitempty" json:"structured_output,omitempty"`
	Streaming        *bool  `yaml:"streaming,omitempty" json:"streaming,omitempty"`
	ContextWindow    int    `yaml:"context_window,omitempty" json:"context_window,omitempty"`
	MaxOutputTokens  int    `yaml:"max_output_tokens,omitempty" json:"max_output_tokens,omitempty"`
	CostTier         int    `yaml:"cost_tier,omitempty" json:"cost_tier,omitempty"`
	LatencyTier      int    `yaml:"latency_tier,omitempty" json:"latency_tier,omitempty"`
	Privacy          string `yaml:"privacy,omitempty" json:"privacy,omitempty"`
}

type AliasConfig struct {
	Route string `yaml:"route,omitempty" json:"route,omitempty"`
	// Direct target shorthand (provider + model) when no route is used.
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`
	Model    string `yaml:"model,omitempty" json:"model,omitempty"`
}

type LoggingConfig struct {
	Level         string `yaml:"level" json:"level"`
	Payloads      string `yaml:"payloads" json:"payloads"` // metadata-only | errors | full | off
	RetentionDays int    `yaml:"retention_days" json:"retention_days"`
}

// Duration wraps time.Duration for YAML marshaling.
type Duration time.Duration

func (d Duration) Duration() time.Duration { return time.Duration(d) }

func (d Duration) MarshalJSON() ([]byte, error) {
	return []byte(`"` + time.Duration(d).String() + `"`), nil
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	s := string(data)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	if s == "" || s == "0" || s == `"0"` {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		var n int64
		if err2 := value.Decode(&n); err2 != nil {
			return err
		}
		*d = Duration(time.Duration(n) * time.Second)
		return nil
	}
	if s == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Default returns a production-safe default configuration.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Host:           "127.0.0.1",
			Port:           8787,
			AuthRequired:   true,
			RequestTimeout: Duration(180 * time.Second),
			MaxRequestSize: "20MiB",
			MaxConcurrency: 64,
			MaxMessages:    200,
			MaxTools:       64,
			StrictMode:     true,
			// Trust only the local reverse proxy by default (Caddy on loopback).
			TrustedProxies: []string{"127.0.0.1/32", "::1/128"},
		},
		Credentials:   CredentialsConfig{Backend: "vault"},
		Providers:     map[string]ProviderConfig{},
		Routes:        map[string]RouteConfig{},
		Aliases:       map[string]AliasConfig{},
		ModelProfiles: map[string]ModelProfileConfig{},
		Optimization:  DefaultOptimization(),
		Logging: LoggingConfig{
			Level:         "info",
			Payloads:      "metadata-only",
			RetentionDays: 14,
		},
		PublicHosting: PublicHostingConfig{
			ExposeHealth:  true,
			ExposeReady:   false,
			ConsolePublic: false,
		},
	}
}

// Load reads and validates a config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.NormalizeProfiles()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes the configuration atomically with restrictive permissions.
func Save(path string, cfg *Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Addr returns host:port for the server.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// NormalizeProfiles migrates legacy flat model_profiles into layered baselines.
// It is idempotent: already-layered profiles (any baseline set) are left intact,
// and legacy fields are cleared after migration so they are not re-serialized.
func (c *Config) NormalizeProfiles() {
	if c.ModelProfiles == nil {
		return
	}
	for id, mp := range c.ModelProfiles {
		// Already layered: trust the baselines.
		if mp.ExternalBaseline != nil || mp.AssessmentBaseline != nil || mp.UserOverrides != nil {
			// Clear transient legacy fields to avoid duplicate serialization.
			mp.Source, mp.Version, mp.Capabilities, mp.Properties = "", "", nil, ModelPropertiesConfig{}
			c.ModelProfiles[id] = mp
			continue
		}
		// Legacy flat profile: migrate based on declared source.
		if len(mp.Capabilities) == 0 && mp.Properties == (ModelPropertiesConfig{}) && mp.Source == "" && mp.Version == "" {
			continue
		}
		var target **ProfileBaseline
		switch mp.Source {
		case "external-consensus", "external":
			target = &mp.ExternalBaseline
		case "self-assessment", "assessment":
			target = &mp.AssessmentBaseline
		default:
			// user override, builtin, unknown/empty -> user overrides
			// (built-in catalog is resolved at runtime; persisted legacy
			// profiles are treated as user overrides per compatibility rule).
			target = &mp.UserOverrides
		}
		bl := &ProfileBaseline{
			Version:      mp.Version,
			Capabilities: mp.Capabilities,
			Properties:   &ModelPropertiesConfig{},
		}
		*bl.Properties = mp.Properties
		*target = bl
		// Clear legacy fields.
		mp.Source, mp.Version, mp.Capabilities, mp.Properties = "", "", nil, ModelPropertiesConfig{}
		c.ModelProfiles[id] = mp
	}
}

// Validate checks configuration integrity (Appendix C).
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be 1-65535")
	}
	if c.Server.Host == "" {
		return fmt.Errorf("server.host is required")
	}
	if c.Server.MaxMessages < 0 {
		return fmt.Errorf("server.max_messages must be >= 0")
	}
	if c.Server.MaxTools < 0 {
		return fmt.Errorf("server.max_tools must be >= 0")
	}
	for _, cidr := range c.Server.TrustedProxies {
		if _, _, err := parseCIDROrIP(cidr); err != nil {
			return fmt.Errorf("server.trusted_proxies: invalid entry %q: %w", cidr, err)
		}
	}
	if c.PublicHosting.ConsolePublic {
		return fmt.Errorf("public_hosting.console_public must be false; the Console is loopback-only and must not be exposed publicly")
	}
	if c.PublicHosting.Enabled {
		if c.PublicHosting.ExternalURL != "" {
			u := strings.TrimSpace(c.PublicHosting.ExternalURL)
			if !strings.HasPrefix(u, "https://") {
				return fmt.Errorf("public_hosting.external_url must use https:// when set")
			}
		}
		// Public hosting assumes loopback backend + reverse proxy. TermRouter must
		// always bind to loopback in this mode; no other setting (including
		// insecure_remote) may override this. The reverse proxy is the public edge.
		if !isLoopbackHost(c.Server.Host) {
			return fmt.Errorf("public_hosting.enabled requires server.host to be loopback (127.0.0.1, ::1, or localhost) behind a reverse proxy; insecure_remote cannot override this")
		}
		if !c.Server.AuthRequired {
			return fmt.Errorf("public_hosting.enabled requires server.auth_required: true")
		}
	}
	backend := strings.ToLower(c.Credentials.Backend)
	switch backend {
	case "keyring", "vault", "env":
	default:
		return fmt.Errorf("credentials.backend must be keyring, vault, or env; got %q", c.Credentials.Backend)
	}
	c.Credentials.Backend = backend

	for name, p := range c.Providers {
		if !slugRE.MatchString(name) {
			return fmt.Errorf("provider %q: id must be lowercase slug [a-z][a-z0-9_-]*", name)
		}
		switch p.Type {
		case "openai", "anthropic", "openai-compatible":
		default:
			return fmt.Errorf("provider %q: unknown type %q", name, p.Type)
		}
		if p.Type == "openai-compatible" && p.BaseURL == "" {
			return fmt.Errorf("provider %q: base_url is required for openai-compatible", name)
		}
		if p.CredentialRef != "" {
			if err := validateCredRef(p.CredentialRef); err != nil {
				return fmt.Errorf("provider %q: %w", name, err)
			}
		}
	}

	for name, r := range c.Routes {
		if !slugRE.MatchString(name) {
			return fmt.Errorf("route %q: id must be lowercase slug", name)
		}
		strategy := r.Strategy
		if strategy == "" {
			if len(r.Candidates) > 0 || r.Smart != nil {
				strategy = "smart"
			} else if len(r.Targets) > 1 {
				strategy = "fallback"
			} else {
				strategy = "direct"
			}
		}
		switch strategy {
		case "direct", "fallback", "smart":
		default:
			return fmt.Errorf("route %q: strategy must be direct, fallback, or smart", name)
		}
		if strategy == "smart" {
			if len(r.Candidates) == 0 && len(r.Targets) == 0 {
				return fmt.Errorf("route %q: smart strategy requires candidates or targets", name)
			}
			cands := r.Candidates
			if len(cands) == 0 {
				// allow targets as candidates shorthand
				for _, t := range r.Targets {
					cands = append(cands, CandidateConfig{Provider: t.Provider, Model: t.Model})
				}
			}
			for i, t := range cands {
				if t.Provider == "" || t.Model == "" {
					return fmt.Errorf("route %q candidate[%d]: provider and model are required", name, i)
				}
				if _, ok := c.Providers[t.Provider]; !ok {
					return fmt.Errorf("route %q candidate[%d]: unknown provider %q", name, i, t.Provider)
				}
			}
			if r.Smart != nil {
				mode := strings.ToLower(r.Smart.Mode)
				if mode != "" && mode != "off" && mode != "shadow" && mode != "live" {
					return fmt.Errorf("route %q: smart.mode must be off, shadow, or live", name)
				}
				if r.Smart.Policy != "" {
					switch strings.ToLower(r.Smart.Policy) {
					case "balanced", "quality", "economy", "fast", "private":
					default:
						return fmt.Errorf("route %q: unknown smart.policy %q", name, r.Smart.Policy)
					}
				}
			}
		} else {
			if len(r.Targets) == 0 {
				return fmt.Errorf("route %q: at least one target is required", name)
			}
			for i, t := range r.Targets {
				if t.Provider == "" || t.Model == "" {
					return fmt.Errorf("route %q target[%d]: provider and model are required", name, i)
				}
				if _, ok := c.Providers[t.Provider]; !ok {
					return fmt.Errorf("route %q target[%d]: unknown provider %q", name, i, t.Provider)
				}
			}
		}
	}

	for id, mp := range c.ModelProfiles {
		baselines := []*ProfileBaseline{mp.ExternalBaseline, mp.AssessmentBaseline, mp.UserOverrides}
		for _, bl := range baselines {
			if bl == nil {
				continue
			}
			for cap, v := range bl.Capabilities {
				if v < 0 || v > 10 {
					return fmt.Errorf("model_profiles %q: capability %q must be 0–10", id, cap)
				}
			}
			if bl.Properties != nil {
				if bl.Properties.CostTier < 0 || bl.Properties.CostTier > 5 {
					return fmt.Errorf("model_profiles %q: cost_tier must be 0–5", id)
				}
				if bl.Properties.LatencyTier < 0 || bl.Properties.LatencyTier > 5 {
					return fmt.Errorf("model_profiles %q: latency_tier must be 0–5", id)
				}
				if bl.Properties.Privacy != "" {
					switch bl.Properties.Privacy {
					case "local", "private-cloud", "cloud":
					default:
						return fmt.Errorf("model_profiles %q: privacy must be local, private-cloud, or cloud", id)
					}
				}
			}
		}
	}

	if c.WebSearch.EffectiveInsecure() && c.PublicHosting.Enabled && !c.WebSearch.UnsafeTLSOverride {
		return fmt.Errorf("web_search.insecure_skip_verify requires web_search.unsafe_tls_override: true in public-hosting mode")
	}

	if err := c.ValidatePricing(); err != nil {
		return err
	}

	if err := c.ValidateOptimization(); err != nil {
		return err
	}

	for name, a := range c.Aliases {
		if !slugRE.MatchString(name) {
			return fmt.Errorf("alias %q: id must be lowercase slug", name)
		}
		if a.Route != "" {
			if _, ok := c.Routes[a.Route]; !ok {
				return fmt.Errorf("alias %q: unknown route %q", name, a.Route)
			}
		} else if a.Provider != "" {
			if _, ok := c.Providers[a.Provider]; !ok {
				return fmt.Errorf("alias %q: unknown provider %q", name, a.Provider)
			}
			if a.Model == "" {
				return fmt.Errorf("alias %q: model is required when using direct provider", name)
			}
		} else {
			return fmt.Errorf("alias %q: must set route or provider+model", name)
		}
	}
	return nil
}

// ValidateOptimization checks the token-optimization configuration.
func (c *Config) ValidateOptimization() error {
	o := c.Optimization
	if !o.Enabled {
		return nil
	}
	if _, err := ParseOptimizationMode(string(o.DefaultMode)); err != nil {
		return err
	}
	if o.TokenEstimation.FallbackCharsPerToken <= 0 {
		return fmt.Errorf("optimization.token_estimation.fallback_chars_per_token must be > 0")
	}
	if o.TokenEstimation.SafetyMultiplier <= 0 {
		return fmt.Errorf("optimization.token_estimation.safety_multiplier must be > 0")
	}
	if o.Conversation.Enabled {
		if o.Conversation.RecentTurnsFull < 0 {
			return fmt.Errorf("optimization.conversation.recent_turns_full must be >= 0")
		}
		if o.Conversation.TargetTokens > 0 && o.Conversation.TriggerTokens > 0 && o.Conversation.TargetTokens >= o.Conversation.TriggerTokens {
			return fmt.Errorf("optimization.conversation.target_tokens must be < trigger_tokens")
		}
	}
	switch strings.ToLower(strings.TrimSpace(o.Output.Mode)) {
	case "", "off", "adaptive":
	default:
		return fmt.Errorf("optimization.output.mode must be off or adaptive")
	}
	if o.SemanticCompression.Enabled && o.SemanticCompression.FailureMode != "bypass" && o.SemanticCompression.FailureMode != "reject" {
		return fmt.Errorf("optimization.semantic_compression.failure_mode must be bypass or reject")
	}
	for name, comp := range o.Compressors {
		if !comp.Enabled {
			continue
		}
		switch strings.ToLower(comp.Transport) {
		case "http", "unix":
		default:
			return fmt.Errorf("optimization.compressors.%s: transport must be http or unix", name)
		}
		if strings.ToLower(comp.Transport) == "http" {
			if comp.Endpoint == "" {
				return fmt.Errorf("optimization.compressors.%s: endpoint is required for http transport", name)
			}
			if !comp.AllowNonLoopback {
				if u, err := url.Parse(comp.Endpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
					return fmt.Errorf("optimization.compressors.%s: endpoint must be an http(s) URL", name)
				}
			}
		}
		const maxSafe int = 64 << 20
		if comp.MaxRequestBytes <= 0 || comp.MaxRequestBytes > maxSafe {
			return fmt.Errorf("optimization.compressors.%s: max_request_bytes must be > 0 and <= 64MiB", name)
		}
		if comp.MaxResponseBytes <= 0 || comp.MaxResponseBytes > maxSafe {
			return fmt.Errorf("optimization.compressors.%s: max_response_bytes must be > 0 and <= 64MiB", name)
		}
	}
	return nil
}

func validateCredRef(ref string) error {
	switch {
	case strings.HasPrefix(ref, "keyring://"):
	case strings.HasPrefix(ref, "vault://"):
	case strings.HasPrefix(ref, "env://"):
	case ref == "none://":
	default:
		return fmt.Errorf("credential_ref must use keyring://, vault://, env://, or none://")
	}
	return nil
}

// parseCIDROrIP accepts "127.0.0.1/32", "10.0.0.0/8", or a bare IP (treated as /32 or /128).
func parseCIDROrIP(s string) (network string, bits int, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, fmt.Errorf("empty")
	}
	if strings.Contains(s, "/") {
		_, n, e := net.ParseCIDR(s)
		if e != nil {
			return "", 0, e
		}
		ones, _ := n.Mask.Size()
		return n.IP.String(), ones, nil
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return "", 0, fmt.Errorf("not an IP or CIDR")
	}
	if ip.To4() != nil {
		return ip.String(), 32, nil
	}
	return ip.String(), 128, nil
}

func isLoopbackHost(host string) bool {
	if host == "127.0.0.1" || host == "::1" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ExportSanitized returns a copy safe for export (credential refs redacted to scheme only).
func (c *Config) ExportSanitized() *Config {
	out := *c
	out.Providers = make(map[string]ProviderConfig, len(c.Providers))
	for k, p := range c.Providers {
		sp := SanitizeProviderConfig(p)
		out.Providers[k] = ProviderConfig{
			Type:          sp.Type,
			BaseURL:       sp.BaseURL,
			CredentialRef: sp.CredentialRef,
			Enabled:       sp.Enabled,
			Headers:       sp.Headers,
			Timeout:       p.Timeout,
			Capabilities:  p.Capabilities,
		}
	}
	out.Routes = make(map[string]RouteConfig, len(c.Routes))
	for k, v := range c.Routes {
		out.Routes[k] = v
	}
	out.Aliases = make(map[string]AliasConfig, len(c.Aliases))
	for k, v := range c.Aliases {
		out.Aliases[k] = v
	}
	out.ModelProfiles = make(map[string]ModelProfileConfig, len(c.ModelProfiles))
	for k, v := range c.ModelProfiles {
		out.ModelProfiles[k] = v
	}
	return &out
}

// ParseProviderModel splits "provider:model" or "provider/model".
func ParseProviderModel(s string) (provider, model string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", fmt.Errorf("empty provider:model")
	}
	if i := strings.IndexByte(s, ':'); i > 0 {
		return s[:i], s[i+1:], nil
	}
	if i := strings.IndexByte(s, '/'); i > 0 {
		return s[:i], s[i+1:], nil
	}
	return "", "", fmt.Errorf("invalid target %q (use provider:model)", s)
}

// ParseMaxRequestSize parses size strings like "20MiB".
func ParseMaxRequestSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 20 << 20, nil
	}
	multipliers := []struct {
		suffix string
		mult   int64
	}{
		{"GiB", 1 << 30}, {"GB", 1e9},
		{"MiB", 1 << 20}, {"MB", 1e6},
		{"KiB", 1 << 10}, {"KB", 1e3},
		{"B", 1},
	}
	for _, m := range multipliers {
		if strings.HasSuffix(s, m.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(s, m.suffix))
			var n int64
			if _, err := fmt.Sscanf(num, "%d", &n); err != nil {
				return 0, fmt.Errorf("invalid size %q", s)
			}
			return n * m.mult, nil
		}
	}
	var n int64
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return n, nil
}
