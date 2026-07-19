package smart

// boolPtr returns a pointer to b.
func boolPtr(b bool) *bool { return &b }

// BuiltinCatalog is the versioned built-in model profile catalog.
// Values are conservative and deployment-oriented (not marketing rankings).
// Unknown models are treated as unprofiled rather than weak.
// Scale is 1–10 (0.5 increments); 0 = unknown.
func BuiltinCatalog() map[string]ModelProfile {
	return map[string]ModelProfile{
		"openai/gpt-4o": {
			ID: "openai/gpt-4o", Version: CatalogVersion, Source: SourceBuiltin,
			Capabilities: map[string]float64{
				CapGeneral: 10, CapReasoning: 8, CapAnalysis: 8, CapCoding: 8,
				CapWriting: 10, CapToolUse: 10, CapInstructionFollowing: 10,
				CapStructuredOutput: 10, CapLongContext: 8, CapMultilingual: 8,
				CapMathematics: 8, CapSummarization: 10, CapExtraction: 8,
			},
			Properties: ModelProperties{
				Vision: boolPtr(true), Tools: boolPtr(true), ParallelTools: boolPtr(true),
				StructuredOutput: boolPtr(true), Streaming: boolPtr(true),
				ContextWindow: 128000, MaxOutputTokens: 16384,
				CostTier: 4, LatencyTier: 3, Privacy: PrivacyCloud,
			},
		},
		"openai/gpt-4o-mini": {
			ID: "openai/gpt-4o-mini", Version: CatalogVersion, Source: SourceBuiltin,
			Capabilities: map[string]float64{
				CapGeneral: 8, CapReasoning: 6, CapAnalysis: 6, CapCoding: 6,
				CapWriting: 8, CapToolUse: 8, CapInstructionFollowing: 8,
				CapStructuredOutput: 8, CapLongContext: 8, CapMultilingual: 8,
				CapMathematics: 6, CapSummarization: 8, CapExtraction: 8,
			},
			Properties: ModelProperties{
				Vision: boolPtr(true), Tools: boolPtr(true), ParallelTools: boolPtr(true),
				StructuredOutput: boolPtr(true), Streaming: boolPtr(true),
				ContextWindow: 128000, MaxOutputTokens: 16384,
				CostTier: 2, LatencyTier: 2, Privacy: PrivacyCloud,
			},
		},
		"openai/o1": {
			ID: "openai/o1", Version: CatalogVersion, Source: SourceBuiltin,
			Capabilities: map[string]float64{
				CapGeneral: 8, CapReasoning: 10, CapAnalysis: 10, CapCoding: 10,
				CapWriting: 6, CapToolUse: 4, CapInstructionFollowing: 8,
				CapStructuredOutput: 6, CapLongContext: 8, CapMultilingual: 6,
				CapMathematics: 10, CapSummarization: 6, CapExtraction: 6,
			},
			Properties: ModelProperties{
				Vision: boolPtr(true), Tools: boolPtr(false), ParallelTools: boolPtr(false),
				StructuredOutput: boolPtr(false), Streaming: boolPtr(true),
				ContextWindow: 200000, MaxOutputTokens: 100000,
				CostTier: 5, LatencyTier: 5, Privacy: PrivacyCloud,
			},
		},
		"anthropic/claude-sonnet-4": {
			ID: "anthropic/claude-sonnet-4", Version: CatalogVersion, Source: SourceBuiltin,
			Capabilities: map[string]float64{
				CapGeneral: 10, CapReasoning: 10, CapAnalysis: 10, CapCoding: 10,
				CapWriting: 10, CapToolUse: 10, CapInstructionFollowing: 10,
				CapStructuredOutput: 8, CapLongContext: 10, CapMultilingual: 8,
				CapMathematics: 8, CapSummarization: 10, CapExtraction: 8,
			},
			Properties: ModelProperties{
				Vision: boolPtr(true), Tools: boolPtr(true), ParallelTools: boolPtr(true),
				StructuredOutput: boolPtr(true), Streaming: boolPtr(true),
				ContextWindow: 200000, MaxOutputTokens: 64000,
				CostTier: 4, LatencyTier: 3, Privacy: PrivacyCloud,
			},
		},
		"anthropic/claude-haiku-3-5": {
			ID: "anthropic/claude-haiku-3-5", Version: CatalogVersion, Source: SourceBuiltin,
			Capabilities: map[string]float64{
				CapGeneral: 8, CapReasoning: 6, CapAnalysis: 6, CapCoding: 6,
				CapWriting: 8, CapToolUse: 8, CapInstructionFollowing: 8,
				CapStructuredOutput: 8, CapLongContext: 8, CapMultilingual: 8,
				CapMathematics: 6, CapSummarization: 8, CapExtraction: 8,
			},
			Properties: ModelProperties{
				Vision: boolPtr(true), Tools: boolPtr(true), ParallelTools: boolPtr(true),
				StructuredOutput: boolPtr(true), Streaming: boolPtr(true),
				ContextWindow: 200000, MaxOutputTokens: 8192,
				CostTier: 2, LatencyTier: 1, Privacy: PrivacyCloud,
			},
		},
		"deepseek/deepseek-chat": {
			ID: "deepseek/deepseek-chat", Version: CatalogVersion, Source: SourceBuiltin,
			Capabilities: map[string]float64{
				CapGeneral: 8, CapReasoning: 8, CapAnalysis: 8, CapCoding: 10,
				CapWriting: 6, CapToolUse: 8, CapInstructionFollowing: 8,
				CapStructuredOutput: 8, CapLongContext: 8, CapMultilingual: 6,
				CapMathematics: 8, CapSummarization: 6, CapExtraction: 6,
			},
			Properties: ModelProperties{
				Vision: boolPtr(false), Tools: boolPtr(true), ParallelTools: boolPtr(false),
				StructuredOutput: boolPtr(true), Streaming: boolPtr(true),
				ContextWindow: 64000, MaxOutputTokens: 8192,
				CostTier: 1, LatencyTier: 2, Privacy: PrivacyCloud,
			},
		},
		"deepseek/deepseek-reasoner": {
			ID: "deepseek/deepseek-reasoner", Version: CatalogVersion, Source: SourceBuiltin,
			Capabilities: map[string]float64{
				CapGeneral: 6, CapReasoning: 10, CapAnalysis: 10, CapCoding: 10,
				CapWriting: 4, CapToolUse: 4, CapInstructionFollowing: 8,
				CapStructuredOutput: 6, CapLongContext: 8, CapMultilingual: 6,
				CapMathematics: 10, CapSummarization: 6, CapExtraction: 6,
			},
			Properties: ModelProperties{
				Vision: boolPtr(false), Tools: boolPtr(false), ParallelTools: boolPtr(false),
				StructuredOutput: boolPtr(false), Streaming: boolPtr(true),
				ContextWindow: 64000, MaxOutputTokens: 8192,
				CostTier: 2, LatencyTier: 4, Privacy: PrivacyCloud,
			},
		},
		"local/qwen-coder": {
			ID: "local/qwen-coder", Version: CatalogVersion, Source: SourceBuiltin,
			Capabilities: map[string]float64{
				CapGeneral: 6, CapReasoning: 8, CapAnalysis: 6, CapCoding: 10,
				CapWriting: 4, CapToolUse: 8, CapInstructionFollowing: 8,
				CapStructuredOutput: 8, CapLongContext: 6, CapMultilingual: 8,
				CapMathematics: 6, CapSummarization: 6, CapExtraction: 6,
			},
			Properties: ModelProperties{
				Vision: boolPtr(false), Tools: boolPtr(true), ParallelTools: boolPtr(false),
				StructuredOutput: boolPtr(true), Streaming: boolPtr(true),
				ContextWindow: 32768, MaxOutputTokens: 8192,
				CostTier: 1, LatencyTier: 1, Privacy: PrivacyLocal,
			},
		},
		"local/small-model": {
			ID: "local/small-model", Version: CatalogVersion, Source: SourceBuiltin,
			Capabilities: map[string]float64{
				CapGeneral: 6, CapReasoning: 4, CapAnalysis: 4, CapCoding: 4,
				CapWriting: 6, CapToolUse: 4, CapInstructionFollowing: 6,
				CapStructuredOutput: 4, CapLongContext: 4, CapMultilingual: 6,
				CapMathematics: 4, CapSummarization: 6, CapExtraction: 6,
			},
			Properties: ModelProperties{
				Vision: boolPtr(false), Tools: boolPtr(false), ParallelTools: boolPtr(false),
				StructuredOutput: boolPtr(false), Streaming: boolPtr(true),
				ContextWindow: 8192, MaxOutputTokens: 2048,
				CostTier: 1, LatencyTier: 1, Privacy: PrivacyLocal,
			},
		},
		// Generic fallbacks keyed by common model id patterns (matched via lookup aliases).
		"anthropic/claude-sonnet": {
			ID: "anthropic/claude-sonnet", Version: CatalogVersion, Source: SourceBuiltin,
			Capabilities: map[string]float64{
				CapGeneral: 10, CapReasoning: 10, CapAnalysis: 10, CapCoding: 10,
				CapWriting: 10, CapToolUse: 10, CapInstructionFollowing: 10,
				CapStructuredOutput: 8, CapLongContext: 10, CapMultilingual: 8,
				CapMathematics: 8, CapSummarization: 10, CapExtraction: 8,
			},
			Properties: ModelProperties{
				Vision: boolPtr(true), Tools: boolPtr(true), ParallelTools: boolPtr(true),
				StructuredOutput: boolPtr(true), Streaming: boolPtr(true),
				ContextWindow: 200000, MaxOutputTokens: 64000,
				CostTier: 4, LatencyTier: 3, Privacy: PrivacyCloud,
			},
		},
		"openai/reasoning-model": {
			ID: "openai/reasoning-model", Version: CatalogVersion, Source: SourceBuiltin,
			Capabilities: map[string]float64{
				CapGeneral: 8, CapReasoning: 10, CapAnalysis: 10, CapCoding: 10,
				CapWriting: 6, CapToolUse: 4, CapInstructionFollowing: 8,
				CapStructuredOutput: 6, CapLongContext: 8, CapMultilingual: 6,
				CapMathematics: 10, CapSummarization: 6, CapExtraction: 6,
			},
			Properties: ModelProperties{
				Vision: boolPtr(true), Tools: boolPtr(false), ParallelTools: boolPtr(false),
				StructuredOutput: boolPtr(false), Streaming: boolPtr(true),
				ContextWindow: 200000, MaxOutputTokens: 100000,
				CostTier: 5, LatencyTier: 5, Privacy: PrivacyCloud,
			},
		},
	}
}

// LookupBuiltin finds a builtin profile by key or model id suffix match.
func LookupBuiltin(key string) (ModelProfile, bool) {
	cat := BuiltinCatalog()
	if p, ok := cat[key]; ok {
		return p, true
	}
	// try model-id only (last path segment) against catalog keys ending with /model
	for k, p := range cat {
		if k == key {
			return p, true
		}
		// suffix: catalog key ends with /key or key is full
		if len(k) > len(key) && k[len(k)-len(key)-1:] == "/"+key {
			return p, true
		}
	}
	return ModelProfile{}, false
}
