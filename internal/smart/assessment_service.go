package smart

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/termrouter/termrouter/internal/storage"
)

// CredChecker checks whether a credential is available for a provider.
type CredChecker func(providerID string) bool

// ProviderChecker tests basic provider/model reachability.
type ProviderChecker func(providerID, modelID string) (reachable, streamingKnown, toolsKnown bool)

// storageBridge adapts the storage layer to the assessment service.
type storageBridge struct {
	store *storage.Store
}

func newStorageBridge(store *storage.Store) *storageBridge {
	return &storageBridge{store: store}
}

func (b *storageBridge) InsertAssessment(ctx context.Context, rec *AssessmentRecord) error {
	data := rec.ToStorage()
	return b.store.InsertAssessment(ctx, data)
}

func (b *storageBridge) UpdateAssessment(ctx context.Context, rec *AssessmentRecord) error {
	data := rec.ToStorage()
	return b.store.UpdateAssessment(ctx, data)
}

func (b *storageBridge) GetAssessment(ctx context.Context, id string) (*AssessmentRecord, error) {
	data, err := b.store.GetAssessment(ctx, id)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("assessment %s not found", id)
	}
	return FromStorage(data), nil
}

func (b *storageBridge) ListAssessments(ctx context.Context, providerID, modelID string) ([]AssessmentSummary, error) {
	items, err := b.store.ListAssessments(ctx, providerID, modelID)
	if err != nil {
		return nil, err
	}
	out := make([]AssessmentSummary, len(items))
	for i, item := range items {
		out[i] = AssessmentSummary{
			AssessmentID:      item.AssessmentID,
			ProviderID:        item.ProviderID,
			ModelID:           item.ModelID,
			Status:            AssessmentStatus(item.Status),
			Depth:             AssessmentDepth(item.Depth),
			BenchmarkVersion:  item.BenchmarkVersion,
			OverallConfidence: item.OverallConfidence,
			StartedAt:         item.StartedAt,
			CompletedAt:       item.CompletedAt,
			AppliedAt:         item.AppliedAt,
			EstimatedCost:     item.EstimatedCost,
		}
	}
	return out, nil
}

func (b *storageBridge) GetLatestAssessment(ctx context.Context, providerID, modelID string) (*AssessmentRecord, error) {
	data, err := b.store.GetLatestAssessment(ctx, providerID, modelID)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	return FromStorage(data), nil
}

// ToStorage converts an AssessmentRecord to storage format.
func (rec *AssessmentRecord) ToStorage() *storage.AssessmentRecordData {
	catsJSON, _ := json.Marshal(rec.Categories)
	var ppJSON string
	if rec.ProposedProfile != nil {
		b, _ := json.Marshal(rec.ProposedProfile)
		ppJSON = string(b)
	}
	return &storage.AssessmentRecordData{
		AssessmentID:          rec.AssessmentID,
		ProviderID:            rec.ProviderID,
		ModelID:               rec.ModelID,
		ConnectionFingerprint: rec.ConnectionFingerprint,
		Status:                string(rec.Status),
		Depth:                 string(rec.Depth),
		BenchmarkVersion:      rec.BenchmarkVersion,
		ScoringVersion:        rec.ScoringVersion,
		CategoriesJSON:        string(catsJSON),
		StartedAt:             rec.StartedAt,
		CompletedAt:           rec.CompletedAt,
		EstimatedTokens:       rec.EstimatedTokens,
		InputTokens:           rec.InputTokens,
		OutputTokens:          rec.OutputTokens,
		EstimatedCost:         rec.EstimatedCost,
		ActualCost:            rec.ActualCost,
		Confidence:            rec.Confidence,
		ProposedProfileJSON:   ppJSON,
		AppliedAt:             rec.AppliedAt,
		AppliedFields:         rec.AppliedFields,
		Error:                 rec.Error,
	}
}

