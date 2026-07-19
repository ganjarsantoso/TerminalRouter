package smart

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/execution"
	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/router"
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
	activeRuns      map[string]context.CancelFunc
	store           AssessmentPersister
	credChecker     CredChecker
	providerChecker ProviderChecker
	profiles        *ProfileStore
	coord           *execution.Coordinator
	cfg             *config.Config
	// DisableAutoRun skips background execution after Start (used by unit tests).
	DisableAutoRun bool
}

// NewModelAssessmentService creates a new assessment service.
func NewModelAssessmentService(
	store *storage.Store,
	credChecker CredChecker,
	providerChecker ProviderChecker,
	profiles *ProfileStore,
	coord *execution.Coordinator,
	cfg *config.Config,
) *ModelAssessmentService {
	return &ModelAssessmentService{
		activeRuns:      map[string]context.CancelFunc{},
		store:           newStorageBridge(store),
		credChecker:     credChecker,
		providerChecker: providerChecker,
		profiles:        profiles,
		coord:           coord,
		cfg:             cfg,
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
	if len(s.activeRuns) > 0 {
		res.ConflictingRun = true
		res.Eligible = false
		res.Reasons = append(res.Reasons, "assessment already running for this model")
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

	if !s.DisableAutoRun {
		s.launchRun(aid)
	}

	return rec, nil
}

// launchRun starts background assessment execution for the given assessment ID.
func (s *ModelAssessmentService) launchRun(assessmentID string) {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.activeRuns[assessmentID] = cancel
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.activeRuns, assessmentID)
			s.mu.Unlock()
			cancel()
		}()
		s.executeAssessment(ctx, assessmentID)
	}()
}

// Execute runs (or re-runs) assessment scoring for an existing record.
// Prefer Start, which launches this automatically.
func (s *ModelAssessmentService) Execute(assessmentID string) {
	s.launchRun(assessmentID)
}

