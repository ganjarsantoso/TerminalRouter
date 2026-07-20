package external

import (
	"net/url"
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
	matchTerms := modelMatchTerms(id)

	for _, r := range results {
		text := r.Title + ". " + r.Snippet
		low := strings.ToLower(text)
		// Only consider results that actually mention this model (in any of its
		// normalized forms). The exact id (e.g. "stepfun-ai/step-3.7-flash") rarely
		// appears verbatim in search snippets; "Step 3.7 Flash" does.
		mentions := false
		for _, t := range matchTerms {
			if t != "" && strings.Contains(low, t) {
				mentions = true
				break
			}
		}
		// Also accept results from known benchmark/aggregator sites, since the
		// search query was already model-specific (e.g. artificialanalysis.ai,
		// benchlm.ai, livebench.ai, swebench, aibenchy, huggingface model pages).
		if !mentions && isBenchmarkSource(r.URL) {
			mentions = true
		}
		if !mentions {
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
				Published:     id,
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

// modelMatchTerms returns normalized lowercased strings that may identify the
// model in free text. It derives several forms from the id/model so that both
// "stepfun-ai/step-3.7-flash" and "Step 3.7 Flash" match.
func modelMatchTerms(id ModelIdentity) []string {
	raw := strings.ToLower(id.Model)
	parts := []string{
		strings.ToLower(id.ID),
		raw,
		strings.ToLower(id.Name),
	}
	// Model without a provider prefix (e.g. "stepfun-ai/step-3.7-flash" -> "step-3.7-flash").
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		parts = append(parts, raw[i+1:])
	}
	// Human-readable form: separators -> spaces, drop provider/org tokens.
	noSep := strings.NewReplacer("/", " ", "_", " ", "-", " ").Replace(raw)
	parts = append(parts, strings.TrimSpace(noSep))
	// Core name without org/provider prefix tokens (e.g. "step-3.7-flash", "3.7 flash").
	tokens := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '/' || r == '_' || r == '-' || r == '.'
	})
	if len(tokens) > 1 {
		// Drop a leading org token like "stepfun" or "ai".
		core := tokens
		if len(core) > 1 && (core[0] == "stepfun" || core[0] == "ai" || core[0] == "nvidia") {
			core = core[1:]
		}
		parts = append(parts, strings.Join(core, " "))
		parts = append(parts, strings.Join(core[len(core)-2:], " ")) // e.g. "3.7 flash"
	}
	// Dedupe.
	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// isBenchmarkSource reports whether a result URL is from a known independent
// benchmark/aggregator site. The search query already targeted the model, so
// such results are almost certainly about it even if the model name isn't in
// the snippet.
func isBenchmarkSource(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	for _, h := range []string{
		"benchlm.ai", "artificialanalysis.ai", "livebench.ai", "swebench.com",
		"lmarena.ai", "aibenchy.com", "huggingface.co", "github.com",
	} {
		if strings.Contains(host, h) {
			return true
		}
	}
	return false
}