// FromStorage converts storage data to AssessmentRecord.
func FromStorage(data *storage.AssessmentRecordData) *AssessmentRecord {
	rec := &AssessmentRecord{
		AssessmentID:          data.AssessmentID,
		ProviderID:            data.ProviderID,
		ModelID:               data.ModelID,
		ConnectionFingerprint: data.ConnectionFingerprint,
		Status:                AssessmentStatus(data.Status),
		Depth:                 AssessmentDepth(data.Depth),
		BenchmarkVersion:      data.BenchmarkVersion,
		ScoringVersion:        data.ScoringVersion,
		StartedAt:             data.StartedAt,
		CompletedAt:           data.CompletedAt,
		EstimatedTokens:       data.EstimatedTokens,
		InputTokens:           data.InputTokens,
		OutputTokens:          data.OutputTokens,
		EstimatedCost:         data.EstimatedCost,
		ActualCost:            data.ActualCost,
		Confidence:            data.Confidence,
		AppliedAt:             data.AppliedAt,
		AppliedFields:         data.AppliedFields,
		Error:                 data.Error,
	}
	if data.CategoriesJSON != "" {
		var cats []AssessmentCategory
		if err := json.Unmarshal([]byte(data.CategoriesJSON), &cats); err == nil {
			rec.Categories = cats
		}
	}
	if data.ProposedProfileJSON != "" {
		var p ModelProfile
		if err := json.Unmarshal([]byte(data.ProposedProfileJSON), &p); err == nil {
			rec.ProposedProfile = &p
		}
	}
	return rec
}

// AssessmentPersister is the interface for persisting assessment data.
type AssessmentPersister interface {
	InsertAssessment(ctx context.Context, rec *AssessmentRecord) error
	UpdateAssessment(ctx context.Context, rec *AssessmentRecord) error
	GetAssessment(ctx context.Context, id string) (*AssessmentRecord, error)
	ListAssessments(ctx context.Context, providerID, modelID string) ([]AssessmentSummary, error)
	GetLatestAssessment(ctx context.Context, providerID, modelID string) (*AssessmentRecord, error)
}

// ModelAssessmentService orchestrates model self-assessment.
type ModelAssessmentService struct {
	mu              sync.Mutex
	activeRuns      map[string]context.Context
	store           AssessmentPersister
	credChecker     CredChecker
	providerChecker ProviderChecker
	profiles        *ProfileStore
}

// NewModelAssessmentService creates a new assessment service.
func NewModelAssessmentService(
	store *storage.Store,
	credChecker CredChecker,
	providerChecker ProviderChecker,
	profiles *ProfileStore,
) *ModelAssessmentService {
	return &ModelAssessmentService{
		activeRuns:      map[string]context.Context{},
		store:           newStorageBridge(store),
		credChecker:     credChecker,
		providerChecker: providerChecker,
		profiles:        profiles,
	}
}

// Preflight checks whether a model is eligible for assessment.
func (s *ModelAssessmentService) Preflight(providerID, modelID string) *AssessmentPreflightResult {
	res := &AssessmentPreflightResult{
		ProviderID: providerID,
		ModelID:    modelID,
		Eligible:   true,
	}

	// Check for conflicting active run
	s.mu.Lock()
	for _, cancel := range s.activeRuns {
		if cancel != nil {
			select {
			case <-cancel.Done():
			default:
				res.ConflictingRun = true
				res.Eligible = false
				res.Reasons = append(res.Reasons, "assessment already running for this model")
			}
		}
	}
	s.mu.Unlock()

	res.ProviderEnabled = true
	res.CredentialAvailable = s.credChecker(providerID)

	if !res.CredentialAvailable {
		res.Eligible = false
		res.Reasons = append(res.Reasons, "credential unavailable")
	}

	reachable, streamingKnown, toolsKnown := s.providerChecker(providerID, modelID)
	res.ModelReachable = reachable
	res.StreamingKnown = streamingKnown
	res.ToolsEndpointKnown = toolsKnown

	if !reachable {
		res.Eligible = false
		res.Reasons = append(res.Reasons, "model unreachable")
	}

	res.AssessmentReady = res.Eligible
	return res
}

