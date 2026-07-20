package external

import (
	"context"
	"time"
)

// Searcher is a pluggable web-search backend used to find current, published
// benchmark figures for a model at runtime. No values are hardcoded; evidence
// is always sourced live (and cached locally).
type Searcher interface {
	// Search returns result snippets (title + body text) for the given query.
	Search(ctx context.Context, query string) ([]SearchResult, error)
}

// SearchResult is a single search hit with extractable text.
type SearchResult struct {
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	URL     string `json:"url"`
}

// PageText is the extracted, benchmark-relevant text of a fetched web page.
type PageText struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// Summarizer uses a language model to read fetched benchmark pages and produce
// structured 0-10 capability estimates for a model. This replaces fragile
// regex extraction with model judgment over real sources.
type Summarizer interface {
	// SummarizeEvidence reads the supplied pages and returns a per-capability
	// estimate on the universal 0-10 scale, with an evidence URL and confidence.
	SummarizeEvidence(ctx context.Context, modelName string, pages []PageText) (Summary, error)
}

// Summary is the structured output of a Summarizer.
type Summary struct {
	// Model is the model identity exactly as the evidence source reported it
	// (e.g. "gpt-5-preview" when the configured target was "gpt-5"). It must be
	// captured independently of the configured identity so variant matching
	// (§18) can detect mismatches instead of always comparing against self.
	Model        string              `json:"model,omitempty"`
	Capabilities []SummaryCapability `json:"capabilities"`
	Confidence   float64              `json:"confidence"`
	Sources      []string             `json:"sources"`
}

// SummaryCapability is one model-judged capability estimate.
type SummaryCapability struct {
	Capability CapabilityKey `json:"capability"`
	Score      float64       `json:"score"`      // 0-10, 0.5 increments
	Confidence float64       `json:"confidence"` // 0-1
	Evidence   string        `json:"evidence"`   // URL or short citation
	Note       string        `json:"note,omitempty"`
}


// SourceID identifies a curated, independent benchmark source.
type SourceID string

const (
	SourceLiveBench       SourceID = "livebench"
	SourceAAII            SourceID = "aa-intelligence-index"
	SourceSWEBench        SourceID = "swebench"
	SourceLMArena         SourceID = "lmarena"
)

// TrustTier expresses how much we trust a source's normalization to the
// universal 0-10 scale.
type TrustTier string

const (
	TrustHigh     TrustTier = "high"
	TrustModerate TrustTier = "moderate"
	TrustLow      TrustTier = "low"
)

// ScaleKind describes the native scoring scale of a source before normalization.
type ScaleKind string

const (
	ScaleZeroToHundred ScaleKind = "0-100"
	ScaleZeroToOne     ScaleKind = "0-1"
	ScaleZeroToTen     ScaleKind = "0-10"
	ScaleElo           ScaleKind = "elo"
)