// executeAssessment sends real test prompts to the model for each category
// and scores responses based on actual output quality.
func (s *ModelAssessmentService) executeAssessment(ctx context.Context, assessmentID string) {
	rec, err := s.store.GetAssessment(context.Background(), assessmentID)
	if err != nil || rec == nil {
		return
	}
	if rec.Status != StatusPending && rec.Status != StatusRunning {
		return
	}
	if s.coord == nil {
		now := time.Now().UTC()
		rec.Status = StatusFailed
		rec.CompletedAt = &now
		_ = s.store.UpdateAssessment(context.Background(), rec)
		return
	}

	rec.Status = StatusRunning
	_ = s.store.UpdateAssessment(context.Background(), rec)

	key := ProfileKey(rec.ProviderID, rec.ModelID)
	p, hasProvider := s.providerConfig(rec.ProviderID)
	if !hasProvider {
		now := time.Now().UTC()
		rec.Status = StatusFailed
		rec.CompletedAt = &now
		_ = s.store.UpdateAssessment(context.Background(), rec)
		return
	}

	plan := &router.Plan{
		Strategy:    "direct",
		PublicModel: rec.ModelID,
		Attempts: []router.Attempt{{
			ProviderID: rec.ProviderID,
			Model:      rec.ModelID,
			Config:     p,
		}},
	}

	_, streamingKnown, toolsKnown := false, false, false
	if s.providerChecker != nil {
		_, streamingKnown, toolsKnown = s.providerChecker(rec.ProviderID, rec.ModelID)
	}

	cats := make([]AssessmentCategory, len(rec.Categories))
	copy(cats, rec.Categories)
	totalIn, totalOut := 0, 0

	for i := range cats {
		if ctx.Err() != nil {
			rec.Status = StatusCancelled
			finishAssessment(rec, cats)
			_ = s.store.UpdateAssessment(context.Background(), rec)
			return
		}

		cats[i].Status = StatusRunning
		rec.Categories = cats
		_ = s.store.UpdateAssessment(context.Background(), rec)

		select {
		case <-ctx.Done():
			rec.Status = StatusCancelled
			finishAssessment(rec, cats)
			_ = s.store.UpdateAssessment(context.Background(), rec)
			return
		default:
		}

		test, ok := categoryTests[cats[i].Name]
		if !ok {
			cats[i].Status = StatusCompleted
			cats[i].Score = 0
			cats[i].Confidence = 0.3
			cats[i].Evidence = "no test prompt defined for this category"
			rec.Categories = cats
			_ = s.store.UpdateAssessment(context.Background(), rec)
			continue
		}

		nreq := &normalization.NormalizedRequest{
			ID:             assessmentID + "_" + cats[i].Name,
			RequestedModel: rec.ModelID,
			Messages: []normalization.Message{{
				Role: normalization.RoleUser,
				Content: []normalization.ContentBlock{{
					Type: normalization.ContentText,
					Text: test.prompt,
				}},
			}},
		}

		if test.system != "" {
			nreq.System = test.system
		}

		start := time.Now()
		result, execErr := s.coord.Execute(ctx, nreq, plan)
		latency := time.Since(start)

		score := 0.0
		passed := 0.0
		total := 1.0
		evidence := ""
		conf := 0.5

		if execErr != nil {
			score = 0
			conf = 0.2
			evidence = fmt.Sprintf("execution error: %s", execErr.Error())
			cats[i].Status = StatusFailed
			rec.Error = fmt.Sprintf("%s failed: %s", cats[i].Name, execErr.Error())
			if rec.Status == StatusRunning {
				rec.Status = StatusPartial
			}
		} else {
			var respText string
			if result.Response != nil {
				for _, b := range result.Response.Content {
					if b.Type == normalization.ContentText {
						respText += b.Text
					}
				}
			}
			if result.Response != nil {
				totalIn += result.Response.Usage.InputTokens
				totalOut += result.Response.Usage.OutputTokens
			}
			score, passed, total, evidence = test.eval(respText, latency)
			if score > 0 {
				conf = 0.6 + (score/10.0)*0.35
				if conf > 0.95 {
					conf = 0.95
				}
			}
			cats[i].Status = StatusCompleted
		}

		cats[i].Score = score
		cats[i].Confidence = conf
		cats[i].TestsPassed = passed
		cats[i].TestsTotal = total
		cats[i].Evidence = evidence
		cats[i].LatencyMs = int(latency.Milliseconds())

		rec.Categories = cats
		rec.InputTokens = totalIn
		rec.OutputTokens = totalOut
		_ = s.store.UpdateAssessment(context.Background(), rec)
	}

	proposed := ModelProfile{
		ID:           key,
		ProviderID:   rec.ProviderID,
		ModelID:      rec.ModelID,
		Version:      rec.BenchmarkVersion,
		Source:       SourceSelfAssess,
		Capabilities: map[string]float64{},
	}
	if streamingKnown {
		proposed.Properties.Streaming = boolPtr(true)
	}
	if toolsKnown {
		proposed.Properties.Tools = boolPtr(true)
	}
	totalCats := len(cats)
	failedCats := 0
	var errMsgs []string
	for _, cat := range cats {
		if cat.Score > 0 {
			proposed.Capabilities[cat.Name] = cat.Score
		}
		if cat.Status == StatusFailed {
			failedCats++
			errMsgs = append(errMsgs, fmt.Sprintf("%s: %s", cat.Name, cat.Evidence))
		}
	}

	now := time.Now().UTC()
	rec.CompletedAt = &now
	rec.Categories = cats
	rec.Confidence = ComputeConfidence(cats, rec.Depth)
	rec.ProposedProfile = &proposed
	rec.InputTokens = totalIn
	rec.OutputTokens = totalOut

	switch {
	case failedCats == totalCats:
		rec.Status = StatusFailed
		rec.Error = fmt.Sprintf("all %d categories failed: %s", totalCats, strings.Join(errMsgs, "; "))
	case failedCats > 0:
		rec.Status = StatusPartial
		rec.Error = fmt.Sprintf("%d/%d categories failed: %s", failedCats, totalCats, strings.Join(errMsgs, "; "))
	default:
		rec.Status = StatusCompleted
	}
	_ = s.store.UpdateAssessment(context.Background(), rec)
}

