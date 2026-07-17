package router

import (
	"fmt"
	"strings"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/provider"
)

// Attempt is one target in an immutable plan.
type Attempt struct {
	ProviderID string
	Model      string
	Config     config.ProviderConfig
	Timeout    config.Duration
}

// Plan is the immutable attempt list for a request.
type Plan struct {
	Alias       string
	Strategy    string
	PublicModel string
	RouteName   string
	Attempts    []Attempt
	// Smart is non-nil when the alias maps to a smart route (before selection applied).
	Smart *SmartPlanMeta
}

// SmartPlanMeta carries smart-route config for the gateway selection stage.
type SmartPlanMeta struct {
	RouteName string
	Route     config.RouteConfig
	Mode      string // off | shadow | live
}

// Resolver maps model/alias names to attempt plans.
type Resolver struct {
	cfg *config.Config
}

func NewResolver(cfg *config.Config) *Resolver {
	return &Resolver{cfg: cfg}
}

// Config returns the underlying config.
func (r *Resolver) Config() *config.Config { return r.cfg }

// Resolve builds an attempt plan for a requested model string.
// Supports: alias, or provider-id/model-id when allowDirect is true.
// For smart routes, returns Strategy "smart" with candidate attempts (config order)
// and Smart metadata; the gateway must run smart selection before execution.
func (r *Resolver) Resolve(requested string, allowDirect bool) (*Plan, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return nil, fmt.Errorf("model is required")
	}
	key := strings.ToLower(requested)

	// Exact alias match (case-insensitive)
	for name, alias := range r.cfg.Aliases {
		if strings.ToLower(name) == key {
			return r.planFromAlias(name, alias)
		}
	}

	// Direct provider/model syntax
	if allowDirect && strings.Contains(requested, "/") {
		parts := strings.SplitN(requested, "/", 2)
		pid, model := parts[0], parts[1]
		p, ok := r.cfg.Providers[pid]
		if !ok {
			return nil, fmt.Errorf("unknown provider %q in model %q", pid, requested)
		}
		if !p.IsEnabled() {
			return nil, fmt.Errorf("provider %q is disabled", pid)
		}
		return &Plan{
			PublicModel: requested,
			Strategy:    "direct",
			Attempts: []Attempt{{
				ProviderID: pid,
				Model:      model,
				Config:     p,
			}},
		}, nil
	}

	return nil, fmt.Errorf("unknown model or alias %q", requested)
}

func (r *Resolver) planFromAlias(name string, alias config.AliasConfig) (*Plan, error) {
	if alias.Route != "" {
		route, ok := r.cfg.Routes[alias.Route]
		if !ok {
			return nil, fmt.Errorf("alias %q references missing route %q", name, alias.Route)
		}
		strategy := route.Strategy
		if strategy == "" {
			if len(route.Candidates) > 0 || route.Smart != nil {
				strategy = "smart"
			} else if len(route.Targets) > 1 {
				strategy = "fallback"
			} else {
				strategy = "direct"
			}
		}

		if strategy == "smart" {
			return r.planSmart(name, alias.Route, route)
		}

		attempts := make([]Attempt, 0, len(route.Targets))
		for _, t := range route.Targets {
			p, ok := r.cfg.Providers[t.Provider]
			if !ok {
				return nil, fmt.Errorf("route %q references unknown provider %q", alias.Route, t.Provider)
			}
			if !p.IsEnabled() {
				continue
			}
			attempts = append(attempts, Attempt{
				ProviderID: t.Provider,
				Model:      t.Model,
				Config:     p,
				Timeout:    t.Timeout,
			})
		}
		if len(attempts) == 0 {
			return nil, fmt.Errorf("route %q has no enabled targets", alias.Route)
		}
		return &Plan{
			Alias:       name,
			Strategy:    strategy,
			PublicModel: name,
			RouteName:   alias.Route,
			Attempts:    attempts,
		}, nil
	}
	// Direct alias → provider+model
	p, ok := r.cfg.Providers[alias.Provider]
	if !ok {
		return nil, fmt.Errorf("alias %q references unknown provider %q", name, alias.Provider)
	}
	if !p.IsEnabled() {
		return nil, fmt.Errorf("provider %q is disabled", alias.Provider)
	}
	return &Plan{
		Alias:       name,
		Strategy:    "direct",
		PublicModel: name,
		Attempts: []Attempt{{
			ProviderID: alias.Provider,
			Model:      alias.Model,
			Config:     p,
		}},
	}, nil
}

