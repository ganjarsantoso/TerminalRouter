package external

import (
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
}

// ImportRecord is a persisted import event.
type ImportRecord struct {
	ProfileID    string             `json:"profile_id"`
	ProposalID   string             `json:"proposal_id"`
	AppliedAt    time.Time          `json:"applied_at"`
	Capabilities map[string]float64 `json:"capabilities"`
}

// Service is the external-evidence service used by console and CLI.
type Service struct {
	store Provider
}

// NewService builds a Service backed by the given persistence provider.
func NewService(store Provider) *Service {
	return &Service{store: store}
}

// RegistryInfo returns metadata about the bundled curated registry.
func (s *Service) RegistryInfo() RegistryInfo {
	return RegistryInfo{
		Version:      registryVersion,
		UpdatedAt:    registryUpdatedAt,
		SourceCount:  len(sources),
		ModelCount:   len(identities),
		EvidenceCount: len(sampleEvidence),
		Sources:      sources,
	}
}

// Search looks up a model by provider/model and returns its consensus profile
// (nil when unresolved).
func (s *Service) Search(providerID, modelID string) (*ConsensusProfile, bool) {
	id, ok := ResolveIdentity(providerID, modelID)
	if !ok {
		return nil, false
	}
	cp := buildConsensus(id)
	return &cp, true
}

// Estimate is an alias for Search returning the consensus profile.
func (s *Service) Estimate(providerID, modelID string) (*ConsensusProfile, bool) {
	return s.Search(providerID, modelID)
}

// BuildProposal constructs a reviewable proposal for a model, comparing the
// external consensus against the model's current profile values.
// current is the map of capability -> current value (0 if absent).
func (s *Service) BuildProposal(providerID, modelID string, current map[string]float64) (*Proposal, bool) {
	cp, ok := s.Search(providerID, modelID)
	if !ok {
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