// Estimate returns a usage estimate for an assessment run.
func (s *ModelAssessmentService) Estimate(providerID, modelID string, depth AssessmentDepth, categories []string) *AssessmentEstimate {
	if len(categories) == 0 {
		categories = defaultCategories(depth)
	}

	est := &AssessmentEstimate{
		ProviderID:     providerID,
		ModelID:        modelID,
		Depth:          depth,
		Categories:     categories,
		LeavesLocal:    !strings.Contains(providerID, "local"),
		ToolTestsRun:   contains(categories, CapToolUse),
		StreamingTests: true,
	}

	switch depth {
	case DepthQuick:
		est.RequestCount = 5 + len(categories)*3
		est.EstimatedTokens = est.RequestCount * 2000
	case DepthStandard:
		est.RequestCount = 10 + len(categories)*5
		est.EstimatedTokens = est.RequestCount * 3000
	case DepthComprehensive:
		est.RequestCount = 20 + len(categories)*8
		est.EstimatedTokens = est.RequestCount * 4000
	}

	// Rough cost estimate (very approximate)
	est.EstimatedCost = float64(est.EstimatedTokens) * 0.000002
	if est.EstimatedCost > 0 {
		est.CostKnown = true
	}

	return est
}

// Start begins a model self-assessment.
func (s *ModelAssessmentService) Start(providerID, modelID string, depth AssessmentDepth, categories []string, limits *AssessmentPlan) (*AssessmentRecord, error) {
	preflight := s.Preflight(providerID, modelID)
	if !preflight.Eligible {
		return nil, fmt.Errorf("preflight failed: %s", strings.Join(preflight.Reasons, "; "))
	}

	if len(categories) == 0 {
		categories = defaultCategories(depth)
	}

	aid := generateAssessmentID()

	plan := limits
	if plan == nil {
		plan = defaultPlan(depth, categories)
	}

	now := time.Now().UTC()
	rec := &AssessmentRecord{
		AssessmentID:     aid,
		ProviderID:       providerID,
		ModelID:          modelID,
		Status:           StatusPending,
		Depth:            depth,
		BenchmarkVersion: BenchmarkPackVersion,
		ScoringVersion:   AssessmentVersion,
		EstimatedTokens:  plan.MaxTokens,
		StartedAt:        &now,
	}

	catResults := make([]AssessmentCategory, len(categories))
	for i, c := range categories {
		catResults[i] = AssessmentCategory{
			Name:   c,
			Status: StatusPending,
		}
	}
	rec.Categories = catResults

	if err := s.store.InsertAssessment(context.Background(), rec); err != nil {
		return nil, fmt.Errorf("persist assessment: %w", err)
	}

	return rec, nil
}

// Cancel stops a running assessment.
func (s *ModelAssessmentService) Cancel(assessmentID string) error {
	s.mu.Lock()
	cancelCtx, _ := s.activeRuns[assessmentID]
	delete(s.activeRuns, assessmentID)
	s.mu.Unlock()

	_ = cancelCtx // suppress unused warning (real cancellation would call cancel)

	rec, err := s.store.GetAssessment(context.Background(), assessmentID)
	if err != nil {
		return fmt.Errorf("assessment not found: %s", assessmentID)
	}
	if rec.Status == StatusRunning || rec.Status == StatusPending {
		rec.Status = StatusCancelled
		now := time.Now().UTC()
		rec.CompletedAt = &now
		return s.store.UpdateAssessment(context.Background(), rec)
	}
	return fmt.Errorf("assessment %s is not running", assessmentID)
}

// GetAssessment returns the assessment record.
func (s *ModelAssessmentService) GetAssessment(assessmentID string) (*AssessmentRecord, error) {
	return s.store.GetAssessment(context.Background(), assessmentID)
}

// ListAssessments returns assessment history for a model.
func (s *ModelAssessmentService) ListAssessments(providerID, modelID string) ([]AssessmentSummary, error) {
	return s.store.ListAssessments(context.Background(), providerID, modelID)
}

