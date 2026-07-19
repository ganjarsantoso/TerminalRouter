package smart

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/termrouter/termrouter/internal/normalization"
)

// RequestFeatures are extracted signals used by the heuristic classifier.
type RequestFeatures struct {
	ApproxTokens       int
	TurnCount          int
	SystemLen          int
	UserText           string
	HasCode            bool
	CodeLangHints      []string
	HasDebugMarkers    bool
	HasMath            bool
	HasProof           bool
	HasVision          bool
	HasTools           bool
	ToolsRequired      bool
	HasStructuredOut   bool
	ConstraintCount    int
	HasArchitecture    bool
	HasComparison      bool
	WantsShortAnswer   bool
	HasWritingMarkers  bool
	HasSummaryMarkers  bool
	HasExtractMarkers  bool
	HasTranslateMarkers bool
	HasReviewMarkers   bool
	HasGenCodeMarkers  bool
	EstimatedContext   int
}

var (
	reCodeFence   = regexp.MustCompile("(?s)```[a-zA-Z0-9_+-]*\\n.*?```")
	reInlineCode  = regexp.MustCompile("`[^`]+`")
	reLangHint    = regexp.MustCompile(`(?i)\b(go|golang|python|javascript|typescript|java|rust|c\+\+|cpp|c#|ruby|php|swift|kotlin|sql|bash|shell)\b`)
	reDebug       = regexp.MustCompile(`(?i)\b(bug|debug|stack\s*trace|panic|segfault|null\s*pointer|race\s*condition|deadlock|exception|traceback|fixing|broken|doesn'?t\s*work|error:)\b`)
	reMath        = regexp.MustCompile(`(?i)(\$[^$]+\$|\\\(|\\\[|\\frac|\\sum|\\int|\bprove\b|\btheorem\b|\bintegral\b|\bderivative\b|\bequation\b|\bmatrix\b)`)
	reProof       = regexp.MustCompile(`(?i)\b(prove that|proof of|by induction|q\.?e\.?d)\b`)
	reArch        = regexp.MustCompile(`(?i)\b(architecture|microservices?|distributed\s+system|scalability|load\s*balancer|event[- ]driven|design\s+pattern|system\s+design)\b`)
	reCompare     = regexp.MustCompile(`(?i)\b(compare|versus|vs\.?|trade-?offs?|pros\s+and\s+cons|which\s+is\s+better)\b`)
	reShort       = regexp.MustCompile(`(?i)\b(yes\s+or\s+no|one\s+word|briefly|in\s+one\s+sentence|tldr|short\s+answer)\b`)
	reWrite       = regexp.MustCompile(`(?i)\b(write\s+(a|an|the)\s+(poem|story|essay|blog|email|letter|novel)|creative\s+writing|rewrite|rephrase)\b`)
	reSummary     = regexp.MustCompile(`(?i)\b(summarize|summary|tl;?dr|key\s+points|synopsis|abstract)\b`)
	reExtract     = regexp.MustCompile(`(?i)\b(extract|parse\s+out|pull\s+out|list\s+all|structured\s+data|json\s+fields)\b`)
	reTranslate   = regexp.MustCompile(`(?i)\b(translate|translation|into\s+(english|spanish|french|german|chinese|japanese|korean|arabic))\b`)
	reReview      = regexp.MustCompile(`(?i)\b(code\s+review|review\s+this\s+(code|pr|pull\s+request)|lgtm|nit:)\b`)
	reGenCode     = regexp.MustCompile(`(?i)\b(implement|write\s+(a|an|the)\s+(function|class|module|api|endpoint)|generate\s+code|scaffold|boilerplate)\b`)
	reReason      = regexp.MustCompile(`(?i)\b(step\s+by\s+step|reason\s+about|think\s+through|multi-step|logic\s+puzzle|riddle)\b`)
	reAnalysis    = regexp.MustCompile(`(?i)\b(analyze|analysis|investigate|root\s+cause|deep\s+dive|evaluate)\b`)
	reExplain     = regexp.MustCompile(`(?i)\b(explain|what\s+is|how\s+does|for\s+a\s+(five|5)[- ]year[- ]old|eli5|in\s+simple\s+terms)\b`)
	reResearch    = regexp.MustCompile(`(?i)\b(research|synthesize|survey\s+of|literature|state\s+of\s+the\s+art)\b`)
	reGreeting    = regexp.MustCompile(`(?i)^(hi|hello|hey|thanks|thank\s+you|good\s+(morning|afternoon|evening))[\s!.?]*$`)
	reSimpleXform = regexp.MustCompile(`(?i)\b(uppercase|lowercase|title\s*case|capitalize|trim|format\s+as|convert\s+to\s+json)\b`)
)

