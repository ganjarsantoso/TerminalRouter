package optimization

import (
	"context"
	"math/rand"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/lui"
	"github.com/termrouter/termrouter/internal/normalization"
)

// Engine is the TermRouter token-optimization engine. It is safe to use with a
// nil Store (optimization still runs, records are simply not persisted).
type Engine struct {
	cfg         config.OptimizationConfig
	estimators  *estimatorRegistry
	compressors *Registry
	store       Store
	log         Logger
	optimizers  []Optimizer
}

// Store persists optimization decision records. It is satisfied by *storage.Store.
type Store interface {
	InsertOptimizationRecord(ctx context.Context, r Record) error
}

// Logger is the minimal logging surface used by the engine.
type Logger interface {
	Warn(msg string, args ...any)
	Info(msg string, args ...any)
	Debug(msg string, args ...any)
}

// NewEngine builds an optimization engine from configuration.
func NewEngine(cfg config.OptimizationConfig, store Store, log Logger) *Engine {
	e := &Engine{
		cfg:         cfg,
		estimators:  newEstimatorRegistry(cfg.TokenEstimation, nil),
		compressors: BuildRegistry(cfg.Compressors),
		store:       store,
		log:         log,
	}
	e.optimizers = []Optimizer{
		ansiOptimizer{},
		jsonCompactOptimizer{},
		dedupeOptimizer{},
		logCompactOptimizer{},
		conversationOptimizer{est: e.estimators},
		outputBudgetOptimizer{cfg: cfg.Output},
	}
	return e
}

// Enabled reports whether the engine is active.
func (e *Engine) Enabled() bool { return e.cfg.Enabled }

// Compressors exposes the plug-in registry for CLI test/inspect commands.
func (e *Engine) Compressors() *Registry { return e.compressors }

// SetCompressor injects a compressor by name for testing. It replaces any
// existing entry with the same name.
func (e *Engine) SetCompressor(name string, c Compressor) {
	if e.compressors == nil {
		e.compressors = &Registry{compressors: map[string]Compressor{}}
	}
	e.compressors.compressors[name] = c
}

// ModeInfo describes the resolved mode decision for a request.
type ModeInfo struct {
	Requested config.OptimizationMode
	Applied   config.OptimizationMode
	Bypassed  bool
	Reason    string
}

// ResolveMode resolves the policy for a request without transforming it.
func (e *Engine) ResolveMode(clientPreference, keyMaxMode string) (ModeInfo, error) {
	req, app, err := ResolveMode(e.cfg, clientPreference, keyMaxMode)
	if err != nil {
		return ModeInfo{}, err
	}
	mi := ModeInfo{Requested: req, Applied: app}
	if app == config.OptModeOff {
		mi.Bypassed = true
		mi.Reason = "optimization disabled by policy"
	}
	return mi, nil
}

