package provider

import (
	"context"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/normalization"
)

// Capability flags for a connection.
type CapabilitySet struct {
	Chat          bool
	Streaming     bool
	Tools         bool
	Vision        bool
	SystemMessage bool
	JSONMode      bool
}

// Model is an upstream model descriptor.
type Model struct {
	ID           string
	DisplayName  string
	Capabilities CapabilitySet
}

// Target is a resolved upstream target for a single attempt.
type Target struct {
	ProviderID string
	Model      string
	Config     config.ProviderConfig
}

// EventStream yields normalized stream events.
type EventStream interface {
	// Recv returns the next event or io.EOF when complete.
	Recv() (normalization.StreamEvent, error)
	// Close releases resources.
	Close() error
}

// Adapter is the provider contract from PRD §8.1.
type Adapter interface {
	Type() string
	Capabilities(conn config.ProviderConfig) CapabilitySet
	Validate(ctx context.Context, conn config.ProviderConfig, credential string) error
	ListModels(ctx context.Context, conn config.ProviderConfig, credential string) ([]Model, error)
	Execute(ctx context.Context, req *normalization.NormalizedRequest, target Target, credential string) (*normalization.NormalizedResponse, error)
	Stream(ctx context.Context, req *normalization.NormalizedRequest, target Target, credential string) (EventStream, error)
	ClassifyError(status int, body []byte, err error) *normalization.Error
}

// Registry maps provider type → adapter.
type Registry struct {
	adapters map[string]Adapter
}

func NewRegistry() *Registry {
	return &Registry{adapters: map[string]Adapter{}}
}

func (r *Registry) Register(a Adapter) {
	r.adapters[a.Type()] = a
}

func (r *Registry) Get(typ string) (Adapter, bool) {
	a, ok := r.adapters[typ]
	return a, ok
}