// GenerateProposal builds a reviewable proposal from completed assessment results.
func (s *ModelAssessmentService) GenerateProposal(assessmentID string, affectedRoutes []string) (*AssessmentProposal, error) {
	rec, err := s.store.GetAssessment(context.Background(), assessmentID)
	if err != nil {
		return nil, err
	}
	if rec.Status != StatusCompleted && rec.Status != StatusPartial {
		return nil, fmt.Errorf("assessment %s is not completed (status: %s)", assessmentID, rec.Status)
	}

	key := ProfileKey(rec.ProviderID, rec.ModelID)
	current, _ := s.profiles.Resolve(rec.ProviderID, rec.ModelID, key)

	proposed := ModelProfile{
		ID:           key,
		ProviderID:   rec.ProviderID,
		ModelID:      rec.ModelID,
		Version:      rec.BenchmarkVersion,
		Source:       SourceSelfAssess,
		Capabilities: map[string]int{},
		Properties:   current.Properties,
	}

	var diffs []ProfileFieldDiff
	for _, cat := range rec.Categories {
		if cat.Score > 0 {
			proposed.Capabilities[cat.Name] = cat.Score
		}

		currentVal := current.Capabilities[cat.Name]
		if currentVal != cat.Score {
			src := SourceBuiltin
			switch current.Source {
			case SourceUser:
				src = SourceUser
			case SourceSelfAssess:
				src = SourceSelfAssess
			}
			diffs = append(diffs, ProfileFieldDiff{
				Field:         cat.Name,
				CurrentValue:  currentVal,
				ProposedValue: cat.Score,
				Source:        src,
				Confidence:    cat.Confidence,
				Recommended:   cat.Confidence >= 0.5 && cat.Score > 0,
			})
		}
	}

	if affectedRoutes == nil {
		affectedRoutes = []string{}
	}

	prop := &AssessmentProposal{
		AssessmentID:      assessmentID,
		ProviderID:        rec.ProviderID,
		ModelID:           rec.ModelID,
		Depth:             rec.Depth,
		CurrentProfile:    &current,
		ProposedProfile:   &proposed,
		Differences:       diffs,
		CategoryResults:   rec.Categories,
		OverallConfidence: rec.Confidence,
		AffectedRoutes:    affectedRoutes,
		BenchmarkVersion:  rec.BenchmarkVersion,
		CreatedAt:         time.Now().UTC(),
	}

	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Field < diffs[j].Field })

	return prop, nil
}

// ApplyProposal applies an accepted assessment proposal to the profile store.
func (s *ModelAssessmentService) ApplyProposal(assessmentID string, acceptedFields []string, preserveUserOverrides bool) (*AssessmentRecord, error) {
	rec, err := s.store.GetAssessment(context.Background(), assessmentID)
	if err != nil {
		return nil, err
	}
	if rec.Status != StatusCompleted && rec.Status != StatusPartial {
		return nil, fmt.Errorf("assessment %s is not completed", assessmentID)
	}

	if rec.ProposedProfile == nil {
		return nil, fmt.Errorf("assessment %s has no proposed profile", assessmentID)
	}

	key := ProfileKey(rec.ProviderID, rec.ModelID)

	applyAll := len(acceptedFields) == 0
	appliedFields := acceptedFields
	if applyAll {
		for _, cat := range rec.Categories {
			if cat.Score > 0 {
				appliedFields = append(appliedFields, cat.Name)
			}
		}
	}

	finalProfile := *rec.ProposedProfile

	if preserveUserOverrides {
		if existing, ok := s.profiles.User[key]; ok {
			for k, v := range existing.Capabilities {
				finalProfile.Capabilities[k] = v
			}
			if existing.Properties.Vision != nil {
				finalProfile.Properties.Vision = existing.Properties.Vision
			}
			if existing.Properties.Tools != nil {
				finalProfile.Properties.Tools = existing.Properties.Tools
			}
			if existing.Properties.ParallelTools != nil {
				finalProfile.Properties.ParallelTools = existing.Properties.ParallelTools
			}
			if existing.Properties.StructuredOutput != nil {
				finalProfile.Properties.StructuredOutput = existing.Properties.StructuredOutput
			}
			if existing.Properties.ContextWindow > 0 {
				finalProfile.Properties.ContextWindow = existing.Properties.ContextWindow
			}
			if existing.Properties.CostTier > 0 {
				finalProfile.Properties.CostTier = existing.Properties.CostTier
			}
			if existing.Properties.LatencyTier > 0 {
				finalProfile.Properties.LatencyTier = existing.Properties.LatencyTier
			}
			if existing.Properties.Privacy != "" {
				finalProfile.Properties.Privacy = existing.Properties.Privacy
			}
		}
	}

	if !applyAll {
		for k := range finalProfile.Capabilities {
			found := false
			for _, f := range acceptedFields {
				if f == k {
					found = true
					break
				}
			}
			if !found {
				delete(finalProfile.Capabilities, k)
			}
		}
	}

	if s.profiles.Assessments == nil {
		s.profiles.Assessments = map[string]ModelProfile{}
	}
	s.profiles.Assessments[key] = finalProfile

	now := time.Now().UTC()
	rec.AppliedAt = &now
	rec.AppliedFields = appliedFields
	return rec, s.store.UpdateAssessment(context.Background(), rec)
}