func (s *ModelAssessmentService) providerConfig(providerID string) (config.ProviderConfig, bool) {
	if s.cfg == nil {
		return config.ProviderConfig{}, false
	}
	p, ok := s.cfg.Providers[providerID]
	return p, ok
}

func finishAssessment(rec *AssessmentRecord, cats []AssessmentCategory) {
	rec.Categories = cats
}

// categoryTest defines a single real test prompt and its evaluation function.
type categoryTest struct {
	prompt string
	system string
	eval   func(response string, latency time.Duration) (score, passed, total float64, evidence string)
}

// categoryTests maps capability names to real test prompts that exercise each dimension.
var categoryTests = map[string]categoryTest{
	CapGeneral: {
		prompt: "Explain what artificial intelligence is in 2-3 sentences. Be concise and accurate.",
		eval:   evalGeneral,
	},
	CapReasoning: {
		prompt: "If a bat and a ball cost $1.10 in total, and the bat costs $1.00 more than the ball, how much does the ball cost? Think step by step before giving your final answer.",
		eval:   evalReasoning,
	},
	CapAnalysis: {
		prompt: "Compare and contrast REST APIs and GraphQL APIs. List two advantages of each approach.",
		eval:   evalAnalysis,
	},
	CapCoding: {
		prompt: "Write a Python function called `is_palindrome` that checks if a string is a palindrome. Include an example usage.",
		eval:   evalCoding,
	},
	CapWriting: {
		prompt: "Write a short paragraph describing a sunset over the ocean. Use vivid language and include sensory details.",
		eval:   evalWriting,
	},
	CapToolUse: {
		prompt: "Can you use tools or functions to answer questions? If yes, list what tools you support.",
		eval:   evalToolUse,
	},
	CapInstructionFollowing: {
		prompt: "Respond with only the single word 'blue' and nothing else. Do not add any other text.",
		eval:   evalInstructionFollowing,
	},
	CapStructuredOutput: {
		prompt: "Give me a JSON object representing a person with the following fields: name (string), age (number), city (string), is_student (boolean). Return ONLY valid JSON, no other text.",
		eval:   evalStructuredOutput,
	},
	CapLongContext: {
		prompt: "Summarize the following text in one sentence:\n\nThe quick brown fox jumps over the lazy dog. This pangram contains every letter of the English alphabet at least once. Pangrams are often used to display typefaces and test computer keyboards. The earliest known pangram, 'The quick brown fox jumps over the lazy dog,' has been in use since at least the late 19th century.",
		eval:   evalLongContext,
	},
	CapMultilingual: {
		prompt: "Translate these three words into French, Spanish, and German: 'hello', 'thank you', 'goodbye'. Format each language on a separate line.",
		eval:   evalMultilingual,
	},
	CapMathematics: {
		prompt: "What is 15 multiplied by 37? Show your work step by step.",
		eval:   evalMathematics,
	},
	CapSummarization: {
		prompt: "Summarize the following article in 2-3 sentences:\n\nClimate change poses one of the most significant challenges of our time. Rising global temperatures have led to more frequent extreme weather events, including hurricanes, droughts, and heatwaves. Scientists warn that without immediate and sustained reductions in greenhouse gas emissions, the impacts will become increasingly severe. Many countries have pledged to achieve net-zero emissions by 2050, though critics argue the current commitments are insufficient to meet the Paris Agreement goals.",
		eval: evalSummarization,
	},
	CapExtraction: {
		prompt: "Extract all email addresses from this text and list them:\n\n'You can reach our sales team at sales@example.com or support@test.org. For partnerships, contact partners@company.co.uk. Our office phone is 555-0123.'",
		eval:   evalExtraction,
	},
}