// Process optimizes req in place and returns the result with accounting. The
// input request may be mutated; callers should treat the returned req as
// authoritative. On any optimizer failure the engine is fail-closed: it records
// a warning and continues with the original content. The returned Record is the
// privacy-conscious decision record; the caller persists it (after optionally
// finalizing with provider actuals).
func (e *Engine) Process(ctx context.Context, req *normalization.NormalizedRequest, oc OptimizationContext) (*normalization.NormalizedRequest, *OptimizationResult, *Record, error) {
	res := &OptimizationResult{
		Request:       req,
		ModeRequested: config.OptModeSafe,
		ModeApplied:   config.OptModeSafe,
		LossClass:     Lossless,
		Reversible:    true,
	}

	if !e.cfg.Enabled {
		return req, res, nil, nil
	}

	mi, err := e.ResolveMode(oc.ClientPreference, oc.KeyMaxMode)
	if err != nil {
		return req, res, nil, err
	}
	res.ModeRequested = mi.Requested
	res.ModeApplied = mi.Applied
	res.Bypassed = mi.Bypassed
	res.BypassReason = mi.Reason

	protected := BuildProtected(req)
	oc.Protected = protected
	oc.Flags = e.modeFlags(mi.Applied)
	oc.ConversationEnabled = e.cfg.Conversation.Enabled && mi.Applied != config.OptModeOff && mi.Applied != config.OptModeSafe
	oc.ConversationTriggerTokens = e.cfg.Conversation.TriggerTokens
	oc.ConversationRecentTurns = e.cfg.Conversation.RecentTurnsFull
	oc.ConversationTargetTokens = e.cfg.Conversation.TargetTokens
	oc.OutputBudgetMode = e.cfg.Output.Mode
	if oc.OutputBudgetMode == "" {
		oc.OutputBudgetMode = "adaptive"
	}

	before := e.estimators.Estimate(req, oc.ProviderID, oc.ModelID)

	if mi.Applied == config.OptModeOff {
		// No transformation; accounting may still run (REV5: off still measures).
		res.InputTokensBefore = before.Total
		res.InputTokensEstimated = before.Total
		return req, res, nil, nil
	}

	// Optional semantic compression (disabled by default; gated by quality).
	if e.cfg.SemanticCompression.Enabled && e.compressors != nil {
		if c, ok := e.compressors.Get(e.cfg.SemanticCompression.Adapter); ok {
			e.applySemanticCompression(ctx, req, oc, res, c)
		}
	}

	// Transactional optimizer pipeline: each stage operates on a clone; on
	// failure the clone is discarded and the authoritative request is preserved.
	authoritative := req
	for _, opt := range e.optimizers {
		if !opt.Supports(oc, mi.Applied) {
			continue
		}
		// Snapshot protected-content fingerprint before the stage.
		preFP := ProtectedFingerprint(authoritative)
		// Clone the authoritative request for this stage.
		clone := CloneRequest(authoritative)
		if clone == nil {
			if e.log != nil {
				e.log.Warn("optimization stage skipped: clone failed",
					"optimizer", opt.Name())
			}
			res.Warnings = append(res.Warnings, "optimizer "+opt.Name()+" bypassed: clone failed")
			continue
		}
		// Record current action/warning counts so we can detect new entries.
		actionsBefore := len(res.Actions)
		warningsBefore := len(res.Warnings)
		stageRes := &OptimizationResult{
			Actions:  res.Actions,
			Warnings: res.Warnings,
		}
		if err := opt.Optimize(ctx, clone, oc, mi.Applied, stageRes); err != nil {
			// Optimizer failed: discard clone, truncate any partial actions.
			res.Actions = res.Actions[:actionsBefore]
			res.Warnings = res.Warnings[:warningsBefore]
			if e.log != nil {
				e.log.Warn("optimization stage failed; continuing with original content",
					"optimizer", opt.Name(), "error", err.Error())
			}
			res.Warnings = append(res.Warnings, "optimizer "+opt.Name()+" bypassed: "+err.Error())
			continue
		}
		// Validate that protected content was not mutated.
		postFP := ProtectedFingerprint(clone)
		if preFP != postFP {
			// Integrity violation: discard clone, truncate any partial actions.
			res.Actions = res.Actions[:actionsBefore]
			res.Warnings = res.Warnings[:warningsBefore]
			if e.log != nil {
				e.log.Warn("optimization stage rejected: protected content mutated",
					"optimizer", opt.Name())
			}
			res.Warnings = append(res.Warnings, "optimizer "+opt.Name()+" bypassed: protected content integrity violation")
			continue
		}
		// Stage succeeded and integrity check passed: commit the clone.
		authoritative = clone
		// Ensure res.Actions and res.Warnings reflect any reallocations from append.
		res.Actions = stageRes.Actions
		res.Warnings = stageRes.Warnings
	}
	*req = *authoritative

	// Cache-prefix stabilization: canonicalize stable request regions (tool
	// ordering) and record an estimated cache opportunity. This does NOT
	// send native cache-control headers to providers; actual cache-hit
	// savings require provider-native cache support which is not yet wired.
	res.ExpectedCachedTokens = e.stabilizePrefix(req, oc)

	// Build LUI envelope for introspection / inspection (not injected into the
	// provider request by default; native prompt remains the wire format).
	env := lui.BuildEnvelope(req, lui.BuildContext{
		RequestID:       oc.RequestID,
		InboundProtocol: oc.InboundProtocol,
		TaskType:        oc.TaskType,
		Complexity:      oc.Complexity,
	})
	if verr := lui.Validate(env); verr != nil {
		if e.log != nil {
			e.log.Warn("lui envelope validation skipped", "error", verr.Error())
		}
	} else {
		res.LUI = env
		res.LUIVersion = env.Version
		res.LUIRenderer = "native_prompt"
		res.Actions = append(res.Actions, Action{
			Kind:        ActionLUIRendered,
			Description: "Built LUI v" + env.Version + " envelope (introspection)",
			Reversible:  true,
			LossClass:   Lossless,
		})
	}

	after := e.estimators.Estimate(req, oc.ProviderID, oc.ModelID)
	res.InputTokensBefore = before.Total
	res.InputTokensEstimated = after.Total
	res.RemovedTokensEstimated = before.Total - after.Total
	if res.RemovedTokensEstimated < 0 {
		res.RemovedTokensEstimated = 0
	}
	e.accumulateLoss(res)
	e.computeSavings(res, oc, before, after)

	// Shadow evaluation (metadata-only; never duplicates provider calls).
	if e.cfg.Evaluation.ShadowMode && e.sampleShadow() {
		e.logShadow(ctx, oc, res)
	}

	rec := RecordFromResult(oc, res)
	return req, res, &rec, nil
}

