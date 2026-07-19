package external

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// evidenceForIdentity returns all curated evidence records for a model identity.
func evidenceForIdentity(identityID string) []EvidenceRecord {
	var out []EvidenceRecord
	for _, e := range sampleEvidence {
		if e.ModelIdentity == identityID {
			out = append(out, e)
		}
	}
	return out
}

// buildConsensus computes the external-consensus profile for a model identity.
func buildConsensus(id ModelIdentity) ConsensusProfile {
	recs := evidenceForIdentity(id.ID)
	byCap := map[CapabilityKey][]EvidenceRecordWithNorm{}
	seen := map[string]bool{}
	var sourceSet []SourceID
	sourceSeen := map[SourceID]bool{}

	for _, r := range recs {
		// Dedupe identical evidence (same source+benchmark+value+capability).
		dkey := fmt.Sprintf("%s|%s|%s|%.4f|%s", r.Source, r.ModelIdentity, r.Benchmark, r.Value, r.Capability)
		if seen[dkey] {
			continue
		}
		seen[dkey] = true

		n := normalize(r)
		en := EvidenceRecordWithNorm{Evidence: r, Normal: n}
		byCap[r.Capability] = append(byCap[r.Capability], en)
		if !sourceSeen[r.Source] {
			sourceSeen[r.Source] = true
			sourceSet = append(sourceSet, r.Source)
		}
	}

	caps := map[CapabilityKey]ConsensusCapability{}
	var overallVals []float64
	for _, c := range CapabilityKeys {
		list := byCap[c]
		if len(list) == 0 {
			continue
		}
		est, conf, low, high, primary := aggregateCapability(list)
		caps[c] = ConsensusCapability{
			Capability:    c,
			Estimate:      roundHalf(est),
			Confidence:    conf,
			LowBand:       roundHalf(low),
			HighBand:      roundHalf(high),
			SourceCount:   len(list),
			Contributing:  list,
			PrimarySource: primary,
		}
		overallVals = append(overallVals, est)
	}

	overall := 0.0
	if len(overallVals) > 0 {
		overall = mean(overallVals)
	}

	return ConsensusProfile{
		ModelIdentity: id.ID,
		ProviderID:    id.Provider,
		ModelID:       id.Model,
		Capabilities:  caps,
		Overall:       roundHalf(overall),
		Sources:       sourceSet,
		Confidence:    overallConfidence(caps),
		GeneratedAt:   time.Now().UTC(),
	}
}

// aggregateCapability returns (estimate, confidence, lowBand, highBand, primarySource).
func aggregateCapability(list []EvidenceRecordWithNorm) (float64, float64, float64, float64, SourceID) {
	if len(list) == 1 {
		n := list[0].Normal
		return n.Normalized, tierWeight(n.Tier) * 0.6, math.Max(0, n.Normalized-1), math.Min(10, n.Normalized+1), n.Source
	}

	// Weighted trimmed mean: sort by normalized, drop the single highest/lowest
	// if we have >=4 samples, weight by trust tier.
	sorted := make([]EvidenceRecordWithNorm, len(list))
	copy(sorted, list)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Normal.Normalized < sorted[j].Normal.Normalized
	})

	lo, hi := 0, len(sorted)-1
	if len(sorted) >= 4 {
		lo++
		hi--
	}

	var wsum, vsum float64
	for i := lo; i <= hi; i++ {
		w := tierWeight(sorted[i].Normal.Tier)
		wsum += w
		vsum += w * sorted[i].Normal.Normalized
	}
	est := vsum / wsum

	// Weighted median as a cross-check; prefer trimmed mean but pull toward median
	// when spread is large (robustness).
	median := weightedMedian(sorted[lo : hi+1])
	spread := sorted[hi].Normal.Normalized - sorted[lo].Normal.Normalized
	robust := est
	if spread > 2.0 {
		robust = est*0.5 + median*0.5
	}

	// Confidence from agreement (inverse of spread) and trust weights.
	agreement := 1.0 - clamp01(spread/10.0)
	avgTrust := 0.0
	for i := lo; i <= hi; i++ {
		avgTrust += tierWeight(sorted[i].Normal.Tier)
	}
	avgTrust /= float64(hi - lo + 1)
	conf := clamp01(agreement*0.6 + avgTrust*0.4)

	band := (1.0 - conf) * 3.0
	low := math.Max(0, robust-band)
	high := math.Min(10, robust+band)

	// Primary source = highest-trust source among the window.
	primary := sorted[lo].Normal.Source
	bestW := tierWeight(sorted[lo].Normal.Tier)
	for i := lo + 1; i <= hi; i++ {
		if w := tierWeight(sorted[i].Normal.Tier); w > bestW {
			bestW = w
			primary = sorted[i].Normal.Source
		}
	}
	return robust, conf, low, high, primary
}

func weightedMedian(items []EvidenceRecordWithNorm) float64 {
	if len(items) == 0 {
		return 0
	}
	type wp struct {
		v float64
		w float64
	}
	ws := make([]wp, len(items))
	wsum := 0.0
	for i, it := range items {
		w := tierWeight(it.Normal.Tier)
		ws[i] = wp{it.Normal.Normalized, w}
		wsum += w
	}
	sort.Slice(ws, func(i, j int) bool { return ws[i].v < ws[j].v })
	target := wsum / 2.0
	cum := 0.0
	for _, x := range ws {
		cum += x.w
		if cum >= target {
			return x.v
		}
	}
	return ws[len(ws)-1].v
}

func mean(vs []float64) float64 {
	if len(vs) == 0 {
		return 0
	}
	s := 0.0
	for _, v := range vs {
		s += v
	}
	return s / float64(len(vs))
}

func overallConfidence(caps map[CapabilityKey]ConsensusCapability) float64 {
	if len(caps) == 0 {
		return 0
	}
	s := 0.0
	for _, c := range caps {
		s += c.Confidence
	}
	return s / float64(len(caps))
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