func evalGeneral(resp string, lat time.Duration) (float64, float64, float64, string) {
	length := len(resp)
	if length < 20 {
		return 1, 0, 1, "response too short"
	}
	if length > 200 {
		return 8, 1, 1, fmt.Sprintf("good length (%d chars), coherent response", length)
	}
	return 6, 1, 1, fmt.Sprintf("adequate response (%d chars)", length)
}

func evalReasoning(resp string, lat time.Duration) (float64, float64, float64, string) {
	low := strings.ToLower(resp)
	if strings.Contains(low, "0.05") || strings.Contains(low, "5 cents") || strings.Contains(low, "5¢") || strings.Contains(low, "$0.05") {
		if strings.Contains(low, "step") || strings.Contains(low, "because") || strings.Contains(low, "therefore") || strings.Contains(low, "so ") {
			return 10, 2, 2, "correct answer with step-by-step reasoning"
		}
		return 8, 1, 2, "correct answer but limited reasoning shown"
	}
	if strings.Contains(low, "10") || strings.Contains(low, "0.10") || strings.Contains(low, "ten") {
		return 1, 0, 1, "gave common wrong answer (10 cents)"
	}
	if len(resp) > 50 {
		return 3, 0, 1, "attempted reasoning but incorrect answer"
	}
	return 1, 0, 1, "no meaningful reasoning"
}

func evalAnalysis(resp string, lat time.Duration) (float64, float64, float64, string) {
	low := strings.ToLower(resp)
	hasRest := strings.Contains(low, "rest")
	hasGraphql := strings.Contains(low, "graphql")
	advantages := 0
	if strings.Contains(low, "advantage") || strings.Contains(low, "pro") || strings.Contains(low, "benefit") {
		advantages++
	}
	if strings.Contains(low, "simplicity") || strings.Contains(low, "simple") || strings.Contains(low, "easier") {
		advantages++
	}
	if strings.Contains(low, "flexib") || strings.Contains(low, "flexible") {
		advantages++
	}
	if strings.Contains(low, "cach") || strings.Contains(low, "cache") {
		advantages++
	}
	if !hasRest && !hasGraphql {
		return 1, 0, 1, "did not discuss REST or GraphQL meaningfully"
	}
	if len(resp) < 80 {
		return 3, 0, 1, "too brief for a comparison"
	}
	if hasRest && hasGraphql && advantages >= 1 {
		return 8, 2, 2, "good comparison covering both technologies"
	}
	return 6, 1, 2, "adequate but shallow comparison"
}

func evalCoding(resp string, lat time.Duration) (float64, float64, float64, string) {
	hasCodeBlock := strings.Contains(resp, "```")
	hasDef := strings.Contains(resp, "def is_palindrome") || strings.Contains(resp, "def is_palindrome(")
	hasReturn := strings.Contains(resp, "return") || strings.Contains(resp, "print")
	if hasCodeBlock && hasDef && hasReturn {
		return 10, 3, 3, "complete code with function definition and example"
	}
	if hasDef && hasReturn {
		return 8, 2, 3, "function defined but missing code block formatting"
	}
	if strings.Contains(resp, "palindrome") && len(resp) > 60 {
		return 6, 1, 3, "discussed palindrome but incomplete code"
	}
	if len(resp) < 30 {
		return 1, 0, 3, "no meaningful code produced"
	}
	return 3, 1, 3, "partial or incorrect code"
}

func evalWriting(resp string, lat time.Duration) (float64, float64, float64, string) {
	sentences := len(strings.Split(resp, "."))
	words := len(strings.Fields(resp))
	if words > 40 && sentences >= 3 {
		return 10, 3, 3, "vivid, well-structured writing with sensory detail"
	}
	if words > 20 && sentences >= 2 {
		return 8, 2, 3, "adequate descriptive writing"
	}
	if words > 10 {
		return 6, 1, 3, "minimal but on topic"
	}
	return 3, 0, 3, "too short, poor quality"
}

