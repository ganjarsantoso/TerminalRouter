// Package smart implements task-aware model selection (Smart Routes).
// It selects which configured model should answer; it does not solve the task.
package smart

import (
	"time"
)

const ClassifierVersion = "heuristic-v1"
const CatalogVersion = "builtin-v1"

// Capability dimension names (1–10 scale; 0 = unknown; 0.5 increments).
const (
	CapGeneral               = "general"
	CapReasoning             = "reasoning"
	CapAnalysis              = "analysis"
	CapCoding                = "coding"
	CapWriting               = "writing"
	CapToolUse               = "tool_use"
	CapInstructionFollowing  = "instruction_following"
	CapStructuredOutput      = "structured_output"
	CapLongContext           = "long_context"
	CapMultilingual          = "multilingual"
	CapMathematics           = "mathematics"
	CapSummarization         = "summarization"
	CapExtraction            = "extraction"
)

// AllCapabilities is the ordered list of capability dimensions.
var AllCapabilities = []string{
	CapGeneral, CapReasoning, CapAnalysis, CapCoding, CapWriting,
	CapToolUse, CapInstructionFollowing, CapStructuredOutput,
	CapLongContext, CapMultilingual, CapMathematics,
	CapSummarization, CapExtraction,
}

// Privacy classes for candidates.
const (
	PrivacyLocal        = "local"
	PrivacyPrivateCloud = "private-cloud"
	PrivacyCloud        = "cloud"
)

// Source identifies where a profile field came from.
const (
	SourceBuiltin     = "builtin"
	SourceUser        = "user"
	SourceObserved    = "observed"
	SourceSelfAssess  = "self-assessment"
	SourceExternal    = "external-consensus"
	SourceUnknown     = "unknown"
)

const AssessmentVersion = "assessment-v1"
const BenchmarkPackVersion = "benchmark-v1"

// Smart modes.
const (
	ModeOff    = "off"
	ModeShadow = "shadow"
	ModeLive   = "live"
)

// Built-in policy names.
const (
	PolicyBalanced = "balanced"
	PolicyQuality  = "quality"
	PolicyEconomy  = "economy"
	PolicyFast     = "fast"
	PolicyPrivate  = "private"
)

// Task type categories (MVP).
const (
	TypeGeneralChat           = "general_chat"
	TypeSimpleTransformation  = "simple_transformation"
	TypeSummarization         = "summarization"
	TypeInformationExtraction = "information_extraction"
	TypeCreativeWriting       = "creative_writing"
	TypeProfessionalWriting   = "professional_writing"
	TypeTranslation           = "translation"
	TypeCodingGeneration      = "coding_generation"
	TypeCodingDebug           = "coding_debug"
	TypeCodeReview            = "code_review"
	TypeTechnicalExplanation  = "technical_explanation"
	TypeArchitectureDesign    = "architecture_design"
	TypeReasoning             = "reasoning"
	TypeMathematics           = "mathematics"
	TypeAnalysis              = "analysis"
	TypeResearchSynthesis     = "research_synthesis"
	TypeToolOperation         = "tool_operation"
	TypeUnknown               = "unknown"
)

// Complexity levels.
const (
	ComplexitySimple  = "simple"
	ComplexityMedium  = "medium"
	ComplexityComplex = "complex"
)

// ModelProperties are factual support and operational properties.
type ModelProperties struct {
	Vision           *bool  `json:"vision,omitempty" yaml:"vision,omitempty"`
	Tools            *bool  `json:"tools,omitempty" yaml:"tools,omitempty"`
	ParallelTools    *bool  `json:"parallel_tools,omitempty" yaml:"parallel_tools,omitempty"`
	StructuredOutput *bool  `json:"structured_output,omitempty" yaml:"structured_output,omitempty"`
	Streaming        *bool  `json:"streaming,omitempty" yaml:"streaming,omitempty"`
	ContextWindow    int    `json:"context_window,omitempty" yaml:"context_window,omitempty"`
	MaxOutputTokens  int    `json:"max_output_tokens,omitempty" yaml:"max_output_tokens,omitempty"`
	CostTier         int    `json:"cost_tier,omitempty" yaml:"cost_tier,omitempty"`     // 1–5
	LatencyTier      int    `json:"latency_tier,omitempty" yaml:"latency_tier,omitempty"` // 1=fastest, 5=slowest
	Privacy          string `json:"privacy,omitempty" yaml:"privacy,omitempty"`           // local | private-cloud | cloud
}