// ExtractFeatures analyzes a normalized request in-memory (no persistence).
func ExtractFeatures(req *normalization.NormalizedRequest) RequestFeatures {
	f := RequestFeatures{}
	if req == nil {
		return f
	}
	var all strings.Builder
	if req.System != "" {
		f.SystemLen = len(req.System)
		all.WriteString(req.System)
		all.WriteByte('\n')
	}
	turns := 0
	for _, m := range req.Messages {
		if m.Role == normalization.RoleUser || m.Role == normalization.RoleAssistant {
			turns++
		}
		for _, c := range m.Content {
			switch c.Type {
			case normalization.ContentText:
				all.WriteString(c.Text)
				all.WriteByte('\n')
			case normalization.ContentImage:
				f.HasVision = true
			case normalization.ContentToolCall, normalization.ContentToolResult:
				f.HasTools = true
			}
		}
	}
	f.TurnCount = turns
	text := all.String()
	f.UserText = text
	f.ApproxTokens = estimateTokens(text)
	f.EstimatedContext = f.ApproxTokens + f.SystemLen/4

	if reCodeFence.MatchString(text) || denseCodeSignals(text) {
		f.HasCode = true
	}
	if langs := reLangHint.FindAllString(text, 8); len(langs) > 0 {
		f.CodeLangHints = uniqueLower(langs)
		if !f.HasCode && len(f.CodeLangHints) > 0 && (strings.Contains(text, "{") || strings.Contains(text, "func ") || strings.Contains(text, "def ")) {
			f.HasCode = true
		}
	}
	f.HasDebugMarkers = reDebug.MatchString(text)
	f.HasMath = reMath.MatchString(text)
	f.HasProof = reProof.MatchString(text)
	f.HasArchitecture = reArch.MatchString(text)
	f.HasComparison = reCompare.MatchString(text)
	f.WantsShortAnswer = reShort.MatchString(text)
	f.HasWritingMarkers = reWrite.MatchString(text)
	f.HasSummaryMarkers = reSummary.MatchString(text)
	f.HasExtractMarkers = reExtract.MatchString(text)
	f.HasTranslateMarkers = reTranslate.MatchString(text)
	f.HasReviewMarkers = reReview.MatchString(text)
	f.HasGenCodeMarkers = reGenCode.MatchString(text)

	if len(req.Tools) > 0 {
		f.HasTools = true
	}
	if req.ToolChoice != nil && (req.ToolChoice.Type == "required" || req.ToolChoice.Type == "tool") {
		f.ToolsRequired = true
		f.HasTools = true
	}
	for _, cap := range req.RequiredCapabilities {
		if strings.EqualFold(cap, "tools") {
			f.HasTools = true
		}
		if strings.EqualFold(cap, "vision") {
			f.HasVision = true
		}
	}
	if req.ResponseFormat != nil {
		if t, _ := req.ResponseFormat["type"].(string); t == "json_object" || t == "json_schema" {
			f.HasStructuredOut = true
		}
	}
	// crude constraint counting: bullet-like lines and "must/should/require"
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "1.") {
			f.ConstraintCount++
		}
	}
	if strings.Count(strings.ToLower(text), "must ") > 0 {
		f.ConstraintCount += strings.Count(strings.ToLower(text), "must ")
	}
	return f
}

func estimateTokens(s string) int {
	// ~4 chars per token approximation
	n := len(s) / 4
	if n < 1 && len(s) > 0 {
		return 1
	}
	return n
}

func denseCodeSignals(text string) bool {
	// braces + common keywords without needing fences
	if strings.Count(text, "{") >= 2 && strings.Count(text, "}") >= 2 {
		return true
	}
	keywords := []string{"func ", "def ", "class ", "import ", "package ", "public static", "fn ", "let mut "}
	hits := 0
	lower := text
	for _, k := range keywords {
		if strings.Contains(lower, k) {
			hits++
		}
	}
	return hits >= 2
}

