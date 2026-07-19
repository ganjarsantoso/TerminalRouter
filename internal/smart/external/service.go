package external

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Provider defines the persistence surface the ExternalEvidenceService needs.
// It is implemented by internal/storage.Store.
type Provider interface {
	// SaveExternalProposal persists a proposal (insert or update by ID).
	SaveExternalProposal(p Proposal) error
	// LoadExternalProposal returns a proposal by ID.
	LoadExternalProposal(id string) (Proposal, bool, error)
	// ListExternalProposals returns proposals, optionally filtered by status.
	ListExternalProposals(status string) ([]Proposal, error)
	// DeleteExternalProposal removes a proposal.
	DeleteExternalProposal(id string) error
	// RecordExternalImport logs that a proposal was applied to a profile.
	RecordExternalImport(profileID, proposalID string, capabilities map[string]float64) error
	// ExternalImportHistory returns recorded imports, newest first.
	ExternalImportHistory(limit int) ([]ImportRecord, error)

	// CacheExternalEvidence stores fetched evidence for a model identity.
	CacheExternalEvidence(recs []EvidenceRecord) error
	// LoadCachedEvidence returns cached evidence for a model identity (newest first).
	LoadCachedEvidence(modelIdentity string, maxAge time.Duration) ([]EvidenceRecord, bool, error)
}

// Service is the external-evidence service used by console and CLI. Evidence is
// fetched live via the injected Searcher, optionally summarized by an LLM, and
// cached locally (offline-first).
type Service struct {
	store      Provider
	searcher   Searcher
	summarizer Summarizer
}

// NewService builds a Service backed by the given persistence provider and a
// web searcher. If searcher is nil, a default live searcher is used. summarizer
// may be nil (falls back to regex extraction).
func NewService(store Provider, searcher Searcher, summarizer Summarizer) *Service {
	if searcher == nil {
		searcher = DefaultSearcher()
	}
	return &Service{store: store, searcher: searcher, summarizer: summarizer}
}

// RegistryInfo returns metadata about the bundled methodology/registry.
func (s *Service) RegistryInfo() RegistryInfo {
	return RegistryInfo{
		Version:      registryVersion,
		UpdatedAt:    registryUpdatedAt,
		SourceCount:  len(sources),
		ModelCount:   len(identities),
		EvidenceCount: 0,
		Sources:      sources,
	}
}

// Search looks up a model by provider/model, fetching live benchmark evidence
// (using cache when fresh) and returning a consensus profile. Returns
// (nil, false) when the model identity is unknown.
func (s *Service) Search(ctx context.Context, providerID, modelID string) (*ConsensusProfile, bool) {
	id, ok := ResolveIdentity(providerID, modelID)
	if !ok {
		return nil, false
	}

	// Use fresh cache if available.
	if s.store != nil {
		if recs, hit, _ := s.store.LoadCachedEvidence(id.ID, 24*time.Hour); hit && len(recs) > 0 {
			cp := buildConsensus(id, recs)
			return &cp, true
		}
	}

	// Fetch live evidence.
	var all []SearchResult
	for _, q := range searchQueries(id) {
		res, err := s.searcher.Search(ctx, q)
		if err != nil {
			continue
		}
		all = append(all, res...)
	}

	var recs []EvidenceRecord
	if s.summarizer != nil {
		recs = summarizeEvidence(ctx, s.summarizer, s.searcher, id, all)
	}
	if len(recs) == 0 {
		// Fallback to regex extraction when no summarizer is configured.
		recs = extractEvidence(id, all)
	}
	if len(recs) == 0 {
		return nil, true // known model, but no evidence found live
	}
	if s.store != nil {
		_ = s.store.CacheExternalEvidence(recs)
	}
	cp := buildConsensus(id, recs)
	return &cp, true
}

// Estimate is an alias for Search.
func (s *Service) Estimate(ctx context.Context, providerID, modelID string) (*ConsensusProfile, bool) {
	return s.Search(ctx, providerID, modelID)
}

// BuildProposal constructs a reviewable proposal for a model, comparing the
// external consensus against the model's current profile values.
// current is the map of capability -> current value (0 if absent).
func (s *Service) BuildProposal(ctx context.Context, providerID, modelID string, current map[string]float64) (*Proposal, bool) {
	cp, ok := s.Search(ctx, providerID, modelID)
	if !ok {
		return nil, false
	}
	if cp == nil || len(cp.Capabilities) == 0 {
		return nil, false
	}
	var fields []ProposalField
	for _, c := range CapabilityKeys {
		cc, has := cp.Capabilities[c]
		if !has {
			continue
		}
		cur, exists := current[c.String()]
		pf := ProposalField{
			Capability: c,
			Proposed:   cc.Estimate,
			Evidence:   cc.Contributing,
		}
		if exists {
			v := cur
			pf.Current = &v
		}
		fields = append(fields, pf)
	}
	if len(fields) == 0 {
		return nil, false
	}
	p := Proposal{
		ID:             uuid.NewString(),
		ProviderID:     providerID,
		ModelID:        modelID,
		ModelIdentity:  cp.ModelIdentity,
		Fields:         fields,
		Overall:        cp.Overall,
		Confidence:     cp.Confidence,
		Sources:        cp.Sources,
		CreatedAt:      time.Now().UTC(),
		Status:         "pending",
		RegistryVersion: registryVersion,
	}
	return &p, true
}

// SaveProposal persists a proposal.
func (s *Service) SaveProposal(p Proposal) error {
	if p.ID == "" {
		return fmt.Errorf("proposal id required")
	}
	return s.store.SaveExternalProposal(p)
}

// GetProposal loads a proposal by id.
func (s *Service) GetProposal(id string) (Proposal, bool, error) {
	return s.store.LoadExternalProposal(id)
}

// ListProposals lists proposals filtered by status ("" = all).
func (s *Service) ListProposals(status string) ([]Proposal, error) {
	return s.store.ListExternalProposals(status)
}

// DismissProposal marks a proposal dismissed (deletes it).
func (s *Service) DismissProposal(id string) error {
	return s.store.DeleteExternalProposal(id)
}

// ApplyProposal returns the capability map produced by a proposal (caller is
// responsible for writing it into the model profile). It also records the import.
func (s *Service) ApplyProposal(p Proposal) (map[string]float64, error) {
	caps := map[string]float64{}
	for _, f := range p.Fields {
		caps[f.Capability.String()] = f.Proposed
	}
	profileID := p.ProviderID + "/" + p.ModelID
	if err := s.store.RecordExternalImport(profileID, p.ID, caps); err != nil {
		return nil, err
	}
	if err := s.store.DeleteExternalProposal(p.ID); err != nil {
		return nil, err
	}
	return caps, nil
}

// ImportHistory returns recorded imports, newest first.
func (s *Service) ImportHistory(limit int) ([]ImportRecord, error) {
	return s.store.ExternalImportHistory(limit)
}
