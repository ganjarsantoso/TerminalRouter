package optimization

import (
	"context"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/normalization"
)

// outputBudgetOptimizer applies the adaptive output-token planner. It never
// raises a client-specified maximum (that is an explicit hard limit) and only
// applies an adaptive recommendation when the client did not pin a value.
type outputBudgetOptimizer struct {
	cfg config.OutputBudgetConfig
}

func (outputBudgetOptimizer) Name() string    { return "output_budget" }
func (outputBudgetOptimizer) Version() string { return "1.0" }
func (outputBudgetOptimizer) Supports(oc OptimizationContext, mode config.OptimizationMode) bool {
	return mode != config.OptModeOff && oc.OutputBudgetMode != "off"
}

func (o outputBudgetOptimizer) Optimize(ctx context.Context, req *normalization.NormalizedRequest, oc OptimizationContext, mode config.OptimizationMode, res *OptimizationResult) error {
	if req.MaxOutputTokens != nil && *req.MaxOutputTokens > 0 {
		// Explicit client requirement: respect exactly, record provenance.
		res.Actions = append(res.Actions, Action{
			Kind:        ActionOutputBudget,
			Description: "Output budget set by explicit client request",
			Reversible:  true,
			LossClass:   Lossless,
		})
		return nil
	}
	if o.cfg.Mode == "off" {
		return nil
	}

	// Adaptive output reduction is not allowed in safe mode.
	if mode == config.OptModeSafe {
		return nil
	}

	rec := o.recommend(oc)
	if rec <= 0 {
		return nil
	}
	req.MaxOutputTokens = &rec
	res.Actions = append(res.Actions, Action{
		Kind:                 ActionOutputBudget,
		Description:          "Adaptive output budget applied (" + oc.Complexity + " task)",
		EstimatedTokensSaved: 0,
		Reversible:           true,
		LossClass:            Lossless,
	})
	return nil
}

// recommend returns the adaptive maximum output tokens for the task profile.
func (o outputBudgetOptimizer) recommend(oc OptimizationContext) int {
	switch oc.Complexity {
	case "simple":
		return orDefault(o.cfg.SimpleMaxTokens, 512)
	case "complex":
		return orDefault(o.cfg.ComplexMaxTokens, 4096)
	case "medium":
		return orDefault(o.cfg.MediumMaxTokens, 2048)
	default:
		return orDefault(o.cfg.DefaultMaxTokens, 2048)
	}
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
