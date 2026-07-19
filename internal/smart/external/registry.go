package external

import "time"

// registryVersion is the bundled curated registry version. Bump when the
// embedded evidence set is refreshed.
const registryVersion = "2025-07-01"

// registryUpdatedAt is the (fixed) build-time reference date for the bundle.
var registryUpdatedAt = time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)

// sources is the curated list of independent benchmark sources.
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
		NativeScale: ScaleZeroToTen,
		Description: "Composite intelligence index on an approximate 0-10 scale combining multiple third-party benchmarks.",
	},
	{
		ID:          SourceSWEBench,
		Name:        "SWE-bench",
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
		Description: "Human-preference Elo arena. Normalized against the arena median to a 0-10 band.",
	},
}

// ModelIdentity is a canonical model with its known provider/alias spellings.
type ModelIdentity struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Aliases  []string `json:"aliases"` // lowercased match keys (any of provider/model, display, etc.)
	Provider string   `json:"provider,omitempty"`
	Model    string   `json:"model,omitempty"`
}

// identities is the curated set of canonical models and their alias spellings.
var identities = []ModelIdentity{
	{
		ID:       "openai-gpt-4o",
		Name:     "GPT-4o",
		Provider: "openai",
		Model:    "gpt-4o",
		Aliases:  []string{"openai/gpt-4o", "gpt-4o", "gpt4o", "openai:gpt-4o"},
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
		Aliases:  []string{"openai/o3", "o3"},
	},
	{
		ID:       "anthropic-claude-3-5-sonnet",
		Name:     "Claude 3.5 Sonnet",
		Provider: "anthropic",
		Model:    "claude-3-5-sonnet-latest",
		Aliases:  []string{"anthropic/claude-3-5-sonnet-latest", "anthropic/claude-3-5-sonnet-20241022", "claude-3-5-sonnet", "claude-3.5-sonnet"},
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
		Aliases:  []string{"meta/llama-3.1-405b", "llama-3.1-405b", "llama-3.1-405b-instruct", "meta-llama-3.1-405b-instruct", "meta-llama-3.1-405b", "llama3.1-405b"},
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

// sampleEvidence is the curated (illustrative) evidence set bundled with the
// registry. Values are representative, not live-fetched. They are normalized to
// the universal 0-10 scale via the per-source normalizers.
var sampleEvidence = []EvidenceRecord{
	// GPT-4o
	{Source: SourceLiveBench, ModelIdentity: "openai-gpt-4o", Benchmark: "livebench/overall", Value: 72.1, Scale: ScaleZeroToHundred, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLiveBench, ModelIdentity: "openai-gpt-4o", Benchmark: "livebench/reasoning", Value: 68.4, Scale: ScaleZeroToHundred, Capability: CapReasoning, ReportedAt: registryUpdatedAt},
	{Source: SourceLiveBench, ModelIdentity: "openai-gpt-4o", Benchmark: "livebench/math", Value: 70.2, Scale: ScaleZeroToHundred, Capability: CapMathematics, ReportedAt: registryUpdatedAt},
	{Source: SourceLiveBench, ModelIdentity: "openai-gpt-4o", Benchmark: "livebench/coding", Value: 65.3, Scale: ScaleZeroToHundred, Capability: CapCoding, ReportedAt: registryUpdatedAt},
	{Source: SourceSWEBench, ModelIdentity: "openai-gpt-4o", Benchmark: "swebench/verified", Value: 51.0, Scale: ScaleZeroToHundred, Capability: CapCoding, ReportedAt: registryUpdatedAt},
	{Source: SourceAAII, ModelIdentity: "openai-gpt-4o", Benchmark: "aa/index", Value: 8.4, Scale: ScaleZeroToTen, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLMArena, ModelIdentity: "openai-gpt-4o", Benchmark: "lmarena/overall", Value: 1285, Scale: ScaleElo, Capability: CapGeneral, ReportedAt: registryUpdatedAt},

	// GPT-4o mini
	{Source: SourceLiveBench, ModelIdentity: "openai-gpt-4o-mini", Benchmark: "livebench/overall", Value: 59.8, Scale: ScaleZeroToHundred, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLiveBench, ModelIdentity: "openai-gpt-4o-mini", Benchmark: "livebench/coding", Value: 54.0, Scale: ScaleZeroToHundred, Capability: CapCoding, ReportedAt: registryUpdatedAt},
	{Source: SourceAAII, ModelIdentity: "openai-gpt-4o-mini", Benchmark: "aa/index", Value: 6.9, Scale: ScaleZeroToTen, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLMArena, ModelIdentity: "openai-gpt-4o-mini", Benchmark: "lmarena/overall", Value: 1201, Scale: ScaleElo, Capability: CapGeneral, ReportedAt: registryUpdatedAt},

	// o3
	{Source: SourceLiveBench, ModelIdentity: "openai-o3", Benchmark: "livebench/overall", Value: 83.5, Scale: ScaleZeroToHundred, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLiveBench, ModelIdentity: "openai-o3", Benchmark: "livebench/reasoning", Value: 88.0, Scale: ScaleZeroToHundred, Capability: CapReasoning, ReportedAt: registryUpdatedAt},
	{Source: SourceLiveBench, ModelIdentity: "openai-o3", Benchmark: "livebench/math", Value: 90.1, Scale: ScaleZeroToHundred, Capability: CapMathematics, ReportedAt: registryUpdatedAt},
	{Source: SourceLiveBench, ModelIdentity: "openai-o3", Benchmark: "livebench/coding", Value: 81.0, Scale: ScaleZeroToHundred, Capability: CapCoding, ReportedAt: registryUpdatedAt},
	{Source: SourceAAII, ModelIdentity: "openai-o3", Benchmark: "aa/index", Value: 9.3, Scale: ScaleZeroToTen, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLMArena, ModelIdentity: "openai-o3", Benchmark: "lmarena/overall", Value: 1402, Scale: ScaleElo, Capability: CapGeneral, ReportedAt: registryUpdatedAt},

	// Claude 3.5 Sonnet
	{Source: SourceLiveBench, ModelIdentity: "anthropic-claude-3-5-sonnet", Benchmark: "livebench/overall", Value: 70.6, Scale: ScaleZeroToHundred, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLiveBench, ModelIdentity: "anthropic-claude-3-5-sonnet", Benchmark: "livebench/coding", Value: 74.0, Scale: ScaleZeroToHundred, Capability: CapCoding, ReportedAt: registryUpdatedAt},
	{Source: SourceLiveBench, ModelIdentity: "anthropic-claude-3-5-sonnet", Benchmark: "livebench/reasoning", Value: 66.0, Scale: ScaleZeroToHundred, Capability: CapReasoning, ReportedAt: registryUpdatedAt},
	{Source: SourceSWEBench, ModelIdentity: "anthropic-claude-3-5-sonnet", Benchmark: "swebench/verified", Value: 64.0, Scale: ScaleZeroToHundred, Capability: CapCoding, ReportedAt: registryUpdatedAt},
	{Source: SourceAAII, ModelIdentity: "anthropic-claude-3-5-sonnet", Benchmark: "aa/index", Value: 8.1, Scale: ScaleZeroToTen, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLMArena, ModelIdentity: "anthropic-claude-3-5-sonnet", Benchmark: "lmarena/overall", Value: 1272, Scale: ScaleElo, Capability: CapGeneral, ReportedAt: registryUpdatedAt},

	// Claude 3.5 Haiku
	{Source: SourceLiveBench, ModelIdentity: "anthropic-claude-3-5-haiku", Benchmark: "livebench/overall", Value: 55.2, Scale: ScaleZeroToHundred, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceAAII, ModelIdentity: "anthropic-claude-3-5-haiku", Benchmark: "aa/index", Value: 6.5, Scale: ScaleZeroToTen, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLMArena, ModelIdentity: "anthropic-claude-3-5-haiku", Benchmark: "lmarena/overall", Value: 1178, Scale: ScaleElo, Capability: CapGeneral, ReportedAt: registryUpdatedAt},

	// Claude 3 Opus
	{Source: SourceLiveBench, ModelIdentity: "anthropic-claude-3-opus", Benchmark: "livebench/overall", Value: 64.0, Scale: ScaleZeroToHundred, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceAAII, ModelIdentity: "anthropic-claude-3-opus", Benchmark: "aa/index", Value: 7.6, Scale: ScaleZeroToTen, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLMArena, ModelIdentity: "anthropic-claude-3-opus", Benchmark: "lmarena/overall", Value: 1252, Scale: ScaleElo, Capability: CapGeneral, ReportedAt: registryUpdatedAt},

	// Gemini 1.5 Pro
	{Source: SourceLiveBench, ModelIdentity: "google-gemini-1-5-pro", Benchmark: "livebench/overall", Value: 69.4, Scale: ScaleZeroToHundred, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLiveBench, ModelIdentity: "google-gemini-1-5-pro", Benchmark: "livebench/coding", Value: 62.0, Scale: ScaleZeroToHundred, Capability: CapCoding, ReportedAt: registryUpdatedAt},
	{Source: SourceLiveBench, ModelIdentity: "google-gemini-1-5-pro", Benchmark: "livebench/longcontext", Value: 71.0, Scale: ScaleZeroToHundred, Capability: CapLongContext, ReportedAt: registryUpdatedAt},
	{Source: SourceSWEBench, ModelIdentity: "google-gemini-1-5-pro", Benchmark: "swebench/verified", Value: 44.0, Scale: ScaleZeroToHundred, Capability: CapCoding, ReportedAt: registryUpdatedAt},
	{Source: SourceAAII, ModelIdentity: "google-gemini-1-5-pro", Benchmark: "aa/index", Value: 7.9, Scale: ScaleZeroToTen, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLMArena, ModelIdentity: "google-gemini-1-5-pro", Benchmark: "lmarena/overall", Value: 1260, Scale: ScaleElo, Capability: CapGeneral, ReportedAt: registryUpdatedAt},

	// Gemini 1.5 Flash
	{Source: SourceLiveBench, ModelIdentity: "google-gemini-1-5-flash", Benchmark: "livebench/overall", Value: 54.0, Scale: ScaleZeroToHundred, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceAAII, ModelIdentity: "google-gemini-1-5-flash", Benchmark: "aa/index", Value: 6.4, Scale: ScaleZeroToTen, Capability: CapGeneral, ReportedAt: registryUpdatedAt},

	// Llama 3.1 405B
	{Source: SourceLiveBench, ModelIdentity: "meta-llama-3-1-405b", Benchmark: "livebench/overall", Value: 62.0, Scale: ScaleZeroToHundred, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLiveBench, ModelIdentity: "meta-llama-3-1-405b", Benchmark: "livebench/coding", Value: 58.0, Scale: ScaleZeroToHundred, Capability: CapCoding, ReportedAt: registryUpdatedAt},
	{Source: SourceSWEBench, ModelIdentity: "meta-llama-3-1-405b", Benchmark: "swebench/verified", Value: 34.0, Scale: ScaleZeroToHundred, Capability: CapCoding, ReportedAt: registryUpdatedAt},
	{Source: SourceAAII, ModelIdentity: "meta-llama-3-1-405b", Benchmark: "aa/index", Value: 7.0, Scale: ScaleZeroToTen, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLMArena, ModelIdentity: "meta-llama-3-1-405b", Benchmark: "lmarena/overall", Value: 1220, Scale: ScaleElo, Capability: CapGeneral, ReportedAt: registryUpdatedAt},

	// DeepSeek V3
	{Source: SourceLiveBench, ModelIdentity: "deepseek-deepseek-v3", Benchmark: "livebench/overall", Value: 67.0, Scale: ScaleZeroToHundred, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLiveBench, ModelIdentity: "deepseek-deepseek-v3", Benchmark: "livebench/coding", Value: 66.0, Scale: ScaleZeroToHundred, Capability: CapCoding, ReportedAt: registryUpdatedAt},
	{Source: SourceSWEBench, ModelIdentity: "deepseek-deepseek-v3", Benchmark: "swebench/verified", Value: 49.0, Scale: ScaleZeroToHundred, Capability: CapCoding, ReportedAt: registryUpdatedAt},
	{Source: SourceAAII, ModelIdentity: "deepseek-deepseek-v3", Benchmark: "aa/index", Value: 7.4, Scale: ScaleZeroToTen, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLMArena, ModelIdentity: "deepseek-deepseek-v3", Benchmark: "lmarena/overall", Value: 1245, Scale: ScaleElo, Capability: CapGeneral, ReportedAt: registryUpdatedAt},

	// DeepSeek R1
	{Source: SourceLiveBench, ModelIdentity: "deepseek-deepseek-r1", Benchmark: "livebench/overall", Value: 74.0, Scale: ScaleZeroToHundred, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLiveBench, ModelIdentity: "deepseek-deepseek-r1", Benchmark: "livebench/reasoning", Value: 84.0, Scale: ScaleZeroToHundred, Capability: CapReasoning, ReportedAt: registryUpdatedAt},
	{Source: SourceLiveBench, ModelIdentity: "deepseek-deepseek-r1", Benchmark: "livebench/math", Value: 86.0, Scale: ScaleZeroToHundred, Capability: CapMathematics, ReportedAt: registryUpdatedAt},
	{Source: SourceAAII, ModelIdentity: "deepseek-deepseek-r1", Benchmark: "aa/index", Value: 8.0, Scale: ScaleZeroToTen, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLMArena, ModelIdentity: "deepseek-deepseek-r1", Benchmark: "lmarena/overall", Value: 1360, Scale: ScaleElo, Capability: CapGeneral, ReportedAt: registryUpdatedAt},

	// Mistral Large
	{Source: SourceLiveBench, ModelIdentity: "mistral-mistral-large", Benchmark: "livebench/overall", Value: 56.0, Scale: ScaleZeroToHundred, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceAAII, ModelIdentity: "mistral-mistral-large", Benchmark: "aa/index", Value: 6.6, Scale: ScaleZeroToTen, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
	{Source: SourceLMArena, ModelIdentity: "mistral-mistral-large", Benchmark: "lmarena/overall", Value: 1185, Scale: ScaleElo, Capability: CapGeneral, ReportedAt: registryUpdatedAt},
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
