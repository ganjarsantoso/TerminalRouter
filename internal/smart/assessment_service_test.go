package smart

import (
	"context"
	"testing"
	"time"
)

// memoryAssessmentStore implements AssessmentStore interface operations using storage types.
// Since the storage bridge uses *storage.Store, we test via the bridge pattern directly.
type testAssessmentStore struct {
	assessments map[string]*AssessmentRecord
}

func newTestAssessmentStore() *testAssessmentStore {
	return &testAssessmentStore{assessments: map[string]*AssessmentRecord{}}
}

func (s *testAssessmentStore) InsertAssessment(ctx context.Context, rec *AssessmentRecord) error {
	s.assessments[rec.AssessmentID] = rec
	return nil
}

func (s *testAssessmentStore) UpdateAssessment(ctx context.Context, rec *AssessmentRecord) error {
	s.assessments[rec.AssessmentID] = rec
	return nil
}

func (s *testAssessmentStore) GetAssessment(ctx context.Context, id string) (*AssessmentRecord, error) {
	rec, ok := s.assessments[id]
	if !ok {
		return nil, nil
	}
	return rec, nil
}

func (s *testAssessmentStore) ListAssessments(ctx context.Context, providerID, modelID string) ([]AssessmentSummary, error) {
	var out []AssessmentSummary
	for _, rec := range s.assessments {
		if rec.ProviderID == providerID && rec.ModelID == modelID {
			out = append(out, AssessmentSummary{
				AssessmentID:      rec.AssessmentID,
				ProviderID:        rec.ProviderID,
				ModelID:           rec.ModelID,
				Status:            rec.Status,
				Depth:             rec.Depth,
				BenchmarkVersion:  rec.BenchmarkVersion,
				OverallConfidence: rec.Confidence,
				StartedAt:         rec.StartedAt,
				CompletedAt:       rec.CompletedAt,
			})
		}
	}
	return out, nil
}

func (s *testAssessmentStore) GetLatestAssessment(ctx context.Context, providerID, modelID string) (*AssessmentRecord, error) {
	var latest *AssessmentRecord
	for _, rec := range s.assessments {
		if rec.ProviderID == providerID && rec.ModelID == modelID {
			if latest == nil || (rec.StartedAt != nil && latest.StartedAt != nil && rec.StartedAt.After(*latest.StartedAt)) {
				r := *rec
				latest = &r
			}
		}
	}
	return latest, nil
}

func newTestService(store AssessmentPersister) *ModelAssessmentService {
	credCheck := func(providerID string) bool { return true }
	provCheck := func(providerID, modelID string) (bool, bool, bool) { return true, true, true }
	profiles := NewProfileStore(nil, true)
	return &ModelAssessmentService{
		activeRuns:      map[string]context.Context{},
		store:           store,
		credChecker:     credCheck,
		providerChecker: provCheck,
		profiles:        profiles,
	}
}

// Ensure testAssessmentStore implements AssessmentPersister.
var _ AssessmentPersister = (*testAssessmentStore)(nil)

func TestPreflight_Eligible(t *testing.T) {
	store := newTestAssessmentStore()
	svc := newTestService(store)
	res := svc.Preflight("openai", "gpt-4o")
	if !res.Eligible {
		t.Errorf("expected eligible, got reasons: %v", res.Reasons)
	}
	if !res.CredentialAvailable {
		t.Error("expected credential available")
	}
	if !res.ModelReachable {
		t.Error("expected model reachable")
	}
}

func TestPreflight_NoCredential(t *testing.T) {
	store := newTestAssessmentStore()
	credCheck := func(providerID string) bool { return false }
	provCheck := func(providerID, modelID string) (bool, bool, bool) { return true, true, true }
	profiles := NewProfileStore(nil, true)
	svc := &ModelAssessmentService{
		activeRuns:      map[string]context.Context{},
		store:           store,
		credChecker:     credCheck,
		providerChecker: provCheck,
		profiles:        profiles,
	}
	var _ AssessmentPersister = store
	res := svc.Preflight("openai", "gpt-4o")
	if res.Eligible {
		t.Error("expected not eligible when credential unavailable")
	}
	if res.CredentialAvailable {
		t.Error("expected credential not available")
	}
}

