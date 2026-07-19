package storage

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/termrouter/termrouter/internal/smart/external"
)

// SaveExternalProposal persists a proposal (insert or replace by ID).
func (s *Store) SaveExternalProposal(p external.Proposal) error {
	fields, err := json.Marshal(p.Fields)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
INSERT INTO external_profile_proposals
  (id, provider_id, model_id, model_identity, fields_json, created_at, status, registry_version)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  provider_id=excluded.provider_id,
  model_id=excluded.model_id,
  model_identity=excluded.model_identity,
  fields_json=excluded.fields_json,
  status=excluded.status,
  registry_version=excluded.registry_version
`, p.ID, p.ProviderID, p.ModelID, p.ModelIdentity, string(fields),
		p.CreatedAt.UTC().Format(time.RFC3339), p.Status, p.RegistryVersion)
	return err
}

// LoadExternalProposal returns a proposal by ID.
func (s *Store) LoadExternalProposal(id string) (external.Proposal, bool, error) {
	var (
		providerID, modelID, modelIdentity, fieldsJSON, status, registryVersion, createdAt string
	)
	err := s.db.QueryRow(`
SELECT provider_id, model_id, model_identity, fields_json, created_at, status, registry_version
FROM external_profile_proposals WHERE id = ?
`, id).Scan(&providerID, &modelID, &modelIdentity, &fieldsJSON, &createdAt, &status, &registryVersion)
	if err == sql.ErrNoRows {
		return external.Proposal{}, false, nil
	}
	if err != nil {
		return external.Proposal{}, false, err
	}
	var fields []external.ProposalField
	if err := json.Unmarshal([]byte(fieldsJSON), &fields); err != nil {
		return external.Proposal{}, false, err
	}
	ca, _ := time.Parse(time.RFC3339, createdAt)
	return external.Proposal{
		ID:             id,
		ProviderID:     providerID,
		ModelID:        modelID,
		ModelIdentity:  modelIdentity,
		Fields:         fields,
		CreatedAt:      ca,
		Status:         status,
		RegistryVersion: registryVersion,
	}, true, nil
}

// ListExternalProposals returns proposals, optionally filtered by status ("" = all).
func (s *Store) ListExternalProposals(status string) ([]external.Proposal, error) {
	rows, err := s.db.Query(`
SELECT id, provider_id, model_id, model_identity, fields_json, created_at, status, registry_version
FROM external_profile_proposals
` + whereStatus(status) + ` ORDER BY created_at DESC`, statusArg(status)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []external.Proposal
	for rows.Next() {
		var (
			id, providerID, modelID, modelIdentity, fieldsJSON, createdAt, st, registryVersion string
		)
		if err := rows.Scan(&id, &providerID, &modelID, &modelIdentity, &fieldsJSON, &createdAt, &st, &registryVersion); err != nil {
			return nil, err
		}
		var fields []external.ProposalField
		if err := json.Unmarshal([]byte(fieldsJSON), &fields); err != nil {
			return nil, err
		}
		ca, _ := time.Parse(time.RFC3339, createdAt)
		out = append(out, external.Proposal{
			ID: id, ProviderID: providerID, ModelID: modelID, ModelIdentity: modelIdentity,
			Fields: fields, CreatedAt: ca, Status: st, RegistryVersion: registryVersion,
		})
	}
	return out, rows.Err()
}

// DeleteExternalProposal removes a proposal.
func (s *Store) DeleteExternalProposal(id string) error {
	_, err := s.db.Exec(`DELETE FROM external_profile_proposals WHERE id = ?`, id)
	return err
}

// RecordExternalImport logs that a proposal was applied to a profile.
func (s *Store) RecordExternalImport(profileID, proposalID string, capabilities map[string]float64) error {
	capJSON, err := json.Marshal(capabilities)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
INSERT INTO external_profile_imports (profile_id, proposal_id, applied_at, capabilities_json)
VALUES (?, ?, ?, ?)
`, profileID, proposalID, time.Now().UTC().Format(time.RFC3339), string(capJSON))
	return err
}

// ExternalImportHistory returns recorded imports, newest first (limit <= 0 = 50).
func (s *Store) ExternalImportHistory(limit int) ([]external.ImportRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
SELECT profile_id, proposal_id, applied_at, capabilities_json
FROM external_profile_imports ORDER BY applied_at DESC LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []external.ImportRecord
	for rows.Next() {
		var profileID, proposalID, appliedAt, capJSON string
		if err := rows.Scan(&profileID, &proposalID, &appliedAt, &capJSON); err != nil {
			return nil, err
		}
		var caps map[string]float64
		if err := json.Unmarshal([]byte(capJSON), &caps); err != nil {
			return nil, err
		}
		ta, _ := time.Parse(time.RFC3339, appliedAt)
		out = append(out, external.ImportRecord{
			ProfileID: profileID, ProposalID: proposalID, AppliedAt: ta, Capabilities: caps,
		})
	}
	return out, rows.Err()
}

func whereStatus(status string) string {
	if status == "" {
		return ""
	}
	return " WHERE status = ?"
}

func statusArg(status string) []interface{} {
	if status == "" {
		return nil
	}
	return []interface{}{status}
}
