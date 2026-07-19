package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var slugRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

// Config is the human-editable TermRouter configuration (no plaintext secrets).
type Config struct {
	Server        ServerConfig                `yaml:"server" json:"server"`
	Credentials   CredentialsConfig           `yaml:"credentials" json:"credentials"`
	Providers     map[string]ProviderConfig   `yaml:"providers" json:"providers"`
	Routes        map[string]RouteConfig      `yaml:"routes" json:"routes"`
	Aliases       map[string]AliasConfig      `yaml:"aliases" json:"aliases"`
	ModelProfiles map[string]ModelProfileConfig `yaml:"model_profiles,omitempty" json:"model_profiles,omitempty"`
	Logging       LoggingConfig               `yaml:"logging" json:"logging"`
}

type ServerConfig struct {
	Host            string        `yaml:"host" json:"host"`
	Port            int           `yaml:"port" json:"port"`
	AuthRequired    bool          `yaml:"auth_required" json:"auth_required"`
	RequestTimeout  Duration      `yaml:"request_timeout" json:"request_timeout"`
	MaxRequestSize  string        `yaml:"max_request_size" json:"max_request_size"`
	MaxConcurrency  int           `yaml:"max_concurrency" json:"max_concurrency"`
	StrictMode      bool          `yaml:"strict_mode" json:"strict_mode"`
	InsecureRemote  bool          `yaml:"insecure_remote" json:"insecure_remote"`
	AllowDirectModel bool         `yaml:"allow_direct_model" json:"allow_direct_model"`
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
}

func (p ProviderConfig) IsEnabled() bool {
	if p.Enabled == nil {
		return true
	}
	return *p.Enabled
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
	Provider  string   `yaml:"provider" json:"provider"`
	Model     string   `yaml:"model" json:"model"`
	Timeout   Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Weight    int      `yaml:"weight,omitempty" json:"weight,omitempty"`
}

// CandidateConfig is a smart-route candidate target.
type CandidateConfig struct {
	Provider string `yaml:"provider" json:"provider"`
	Model    string `yaml:"model" json:"model"`
	Profile  string `yaml:"profile,omitempty" json:"profile,omitempty"`
}

// SmartConfig configures task-aware selection for strategy: smart.
type SmartConfig struct {
	Mode                string                 `yaml:"mode,omitempty" json:"mode,omitempty"` // off | shadow | live
	Policy              string                 `yaml:"policy,omitempty" json:"policy,omitempty"`
	Classifier          SmartClassifierConfig  `yaml:"classifier,omitempty" json:"classifier,omitempty"`
	ConfidenceThreshold float64                `yaml:"confidence_threshold,omitempty" json:"confidence_threshold,omitempty"`
	LowConfidenceTarget string                 `yaml:"low_confidence_target,omitempty" json:"low_confidence_target,omitempty"` // provider:model
	MinimumTaskMatch    float64                `yaml:"minimum_task_match,omitempty" json:"minimum_task_match,omitempty"`
	StrictProfiles      *bool                  `yaml:"strict_profiles,omitempty" json:"strict_profiles,omitempty"`
	SessionAffinity     SessionAffinityConfig  `yaml:"session_affinity,omitempty" json:"session_affinity,omitempty"`
	Logging             SmartLoggingConfig     `yaml:"logging,omitempty" json:"logging,omitempty"`
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

// ModelProfileConfig is a user-defined or override model capability profile.
type ModelProfileConfig struct {
	Source       string             `yaml:"source,omitempty" json:"source,omitempty"`
	Version      string             `yaml:"version,omitempty" json:"version,omitempty"`
	Capabilities map[string]float64 `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Properties   ModelPropertiesConfig `yaml:"properties,omitempty" json:"properties,omitempty"`
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
	Route  string `yaml:"route,omitempty" json:"route,omitempty"`
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
			StrictMode:     true,
		},
		Credentials:   CredentialsConfig{Backend: "vault"},
		Providers:     map[string]ProviderConfig{},
		Routes:        map[string]RouteConfig{},
		Aliases:       map[string]AliasConfig{},
		ModelProfiles: map[string]ModelProfileConfig{},
		Logging: LoggingConfig{
			Level:         "info",
			Payloads:      "metadata-only",
			RetentionDays: 14,
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

// Validate checks configuration integrity (Appendix C).
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be 1-65535")
	}
	if c.Server.Host == "" {
		return fmt.Errorf("server.host is required")
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
		for cap, v := range mp.Capabilities {
			if v < 0 || v > 10 {
				return fmt.Errorf("model_profiles %q: capability %q must be 0–10", id, cap)
			}
		}
		if mp.Properties.CostTier < 0 || mp.Properties.CostTier > 5 {
			return fmt.Errorf("model_profiles %q: cost_tier must be 0–5", id)
		}
		if mp.Properties.LatencyTier < 0 || mp.Properties.LatencyTier > 5 {
			return fmt.Errorf("model_profiles %q: latency_tier must be 0–5", id)
		}
		if mp.Properties.Privacy != "" {
			switch mp.Properties.Privacy {
			case "local", "private-cloud", "cloud":
			default:
				return fmt.Errorf("model_profiles %q: privacy must be local, private-cloud, or cloud", id)
			}
		}
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

// ExportSanitized returns a copy safe for export (credential refs redacted to scheme only).
func (c *Config) ExportSanitized() *Config {
	out := *c
	out.Providers = make(map[string]ProviderConfig, len(c.Providers))
	for k, p := range c.Providers {
		cp := p
		if cp.CredentialRef != "" {
			scheme := strings.SplitN(cp.CredentialRef, "://", 2)[0]
			cp.CredentialRef = scheme + "://[redacted]"
		}
		out.Providers[k] = cp
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