func TestEstimate_Quick(t *testing.T) {
	store := newTestAssessmentStore()
	svc := newTestService(store)
	est := svc.Estimate("openai", "gpt-4o", DepthQuick, nil)
	if est.RequestCount <= 0 {
		t.Errorf("expected positive request count, got %d", est.RequestCount)
	}
	if est.Depth != DepthQuick {
		t.Errorf("expected quick depth, got %s", est.Depth)
	}
	if !est.LeavesLocal {
		t.Error("expected leaves_local true for openai")
	}
}

func TestEstimate_Standard(t *testing.T) {
	store := newTestAssessmentStore()
	svc := newTestService(store)
	est := svc.Estimate("openai", "gpt-4o", DepthStandard, nil)
	if est.RequestCount <= 0 {
		t.Errorf("expected positive request count, got %d", est.RequestCount)
	}
	if est.Depth != DepthStandard {
		t.Errorf("expected standard depth, got %s", est.Depth)
	}
}

func TestEstimate_Local(t *testing.T) {
	store := newTestAssessmentStore()
	svc := newTestService(store)
	est := svc.Estimate("local", "qwen-coder", DepthStandard, nil)
	if est.LeavesLocal {
		t.Error("expected leaves_local false for local provider")
	}
}

func TestStartAssessment(t *testing.T) {
	store := newTestAssessmentStore()
	svc := newTestService(store)
	rec, err := svc.Start("openai", "gpt-4o", DepthStandard, nil, nil)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if rec.AssessmentID == "" {
		t.Error("expected non-empty assessment ID")
	}
	if rec.Status != StatusPending {
		t.Errorf("expected status pending, got %s", rec.Status)
	}
	if rec.Depth != DepthStandard {
		t.Errorf("expected standard depth, got %s", rec.Depth)
	}
	if rec.BenchmarkVersion != BenchmarkPackVersion {
		t.Errorf("expected benchmark version %s, got %s", BenchmarkPackVersion, rec.BenchmarkVersion)
	}
	if rec.StartedAt == nil {
		t.Error("expected started_at to be set")
	}
	if len(rec.Categories) == 0 {
		t.Error("expected non-empty categories")
	}
}

func TestStartAssessment_Quick(t *testing.T) {
	store := newTestAssessmentStore()
	svc := newTestService(store)
	rec, err := svc.Start("openai", "gpt-4o", DepthQuick, []string{CapGeneral, CapCoding}, nil)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if rec.Depth != DepthQuick {
		t.Errorf("expected quick depth, got %s", rec.Depth)
	}
	if len(rec.Categories) != 2 {
		t.Errorf("expected 2 categories, got %d", len(rec.Categories))
	}
	for _, cat := range rec.Categories {
		if cat.Status != StatusPending {
			t.Errorf("expected pending status for category %s, got %s", cat.Name, cat.Status)
		}
	}
}

func TestStartAssessment_CustomCategories(t *testing.T) {
	store := newTestAssessmentStore()
	svc := newTestService(store)
	cats := []string{CapCoding, CapReasoning, CapToolUse}
	rec, err := svc.Start("openai", "gpt-4o", DepthStandard, cats, nil)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if len(rec.Categories) != 3 {
		t.Errorf("expected 3 categories, got %d", len(rec.Categories))
	}
}

func TestCancelAssessment(t *testing.T) {
	store := newTestAssessmentStore()
	svc := newTestService(store)
	rec, err := svc.Start("openai", "gpt-4o", DepthStandard, nil, nil)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	err = svc.Cancel(rec.AssessmentID)
	if err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}
	rec, err = svc.GetAssessment(rec.AssessmentID)
	if err != nil {
		t.Fatalf("GetAssessment failed: %v", err)
	}
	if rec.Status != StatusCancelled {
		t.Errorf("expected cancelled status, got %s", rec.Status)
	}
}

func TestListAssessments(t *testing.T) {
	store := newTestAssessmentStore()
	svc := newTestService(store)
	svc.Start("openai", "gpt-4o", DepthQuick, nil, nil)
	svc.Start("openai", "gpt-4o", DepthStandard, nil, nil)
	svc.Start("local", "qwen-coder", DepthQuick, nil, nil)

	list, err := svc.ListAssessments("openai", "gpt-4o")
	if err != nil {
		t.Fatalf("ListAssessments failed: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 assessments for openai/gpt-4o, got %d", len(list))
	}

	list2, err := svc.ListAssessments("local", "qwen-coder")
	if err != nil {
		t.Fatalf("ListAssessments failed: %v", err)
	}
	if len(list2) != 1 {
		t.Errorf("expected 1 assessment for local/qwen-coder, got %d", len(list2))
	}
}

