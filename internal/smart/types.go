// Package smart implements task-aware model selection (Smart Routes).
// It selects which configured model should answer; it does not solve the task.
package smart

import (
	"time"
)

const ClassifierVersion = "heuristic-v1"
const CatalogVersion = "builtin-v1"

// Capability dimension names (1–5 scale; 0 = unknown).
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
	SourceBuiltin  = "builtin"
	SourceUser     = "user"
	SourceObserved = "observed"
	SourceUnknown  = "unknown"
)

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
	Source       string         `json:"source" yaml:"source,omitempty"`
	Capabilities map[string]int `json:"capabilities" yaml:"capabilities,omitempty"`
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
	Complexity       string           `json:"complexity"`
	Requirements     map[string]int   `json:"requirements"`
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