func (r *Resolver) planSmart(aliasName, routeName string, route config.RouteConfig) (*Plan, error) {
	mode := "shadow"
	if route.Smart != nil && route.Smart.Mode != "" {
		mode = strings.ToLower(route.Smart.Mode)
	}
	if mode == "off" {
		// Treat as deterministic fallback over candidates/targets
		return r.planDeterministicFromSmart(aliasName, routeName, route)
	}

	cands := route.Candidates
	if len(cands) == 0 {
		for _, t := range route.Targets {
			cands = append(cands, config.CandidateConfig{Provider: t.Provider, Model: t.Model})
		}
	}
	attempts := make([]Attempt, 0, len(cands))
	for _, t := range cands {
		p, ok := r.cfg.Providers[t.Provider]
		if !ok {
			return nil, fmt.Errorf("route %q references unknown provider %q", routeName, t.Provider)
		}
		if !p.IsEnabled() {
			continue
		}
		attempts = append(attempts, Attempt{
			ProviderID: t.Provider,
			Model:      t.Model,
			Config:     p,
		})
	}
	if len(attempts) == 0 {
		return nil, fmt.Errorf("route %q has no enabled candidates", routeName)
	}
	return &Plan{
		Alias:       aliasName,
		Strategy:    "smart",
		PublicModel: aliasName,
		RouteName:   routeName,
		Attempts:    attempts,
		Smart: &SmartPlanMeta{
			RouteName: routeName,
			Route:     route,
			Mode:      mode,
		},
	}, nil
}

func (r *Resolver) planDeterministicFromSmart(aliasName, routeName string, route config.RouteConfig) (*Plan, error) {
	// Use default or first candidate as single direct target when smart is off
	var provider, model string
	if route.Default != "" {
		p, m, err := config.ParseProviderModel(route.Default)
		if err == nil {
			provider, model = p, m
		}
	}
	if provider == "" && route.Smart != nil && route.Smart.LowConfidenceTarget != "" {
		p, m, err := config.ParseProviderModel(route.Smart.LowConfidenceTarget)
		if err == nil {
			provider, model = p, m
		}
	}
	if provider == "" {
		if len(route.Candidates) > 0 {
			provider, model = route.Candidates[0].Provider, route.Candidates[0].Model
		} else if len(route.Targets) > 0 {
			provider, model = route.Targets[0].Provider, route.Targets[0].Model
		}
	}
	p, ok := r.cfg.Providers[provider]
	if !ok {
		return nil, fmt.Errorf("route %q default provider %q missing", routeName, provider)
	}
	return &Plan{
		Alias:       aliasName,
		Strategy:    "direct",
		PublicModel: aliasName,
		RouteName:   routeName,
		Attempts: []Attempt{{
			ProviderID: provider,
			Model:      model,
			Config:     p,
		}},
	}, nil
}

// ToProviderTarget converts an attempt to a provider.Target.
func ToProviderTarget(a Attempt) provider.Target {
	return provider.Target{
		ProviderID: a.ProviderID,
		Model:      a.Model,
		Config:     a.Config,
	}
}

// ListPublicModels returns aliases for /v1/models.
func (r *Resolver) ListPublicModels() []string {
	out := make([]string, 0, len(r.cfg.Aliases))
	for name := range r.cfg.Aliases {
		out = append(out, name)
	}
	return out
}

// BuildAttempts constructs Attempt list from provider/model pairs.
func (r *Resolver) BuildAttempts(pairs []struct{ Provider, Model string }) ([]Attempt, error) {
	var out []Attempt
	for _, pair := range pairs {
		p, ok := r.cfg.Providers[pair.Provider]
		if !ok {
			return nil, fmt.Errorf("unknown provider %q", pair.Provider)
		}
		if !p.IsEnabled() {
			continue
		}
		out = append(out, Attempt{ProviderID: pair.Provider, Model: pair.Model, Config: p})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no enabled attempts")
	}
	return out, nil
}