func TestComputeCategoryScore(t *testing.T) {
	tests := []struct {
		passed, total int
		expected      int
	}{
		{10, 10, 5},
		{8, 10, 4},
		{6, 10, 3},
		{3, 10, 2},
		{1, 10, 1},
		{0, 10, 0},
		{0, 0, 0},
	}
	for _, tt := range tests {
		got := ComputeCategoryScore(tt.passed, tt.total)
		if got != tt.expected {
			t.Errorf("ComputeCategoryScore(%d, %d) = %d, want %d", tt.passed, tt.total, got, tt.expected)
		}
	}
}

func TestComputeConfidence(t *testing.T) {
	cats := []AssessmentCategory{
		{Name: CapGeneral, Confidence: 0.8},
		{Name: CapCoding, Confidence: 0.9},
		{Name: CapReasoning, Confidence: 0.7},
	}
	conf := ComputeConfidence(cats, DepthStandard)
	if conf <= 0 || conf > 1 {
		t.Errorf("expected confidence between 0 and 1, got %f", conf)
	}
	// Standard depth applies 0.95 multiplier
	expected := (0.8 + 0.9 + 0.7) / 3.0 * 0.95
	if conf != expected {
		diff := conf - expected
		if diff < 0 {
			diff = -diff
		}
		if diff > 0.0001 {
			t.Errorf("expected confidence %f, got %f (diff=%e)", expected, conf, diff)
		}
	}
}

func TestComputeConfidence_Empty(t *testing.T) {
	conf := ComputeConfidence(nil, DepthStandard)
	if conf != 0 {
		t.Errorf("expected 0 for empty categories, got %f", conf)
	}
}

func TestDetectOutdated(t *testing.T) {
	rec := &AssessmentRecord{
		BenchmarkVersion: "benchmark-v1",
		Status:           StatusCompleted,
	}
	status := DetectOutdated(rec, "benchmark-v2")
	if status != ProfileAssessmentOutdated {
		t.Errorf("expected outdated, got %s", status)
	}

	status = DetectOutdated(rec, "benchmark-v1")
	if status != ProfileAssessed {
		t.Errorf("expected assessed, got %s", status)
	}
}

func TestDetectOutdated_Nil(t *testing.T) {
	status := DetectOutdated(nil, "benchmark-v1")
	if status != ProfileNotProfiled {
		t.Errorf("expected not_profiled, got %s", status)
	}
}

func TestDetectOutdated_Failed(t *testing.T) {
	rec := &AssessmentRecord{
		BenchmarkVersion: "benchmark-v1",
		Status:           StatusFailed,
	}
	status := DetectOutdated(rec, "benchmark-v1")
	if status != ProfileAssessmentFailed {
		t.Errorf("expected assessment_failed, got %s", status)
	}
}

func TestGenerateProposal(t *testing.T) {
	store := newTestAssessmentStore()
	svc := newTestService(store)
	rec, _ := svc.Start("openai", "gpt-4o", DepthStandard, []string{CapGeneral, CapCoding}, nil)
	// Complete the assessment
	now := time.Now().UTC()
	rec.Status = StatusCompleted
	rec.CompletedAt = &now
	rec.Confidence = 0.85
	rec.Categories = []AssessmentCategory{
		{Name: CapGeneral, Score: 4, Confidence: 0.8, TestsPassed: 8, TestsTotal: 10},
		{Name: CapCoding, Score: 5, Confidence: 0.9, TestsPassed: 10, TestsTotal: 10},
	}
	store.UpdateAssessment(nil, rec)

	prop, err := svc.GenerateProposal(rec.AssessmentID, []string{"auto-coding"})
	if err != nil {
		t.Fatalf("GenerateProposal failed: %v", err)
	}
	if prop.AssessmentID != rec.AssessmentID {
		t.Errorf("expected assessment ID %s, got %s", rec.AssessmentID, prop.AssessmentID)
	}
	if prop.ProposedProfile == nil {
		t.Fatal("expected non-nil proposed profile")
	}
	if len(prop.Differences) == 0 && (prop.CurrentProfile.Capabilities[CapGeneral] != 0 || prop.ProposedProfile.Capabilities[CapGeneral] != 4) {
		// the differences might exist or not depending on current profile
	}
	if prop.ProposedProfile.Capabilities[CapGeneral] != 4 {
		t.Errorf("expected general score 4, got %d", prop.ProposedProfile.Capabilities[CapGeneral])
	}
	if prop.ProposedProfile.Capabilities[CapCoding] != 5 {
		t.Errorf("expected coding score 5, got %d", prop.ProposedProfile.Capabilities[CapCoding])
	}
	if len(prop.AffectedRoutes) != 1 || prop.AffectedRoutes[0] != "auto-coding" {
		t.Errorf("expected affected route auto-coding, got %v", prop.AffectedRoutes)
	}
}