func uniqueLower(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.ToLower(s)
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Classify runs the deterministic heuristic-v1 classifier.
func Classify(req *normalization.NormalizedRequest) TaskProfile {
	f := ExtractFeatures(req)
	return classifyFeatures(f, req)
}

func classifyFeatures(f RequestFeatures, req *normalization.NormalizedRequest) TaskProfile {
	scores := map[string]float64{
		TypeGeneralChat:           0.15,
		TypeSimpleTransformation:  0,
		TypeSummarization:         0,
		TypeInformationExtraction: 0,
		TypeCreativeWriting:       0,
		TypeProfessionalWriting:   0,
		TypeTranslation:           0,
		TypeCodingGeneration:      0,
		TypeCodingDebug:           0,
		TypeCodeReview:            0,
		TypeTechnicalExplanation:  0,
		TypeArchitectureDesign:    0,
		TypeReasoning:             0,
		TypeMathematics:           0,
		TypeAnalysis:              0,
		TypeResearchSynthesis:     0,
		TypeToolOperation:         0,
	}

	text := strings.TrimSpace(f.UserText)
	if reGreeting.MatchString(text) && f.ApproxTokens < 20 {
		scores[TypeGeneralChat] = 0.95
	}
	if f.HasSummaryMarkers {
		scores[TypeSummarization] += 0.85
	}
	if f.HasExtractMarkers {
		scores[TypeInformationExtraction] += 0.8
	}
	if f.HasWritingMarkers {
		scores[TypeCreativeWriting] += 0.75
	}
	if f.HasTranslateMarkers {
		scores[TypeTranslation] += 0.9
	}
	if f.HasReviewMarkers {
		scores[TypeCodeReview] += 0.9
	}
	if f.HasGenCodeMarkers || (f.HasCode && f.HasGenCodeMarkers) {
		scores[TypeCodingGeneration] += 0.75
	}
	if f.HasCode && f.HasDebugMarkers {
		scores[TypeCodingDebug] += 0.95
	} else if f.HasDebugMarkers {
		scores[TypeCodingDebug] += 0.7
	} else if f.HasCode {
		scores[TypeCodingGeneration] += 0.45
		scores[TypeCodeReview] += 0.25
	}
	if f.HasArchitecture {
		scores[TypeArchitectureDesign] += 0.85
	}
	if f.HasMath || f.HasProof {
		scores[TypeMathematics] += 0.85
		if f.HasProof {
			scores[TypeMathematics] += 0.1
			scores[TypeReasoning] += 0.5
		}
	}
	if reReason.MatchString(f.UserText) {
		scores[TypeReasoning] += 0.8
	}
	if reAnalysis.MatchString(f.UserText) {
		scores[TypeAnalysis] += 0.75
	}
	if reExplain.MatchString(f.UserText) {
		// Technical terms alone must not force complex coding.
		scores[TypeTechnicalExplanation] += 0.7
		if !f.HasCode && !f.HasDebugMarkers {
			scores[TypeCodingDebug] *= 0.2
			scores[TypeCodingGeneration] *= 0.3
		}
	}
	if reResearch.MatchString(f.UserText) {
		scores[TypeResearchSynthesis] += 0.8
	}
	if f.HasTools || f.ToolsRequired {
		scores[TypeToolOperation] += 0.7
		if f.ToolsRequired {
			scores[TypeToolOperation] += 0.25
		}
	}
	if reSimpleXform.MatchString(f.UserText) && f.ApproxTokens < 200 {
		scores[TypeSimpleTransformation] += 0.8
	}
	if f.HasComparison {
		scores[TypeAnalysis] += 0.35
		scores[TypeResearchSynthesis] += 0.2
	}

	primary, primaryScore := TypeUnknown, 0.0
	var secondary []string
	for t, s := range scores {
		if s > primaryScore {
			primary, primaryScore = t, s
		}
	}
	for t, s := range scores {
		if t != primary && s >= 0.45 {
			secondary = append(secondary, t)
		}
	}
	// stable secondary order
	for i := 1; i < len(secondary); i++ {
		j := i
		for j > 0 && secondary[j] < secondary[j-1] {
			secondary[j], secondary[j-1] = secondary[j-1], secondary[j]
			j--
		}
	}

	complexity := ComplexitySimple
	if f.ApproxTokens > 1500 || f.ConstraintCount >= 5 || f.HasArchitecture || f.HasProof {
		complexity = ComplexityComplex
	} else if f.ApproxTokens > 400 || f.HasCode || f.HasDebugMarkers || f.HasMath || f.ConstraintCount >= 2 || f.TurnCount > 4 {
		complexity = ComplexityMedium
	}
	// "Explain X to a five-year-old" stays simple even with technical words
	if reExplain.MatchString(f.UserText) && !f.HasCode && !f.HasDebugMarkers && f.ApproxTokens < 300 {
		complexity = ComplexitySimple
	}
	if f.WantsShortAnswer && complexity == ComplexityComplex {
		complexity = ComplexityMedium
	}

	reqs := map[string]float64{
		CapGeneral: 4, CapReasoning: 2, CapAnalysis: 2, CapCoding: 2,
		CapWriting: 2, CapToolUse: 0, CapMathematics: 2, CapLongContext: 2,
		CapSummarization: 2, CapExtraction: 2, CapStructuredOutput: 2,
	}
	switch primary {
	case TypeCodingDebug:
		reqs[CapCoding] = 10
		reqs[CapReasoning] = 8
		reqs[CapAnalysis] = 6
	case TypeCodingGeneration:
		reqs[CapCoding] = 8
		reqs[CapReasoning] = 6
	case TypeCodeReview:
		reqs[CapCoding] = 8
		reqs[CapAnalysis] = 8
		reqs[CapReasoning] = 6
	case TypeMathematics:
		reqs[CapMathematics] = 10
		reqs[CapReasoning] = 10
	case TypeReasoning:
		reqs[CapReasoning] = 10
		reqs[CapAnalysis] = 6
	case TypeArchitectureDesign:
		reqs[CapAnalysis] = 10
		reqs[CapReasoning] = 8
		reqs[CapCoding] = 6
	case TypeAnalysis, TypeResearchSynthesis:
		reqs[CapAnalysis] = 10
		reqs[CapReasoning] = 6
		reqs[CapLongContext] = 6
	case TypeSummarization:
		reqs[CapSummarization] = 10
		reqs[CapWriting] = 6
		if f.ApproxTokens > 2000 {
			reqs[CapLongContext] = 8
		}
	case TypeInformationExtraction:
		reqs[CapExtraction] = 10
		reqs[CapStructuredOutput] = 8
	case TypeCreativeWriting, TypeProfessionalWriting:
		reqs[CapWriting] = 10
		reqs[CapGeneral] = 6
	case TypeTranslation:
		reqs[CapMultilingual] = 10
		reqs[CapWriting] = 6
	case TypeToolOperation:
		reqs[CapToolUse] = 10
		reqs[CapInstructionFollowing] = 8
	case TypeTechnicalExplanation:
		reqs[CapGeneral] = 6
		reqs[CapWriting] = 6
		reqs[CapReasoning] = 4
	case TypeSimpleTransformation, TypeGeneralChat:
		reqs[CapGeneral] = 4
	}

	if complexity == ComplexityComplex {
		if reqs[CapReasoning] < 8 {
			reqs[CapReasoning] += 2
		}
		if reqs[CapAnalysis] < 6 {
			reqs[CapAnalysis] += 2
		}
	}
	if f.EstimatedContext > 8000 {
		reqs[CapLongContext] = maxFloat(reqs[CapLongContext], 8)
	}
	if f.EstimatedContext > 30000 {
		reqs[CapLongContext] = 10
	}

	hard := HardRequirements{
		Tools:                f.ToolsRequired || (f.HasTools && req != nil && req.ToolChoice != nil && req.ToolChoice.Type != "none"),
		Vision:               f.HasVision,
		StructuredOutput:     f.HasStructuredOut,
		MinimumContextWindow: f.EstimatedContext + 512,
	}
	if req != nil && req.MaxOutputTokens != nil {
		hard.MaxOutputTokens = *req.MaxOutputTokens
	}
	// Tools present as optional still mark soft requirement but not always hard.
	if f.ToolsRequired {
		hard.Tools = true
		reqs[CapToolUse] = maxFloat(reqs[CapToolUse], 8)
	} else if f.HasTools {
		reqs[CapToolUse] = maxFloat(reqs[CapToolUse], 6)
	}

	// Confidence from margin and signal strength
	secondBest := 0.0
	for t, s := range scores {
		if t != primary && s > secondBest {
			secondBest = s
		}
	}
	margin := primaryScore - secondBest
	confidence := clamp01(0.45 + primaryScore*0.35 + margin*0.25)
	if primary == TypeUnknown || primaryScore < 0.35 {
		confidence = 0.35
		primary = TypeUnknown
	}
	if f.ApproxTokens < 5 && !f.HasTools && !f.HasVision {
		confidence = min(confidence, 0.55)
	}

	prefs := TaskPreferences{Latency: "medium", Cost: "balanced", Privacy: "any"}
	if f.WantsShortAnswer || complexity == ComplexitySimple {
		prefs.Latency = "low"
	}
	if complexity == ComplexityComplex {
		prefs.Cost = "high"
	}

	return TaskProfile{
		PrimaryType:       primary,
		SecondaryTypes:    secondary,
		Complexity:        complexity,
		Requirements:      reqs,
		HardRequirements:  hard,
		Preferences:       prefs,
		Confidence:        confidence,
		Classifier:        "heuristic",
		ClassifierVersion: ClassifierVersion,
	}
}

// ClassifyPrompt is a convenience for CLI dry-run classification.
func ClassifyPrompt(prompt string) TaskProfile {
	req := &normalization.NormalizedRequest{
		Messages: []normalization.Message{{
			Role:    normalization.RoleUser,
			Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: prompt}},
		}},
	}
	return Classify(req)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// printableRuneRatio is available for future adversarial length inflation checks.
func printableRuneRatio(s string) float64 {
	if len(s) == 0 {
		return 1
	}
	ok, total := 0, 0
	for _, r := range s {
		total++
		if unicode.IsPrint(r) || unicode.IsSpace(r) {
			ok++
		}
	}
	return float64(ok) / float64(total)
}