// ModelProfile describes a specific provider+model deployment.
type ModelProfile struct {
	ID           string         `json:"id" yaml:"-"`
	ProviderID   string         `json:"provider_id" yaml:"-"`
	ModelID      string         `json:"model_id" yaml:"-"`
	Version      string         `json:"version" yaml:"version,omitempty"`
	Source       string           `json:"source" yaml:"source,omitempty"`
	Capabilities map[string]float64 `json:"capabilities" yaml:"capabilities,omitempty"`
	Properties   ModelProperties `json:"properties" yaml:"properties,omitempty"`
}

// ProfileKey returns provider/model id used in catalogs and CLI.
func ProfileKey(providerID, modelID string) string {
	return providerID + "/" + modelID
}

// HardRequirements are mandatory request constraints.
type HardRequirements struct {
	Tools                bool `json:"tools"`
	Vision               bool `json:"vision"`
	StructuredOutput     bool `json:"structured_output"`
	ParallelTools        bool `json:"parallel_tools"`
	MinimumContextWindow int  `json:"minimum_context_window"`
	MaxOutputTokens      int  `json:"max_output_tokens"`
}

// TaskPreferences are soft preference signals from classification.
type TaskPreferences struct {
	Latency string `json:"latency"` // low | medium | high
	Cost    string `json:"cost"`    // low | balanced | high
	Privacy string `json:"privacy"` // any | local | private
}

// TaskProfile is the structured analysis of a request.
type TaskProfile struct {
	PrimaryType      string           `json:"primary_type"`
	SecondaryTypes   []string         `json:"secondary_types,omitempty"`
	Complexity       string             `json:"complexity"`
	Requirements     map[string]float64 `json:"requirements"`
	HardRequirements HardRequirements `json:"hard_requirements"`
	Preferences      TaskPreferences  `json:"preferences"`
	Confidence       float64          `json:"confidence"`
	Classifier       string           `json:"classifier"`
	ClassifierVersion string          `json:"classifier_version"`
}

// PolicyWeights are scoring weights (should sum ~1.0 after normalization).
type PolicyWeights struct {
	TaskMatch          float64 `json:"task_match" yaml:"task_match"`
	SpecializedMatch   float64 `json:"specialized_match" yaml:"specialized_match"`
	Quality            float64 `json:"quality" yaml:"quality"`
	Reliability        float64 `json:"reliability" yaml:"reliability"`
	Cost               float64 `json:"cost" yaml:"cost"`
	Latency            float64 `json:"latency" yaml:"latency"`
}

// Policy is a named routing policy.
type Policy struct {
	Name               string        `json:"name"`
	Weights            PolicyWeights `json:"weights"`
	AllowedPrivacy     []string      `json:"allowed_privacy,omitempty"`
	MaxCostTier        int           `json:"max_cost_tier,omitempty"` // 0 = no ceiling
	MinimumTaskMatch   float64       `json:"minimum_task_match,omitempty"`
}

// Candidate is a selectable provider/model pair on a smart route.
type Candidate struct {
	Provider  string
	Model     string
	ProfileID string // optional explicit profile key
	Order     int    // configuration order for tie-break
}

// ComponentScores holds per-dimension contribution to final score.
type ComponentScores struct {
	TaskMatch        float64 `json:"task_match"`
	SpecializedMatch float64 `json:"specialized_match"`
	Quality          float64 `json:"quality"`
	Reliability      float64 `json:"reliability"`
	Cost             float64 `json:"cost"`
	Latency          float64 `json:"latency"`
	HealthPenalty    float64 `json:"health_penalty"`
	Uncertainty      float64 `json:"uncertainty_penalty"`
}

// CandidateEvaluation is the result of filtering/scoring one candidate.
type CandidateEvaluation struct {
	Provider         string          `json:"provider"`
	Model            string          `json:"model"`
	ProfileID        string          `json:"profile_id"`
	Eligible         bool            `json:"eligible"`
	RejectionReasons []string        `json:"rejection_reasons,omitempty"`
	Components       ComponentScores `json:"components,omitempty"`
	FinalScore       float64         `json:"final_score"`
	Explanation      []string        `json:"explanation,omitempty"`
	TaskMatch        float64         `json:"task_match"`
}

// SessionAffinityResult records affinity influence on the decision.
type SessionAffinityResult struct {
	Hit            bool   `json:"hit"`
	SessionID      string `json:"session_id,omitempty"`
	PinnedProvider string `json:"pinned_provider,omitempty"`
	PinnedModel    string `json:"pinned_model,omitempty"`
	Reclassified   bool   `json:"reclassified"`
	Reason         string `json:"reason,omitempty"`
}