// DetectOutdated checks if an assessment is outdated based on version changes.
func DetectOutdated(rec *AssessmentRecord, currentBuiltinVersion string) ProfileStatus {
	if rec == nil {
		return ProfileNotProfiled
	}
	switch rec.Status {
	case StatusFailed:
		return ProfileAssessmentFailed
	case StatusCancelled, StatusPartial:
		return ProfileAssessmentAvail
	}
	if rec.BenchmarkVersion != currentBuiltinVersion && currentBuiltinVersion != "" {
		return ProfileAssessmentOutdated
	}
	return ProfileAssessed
}

// ComputeConfidence calculates overall confidence from category results.
func ComputeConfidence(categories []AssessmentCategory, depth AssessmentDepth) float64 {
	if len(categories) == 0 {
		return 0
	}
	var total float64
	for _, c := range categories {
		total += c.Confidence
	}
	avg := total / float64(len(categories))

	switch depth {
	case DepthQuick:
		avg *= 0.85
	case DepthStandard:
		avg *= 0.95
	}

	return math.Min(avg, 1.0)
}

// ComputeCategoryScore maps a pass rate to a 0-5 score.
func ComputeCategoryScore(passed, total int) int {
	if total == 0 {
		return 0
	}
	rate := float64(passed) / float64(total)
	switch {
	case rate >= 0.95:
		return 5
	case rate >= 0.80:
		return 4
	case rate >= 0.60:
		return 3
	case rate >= 0.30:
		return 2
	case rate >= 0.10:
		return 1
	default:
		return 0
	}
}

func generateAssessmentID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "assess_" + hex.EncodeToString(b)
}

func defaultCategories(depth AssessmentDepth) []string {
	core := []string{CapGeneral, CapReasoning, CapAnalysis, CapCoding, CapToolUse, CapStructuredOutput}
	switch depth {
	case DepthQuick:
		return core
	case DepthStandard:
		return append(core, CapWriting, CapInstructionFollowing, CapMathematics)
	case DepthComprehensive:
		return AllCapabilities
	default:
		return core
	}
}

func defaultPlan(depth AssessmentDepth, categories []string) *AssessmentPlan {
	catCount := len(categories)
	switch depth {
	case DepthQuick:
		return &AssessmentPlan{
			Depth:       depth,
			Categories:  categories,
			MaxRequests: 5 + catCount*3,
			MaxTokens:   50000,
			Concurrency: 2,
		}
	case DepthStandard:
		return &AssessmentPlan{
			Depth:       depth,
			Categories:  categories,
			MaxRequests: 10 + catCount*5,
			MaxTokens:   150000,
			Concurrency: 3,
		}
	case DepthComprehensive:
		return &AssessmentPlan{
			Depth:       depth,
			Categories:  categories,
			MaxRequests: 20 + catCount*8,
			MaxTokens:   300000,
			Concurrency: 4,
		}
	default:
		return defaultPlan(DepthStandard, categories)
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
