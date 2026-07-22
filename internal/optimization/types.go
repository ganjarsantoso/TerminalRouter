package optimization

import (
	"context"
	"time"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/lui"
	"github.com/termrouter/termrouter/internal/normalization"
)

// LossClass classifies the most severe transformation applied by an optimizer.
type LossClass string

const (
	// Lossless means no information was removed or altered in meaning.
	Lossless LossClass = "lossless"
	// Selective means some non-essential content was dropped but required
	// operational state (constraints, failures, paths) was preserved.
	Selective LossClass = "selective"
	// Lossy means semantic compression that may alter phrasing/structure.
	Lossy LossClass = "lossy"
)

// ActionKind enumerates the deterministic optimizations that may be applied.
type ActionKind string

const (
	ActionPromptPrefixStabilized    ActionKind = "prompt_prefix_stabilized"
	ActionDuplicateRemoved          ActionKind = "duplicate_removed"
	ActionANSIStripped              ActionKind = "ansi_stripped"
	ActionJSONCompacted             ActionKind = "json_compacted"
	ActionLogCompacted              ActionKind = "log_compacted"
	ActionToolResultCompacted       ActionKind = "tool_result_compacted"
	ActionConversationTrimmed       ActionKind = "conversation_trimmed"
	ActionOutputBudget              ActionKind = "output_budget"
	ActionSemanticCompressed        ActionKind = "semantic_compressed"
	ActionSemanticCompressionShadow ActionKind = "semantic_compression_shadow_evaluated"
	ActionLUIRendered               ActionKind = "lui_rendered"
	ActionBypassed                  ActionKind = "bypassed"
)

// Action records a single optimization step for auditability.
type Action struct {
	Kind                 ActionKind
	Description          string
	EstimatedTokensSaved int
	Reversible           bool
	LossClass            LossClass
}

// TokenBreakdown separates estimated token usage by request region.
type TokenBreakdown struct {
	System           int
	MessageHistory   int
	CurrentUser      int
	ToolDefinitions  int
	ToolResults      int
	RetrievedContext int
	ImagesEstimated  int
	ProtocolOverhead int
	Total            int
	Source           string // estimated | provider_reported
}

// OptimizationContext carries request-scoped metadata into the optimizers.
// It must never contain secrets, credentials, or raw authorization material.
type OptimizationContext struct {
	RequestID       string
	ClientKeyID     string
	RouteName       string
	ProviderID      string
	ModelID         string
	InboundProtocol string
	PublicHosting   bool
	Stream          bool
	// ClientPreference is the mode requested by the client (header/metadata),
	// already normalized ("" means none).
	ClientPreference string
	// KeyMaxMode is the maximum mode permitted by the client key policy
	// ("" means no additional restriction beyond server policy).
	KeyMaxMode   string
	ModelProfile map[string]float64
	Pricing      *config.Price

	// Flags are the enabled deterministic optimization flags for the resolved
	// mode (e.g. "strip_ansi", "compact_json", "deduplicate", "compact_logs").
	Flags []string
	// TaskType and Complexity drive adaptive output budgeting.
	TaskType   string
	Complexity string
	// OutputBudgetMode is the resolved output-budget mode ("off"/"adaptive").
	OutputBudgetMode string
	// Protected is the precomputed protected-content map (never nil when set).
	Protected *ProtectedContent
	// Conversation window parameters (zero means disabled).
	ConversationEnabled       bool
	ConversationTriggerTokens int
	ConversationRecentTurns   int
	ConversationTargetTokens  int
}

// OptimizationResult is the outcome of one optimization pass.
type OptimizationResult struct {
	Request                   *normalization.NormalizedRequest
	LUI                       *lui.Envelope
	InputTokensBefore         int
	InputTokensEstimated      int
	ExpectedCachedTokens      int
	RemovedTokensEstimated    int
	CompressionTokens         int
	ShadowEvaluated           bool
	HypotheticalSavingsTokens int
	EstimatedGrossSavingUSD   float64
	EstimatedOptimizerCost    float64
	EstimatedNetSavingUSD     float64
	AddedLatency              time.Duration
	Actions                   []Action
	Warnings                  []string
	LossClass                 LossClass
	Reversible                bool
	Bypassed                  bool
	BypassReason              string
	// ModeRequested and ModeApplied record policy resolution.
	ModeRequested config.OptimizationMode
	ModeApplied   config.OptimizationMode
	// LUIContext carries the built LUI envelope alongside its metadata.
	LUIVersion  string
	LUIRenderer string
	// DedupSources records the retained first-occurrence sources for dedup
	// references. Conversation trimming must not delete the source message while
	// references to it remain. No raw content is persisted in audit logs.
	DedupSources []DedupSource
}

// DedupSource records the location of a retained dedup source message and its
// associated reference ID.
type DedupSource struct {
	MessageIndex int
	ContentIndex int
	ReferenceID  string
}

// TokenEstimator estimates request token counts with a known or fallback method.
type TokenEstimator interface {
	Name() string
	// Supports reports whether the estimator can accurately count tokens for
	// the given provider/model (exact tokenizer, family tokenizer, or compatible).
	Supports(provider, model string) bool
	CountRequest(req *normalization.NormalizedRequest) (TokenBreakdown, error)
	CountText(text string) (int, error)
}

// Optimizer is a single deterministic or semantic optimization stage.
type Optimizer interface {
	Name() string
	Version() string
	// Supports reports whether the optimizer should run for the context/mode.
	Supports(ctx OptimizationContext, mode config.OptimizationMode) bool
	// Optimize transforms req in place (or returns a new request) and appends
	// actions. It must be fail-closed: on error it returns the original request
	// unchanged with Bypassed set.
	Optimize(ctx context.Context, req *normalization.NormalizedRequest, oc OptimizationContext, mode config.OptimizationMode, res *OptimizationResult) error
}

// ModeName returns a stable string for a mode.
func ModeName(m config.OptimizationMode) string { return string(m) }