func TestGenerateProposal_NotCompleted(t *testing.T) {
	store := newTestAssessmentStore()
	svc := newTestService(store)
	rec, _ := svc.Start("openai", "gpt-4o", DepthStandard, nil, nil)
	_, err := svc.GenerateProposal(rec.AssessmentID, nil)
	if err == nil {
		t.Error("expected error for incomplete assessment")
	}
}

func TestApplyProposal(t *testing.T) {
	store := newTestAssessmentStore()
	svc := newTestService(store)
	rec, _ := svc.Start("openai", "gpt-4o", DepthStandard, []string{CapGeneral, CapCoding}, nil)
	now := time.Now().UTC()
	rec.Status = StatusCompleted
	rec.CompletedAt = &now
	rec.Confidence = 0.85
	propProfile := &ModelProfile{
		ID:         "openai/gpt-4o",
		ProviderID: "openai",
		ModelID:    "gpt-4o",
		Version:    BenchmarkPackVersion,
		Source:     SourceSelfAssess,
		Capabilities: map[string]int{
			CapGeneral: 4,
			CapCoding:  5,
		},
		Properties: ModelProperties{
			ContextWindow: 128000,
		},
	}
	rec.ProposedProfile = propProfile
	store.UpdateAssessment(nil, rec)

	updated, err := svc.ApplyProposal(rec.AssessmentID, []string{CapGeneral}, true)
	if err != nil {
		t.Fatalf("ApplyProposal failed: %v", err)
	}
	if updated.AppliedAt == nil {
		t.Error("expected applied_at to be set")
	}
	if len(updated.AppliedFields) != 1 || updated.AppliedFields[0] != CapGeneral {
		t.Errorf("expected applied fields [general], got %v", updated.AppliedFields)
	}

	// Verify the assessment baseline was stored
	stored, ok := svc.profiles.Assessments["openai/gpt-4o"]
	if !ok {
		t.Fatal("expected assessment baseline to be stored in profiles")
	}
	if stored.Source != SourceSelfAssess {
		t.Errorf("expected source self-assessment, got %s", stored.Source)
	}
}

func TestApplyProposal_NotCompleted(t *testing.T) {
	store := newTestAssessmentStore()
	svc := newTestService(store)
	rec, _ := svc.Start("openai", "gpt-4o", DepthStandard, nil, nil)
	_, err := svc.ApplyProposal(rec.AssessmentID, nil, true)
	if err == nil {
		t.Error("expected error for incomplete assessment")
	}
}

func TestProfileStoreResolve_WithAssessments(t *testing.T) {
	userProfiles := map[string]ModelProfile{
		"openai/gpt-4o": {
			ID: "openai/gpt-4o", Source: SourceUser,
			Capabilities: map[string]int{CapGeneral: 5},
		},
	}
	assessProfiles := map[string]ModelProfile{
		"openai/gpt-4o": {
			ID: "openai/gpt-4o", Source: SourceSelfAssess,
			Capabilities: map[string]int{CapGeneral: 4, CapCoding: 4},
		},
	}
	ps := NewProfileStoreWithAssessments(userProfiles, assessProfiles, true)
	prof, found := ps.Resolve("openai", "gpt-4o", "")
	if !found {
		t.Fatal("expected profile to be resolved")
	}
	if prof.Source != SourceUser {
		t.Errorf("expected user source (highest precedence), got %s", prof.Source)
	}
	if prof.Capabilities[CapGeneral] != 5 {
		t.Errorf("expected general 5 from user override, got %d", prof.Capabilities[CapGeneral])
	}
}