func evalToolUse(resp string, lat time.Duration) (float64, float64, float64, string) {
	low := strings.ToLower(resp)
	if strings.Contains(low, "yes") || strings.Contains(low, "function") || strings.Contains(low, "tool") {
		if len(resp) > 30 {
			return 8, 2, 2, "acknowledged tool use capability with details"
		}
		return 6, 1, 2, "acknowledged tool use"
	}
	return 3, 0, 2, "no clear indication of tool use support"
}

func evalInstructionFollowing(resp string, lat time.Duration) (float64, float64, float64, string) {
	trimmed := strings.TrimSpace(resp)
	lowTrimmed := strings.ToLower(trimmed)
	if lowTrimmed == "blue" || lowTrimmed == "\"blue\"" || lowTrimmed == "'blue'" || lowTrimmed == "blue." {
		return 10, 1, 1, "perfectly followed instruction"
	}
	if strings.Contains(lowTrimmed, "blue") && len(trimmed) < 20 {
		return 6, 0, 1, "contains 'blue' but added extra text"
	}
	return 1, 0, 1, "did not follow instruction"
}

var jsonLike = regexp.MustCompile(`\{[^}]*\}`)

func evalStructuredOutput(resp string, lat time.Duration) (float64, float64, float64, string) {
	matches := jsonLike.FindString(resp)
	if matches == "" {
		// Try to find JSON object
		return 1, 0, 1, "no JSON object found in response"
	}
	var parsed any
	if err := json.Unmarshal([]byte(matches), &parsed); err != nil {
		return 3, 0, 1, "response contains JSON-like structure but not valid JSON"
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return 3, 0, 1, "response is valid JSON but not an object"
	}
	fields := 0
	if _, ok := obj["name"]; ok {
		fields++
	}
	if _, ok := obj["age"]; ok {
		fields++
	}
	if _, ok := obj["city"]; ok {
		fields++
	}
	if _, ok := obj["is_student"]; ok {
		fields++
	}
	if fields >= 4 {
		return 10, 4, 4, "valid JSON with all required fields"
	}
	return 6, float64(fields), 4, fmt.Sprintf("valid JSON with %d/4 required fields", fields)
}

func evalLongContext(resp string, lat time.Duration) (float64, float64, float64, string) {
	low := strings.ToLower(resp)
	if len(resp) < 20 {
		return 1, 0, 1, "response too short"
	}
	if strings.Contains(low, "pangram") || strings.Contains(low, "fox") || strings.Contains(low, "alphabet") || strings.Contains(low, "keyboard") || strings.Contains(low, "typeface") {
		return 8, 2, 2, "accurately summarized the key points"
	}
	return 3, 0, 2, "summary missing key details"
}

func evalMultilingual(resp string, lat time.Duration) (float64, float64, float64, string) {
	low := strings.ToLower(resp)
	languages := 0
	if strings.Contains(low, "bonjour") || strings.Contains(low, "merci") || strings.Contains(low, "au revoir") || strings.Contains(resp, "hello") {
		languages++
	}
	if strings.Contains(low, "hola") || strings.Contains(low, "gracias") || strings.Contains(low, "adiós") || strings.Contains(low, "adios") {
		languages++
	}
	if strings.Contains(low, "hallo") || strings.Contains(low, "danke") || strings.Contains(low, "auf wiedersehen") || strings.Contains(low, "tschüss") || strings.Contains(low, "tschus") {
		languages++
	}
	langCount := 0
	if strings.Contains(low, "french") || strings.Contains(low, "fran") || strings.Contains(low, "français") || strings.Contains(low, "francais") {
		langCount++
	}
	if strings.Contains(low, "spanish") || strings.Contains(low, "español") || strings.Contains(low, "espanol") {
		langCount++
	}
	if strings.Contains(low, "german") || strings.Contains(low, "deutsch") {
		langCount++
	}
	if languages >= 2 && langCount >= 2 {
		return 10, 3, 3, "good multilingual response with translations"
	}
	if languages >= 1 || langCount >= 2 {
		return 6, 1, 3, "partial multilingual response"
	}
	return 1, 0, 3, "no meaningful translation"
}