// FinalizeAndPersist fills provider-reported actuals into the record and
// persists it. Negative actuals are ignored (e.g. streaming with no post-hoc
// measurement), preserving the preflight estimate. Cache actuals are stored
// in separate fields from the estimated cache opportunity.
func (e *Engine) FinalizeAndPersist(ctx context.Context, rec *Record, inputTokens, outputTokens, cachedTokens int) {
	if rec == nil {
		return
	}
	if inputTokens >= 0 {
		rec.ProviderInputTokensActual = inputTokens
	}
	if outputTokens >= 0 {
		rec.ProviderOutputTokensActual = outputTokens
	}
	if cachedTokens >= 0 {
		rec.CacheReadTokensActual = cachedTokens
		if rec.CacheStatus == "" || rec.CacheStatus == "cache_opportunity_estimated" {
			rec.CacheStatus = "cache_reported_by_provider"
		}
	}
	if e.store == nil {
		return
	}
	if err := e.store.InsertOptimizationRecord(ctx, *rec); err != nil && e.log != nil {
		e.log.Warn("failed to persist optimization record", "error", err.Error())
	}
}

// modeFlags returns the enabled deterministic flags for a resolved mode.
func (e *Engine) modeFlags(mode config.OptimizationMode) []string {
	d := e.cfg.Deterministic
	var flags []string
	if d.StripANSI {
		flags = append(flags, "strip_ansi")
	}
	if d.CompactJSON {
		flags = append(flags, "compact_json")
	}
	if mode != config.OptModeSafe && mode != config.OptModeOff && d.Deduplicate {
		flags = append(flags, "deduplicate")
	}
	if mode != config.OptModeSafe && mode != config.OptModeOff && d.CompactLogs {
		flags = append(flags, "compact_logs")
	}
	return flags
}

// stabilizePrefix canonicalizes stable request regions (tool ordering) and
// estimates the cacheable prefix token count. This is a cache-prefix
// stabilization and estimated cache opportunity — it does NOT send native
// cache-control headers. Provider-native cache support is not yet wired;
// ExpectedCachedTokens represents estimated opportunity, not actual cache
// hits. The returned estimate is stored in CacheOpportunityTokensEst on the
// Record, separate from CacheReadTokensActual / CacheWriteTokensActual which
// are populated from provider response headers when available.
func (e *Engine) stabilizePrefix(req *normalization.NormalizedRequest, oc OptimizationContext) int {
	if !e.cfg.PromptCache.Enabled || len(req.Tools) < 2 {
		return 0
	}
	// Canonical stable ordering of tool definitions by name (order is
	// semantically irrelevant for tool choice).
	stableSortTools(req.Tools)
	res := e.estimators.Estimate(req, oc.ProviderID, oc.ModelID)
	prefixTokens := res.System + res.ToolDefinitions
	if prefixTokens < e.cfg.PromptCache.MinimumPrefixTokens {
		return 0
	}
	return prefixTokens
}

func (e *Engine) accumulateLoss(res *OptimizationResult) {
	order := map[LossClass]int{Lossless: 0, Selective: 1, Lossy: 2}
	worst := Lossless
	for _, a := range res.Actions {
		if order[a.LossClass] > order[worst] {
			worst = a.LossClass
		}
		if !a.Reversible {
			res.Reversible = false
		}
	}
	res.LossClass = worst
}

