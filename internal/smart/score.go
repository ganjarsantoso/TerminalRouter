package smart

import (
	"math"
	"sort"
)

// specializedCaps are dimensions that contribute to specialization match.
var specializedCaps = []string{
	CapCoding, CapReasoning, CapMathematics, CapAnalysis,
	CapToolUse, CapWriting, CapLongContext, CapExtraction, CapSummarization,
}

// ScoreCandidate computes soft preference score for an eligible candidate.
func ScoreCandidate(profile ModelProfile, task TaskProfile, policy Policy, order int, reliability float64) CandidateEvaluation {
	ev := CandidateEvaluation{
		Provider:  profile.ProviderID,
		Model:     profile.ModelID,
		ProfileID: profile.ID,
		Eligible:  true,
	}
	if ev.ProfileID == "" {
		ev.ProfileID = ProfileKey(profile.ProviderID, profile.ModelID)
	}

	taskMatch := capabilityMatch(task.Requirements, profile)
	ev.TaskMatch = taskMatch

	// Floor rejection
	minMatch := policy.MinimumTaskMatch
	if minMatch <= 0 {
		minMatch = 0.60
	}
	if taskMatch < minMatch {
		ev.Eligible = false
		ev.RejectionReasons = []string{"below minimum task match floor"}
		ev.FinalScore = taskMatch
		return ev
	}

	spec := specializationMatch(task.Requirements, profile)
	quality := qualityScore(profile)
	rel := reliability
	if rel <= 0 {
		rel = 0.85 // neutral default when unknown
	}
	cost := costPreference(profile.Properties.CostTier) // higher = cheaper preferred
	lat := latencyPreference(profile.Properties.LatencyTier)

	// Uncertainty for unknown/missing capability values
	uncert := uncertaintyPenalty(profile, task)

	w := policy.Weights
	final := w.TaskMatch*taskMatch +
		w.SpecializedMatch*spec +
		w.Quality*quality +
		w.Reliability*rel +
		w.Cost*cost +
		w.Latency*lat -
		uncert

	// Tiny order bias for deterministic stability (not enough to override real score diffs)
	final -= float64(order) * 1e-6

	ev.Components = ComponentScores{
		TaskMatch:        taskMatch,
		SpecializedMatch: spec,
		Quality:          quality,
		Reliability:      rel,
		Cost:             cost,
		Latency:          lat,
		Uncertainty:      uncert,
	}
	ev.FinalScore = final
	ev.Explanation = buildScoreReasons(profile, task, taskMatch, spec, cost, lat)
	return ev
}

// capabilityMatch compares required levels with profile; excess unused capability is not rewarded.
func capabilityMatch(req map[string]float64, profile ModelProfile) float64 {
	if len(req) == 0 {
		return 0.7
	}
	var weightSum, scoreSum float64
	for cap, need := range req {
		if need <= 0 {
			continue
		}
		have := profile.Cap(cap)
		w := float64(need)
		weightSum += w
		if have <= 0 {
			// unknown: partial credit
			scoreSum += w * 0.4
			continue
		}
		// ratio capped at 1.0 — no bonus for overshoot
		ratio := float64(have) / float64(need)
		if ratio > 1 {
			ratio = 1
		}
		// soft penalty when have is much lower
		if have < need {
			ratio = ratio * ratio // quadratic shortfall penalty
		}
		scoreSum += w * ratio
	}
	if weightSum == 0 {
		return 0.7
	}
	return scoreSum / weightSum
}

func specializationMatch(req map[string]float64, profile ModelProfile) float64 {
	// Focus on highest task requirements among specialized dims
	type pair struct {
		cap  string
		need float64
	}
	var top []pair
	for _, cap := range specializedCaps {
		if n := req[cap]; n >= 6 {
			top = append(top, pair{cap, n})
		}
	}
	if len(top) == 0 {
		return 0.5
	}
	var sum float64
	for _, p := range top {
		have := profile.Cap(p.cap)
		if have <= 0 {
			sum += 0.3
			continue
		}
		r := float64(have) / 10.0
		// weight by how much the task needs it
		sum += r * (float64(p.need) / 10.0)
	}
	return sum / float64(len(top))
}

