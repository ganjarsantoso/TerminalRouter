package external

import (
	"math"
	"sort"
	"time"
)

// buildConsensus computes the external-consensus profile for a model identity
// from a set of (live-fetched or cached) evidence records. It applies the §18
// variant-matching rules (exclude incompatible/family-only, reduce strong-
// probable with mandatory review) and the §19 experiment dedup + source-family
// contribution caps.
func buildConsensus(id ModelIdentity, recs []EvidenceRecord) ConsensusProfile {
	byCap := map[CapabilityKey][]EvidenceRecordWithNorm{}
	excludedByCap := map[CapabilityKey]int{}
	var sourceSet []SourceID
	sourceSeen := map[SourceID]bool{}

	// Track canonical experiments so mirrors fold into provenance rather than
	// inflating the evidence set (§19).
	seenExpIdx := map[string]int{}
	seenExpCap := map[string]CapabilityKey{}

	for _, r := range recs {
		// Schema/registry validation (§16): reject records that are malformed
		// or outside the native range. Unverified (non-approved-domain) records
		// contribute zero automatic score weight unless explicitly promoted.
		if vr := ValidateEvidenceRecord(r); !vr.OK || vr.Unverified {
			continue
		}

		// Variant matching (§18): decide whether this evidence may contribute.
		match := id.Match(r.Published)
		if !match.Contributes {
			excludedByCap[r.Capability]++
			continue
		}

		// Dedupe by canonical experiment key (§19): keep the first, fold the
		// rest into provenance URLs.
		ekey := CanonicalExperimentKey(r, id)
		if idx, ok := seenExpIdx[ekey]; ok {
			if r.URL != "" {
				cap0 := seenExpCap[ekey]
				byCap[cap0][idx].Evidence.ProvenanceURLs = appendUnique(byCap[cap0][idx].Evidence.ProvenanceURLs, r.URL)
			}
			continue
		}

		n := normalize(r)
		en := EvidenceRecordWithNorm{Evidence: r, Normal: n, Match: match, Weight: match.Weight}
		seenExpIdx[ekey] = len(byCap[r.Capability])
		seenExpCap[ekey] = r.Capability
		byCap[r.Capability] = append(byCap[r.Capability], en)
		if !sourceSeen[r.Source] {
			sourceSeen[r.Source] = true
			sourceSet = append(sourceSet, r.Source)
		}
	}

	caps := map[CapabilityKey]ConsensusCapability{}
	var overallVals []float64
	mandatory := false
	for _, c := range CapabilityKeys {
		list := byCap[c]
		if len(list) == 0 {
			continue
		}
		// Source-family contribution caps (§19).
		capped, capsExcluded := applyContributionCaps(list)
		excludedByCap[c] += capsExcluded
		if len(capped) == 0 {
			continue
		}
		est, conf, low, high, primary, review := aggregateCapability(capped)
		if review {
			mandatory = true
		}
		caps[c] = ConsensusCapability{
			Capability:     c,
			Estimate:       roundHalf(est),
			Confidence:     conf,
			LowBand:        roundHalf(low),
			HighBand:       roundHalf(high),
			SourceCount:    len(capped),
			Contributing:   capped,
			PrimarySource:  primary,
			MandatoryReview: review,
			ExcludedCount:  excludedByCap[c],
		}
		overallVals = append(overallVals, est)
	}

	overall := 0.0
	if len(overallVals) > 0 {
		overall = mean(overallVals)
	}

	return ConsensusProfile{
		ModelIdentity:   id.ID,
		ProviderID:      id.Provider,
		ModelID:         id.Model,
		Capabilities:    caps,
		Overall:         roundHalf(overall),
		Sources:         sourceSet,
		Confidence:      overallConfidence(caps),
		GeneratedAt:     time.Now().UTC(),
		MandatoryReview: mandatory,
	}
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// aggregateCapability returns (estimate, confidence, lowBand, highBand,
// primarySource, mandatoryReview).
func aggregateCapability(list []EvidenceRecordWithNorm) (float64, float64, float64, float64, SourceID, bool) {
	mandatoryReview := false
	for _, e := range list {
		if e.Match.MandatoryReview {
			mandatoryReview = true
		}
	}
	if len(list) == 1 {
		n := list[0].Normal
		return n.Normalized, recordWeight(list[0]) * 0.6, math.Max(0, n.Normalized-1), math.Min(10, n.Normalized+1), n.Source, mandatoryReview
	}

	// Weighted trimmed mean: sort by normalized, drop the single highest/lowest
	// if we have >=4 samples, weight by trust tier * identity-match weight.
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
		w := recordWeight(sorted[i])
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
		avgTrust += recordWeight(sorted[i])
	}
	avgTrust /= float64(hi - lo + 1)
	conf := clamp01(agreement*0.6 + avgTrust*0.4)

	band := (1.0 - conf) * 3.0
	low := math.Max(0, robust-band)
	high := math.Min(10, robust+band)

	// Primary source = highest-weighted source among the window.
	primary := sorted[lo].Normal.Source
	bestW := recordWeight(sorted[lo])
	for i := lo + 1; i <= hi; i++ {
		if w := recordWeight(sorted[i]); w > bestW {
			bestW = w
			primary = sorted[i].Normal.Source
		}
	}
	return robust, conf, low, high, primary, mandatoryReview
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
