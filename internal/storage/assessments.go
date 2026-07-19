package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// AssessmentRecordData is the raw persisted assessment data.
type AssessmentRecordData struct {
	AssessmentID          string           `json:"assessment_id"`
	ProviderID            string           `json:"provider_id"`
	ModelID               string           `json:"model_id"`
	ConnectionFingerprint string           `json:"connection_fingerprint,omitempty"`
	Status                string           `json:"status"`
	Depth                 string           `json:"depth"`
	BenchmarkVersion      string           `json:"benchmark_version"`
	ScoringVersion        string           `json:"scoring_version"`
	CategoriesJSON        string           `json:"categories_json"`
	StartedAt             *time.Time       `json:"started_at,omitempty"`
	CompletedAt           *time.Time       `json:"completed_at,omitempty"`
	EstimatedTokens       int              `json:"estimated_tokens"`
	InputTokens           int              `json:"input_tokens"`
	OutputTokens          int              `json:"output_tokens"`
	EstimatedCost         float64          `json:"estimated_cost"`
	ActualCost            float64          `json:"actual_cost"`
	Confidence            float64          `json:"confidence"`
	ProposedProfileJSON   string           `json:"proposed_profile_json,omitempty"`
	AppliedAt             *time.Time       `json:"applied_at,omitempty"`
	AppliedFields         []string         `json:"applied_fields,omitempty"`
	Error                 string           `json:"error,omitempty"`
}

// AssessmentSummaryData is a lightweight row for listing.
type AssessmentSummaryData struct {
	AssessmentID      string     `json:"assessment_id"`
	ProviderID        string     `json:"provider_id"`
	ModelID           string     `json:"model_id"`
	Status            string     `json:"status"`
	Depth             string     `json:"depth"`
	BenchmarkVersion  string     `json:"benchmark_version"`
	OverallConfidence float64    `json:"overall_confidence"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	AppliedAt         *time.Time `json:"applied_at,omitempty"`
	EstimatedCost     float64    `json:"estimated_cost"`
}

// InsertAssessment persists a new assessment record.
func (s *Store) InsertAssessment(ctx context.Context, data *AssessmentRecordData) error {
	catsJSON := data.CategoriesJSON
	if catsJSON == "" {
		catsJSON = "[]"
	}
	var ppJSON any
	if data.ProposedProfileJSON != "" {
		ppJSON = data.ProposedProfileJSON
	}
	appliedFields, _ := json.Marshal(data.AppliedFields)
	var started, completed, applied any
	if data.StartedAt != nil {
		started = data.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if data.CompletedAt != nil {
		completed = data.CompletedAt.UTC().Format(time.RFC3339Nano)
	}
	if data.AppliedAt != nil {
		applied = data.AppliedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO model_assessments(
			assessment_id, provider_id, model_id, connection_fingerprint, status, depth,
			benchmark_version, scoring_version, categories_json,
			started_at, completed_at, estimated_tokens, input_tokens, output_tokens,
			estimated_cost, actual_cost, confidence, proposed_profile_json,
			applied_at, applied_fields, error_text
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		data.AssessmentID, data.ProviderID, data.ModelID, nullStr(data.ConnectionFingerprint),
		data.Status, data.Depth, data.BenchmarkVersion, data.ScoringVersion, catsJSON,
		started, completed, data.EstimatedTokens, data.InputTokens, data.OutputTokens,
		data.EstimatedCost, data.ActualCost, data.Confidence, ppJSON,
		applied, string(appliedFields), nullStr(data.Error),
	)
	return err
}

// UpdateAssessment updates an existing assessment record.
func (s *Store) UpdateAssessment(ctx context.Context, data *AssessmentRecordData) error {
	catsJSON := data.CategoriesJSON
	if catsJSON == "" {
		catsJSON = "[]"
	}
	var ppJSON any
	if data.ProposedProfileJSON != "" {
		ppJSON = data.ProposedProfileJSON
	}
	appliedFields, _ := json.Marshal(data.AppliedFields)
	var started, completed, applied any
	if data.StartedAt != nil {
		started = data.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if data.CompletedAt != nil {
		completed = data.CompletedAt.UTC().Format(time.RFC3339Nano)
	}
	if data.AppliedAt != nil {
		applied = data.AppliedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE model_assessments SET
			status=?, categories_json=?, started_at=?, completed_at=?,
			input_tokens=?, output_tokens=?, actual_cost=?, confidence=?,
			proposed_profile_json=?, applied_at=?, applied_fields=?, error_text=?
		WHERE assessment_id=?`,
		data.Status, catsJSON, started, completed,
		data.InputTokens, data.OutputTokens, data.ActualCost, data.Confidence,
		ppJSON, applied, string(appliedFields), nullStr(data.Error),
		data.AssessmentID,
	)
	return err
}