func (e *Engine) computeSavings(res *OptimizationResult, oc OptimizationContext, before, after TokenBreakdown) {
	removed := res.RemovedTokensEstimated
	if removed <= 0 {
		return
	}
	if oc.Pricing != nil {
		perToken := oc.Pricing.InputUSDPerMillion / 1_000_000
		res.EstimatedGrossSavingUSD = float64(removed) * perToken
	}
	res.EstimatedNetSavingUSD = res.EstimatedGrossSavingUSD - res.EstimatedOptimizerCost
	if res.EstimatedNetSavingUSD < 0 {
		res.EstimatedNetSavingUSD = 0
	}
}

func (e *Engine) sampleShadow() bool {
	if e.cfg.Evaluation.SampleRate <= 0 {
		return false
	}
	if e.cfg.Evaluation.SampleRate >= 1 {
		return true
	}
	return randFloat() < e.cfg.Evaluation.SampleRate
}

func (e *Engine) logShadow(ctx context.Context, oc OptimizationContext, res *OptimizationResult) {
	if e.log == nil {
		return
	}
	e.log.Info("optimization shadow sample",
		"request_id", oc.RequestID,
		"mode", string(res.ModeApplied),
		"tokens_before", res.InputTokensBefore,
		"tokens_after", res.InputTokensEstimated,
		"estimated_saving_usd", res.EstimatedNetSavingUSD,
		"loss_class", string(res.LossClass),
	)
}

func (e *Engine) applySemanticCompression(ctx context.Context, req *normalization.NormalizedRequest, oc OptimizationContext, res *OptimizationResult, c Compressor) {
	// Shadow-only evaluation: the request is NEVER mutated. Savings are recorded
	// as hypothetical and excluded from realized optimization totals.
	if len(req.Messages) < 2 {
		return
	}

	// Find the last user message index — it is the current user turn and is protected.
	lastUserIdx := -1
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == normalization.RoleUser {
			lastUserIdx = i
			break
		}
	}

	// Collect eligible text from OLD conversation turns (before the current user turn)
	// and from non-current tool results.
	var eligibleTexts []string
	totalEligible := 0
	for i, m := range req.Messages {
		if i == lastUserIdx {
			continue
		}
		if m.Role != normalization.RoleAssistant && m.Role != normalization.RoleTool {
			continue
		}
		for _, blk := range m.Content {
			if blk.Type == normalization.ContentText && len(blk.Text) > 0 {
				eligibleTexts = append(eligibleTexts, blk.Text)
				totalEligible += len(blk.Text)
			}
		}
	}

	if totalEligible < e.cfg.SemanticCompression.MinimumInputTokens {
		return
	}

	combined := joinTexts(eligibleTexts)
	if combined == "" {
		return
	}

	resp, err := c.Compress(ctx, CompressionRequest{
		RequestID:    oc.RequestID,
		ContentClass: "old_conversation",
		Text:         combined,
		TargetTokens: totalEligible / 2,
	})
	if err != nil {
		fm := e.cfg.SemanticCompression.FailureMode
		if fm == "reject" {
			res.Warnings = append(res.Warnings, "semantic compression rejected: "+err.Error())
			return
		}
		// bypass: record warning and continue
		res.Warnings = append(res.Warnings, "semantic compression bypassed: "+err.Error())
		return
	}

	estOut := e.estimators.CountText(resp.Text, oc.ProviderID, oc.ModelID)
	saved := e.estimators.CountText(combined, oc.ProviderID, oc.ModelID) - estOut
	if saved < e.cfg.SemanticCompression.MinimumExpectedSavingsTokens {
		return
	}

	res.ShadowEvaluated = true
	res.HypotheticalSavingsTokens = saved
	res.Actions = append(res.Actions, Action{
		Kind:                 ActionSemanticCompressionShadow,
		Description:          "Shadow semantic compression evaluation (" + c.Name() + ") — hypothetical only",
		EstimatedTokensSaved: saved,
		Reversible:           false,
		LossClass:            Lossy,
	})
}

// joinTexts concatenates texts with a newline separator.
func joinTexts(texts []string) string {
	var b []byte
	for i, t := range texts {
		if i > 0 {
			b = append(b, '\n')
		}
		b = append(b, []byte(t)...)
	}
	return string(b)
}

func stableSortTools(tools []normalization.Tool) {
	for i := 1; i < len(tools); i++ {
		for j := i; j > 0 && tools[j].Name < tools[j-1].Name; j-- {
			tools[j], tools[j-1] = tools[j-1], tools[j]
		}
	}
}

func randFloat() float64 { return rand.Float64() }