func qualityScore(profile ModelProfile) float64 {
	// Average of key quality-ish capabilities
	keys := []string{CapGeneral, CapReasoning, CapInstructionFollowing, CapAnalysis}
	var sum, n float64
	for _, k := range keys {
		v := profile.Cap(k)
		if v > 0 {
			sum += float64(v)
			n++
		}
	}
	if n == 0 {
		return 0.5
	}
	return (sum / n) / 10.0
}

func costPreference(tier int) float64 {
	if tier <= 0 {
		return 0.5
	}
	// tier 1 (cheap) → 1.0, tier 5 (expensive) → 0.0
	return (6.0 - float64(tier)) / 5.0
}

func latencyPreference(tier int) float64 {
	if tier <= 0 {
		return 0.5
	}
	// tier 1 (fast) → 1.0, tier 5 (slow) → 0.0
	return (6.0 - float64(tier)) / 5.0
}

func uncertaintyPenalty(profile ModelProfile, task TaskProfile) float64 {
	if profile.Source == SourceUnknown {
		return 0.15
	}
	pen := 0.0
	for cap, need := range task.Requirements {
		if need >= 6 && profile.Cap(cap) == 0 {
			pen += 0.02
		}
	}
	if pen > 0.12 {
		pen = 0.12
	}
	return pen
}

func buildScoreReasons(profile ModelProfile, task TaskProfile, taskMatch, spec, cost, lat float64) []string {
	var reasons []string
	if task.Requirements[CapCoding] >= 8 && profile.Cap(CapCoding) >= 8 {
		reasons = append(reasons, "strong coding profile")
	}
	if task.Requirements[CapReasoning] >= 8 && profile.Cap(CapReasoning) >= 8 {
		reasons = append(reasons, "strong reasoning match")
	}
	if task.Requirements[CapAnalysis] >= 8 && profile.Cap(CapAnalysis) >= 8 {
		reasons = append(reasons, "strong analysis match")
	}
	if task.Requirements[CapToolUse] >= 6 && profile.Cap(CapToolUse) >= 8 {
		reasons = append(reasons, "strong tool-use profile")
	}
	if profile.Properties.CostTier > 0 && profile.Properties.CostTier <= 2 {
		reasons = append(reasons, "low cost tier")
	} else if profile.Properties.CostTier == 3 {
		reasons = append(reasons, "medium cost tier")
	}
	if profile.Properties.LatencyTier > 0 && profile.Properties.LatencyTier <= 2 {
		reasons = append(reasons, "low latency tier")
	}
	if profile.Properties.Privacy == PrivacyLocal {
		reasons = append(reasons, "local privacy")
	}
	if taskMatch >= 0.8 {
		reasons = append(reasons, "high task capability match")
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "best available score under policy")
	}
	return reasons
}

// SortEvaluations sorts eligible candidates by score then tie-break rules.
// Ineligible candidates are sorted after eligible ones.
func SortEvaluations(evals []CandidateEvaluation, order map[string]int) {
	sort.SliceStable(evals, func(i, j int) bool {
		a, b := evals[i], evals[j]
		if a.Eligible != b.Eligible {
			return a.Eligible
		}
		if !a.Eligible {
			return false
		}
		// 1. Higher final score
		if math.Abs(a.FinalScore-b.FinalScore) > 1e-9 {
			return a.FinalScore > b.FinalScore
		}
		// 2. Higher task-match
		if math.Abs(a.TaskMatch-b.TaskMatch) > 1e-9 {
			return a.TaskMatch > b.TaskMatch
		}
		// 3. Higher reliability component
		if math.Abs(a.Components.Reliability-b.Components.Reliability) > 1e-9 {
			return a.Components.Reliability > b.Components.Reliability
		}
		// 4. Lower cost (higher cost component score means cheaper)
		if math.Abs(a.Components.Cost-b.Components.Cost) > 1e-9 {
			return a.Components.Cost > b.Components.Cost
		}
		// 5. Lower latency (higher latency component = faster)
		if math.Abs(a.Components.Latency-b.Components.Latency) > 1e-9 {
			return a.Components.Latency > b.Components.Latency
		}
		// 6. Earlier config order
		ka, kb := ProfileKey(a.Provider, a.Model), ProfileKey(b.Provider, b.Model)
		oa, ob := order[ka], order[kb]
		if oa != ob {
			return oa < ob
		}
		// 7. Lexical
		return ka < kb
	})
}
