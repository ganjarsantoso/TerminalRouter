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
// for a model directly from its provider/model. Any model is supported; there
// is no curated directory. Each query targets independent benchmark sources.
func searchQueries(providerID, modelID string) []string {
	name := modelID
	model := modelID
	return []string{
		name + " benchmarks LiveBench SWE-bench Artificial Analysis Intelligence Index",
		model + " SWE-bench Verified percentage resolved",
		name + " LiveBench overall score reasoning coding math percentage",
		name + " Artificial Analysis Intelligence Index score",
		name + " LMArena Elo arena score",
		name + " MMLU-Pro GPQA Diamond MATH-500 benchmark scores",
	}
}