func evalMathematics(resp string, lat time.Duration) (float64, float64, float64, string) {
	low := strings.ToLower(resp)
	if strings.Contains(resp, "555") || strings.Contains(resp, "15*37") || strings.Contains(resp, "15 × 37") {
		if strings.Contains(low, "step") || strings.Contains(low, "×") || strings.Contains(resp, "+") || strings.Contains(resp, "=") {
			return 10, 2, 2, "correct answer with step-by-step work"
		}
		return 8, 1, 2, "correct answer but limited work shown"
	}
	if len(resp) > 50 {
		return 3, 0, 2, "attempted but incorrect answer"
	}
	return 1, 0, 2, "no meaningful calculation"
}

func evalSummarization(resp string, lat time.Duration) (float64, float64, float64, string) {
	low := strings.ToLower(resp)
	if len(resp) < 30 {
		return 1, 0, 1, "summary too short"
	}
	keyPoints := 0
	if strings.Contains(low, "climate") || strings.Contains(low, "warming") || strings.Contains(low, "temperature") {
		keyPoints++
	}
	if strings.Contains(low, "emission") || strings.Contains(low, "greenhouse") || strings.Contains(low, "carbon") {
		keyPoints++
	}
	if strings.Contains(low, "2050") || strings.Contains(low, "net-zero") || strings.Contains(low, "net zero") || strings.Contains(low, "paris") {
		keyPoints++
	}
	if strings.Contains(low, "weather") || strings.Contains(low, "extreme") || strings.Contains(low, "hurricane") || strings.Contains(low, "drought") {
		keyPoints++
	}
	if keyPoints >= 3 {
		return 10, 3, 3, "comprehensive summary covering key points"
	}
	if keyPoints >= 2 {
		return 8, 2, 3, "good summary with some key points"
	}
	if keyPoints >= 1 {
		return 6, 1, 3, "basic summary"
	}
	return 3, 0, 3, "poor summary missing key information"
}

func evalExtraction(resp string, lat time.Duration) (float64, float64, float64, string) {
	emails := []string{"sales@example.com", "support@test.org", "partners@company.co.uk"}
	found := 0
	for _, e := range emails {
		if strings.Contains(resp, e) {
			found++
		}
	}
	switch found {
	case 3:
		return 10, 3, 3, "all email addresses correctly extracted"
	case 2:
		return 8, 2, 3, fmt.Sprintf("%d/3 email addresses found", found)
	case 1:
		return 6, 1, 3, fmt.Sprintf("only %d/3 email addresses found", found)
	default:
		return 1, 0, 3, "no email addresses extracted"
	}
}

// Cancel stops a running assessment.
func (s *ModelAssessmentService) Cancel(assessmentID string) error {
	s.mu.Lock()
	cancel, ok := s.activeRuns[assessmentID]
	if ok {
		delete(s.activeRuns, assessmentID)
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	rec, err := s.store.GetAssessment(context.Background(), assessmentID)
	if err != nil || rec == nil {
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
		Capabilities: map[string]float64{},
		Properties:   current.Properties,
	}

	var diffs []ProfileFieldDiff
	for _, cat := range rec.Categories {
		if cat.Score > 0 {
			proposed.Capabilities[cat.Name] = cat.Score
		}

		currentVal := current.Capabilities[cat.Name]
		if currentVal != cat.Score {
			src := current.Source
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

// ComputeCategoryScore maps a pass rate to a 0-10 score.
func ComputeCategoryScore(passed, total float64) float64 {
	if total == 0 {
		return 0
	}
	rate := passed / total
	switch {
	case rate >= 0.95:
		return 10
	case rate >= 0.80:
		return 8
	case rate >= 0.60:
		return 6
	case rate >= 0.30:
		return 3
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