// GetAssessment returns a single assessment record.
func (s *Store) GetAssessment(ctx context.Context, id string) (*AssessmentRecordData, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT assessment_id, provider_id, model_id, connection_fingerprint, status, depth,
			benchmark_version, scoring_version, categories_json,
			started_at, completed_at, estimated_tokens, input_tokens, output_tokens,
			estimated_cost, actual_cost, confidence, proposed_profile_json,
			applied_at, applied_fields, error_text
		FROM model_assessments WHERE assessment_id=?`, id)
	return scanAssessmentData(row)
}

// ListAssessments returns assessment summaries for a given model.
func (s *Store) ListAssessments(ctx context.Context, providerID, modelID string) ([]AssessmentSummaryData, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT assessment_id, provider_id, model_id, status, depth,
			benchmark_version, confidence, started_at, completed_at, applied_at, estimated_cost
		FROM model_assessments
		WHERE provider_id=? AND model_id=?
		ORDER BY started_at DESC`, providerID, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AssessmentSummaryData
	for rows.Next() {
		s, err := scanAssessmentSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// GetLatestAssessment returns the most recent assessment for a model.
func (s *Store) GetLatestAssessment(ctx context.Context, providerID, modelID string) (*AssessmentRecordData, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT assessment_id, provider_id, model_id, connection_fingerprint, status, depth,
			benchmark_version, scoring_version, categories_json,
			started_at, completed_at, estimated_tokens, input_tokens, output_tokens,
			estimated_cost, actual_cost, confidence, proposed_profile_json,
			applied_at, applied_fields, error_text
		FROM model_assessments
		WHERE provider_id=? AND model_id=?
		ORDER BY started_at DESC LIMIT 1`, providerID, modelID)
	d, err := scanAssessmentData(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

func scanAssessmentData(row scannable) (*AssessmentRecordData, error) {
	var r AssessmentRecordData
	var status, depth, bv, sv string
	var catsJSON, ppJSON, afJSON sql.NullString
	var started, completed, applied sql.NullString
	var errText sql.NullString
	var cf sql.NullString

	err := row.Scan(&r.AssessmentID, &r.ProviderID, &r.ModelID, &cf, &status, &depth,
		&bv, &sv, &catsJSON, &started, &completed,
		&r.EstimatedTokens, &r.InputTokens, &r.OutputTokens,
		&r.EstimatedCost, &r.ActualCost, &r.Confidence, &ppJSON,
		&applied, &afJSON, &errText)
	if err != nil {
		return nil, err
	}
	r.Status = status
	r.Depth = depth
	r.BenchmarkVersion = bv
	r.ScoringVersion = sv
	if cf.Valid {
		r.ConnectionFingerprint = cf.String
	}
	if catsJSON.Valid {
		r.CategoriesJSON = catsJSON.String
	}
	if ppJSON.Valid {
		r.ProposedProfileJSON = ppJSON.String
	}
	if afJSON.Valid {
		_ = json.Unmarshal([]byte(afJSON.String), &r.AppliedFields)
	}
	if errText.Valid {
		r.Error = errText.String
	}
	if started.Valid {
		t, _ := time.Parse(time.RFC3339Nano, started.String)
		r.StartedAt = &t
	}
	if completed.Valid {
		t, _ := time.Parse(time.RFC3339Nano, completed.String)
		r.CompletedAt = &t
	}
	if applied.Valid {
		t, _ := time.Parse(time.RFC3339Nano, applied.String)
		r.AppliedAt = &t
	}
	return &r, nil
}

func scanAssessmentSummary(row scannable) (*AssessmentSummaryData, error) {
	var s AssessmentSummaryData
	var status, depth, bv string
	var started, completed, applied sql.NullString
	err := row.Scan(&s.AssessmentID, &s.ProviderID, &s.ModelID, &status, &depth, &bv,
		&s.OverallConfidence, &started, &completed, &applied, &s.EstimatedCost)
	if err != nil {
		return nil, err
	}
	s.Status = status
	s.Depth = depth
	s.BenchmarkVersion = bv
	if started.Valid {
		t, _ := time.Parse(time.RFC3339Nano, started.String)
		s.StartedAt = &t
	}
	if completed.Valid {
		t, _ := time.Parse(time.RFC3339Nano, completed.String)
		s.CompletedAt = &t
	}
	if applied.Valid {
		t, _ := time.Parse(time.RFC3339Nano, applied.String)
		s.AppliedAt = &t
	}
	return &s, nil
}
