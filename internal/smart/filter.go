package smart

import (
	"fmt"
	"strings"
)

// ProviderState is runtime eligibility info for a candidate's provider.
type ProviderState struct {
	Enabled       bool
	CircuitOpen   bool
	HasCredential bool
}

// FilterContext carries request-level constraints for hard filtering.
type FilterContext struct {
	Task           TaskProfile
	Policy         Policy
	StrictProfiles bool
	// Provider states keyed by provider id.
	Providers map[string]ProviderState
	// Override max cost tier (0 = none).
	MaxCostTier int
}

// FilterCandidate applies hard constraints before scoring.
// Returns evaluation with Eligible=false and rejection reasons when rejected.
func FilterCandidate(c Candidate, profile ModelProfile, profileFound bool, ctx FilterContext) CandidateEvaluation {
	ev := CandidateEvaluation{
		Provider:  c.Provider,
		Model:     c.Model,
		ProfileID: profile.ID,
		Eligible:  true,
	}
	if profile.ID == "" {
		ev.ProfileID = ProfileKey(c.Provider, c.Model)
	}

	var reasons []string

	if !profileFound {
		if ctx.StrictProfiles {
			reasons = append(reasons, "unprofiled candidate (strict mode)")
		}
		// permissive: allow through with uncertainty later
	}

	// Provider state
	if ps, ok := ctx.Providers[c.Provider]; ok {
		if !ps.Enabled {
			reasons = append(reasons, "provider disabled")
		}
		if ps.CircuitOpen {
			reasons = append(reasons, "provider circuit open")
		}
		if !ps.HasCredential {
			reasons = append(reasons, "credential unavailable")
		}
	}

	hard := ctx.Task.HardRequirements

	if hard.Vision {
		ok, known := profile.Supports("vision")
		if !known || !ok {
			reasons = append(reasons, "vision unsupported or unknown")
		}
	}
	if hard.Tools {
		ok, known := profile.Supports("tools")
		if !known || !ok {
			reasons = append(reasons, "tools unsupported or unknown")
		}
	}
	if hard.ParallelTools {
		ok, known := profile.Supports("parallel_tools")
		if !known || !ok {
			reasons = append(reasons, "parallel tools unsupported or unknown")
		}
	}
	if hard.StructuredOutput {
		ok, known := profile.Supports("structured_output")
		if !known || !ok {
			reasons = append(reasons, "structured output unsupported or unknown")
		}
	}
	if hard.MinimumContextWindow > 0 && profile.Properties.ContextWindow > 0 {
		if profile.Properties.ContextWindow < hard.MinimumContextWindow {
			reasons = append(reasons, fmt.Sprintf("context window too small (%d < %d)",
				profile.Properties.ContextWindow, hard.MinimumContextWindow))
		}
	}
	if hard.MaxOutputTokens > 0 && profile.Properties.MaxOutputTokens > 0 {
		if profile.Properties.MaxOutputTokens < hard.MaxOutputTokens {
			reasons = append(reasons, fmt.Sprintf("max output tokens too small (%d < %d)",
				profile.Properties.MaxOutputTokens, hard.MaxOutputTokens))
		}
	}

	// Policy privacy constraint
	if len(ctx.Policy.AllowedPrivacy) > 0 {
		priv := profile.Properties.Privacy
		if priv == "" {
			priv = PrivacyCloud // conservative default for unknown
		}
		allowed := false
		for _, a := range ctx.Policy.AllowedPrivacy {
			if a == priv {
				allowed = true
				break
			}
		}
		if !allowed {
			reasons = append(reasons, fmt.Sprintf("privacy %q not allowed by %s policy", priv, ctx.Policy.Name))
		}
	}

	// Cost ceiling
	ceiling := ctx.Policy.MaxCostTier
	if ctx.MaxCostTier > 0 && (ceiling == 0 || ctx.MaxCostTier < ceiling) {
		ceiling = ctx.MaxCostTier
	}
	if ceiling > 0 && profile.Properties.CostTier > ceiling {
		reasons = append(reasons, fmt.Sprintf("exceeds %s policy cost ceiling (%d > %d)",
			ctx.Policy.Name, profile.Properties.CostTier, ceiling))
	}

	if len(reasons) > 0 {
		ev.Eligible = false
		ev.RejectionReasons = reasons
	}
	return ev
}

// ApplyRequireCaps marks additional hard requirements from client override.
func ApplyRequireCaps(task *TaskProfile, caps []string) {
	for _, c := range caps {
		switch strings.ToLower(strings.TrimSpace(c)) {
		case "tools", "tool_use":
			task.HardRequirements.Tools = true
			if task.Requirements[CapToolUse] < 4 {
				task.Requirements[CapToolUse] = 4
			}
		case "vision":
			task.HardRequirements.Vision = true
		case "structured_output":
			task.HardRequirements.StructuredOutput = true
		case "coding":
			if task.Requirements[CapCoding] < 4 {
				task.Requirements[CapCoding] = 4
			}
		case "reasoning":
			if task.Requirements[CapReasoning] < 4 {
				task.Requirements[CapReasoning] = 4
			}
		}
	}
}