// Decision is an immutable smart-routing decision.
type Decision struct {
	RequestID            string                 `json:"request_id"`
	RouteID              string                 `json:"route_id"`
	RequestedAlias       string                 `json:"requested_alias,omitempty"`
	Mode                 string                 `json:"mode"`
	Policy               string                 `json:"policy"`
	Task                 TaskProfile            `json:"task"`
	Evaluations          []CandidateEvaluation  `json:"evaluations"`
	SelectedProvider     string                 `json:"selected_provider,omitempty"`
	SelectedModel        string                 `json:"selected_model,omitempty"`
	SelectionScore       float64                `json:"selection_score,omitempty"`
	SelectionReasons     []string               `json:"selection_reasons,omitempty"`
	ShadowRecommendation string                 `json:"shadow_recommendation,omitempty"` // provider/model
	UsedDefault          bool                   `json:"used_default"`
	DefaultReason        string                 `json:"default_reason,omitempty"`
	SessionAffinity      SessionAffinityResult  `json:"session_affinity"`
	CatalogVersion       string                 `json:"catalog_version"`
	CreatedAt            time.Time              `json:"created_at"`
}

// SelectedKey returns provider/model for the selected target.
func (d *Decision) SelectedKey() string {
	if d.SelectedProvider == "" {
		return ""
	}
	return ProfileKey(d.SelectedProvider, d.SelectedModel)
}

// Override holds optional client/request overrides for smart selection.
type Override struct {
	Policy       string
	MaxCostTier  int
	SessionID    string
	Reclassify   bool
	RequireCaps  []string // e.g. coding, tools
}

// AssessmentDepth defines how thorough an assessment is.
type AssessmentDepth string

const (
	DepthQuick        AssessmentDepth = "quick"
	DepthStandard     AssessmentDepth = "standard"
	DepthComprehensive AssessmentDepth = "comprehensive"
)

// AssessmentStatus tracks the lifecycle of an assessment.
type AssessmentStatus string

const (
	StatusPending     AssessmentStatus = "pending"
	StatusRunning     AssessmentStatus = "running"
	StatusCompleted   AssessmentStatus = "completed"
	StatusFailed      AssessmentStatus = "failed"
	StatusCancelled   AssessmentStatus = "cancelled"
	StatusPartial     AssessmentStatus = "partial"
)

// ProfileStatus indicates the overall profiled state of a model.
type ProfileStatus string

const (
	ProfileNotProfiled       ProfileStatus = "not_profiled"
	ProfileBuiltIn           ProfileStatus = "built_in"
	ProfileAssessmentAvail   ProfileStatus = "assessment_available"
	ProfileAssessed          ProfileStatus = "assessed"
	ProfileUserModified      ProfileStatus = "user_modified"
	ProfileAssessmentOutdated ProfileStatus = "assessment_outdated"
	ProfileAssessmentFailed  ProfileStatus = "assessment_failed"
)

// AssessmentCategory is a single benchmark category.
type AssessmentCategory struct {
	Name        string           `json:"name"`
	Status      AssessmentStatus `json:"status"`
	Score       float64          `json:"score"`              // 0-10 (0.5 increments)
	Confidence  float64          `json:"confidence"`          // 0.0-1.0
	TestsPassed float64         `json:"tests_passed"`
	TestsTotal  float64          `json:"tests_total"`
	LatencyMs   int              `json:"latency_ms,omitempty"`
	Evidence    string           `json:"evidence,omitempty"` // summary text
}

// AssessmentPlan describes the tests to run.
type AssessmentPlan struct {
	ProviderID      string          `json:"provider_id"`
	ModelID         string          `json:"model_id"`
	Depth           AssessmentDepth `json:"depth"`
	Categories      []string        `json:"categories"`
	MaxRequests     int             `json:"max_requests"`
	MaxTokens       int             `json:"max_tokens"`
	MaxCost         float64         `json:"max_cost,omitempty"`
	RequestTimeout  time.Duration   `json:"request_timeout_ns,omitempty"`
	OverallTimeout  time.Duration   `json:"overall_timeout_ns,omitempty"`
	Concurrency     int             `json:"concurrency"`
}

// AssessmentEstimate contains preflight usage estimates.
type AssessmentEstimate struct {
	ProviderID       string   `json:"provider_id"`
	ModelID          string   `json:"model_id"`
	Depth            AssessmentDepth `json:"depth"`
	RequestCount     int      `json:"request_count"`
	EstimatedTokens  int      `json:"estimated_tokens"`
	EstimatedCost    float64  `json:"estimated_cost,omitempty"`
	CostKnown        bool     `json:"cost_known"`
	LeavesLocal      bool     `json:"leaves_local"`
	ToolTestsRun     bool     `json:"tool_tests_run"`
	StreamingTests   bool     `json:"streaming_tests"`
	Categories       []string `json:"categories"`
}

