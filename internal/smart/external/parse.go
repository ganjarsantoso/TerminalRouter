package external

import (
	"regexp"
	"strconv"
	"strings"
)

// benchmarkPattern describes how to recognize a benchmark figure in free text
// and which capability/source it maps to.
type benchmarkPattern struct {
	re       *regexp.Regexp
	source   SourceID
	cap      CapabilityKey
	scale    ScaleKind
	label    string
}

// pct extracts a percentage value near a benchmark keyword. We look for the
// benchmark name followed (within a few words) by a number with optional %.
var benchmarkPatterns = []benchmarkPattern{
	{
		re:     regexp.MustCompile(`(?i)livebench[^%0-9]{0,40}?([0-9]{1,3}(?:\.[0-9]+)?)\s*%`),
		source: SourceLiveBench, cap: CapGeneral, scale: ScaleZeroToHundred, label: "livebench/overall",
	},
	{
		re:     regexp.MustCompile(`(?i)overall score of ([0-9]{1,3}(?:\.[0-9]+)?)/100`),
		source: SourceLiveBench, cap: CapGeneral, scale: ScaleZeroToHundred, label: "livebench/overall",
	},
	{
		re:     regexp.MustCompile(`(?i)overall[^%0-9]{0,40}?([0-9]{1,3}(?:\.[0-9]+)?)\s*/\s*100`),
		source: SourceLiveBench, cap: CapGeneral, scale: ScaleZeroToHundred, label: "livebench/overall",
	},
	{
		re:     regexp.MustCompile(`(?i)livebench[^%0-9]{0,40}?reasoning[^%0-9]{0,30}?([0-9]{1,3}(?:\.[0-9]+)?)\s*%`),
		source: SourceLiveBench, cap: CapReasoning, scale: ScaleZeroToHundred, label: "livebench/reasoning",
	},
	{
		re:     regexp.MustCompile(`(?i)livebench[^%0-9]{0,40}?math[^%0-9]{0,30}?([0-9]{1,3}(?:\.[0-9]+)?)\s*%`),
		source: SourceLiveBench, cap: CapMathematics, scale: ScaleZeroToHundred, label: "livebench/math",
	},
	{
		re:     regexp.MustCompile(`(?i)(?:swe[- ]?bench[^%0-9]{0,40}?verified[^%0-9]{0,30}?([0-9]{1,3}(?:\.[0-9]+)?)\s*%)|(?:([0-9]{1,3}(?:\.[0-9]+)?)\s*%[^%0-9]{0,30}?swe[- ]?bench)`),
		source: SourceSWEBench, cap: CapCoding, scale: ScaleZeroToHundred, label: "swebench/verified",
	},
	{
		re:     regexp.MustCompile(`(?i)(?:swe[- ]?bench[^%0-9]{0,40}?([0-9]{1,3}(?:\.[0-9]+)?)\s*%)|(?:([0-9]{1,3}(?:\.[0-9]+)?)\s*%[^%0-9]{0,40}?swe[- ]?bench)`),
		source: SourceSWEBench, cap: CapCoding, scale: ScaleZeroToHundred, label: "swebench",
	},
	{
		re:     regexp.MustCompile(`(?i)artificial analysis intelligence index[^0-9]{0,40}?([0-9]{1,3}(?:\.[0-9]+)?)`),
		source: SourceAAII, cap: CapGeneral, scale: ScaleZeroToHundred, label: "aa/index",
	},
	{
		re:     regexp.MustCompile(`(?i)intelligence index[^0-9]{0,40}?([0-9]{1,3}(?:\.[0-9]+)?)`),
		source: SourceAAII, cap: CapGeneral, scale: ScaleZeroToHundred, label: "aa/index",
	},
	{
		re:     regexp.MustCompile(`(?i)lmarena[^0-9]{0,40}?elo[^0-9]{0,30}?([0-9]{3,4})`),
		source: SourceLMArena, cap: CapGeneral, scale: ScaleElo, label: "lmarena/overall",
	},
	{
		re:     regexp.MustCompile(`(?i)arena[^0-9]{0,40}?(elo|score)[^0-9]{0,30}?([0-9]{3,4})`),
		source: SourceLMArena, cap: CapGeneral, scale: ScaleElo, label: "lmarena/overall",
	},
	{
		re:     regexp.MustCompile(`(?i)mmlu[- ]?pro[^%0-9]{0,40}?([0-9]{1,3}(?:\.[0-9]+)?)\s*%`),
		source: SourceAAII, cap: CapGeneral, scale: ScaleZeroToHundred, label: "mmlu-pro",
	},
	{
		re:     regexp.MustCompile(`(?i)gpqa[^%0-9]{0,40}?([0-9]{1,3}(?:\.[0-9]+)?)\s*%`),
		source: SourceAAII, cap: CapReasoning, scale: ScaleZeroToHundred, label: "gpqa",
	},
	{
		re:     regexp.MustCompile(`(?i)math[- ]?500[^%0-9]{0,40}?([0-9]{1,3}(?:\.[0-9]+)?)\s*%`),
		source: SourceAAII, cap: CapMathematics, scale: ScaleZeroToHundred, label: "math500",
	},
	{
		re:     regexp.MustCompile(`(?i)humaneval[^%0-9]{0,40}?([0-9]{1,3}(?:\.[0-9]+)?)\s*%`),
		source: SourceAAII, cap: CapCoding, scale: ScaleZeroToHundred, label: "humaneval",
	},
	{
		re:     regexp.MustCompile(`(?i)ifeval[^%0-9]{0,40}?([0-9]{1,3}(?:\.[0-9]+)?)\s*%`),
		source: SourceAAII, cap: CapInstructionFollowing, scale: ScaleZeroToHundred, label: "ifeval",
	},
	{
		re:     regexp.MustCompile(`(?i)mmmu[^%0-9]{0,40}?([0-9]{1,3}(?:\.[0-9]+)?)\s*%`),
		source: SourceAAII, cap: CapGeneral, scale: ScaleZeroToHundred, label: "mmmu",
	},
}

// extractEvidence pulls benchmark figures from a set of search results for a
// given canonical model identity.
func extractEvidence(id ModelIdentity, results []SearchResult) []EvidenceRecord {
	var out []EvidenceRecord
	seen := map[string]bool{}
	modelLower := strings.ToLower(id.Model)
	nameLower := strings.ToLower(id.Name)

	for _, r := range results {
		text := r.Title + ". " + r.Snippet
		low := strings.ToLower(text)
		// Only consider results that actually mention this model.
		if !strings.Contains(low, modelLower) && !strings.Contains(low, nameLower) && !strings.Contains(low, strings.ToLower(id.ID)) {
			continue
		}
		for _, p := range benchmarkPatterns {
			m := p.re.FindStringSubmatch(text)
			if m == nil {
				continue
			}
			var raw string
			for _, g := range m[1:] {
				if g != "" {
					raw = g
					break
				}
			}
			if raw == "" {
				continue
			}
			val, err := strconv.ParseFloat(raw, 64)
			if err != nil || val <= 0 {
				continue
			}
			dkey := string(p.source) + "|" + p.label + "|" + strconv.FormatFloat(val, 'f', 2, 64)
			if seen[dkey] {
				continue
			}
			seen[dkey] = true
			out = append(out, EvidenceRecord{
				Source:        p.source,
				ModelIdentity: id.ID,
				Benchmark:     p.label,
				Value:         val,
				Scale:         p.scale,
				Capability:    p.cap,
				ReportedAt:    registryUpdatedAt,
				URL:           r.URL,
			})
		}
	}
	return out
}