func TestProfileStoreResolve_AssessmentBaseline(t *testing.T) {
	assessProfiles := map[string]ModelProfile{
		"openai/gpt-4o": {
			ID: "openai/gpt-4o", Source: SourceSelfAssess,
			Capabilities: map[string]int{CapGeneral: 4, CapCoding: 4},
		},
	}
	ps := NewProfileStoreWithAssessments(nil, assessProfiles, true)
	prof, found := ps.Resolve("openai", "gpt-4o", "")
	if !found {
		t.Fatal("expected profile to be resolved")
	}
	if prof.Source != SourceSelfAssess {
		t.Errorf("expected self-assessment source, got %s", prof.Source)
	}
	if prof.Capabilities[CapGeneral] != 4 {
		t.Errorf("expected general 4, got %d", prof.Capabilities[CapGeneral])
	}
}

func TestDefaultCategories(t *testing.T) {
	quickCats := defaultCategories(DepthQuick)
	if len(quickCats) != 6 {
		t.Errorf("expected 6 quick categories, got %d: %v", len(quickCats), quickCats)
	}

	stdCats := defaultCategories(DepthStandard)
	if len(stdCats) <= 6 {
		t.Errorf("expected more than 6 standard categories, got %d: %v", len(stdCats), stdCats)
	}

	compCats := defaultCategories(DepthComprehensive)
	if len(compCats) < len(AllCapabilities) {
		t.Errorf("expected comprehensive categories to include all capabilities")
	}
}

func TestDefaultPlan(t *testing.T) {
	cats := []string{CapGeneral, CapCoding}
	plan := defaultPlan(DepthQuick, cats)
	if plan.MaxRequests <= 0 {
		t.Errorf("expected positive max_requests, got %d", plan.MaxRequests)
	}
	if plan.Concurrency <= 0 {
		t.Errorf("expected positive concurrency, got %d", plan.Concurrency)
	}

	plan2 := defaultPlan(DepthStandard, cats)
	if plan2.MaxRequests <= plan.MaxRequests {
		t.Errorf("expected standard plan to have more requests than quick plan")
	}
}

func TestToFromStorage(t *testing.T) {
	now := time.Now().UTC()
	rec := &AssessmentRecord{
		AssessmentID:     "assess_test_123",
		ProviderID:       "openai",
		ModelID:          "gpt-4o",
		Status:           StatusCompleted,
		Depth:            DepthStandard,
		BenchmarkVersion: BenchmarkPackVersion,
		ScoringVersion:   AssessmentVersion,
		StartedAt:        &now,
		CompletedAt:      &now,
		Confidence:       0.85,
		Categories: []AssessmentCategory{
			{Name: CapGeneral, Score: 4, Confidence: 0.8, TestsPassed: 8, TestsTotal: 10},
		},
		ProposedProfile: &ModelProfile{
			ID: "openai/gpt-4o", Source: SourceSelfAssess,
			Capabilities: map[string]int{CapGeneral: 4},
		},
		AppliedFields: []string{CapGeneral},
	}

	data := rec.ToStorage()
	if data.AssessmentID != rec.AssessmentID {
		t.Errorf("expected assessment ID %s, got %s", rec.AssessmentID, data.AssessmentID)
	}

	restored := FromStorage(data)
	if restored.AssessmentID != rec.AssessmentID {
		t.Errorf("expected assessment ID %s, got %s", rec.AssessmentID, restored.AssessmentID)
	}
	if restored.Confidence != rec.Confidence {
		t.Errorf("expected confidence %f, got %f", rec.Confidence, restored.Confidence)
	}
	if restored.Status != rec.Status {
		t.Errorf("expected status %s, got %s", rec.Status, restored.Status)
	}
	if len(restored.Categories) != len(rec.Categories) {
		t.Errorf("expected %d categories, got %d", len(rec.Categories), len(restored.Categories))
	}
	if restored.ProposedProfile == nil {
		t.Fatal("expected non-nil proposed profile")
	}
	if restored.ProposedProfile.Capabilities[CapGeneral] != 4 {
		t.Errorf("expected general 4, got %d", restored.ProposedProfile.Capabilities[CapGeneral])
	}
}
