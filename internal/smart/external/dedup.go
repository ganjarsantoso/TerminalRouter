package external

import (
	"strconv"
	"strings"
)

// CanonicalExperimentKey derives a stable key for one benchmark experiment (§19)
// from as many of the recommended fields as are available. The same result
// published on a leaderboard, model card, mirror, or blog collapses to one key;
// the duplicate sources are stored as provenance, not independent evidence.
//
// Fields: benchmark id, exact model identity, harness, reasoning setting, raw
// score, evaluation date, run/submission id, original publisher.
func CanonicalExperimentKey(rec EvidenceRecord, configured ModelIdentity) string {
	parts := []string{
		rec.Benchmark,
		configured.CanonicalKey(),
		strings.ToLower(strings.TrimSpace(rec.Harness)),
		strings.ToLower(strings.TrimSpace(rec.ReasoningSetting)),
		strconv.FormatFloat(rec.RawScore, 'f', 2, 64),
		strings.ToLower(strings.TrimSpace(rec.EvaluationDate)),
		strings.ToLower(strings.TrimSpace(rec.RunID)),
		strings.ToLower(strings.TrimSpace(rec.OriginalPublisher)),
	}
	return strings.Join(parts, "|")
}

// benchmarkFamily returns the top-level family of a benchmark label
// (e.g. "livebench/overall" -> "livebench"). Used for source-family caps (§19).
func benchmarkFamily(b string) string {
	if i := strings.Index(b, "/"); i >= 0 {
		return b[:i]
	}
	return b
}

// applyContributionCaps prevents a single source organization or benchmark
// family from dominating a capability dimension merely by producing more
// records (§19). It caps the number of contributing records per source and per
// benchmark family, returning the trimmed list and how many were excluded.
func applyContributionCaps(list []EvidenceRecordWithNorm) ([]EvidenceRecordWithNorm, int) {
	const maxPerSource = 4
	const maxPerBenchmarkFamily = 6
	srcCount := map[SourceID]int{}
	famCount := map[string]int{}
	out := make([]EvidenceRecordWithNorm, 0, len(list))
	excluded := 0
	for _, e := range list {
		s := e.Evidence.Source
		fam := benchmarkFamily(e.Evidence.Benchmark)
		if srcCount[s] >= maxPerSource || famCount[fam] >= maxPerBenchmarkFamily {
			excluded++
			continue
		}
		srcCount[s]++
		famCount[fam]++
		out = append(out, e)
	}
	return out, excluded
}

// recordWeight combines the source trust tier with the identity-match weight.
func recordWeight(e EvidenceRecordWithNorm) float64 {
	w := tierWeight(e.Normal.Tier) * e.Weight
	if w <= 0 {
		return 0
	}
	return w
}
