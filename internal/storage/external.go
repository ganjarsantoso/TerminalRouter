package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
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
  (id, provider_id, model_id, model_identity, fields_json, created_at, status, registry_version, mandatory_review)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  provider_id=excluded.provider_id,
  model_id=excluded.model_id,
  model_identity=excluded.model_identity,
  fields_json=excluded.fields_json,
  status=excluded.status,
  registry_version=excluded.registry_version,
  mandatory_review=excluded.mandatory_review
`, p.ID, p.ProviderID, p.ModelID, p.ModelIdentity, string(fields),
		p.CreatedAt.UTC().Format(time.RFC3339), p.Status, p.RegistryVersion, boolToInt(p.MandatoryReview))
	return err
}

// LoadExternalProposal returns a proposal by ID.
func (s *Store) LoadExternalProposal(id string) (external.Proposal, bool, error) {
	var (
		providerID, modelID, modelIdentity, fieldsJSON, status, registryVersion, createdAt string
		mandatoryReview                                                                   int
	)
	err := s.db.QueryRow(`
SELECT provider_id, model_id, model_identity, fields_json, created_at, status, registry_version, mandatory_review
FROM external_profile_proposals WHERE id = ?
`, id).Scan(&providerID, &modelID, &modelIdentity, &fieldsJSON, &createdAt, &status, &registryVersion, &mandatoryReview)
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
		ID:              id,
		ProviderID:      providerID,
		ModelID:         modelID,
		ModelIdentity:   modelIdentity,
		Fields:          fields,
		CreatedAt:       ca,
		Status:          status,
		RegistryVersion: registryVersion,
		MandatoryReview: mandatoryReview != 0,
	}, true, nil
}

// ListExternalProposals returns proposals, optionally filtered by status ("" = all).
func (s *Store) ListExternalProposals(status string) ([]external.Proposal, error) {
	rows, err := s.db.Query(`
SELECT id, provider_id, model_id, model_identity, fields_json, created_at, status, registry_version, mandatory_review
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
			mandatoryReview                                                              int
		)
		if err := rows.Scan(&id, &providerID, &modelID, &modelIdentity, &fieldsJSON, &createdAt, &st, &registryVersion, &mandatoryReview); err != nil {
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
			MandatoryReview: mandatoryReview != 0,
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

// CacheExternalEvidence stores fetched evidence records for a model identity,
// replacing any prior cache for that identity.
func (s *Store) CacheExternalEvidence(recs []external.EvidenceRecord) error {
	if len(recs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM external_evidence_records WHERE model_identity = ?`, recs[0].ModelIdentity); err != nil {
		return err
	}
	for _, r := range recs {
		id := fmt.Sprintf("%s|%s|%s|%s", r.ModelIdentity, r.Source, r.Benchmark, time.Now().UTC().Format(time.RFC3339Nano))
		_, err := tx.Exec(`
INSERT INTO external_evidence_records (id, source, model_identity, benchmark, value, scale, capability, reported_at, url, notes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, id, string(r.Source), r.ModelIdentity, r.Benchmark, r.Value, string(r.Scale), string(r.Capability),
			r.ReportedAt.UTC().Format(time.RFC3339), r.URL, r.Notes)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LoadCachedEvidence returns cached evidence for a model identity if it is
// fresher than maxAge. The boolean is false when stale or absent.
func (s *Store) LoadCachedEvidence(modelIdentity string, maxAge time.Duration) ([]external.EvidenceRecord, bool, error) {
	rows, err := s.db.Query(`
SELECT source, benchmark, value, scale, capability, reported_at, url, notes
FROM external_evidence_records WHERE model_identity = ? ORDER BY reported_at DESC
`, modelIdentity)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []external.EvidenceRecord
	var newest time.Time
	for rows.Next() {
		var source, benchmark, scale, capability, reportedAt, url, notes string
		var value float64
		if err := rows.Scan(&source, &benchmark, &value, &scale, &capability, &reportedAt, &url, &notes); err != nil {
			return nil, false, err
		}
		rt, _ := time.Parse(time.RFC3339, reportedAt)
		if rt.After(newest) {
			newest = rt
		}
		out = append(out, external.EvidenceRecord{
			Source:        external.SourceID(source),
			ModelIdentity: modelIdentity,
			Benchmark:     benchmark,
			Value:         value,
			Scale:         external.ScaleKind(scale),
			Capability:    external.CapabilityKey(capability),
			ReportedAt:    rt,
			URL:           url,
			Notes:         notes,
		})
	}
	if len(out) == 0 {
		return nil, false, rows.Err()
	}
	if maxAge > 0 && time.Since(newest) > maxAge {
		return out, false, nil
	}
	return out, true, nil
}
