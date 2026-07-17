package smart

import (
	"fmt"
	"strings"
)

// FormatDecision produces human-readable explanation text (no raw prompts).
func FormatDecision(d *Decision) string {
	if d == nil {
		return "no decision"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "SMART ROUTE: %s\n", d.RouteID)
	if d.RequestedAlias != "" {
		fmt.Fprintf(&b, "REQUESTED ALIAS: %s\n", d.RequestedAlias)
	}
	fmt.Fprintf(&b, "MODE: %s\nPOLICY: %s\n\n", d.Mode, d.Policy)

	fmt.Fprintf(&b, "Task classification:\n")
	fmt.Fprintf(&b, "  Primary type:       %s\n", d.Task.PrimaryType)
	fmt.Fprintf(&b, "  Complexity:         %s\n", d.Task.Complexity)
	fmt.Fprintf(&b, "  Coding requirement: %d/5\n", d.Task.Requirements[CapCoding])
	fmt.Fprintf(&b, "  Reasoning:          %d/5\n", d.Task.Requirements[CapReasoning])
	fmt.Fprintf(&b, "  Analysis:           %d/5\n", d.Task.Requirements[CapAnalysis])
	toolReq := "not required"
	if d.Task.HardRequirements.Tools {
		toolReq = "required"
	} else if d.Task.Requirements[CapToolUse] >= 3 {
		toolReq = "preferred"
	}
	fmt.Fprintf(&b, "  Tool use:           %s\n", toolReq)
	fmt.Fprintf(&b, "  Confidence:         %.2f\n", d.Task.Confidence)
	fmt.Fprintf(&b, "  Classifier:         %s\n\n", d.Task.ClassifierVersion)

	fmt.Fprintf(&b, "Eligible candidates:\n")
	rank := 0
	for _, ev := range d.Evaluations {
		if !ev.Eligible {
			continue
		}
		rank++
		fmt.Fprintf(&b, "  %d. %s/%-28s score %.2f\n", rank, ev.Provider, ev.Model, ev.FinalScore)
	}
	if rank == 0 {
		b.WriteString("  (none)\n")
	}
	b.WriteByte('\n')

	fmt.Fprintf(&b, "Rejected candidates:\n")
	rejected := 0
	for _, ev := range d.Evaluations {
		if ev.Eligible {
			continue
		}
		rejected++
		fmt.Fprintf(&b, "  %s/%s\n", ev.Provider, ev.Model)
		for _, r := range ev.RejectionReasons {
			fmt.Fprintf(&b, "    Reason: %s\n", r)
		}
	}
	if rejected == 0 {
		b.WriteString("  (none)\n")
	}
	b.WriteByte('\n')

	fmt.Fprintf(&b, "Selected:\n  %s\n\n", d.SelectedKey())
	if d.UsedDefault {
		fmt.Fprintf(&b, "Default used: %s\n\n", d.DefaultReason)
	}
	if d.SessionAffinity.Hit {
		fmt.Fprintf(&b, "Session affinity: hit (%s)\n\n", d.SessionAffinity.Reason)
	} else if d.SessionAffinity.Reclassified {
		fmt.Fprintf(&b, "Session affinity: reclassified (%s)\n\n", d.SessionAffinity.Reason)
	}

	fmt.Fprintf(&b, "Primary reasons:\n")
	for _, r := range d.SelectionReasons {
		fmt.Fprintf(&b, "  + %s\n", r)
	}
	if d.Mode == ModeShadow && d.ShadowRecommendation != "" {
		fmt.Fprintf(&b, "\nShadow recommendation: %s\n", d.ShadowRecommendation)
		fmt.Fprintf(&b, "(Shadow mode does not change live traffic.)\n")
	}
	return b.String()
}

// FormatStoredDecision formats a completed request explain view.
func FormatStoredDecision(d *Decision, attempts []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "REQUEST: %s\n", d.RequestID)
	fmt.Fprintf(&b, "ROUTE: %s\n", d.RouteID)
	fmt.Fprintf(&b, "POLICY: %s\n\n", d.Policy)
	fmt.Fprintf(&b, "Classification:\n  %s / %s / confidence %.2f\n\n",
		d.Task.PrimaryType, d.Task.Complexity, d.Task.Confidence)
	fmt.Fprintf(&b, "Smart selection:\n  %s\n\n", d.SelectedKey())
	if len(attempts) > 0 {
		for i, a := range attempts {
			fmt.Fprintf(&b, "Attempt %d:\n  %s\n", i+1, a)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