// SourceMeta describes a curated benchmark source.
type SourceMeta struct {
	ID          SourceID  `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	// Domains is the allowlist of hostnames from which automatic scoring is
	// permitted. Subdomains of any listed domain are allowed. Fetched pages
	// whose host is not in this allowlist may be displayed but contribute zero
	// automatic score weight (see §15).
	Domains     []string `json:"domains"`
	TrustTier   TrustTier `json:"trust_tier"`
	NativeScale ScaleKind `json:"native_scale"`
	Description string    `json:"description"`
}

// CapabilityMapKey is the universal capability dimension used by the router
// (kept consistent with internal/smart capability keys).
type CapabilityKey string

// Canonical capability dimensions (must match internal/smart capability set).
const (
	CapGeneral              CapabilityKey = "general"
	CapReasoning            CapabilityKey = "reasoning"
	CapAnalysis             CapabilityKey = "analysis"
	CapCoding               CapabilityKey = "coding"
	CapWriting              CapabilityKey = "writing"
	CapToolUse              CapabilityKey = "tool_use"
	CapInstructionFollowing CapabilityKey = "instruction_following"
	CapStructuredOutput     CapabilityKey = "structured_output"
	CapLongContext          CapabilityKey = "long_context"
	CapMultilingual         CapabilityKey = "multilingual"
	CapMathematics          CapabilityKey = "mathematics"
	CapSummarization        CapabilityKey = "summarization"
	CapExtraction           CapabilityKey = "extraction"
)

// CapabilityKeys lists all canonical dimensions.
var CapabilityKeys = []CapabilityKey{
	CapGeneral, CapReasoning, CapAnalysis, CapCoding, CapWriting,
	CapToolUse, CapInstructionFollowing, CapStructuredOutput, CapLongContext,
	CapMultilingual, CapMathematics, CapSummarization, CapExtraction,
}

// String returns the capability key as a string.
func (c CapabilityKey) String() string { return string(c) }

// NormalizedScore is a source value mapped to the universal 0-10 scale.
type NormalizedScore struct {
	Source     SourceID      `json:"source"`
	Raw        float64       `json:"raw"`
	RawScale   ScaleKind     `json:"raw_scale"`
	Normalized float64       `json:"normalized"`
	Tier       TrustTier     `json:"tier"`
}

// EvidenceRecord is a single observation of a model on a source, optionally
// mapped to one or more universal capability dimensions.
type EvidenceRecord struct {
	Source        SourceID       `json:"source"`
	ModelIdentity string         `json:"model_identity"` // canonical identity id
	Benchmark     string         `json:"benchmark"`      // e.g. "livebench/overall"
	Value         float64        `json:"value"`          // native scale
	Scale         ScaleKind      `json:"scale"`
	Capability    CapabilityKey  `json:"capability"` // primary capability this maps to
	ReportedAt    time.Time      `json:"reported_at"`
	URL           string         `json:"url,omitempty"`
	Notes         string         `json:"notes,omitempty"`

	// Published is the model identity as claimed by the source (e.g. the model
	// name printed on a leaderboard), used for variant matching (§18). When the
	// source does not state a distinct identity, it defaults to the configured
	// identity and is treated as an exact match.
	Published ModelIdentity `json:"published_identity,omitempty"`

	// Experiment identity fields (§19) used to deduplicate the same benchmark
	// result that appears on multiple sites (leaderboard, model card, mirror,
	// blog, snippet). Populated when available; the canonical experiment key
	// tolerates missing fields.
	Harness          string    `json:"harness,omitempty"`
	ReasoningSetting string    `json:"reasoning_setting,omitempty"`
	RawScore         float64   `json:"raw_score,omitempty"`
	EvaluationDate   string    `json:"evaluation_date,omitempty"`
	RunID            string    `json:"run_id,omitempty"`
	OriginalPublisher string   `json:"original_publisher,omitempty"`
	// ProvenanceURLs collects mirror/duplicate URLs folded into this canonical
	// record. Mirrors are provenance, not independent evidence.
	ProvenanceURLs []string `json:"provenance_urls,omitempty"`
}

// EvidenceRecordWithNorm carries an evidence record plus its normalized form.
type EvidenceRecordWithNorm struct {
	Evidence  EvidenceRecord `json:"evidence"`
	Normal    NormalizedScore `json:"normalized"`
	// Match is the outcome of matching the published identity against the
	// configured identity (§18).
	Match IdentityMatch `json:"match"`
	// Weight is the effective contribution weight after identity matching
	// (1.0 exact, 0.5 strong-probable, 0 excluded).
	Weight float64 `json:"weight"`
}

// ConsensusCapability is the aggregated estimate for one capability dimension.
type ConsensusCapability struct {
	Capability   CapabilityKey          `json:"capability"`
	Estimate     float64                `json:"estimate"` // consensus 0-10
	Confidence   float64                `json:"confidence"` // 0-1
	LowBand      float64                `json:"low_band"`
	HighBand     float64                `json:"high_band"`
	SourceCount  int                    `json:"source_count"`
	Contributing []EvidenceRecordWithNorm `json:"contributing"`
	PrimarySource SourceID              `json:"primary_source,omitempty"`
	// MandatoryReview is true when any contributing record matched at the
	// strong-probable level and therefore requires human sign-off before the
	// proposal is applied (§18).
	MandatoryReview bool `json:"mandatory_review"`
	// ExcludedCount is the number of evidence records for this capability that
	// were excluded by identity matching (family-only / incompatible) or source
	// contribution caps (§18/§19).
	ExcludedCount int `json:"excluded_count"`
}

// ConsensusProfile is the full external-consensus profile for a model.
type ConsensusProfile struct {
	ModelIdentity string                        `json:"model_identity"`
	ProviderID    string                        `json:"provider_id,omitempty"`
	ModelID       string                        `json:"model_id,omitempty"`
	Capabilities  map[CapabilityKey]ConsensusCapability `json:"capabilities"`
	Overall       float64                       `json:"overall"`
	Sources       []SourceID                    `json:"sources"`
	Confidence    float64                       `json:"confidence"`
	GeneratedAt   time.Time                     `json:"generated_at"`
	// MandatoryReview is true when any contributing evidence matched at the
	// strong-probable level (§18), requiring human sign-off before apply.
	MandatoryReview bool `json:"mandatory_review"`
}

// ProposalField is a single capability change proposed for a profile.
type ProposalField struct {
	Capability CapabilityKey `json:"capability"`
	Current    *float64      `json:"current,omitempty"`
	Proposed   float64       `json:"proposed"`
	Evidence   []EvidenceRecordWithNorm `json:"evidence"`
}

// Proposal is a reviewable set of external-capability updates for a model profile.
type Proposal struct {
	ID            string          `json:"id"`
	ProviderID    string          `json:"provider_id"`
	ModelID       string          `json:"model_id"`
	ModelIdentity string          `json:"model_identity"`
	Fields        []ProposalField `json:"fields"`
	Overall       float64         `json:"overall"`
	Confidence    float64         `json:"confidence"`
	Sources       []SourceID      `json:"sources"`
	CreatedAt     time.Time       `json:"created_at"`
	Status        string          `json:"status"` // pending | applied | dismissed
	RegistryVersion string        `json:"registry_version"`
	// MandatoryReview is true when any field requires human sign-off (§18):
	// the proposal must not be auto-applied.
	MandatoryReview bool `json:"mandatory_review"`
}

// RegistryInfo describes the bundled curated registry state.
type RegistryInfo struct {
	Version      string    `json:"version"`
	UpdatedAt    time.Time `json:"updated_at"`
	SourceCount  int       `json:"source_count"`
	ModelCount   int       `json:"model_count"`
	EvidenceCount int      `json:"evidence_count"`
	Sources      []SourceMeta `json:"sources"`
}

// ImportRecord is a persisted import event.
type ImportRecord struct {
	ProfileID    string             `json:"profile_id"`
	ProposalID   string             `json:"proposal_id"`
	AppliedAt    time.Time          `json:"applied_at"`
	Capabilities map[string]float64 `json:"capabilities"`
}