// AssessmentRecord is a persisted assessment run.
type AssessmentRecord struct {
	AssessmentID       string            `json:"assessment_id"`
	ProviderID         string            `json:"provider_id"`
	ModelID            string            `json:"model_id"`
	ConnectionFingerprint string         `json:"connection_fingerprint,omitempty"`
	Status             AssessmentStatus  `json:"status"`
	Depth              AssessmentDepth   `json:"depth"`
	BenchmarkVersion   string            `json:"benchmark_version"`
	ScoringVersion     string            `json:"scoring_version"`
	Categories         []AssessmentCategory `json:"categories"`
	StartedAt          *time.Time        `json:"started_at,omitempty"`
	CompletedAt        *time.Time        `json:"completed_at,omitempty"`
	EstimatedTokens    int               `json:"estimated_tokens"`
	InputTokens        int               `json:"input_tokens"`
	OutputTokens       int               `json:"output_tokens"`
	EstimatedCost      float64           `json:"estimated_cost,omitempty"`
	ActualCost         float64           `json:"actual_cost,omitempty"`
	Confidence         float64           `json:"confidence"` // overall
	ProposedProfile    *ModelProfile     `json:"proposed_profile,omitempty"`
	AppliedAt          *time.Time        `json:"applied_at,omitempty"`
	AppliedFields      []string          `json:"applied_fields,omitempty"`
	Error              string            `json:"error,omitempty"`
}

// AssessmentProposal is the reviewable output of an assessment.
type AssessmentProposal struct {
	AssessmentID      string                  `json:"assessment_id"`
	ProviderID        string                  `json:"provider_id"`
	ModelID           string                  `json:"model_id"`
	Depth             AssessmentDepth         `json:"depth"`
	CurrentProfile    *ModelProfile           `json:"current_profile"`
	ProposedProfile   *ModelProfile           `json:"proposed_profile"`
	Differences       []ProfileFieldDiff      `json:"differences"`
	CategoryResults   []AssessmentCategory    `json:"category_results"`
	OverallConfidence float64                 `json:"overall_confidence"`
	AffectedRoutes    []string                `json:"affected_routes,omitempty"`
	BenchmarkVersion  string                  `json:"benchmark_version"`
	CreatedAt         time.Time               `json:"created_at"`
}

// ProfileFieldDiff shows a before/after for one profile field.
type ProfileFieldDiff struct {
	Field          string  `json:"field"`
	CurrentValue   any     `json:"current_value"`
	ProposedValue  any     `json:"proposed_value"`
	Source         string  `json:"source"`
	Confidence     float64 `json:"confidence"`
	Recommended    bool    `json:"recommended"`
}

// ApplyProposalRequest is the payload to apply an assessment proposal.
type ApplyProposalRequest struct {
	AssessmentID         string   `json:"assessment_id"`
	AcceptedFields       []string `json:"accepted_fields"` // empty = all
	PreserveUserOverrides bool    `json:"preserve_user_overrides"`
}

// AssessmentSummary is a lightweight row for listing.
type AssessmentSummary struct {
	AssessmentID      string           `json:"assessment_id"`
	ProviderID        string           `json:"provider_id"`
	ModelID           string           `json:"model_id"`
	Status            AssessmentStatus `json:"status"`
	Depth             AssessmentDepth  `json:"depth"`
	BenchmarkVersion  string           `json:"benchmark_version"`
	OverallConfidence float64          `json:"overall_confidence"`
	StartedAt         *time.Time       `json:"started_at,omitempty"`
	CompletedAt       *time.Time       `json:"completed_at,omitempty"`
	AppliedAt         *time.Time       `json:"applied_at,omitempty"`
	EstimatedCost     float64          `json:"estimated_cost,omitempty"`
}

// AssessmentPreflightResult contains preflight check outcomes.
type AssessmentPreflightResult struct {
	Eligible           bool     `json:"eligible"`
	Reasons            []string `json:"reasons,omitempty"`
	ProviderID         string   `json:"provider_id"`
	ModelID            string   `json:"model_id"`
	ProviderEnabled    bool     `json:"provider_enabled"`
	CredentialAvailable bool    `json:"credential_available"`
	ModelReachable     bool     `json:"model_reachable"`
	StreamingKnown     bool     `json:"streaming_known"`
	ToolsEndpointKnown bool     `json:"tools_endpoint_known"`
	AssessmentReady    bool     `json:"assessment_ready"`
	ConflictingRun     bool     `json:"conflicting_run"`
}


