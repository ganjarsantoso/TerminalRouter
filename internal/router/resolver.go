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
	Alias      string
	Strategy   string
	PublicModel string
	Attempts   []Attempt
}

// Resolver maps model/alias names to attempt plans.
type Resolver struct {
	cfg *config.Config
}

func NewResolver(cfg *config.Config) *Resolver {
	return &Resolver{cfg: cfg}
}

// Resolve builds an attempt plan for a requested model string.
// Supports: alias, or provider-id/model-id when allowDirect is true.
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
			if len(route.Targets) > 1 {
				strategy = "fallback"
			} else {
				strategy = "direct"
			}
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
