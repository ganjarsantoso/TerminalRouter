package external

import "time"

// registryVersion is the bundled methodology/registry version. Bump when the
// source methodology or capability mapping changes (not when model scores
// change — those are fetched live).
const registryVersion = "2026.07"

// registryUpdatedAt is the (fixed) build-time reference date for the methodology bundle.
var registryUpdatedAt = time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)

// sources is the curated list of independent benchmark sources we recognize in
// search results. These define normalization methodology; the actual numbers
// are fetched live, never hardcoded.
var sources = []SourceMeta{
	{
		ID:          SourceLiveBench,
		Name:        "LiveBench",
		URL:         "https://livebench.ai",
		TrustTier:   TrustHigh,
		NativeScale: ScaleZeroToHundred,
		Description: "Contamination-free, automatically-scored LLM benchmark covering reasoning, math, coding, and language tasks. Reported as percentage (0-100).",
	},
	{
		ID:          SourceAAII,
		Name:        "Artificial Analysis Intelligence Index",
		URL:         "https://artificialanalysis.ai",
		TrustTier:   TrustModerate,
		NativeScale: ScaleZeroToHundred,
		Description: "Composite intelligence index (0-100) combining multiple third-party benchmarks across agents, coding, general capability, and scientific reasoning.",
	},
	{
		ID:          SourceSWEBench,
		Name:        "SWE-bench Verified",
		URL:         "https://www.swebench.com",
		TrustTier:   TrustHigh,
		NativeScale: ScaleZeroToHundred,
		Description: "Real-world software engineering resolution rate (0-100%). Maps primarily to the coding capability.",
	},
	{
		ID:          SourceLMArena,
		Name:        "LMArena",
		URL:         "https://lmarena.ai",
		TrustTier:   TrustModerate,
		NativeScale: ScaleElo,
		Description: "Human-preference Elo arena, normalized against the arena median to a 0-10 band.",
	},
}

// ModelIdentity is a canonical model with its known provider/alias spellings.
// Used only for matching the configured provider/model against search results;
// it carries NO scores (those are fetched live).
type ModelIdentity struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Aliases  []string `json:"aliases"` // lowercased match keys (any of provider/model, display, etc.)
	Provider string   `json:"provider,omitempty"`
	Model    string   `json:"model,omitempty"`
}

// identities is the curated set of canonical models and their alias spellings.
// This is an identity directory only — no benchmark values are stored here.
var identities = []ModelIdentity{
	{
		ID:       "openai-gpt-4o",
		Name:     "GPT-4o",
		Provider: "openai",
		Model:    "gpt-4o",
		Aliases:  []string{"openai/gpt-4o", "gpt-4o", "gpt4o", "openai:gpt-4o", "gpt-4o-2024-08-06", "gpt-4o-2024-05-13"},
	},
	{
		ID:       "openai-gpt-4o-mini",
		Name:     "GPT-4o mini",
		Provider: "openai",
		Model:    "gpt-4o-mini",
		Aliases:  []string{"openai/gpt-4o-mini", "gpt-4o-mini", "gpt4o-mini"},
	},
	{
		ID:       "openai-o3",
		Name:     "OpenAI o3",
		Provider: "openai",
		Model:    "o3",
		Aliases:  []string{"openai/o3", "o3", "o3-mini"},
	},
	{
		ID:       "anthropic-claude-3-5-sonnet",
		Name:     "Claude 3.5 Sonnet",
		Provider: "anthropic",
		Model:    "claude-3-5-sonnet-latest",
		Aliases:  []string{"anthropic/claude-3-5-sonnet-latest", "anthropic/claude-3-5-sonnet-20241022", "claude-3-5-sonnet", "claude-3.5-sonnet", "claude-3-5-sonnet-20241022"},
	},
	{
		ID:       "anthropic-claude-3-5-haiku",
		Name:     "Claude 3.5 Haiku",
		Provider: "anthropic",
		Model:    "claude-3-5-haiku-latest",
		Aliases:  []string{"anthropic/claude-3-5-haiku-latest", "claude-3-5-haiku", "claude-3.5-haiku"},
	},
	{
		ID:       "anthropic-claude-3-opus",
		Name:     "Claude 3 Opus",
		Provider: "anthropic",
		Model:    "claude-3-opus-latest",
		Aliases:  []string{"anthropic/claude-3-opus-latest", "anthropic/claude-3-opus-20240229", "claude-3-opus", "claude-3-opus-20240229"},
	},
	{
		ID:       "google-gemini-1-5-pro",
		Name:     "Gemini 1.5 Pro",
		Provider: "google",
		Model:    "gemini-1.5-pro",
		Aliases:  []string{"google/gemini-1.5-pro", "gemini-1.5-pro", "gemini-1.5-pro-latest"},
	},
	{
		ID:       "google-gemini-1-5-flash",
		Name:     "Gemini 1.5 Flash",
		Provider: "google",
		Model:    "gemini-1.5-flash",
		Aliases:  []string{"google/gemini-1.5-flash", "gemini-1.5-flash", "gemini-1.5-flash-latest"},
	},
	{
		ID:       "meta-llama-3-1-405b",
		Name:     "Llama 3.1 405B",
		Provider: "meta",
		Model:    "llama-3.1-405b",
		Aliases:  []string{"meta/llama-3.1-405b", "llama-3.1-405b", "llama-3.1-405b-instruct", "meta-llama-3.1-405b-instruct", "llama3.1-405b"},
	},
	{
		ID:       "deepseek-deepseek-v3",
		Name:     "DeepSeek V3",
		Provider: "deepseek",
		Model:    "deepseek-chat",
		Aliases:  []string{"deepseek/deepseek-chat", "deepseek-chat", "deepseek-v3"},
	},
	{
		ID:       "deepseek-deepseek-r1",
		Name:     "DeepSeek R1",
		Provider: "deepseek",
		Model:    "deepseek-reasoner",
		Aliases:  []string{"deepseek/deepseek-reasoner", "deepseek-reasoner", "deepseek-r1"},
	},
	{
		ID:       "mistral-mistral-large",
		Name:     "Mistral Large",
		Provider: "mistral",
		Model:    "mistral-large-latest",
		Aliases:  []string{"mistral/mistral-large-latest", "mistral-large", "mistral-large-latest"},
	},
}

// sourceMetaByID returns the meta for a source id.
func sourceMetaByID(id SourceID) (SourceMeta, bool) {
	for _, s := range sources {
		if s.ID == id {
			return s, true
		}
	}
	return SourceMeta{}, false
}

// searchQueries builds the live-search queries used to gather benchmark evidence
// for a model. Each query targets one independent source.
func searchQueries(id ModelIdentity) []string {
	name := id.Name
	model := id.Model
	return []string{
		name + " benchmarks LiveBench SWE-bench Artificial Analysis Intelligence Index",
		model + " SWE-bench Verified percentage resolved",
		name + " LiveBench overall score reasoning coding math percentage",
		name + " Artificial Analysis Intelligence Index score",
		name + " LMArena Elo arena score",
		name + " MMLU-Pro GPQA Diamond MATH-500 benchmark scores",
	}
}
