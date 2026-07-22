package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/termrouter/termrouter/internal/app"
	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/lui"
	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/optimization"
	"github.com/termrouter/termrouter/internal/storage"
)

func newOptimizeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "optimize",
		Short: "Token optimization analysis, dry-run, and reporting",
	}
	cmd.AddCommand(
		optimizeAnalyzeCmd(),
		optimizeDryRunCmd(),
		optimizeCompareCmd(),
		optimizeStatusCmd(),
		optimizeReportCmd(),
		optimizePluginsCmd(),
	)
	return cmd
}

func newLUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lui",
		Short: "LUI v0.1 semantic packet validation, rendering, and inspection",
	}
	cmd.AddCommand(
		luiValidateCmd(),
		luiRenderCmd(),
		luiInspectCmd(),
	)
	return cmd
}

func loadNormalized(path string) (*normalization.NormalizedRequest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var req normalization.NormalizedRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("parse request %q: %w", path, err)
	}
	if req.ID == "" {
		req.ID = "cli-optimize"
	}
	return &req, nil
}

func buildEngineForCLI() (*config.Config, *storage.Store, *optimization.Engine, error) {
	home, err := homeDir()
	if err != nil {
		return nil, nil, nil, err
	}
	cfg, _, store, _, err := app.LoadRuntime(home)
	if err != nil {
		return nil, nil, nil, err
	}
	engine := optimization.NewEngine(cfg.Optimization, store, nil)
	return cfg, store, engine, nil
}

func optimizationContextForCLI(req *normalization.NormalizedRequest, model, pref string) optimization.OptimizationContext {
	prov, m := "", ""
	if model != "" {
		if p, ok := splitModel(model); ok {
			prov, m = p, m2(model, p)
		} else {
			m = model
		}
	}
	return optimization.OptimizationContext{
		RequestID:        req.ID,
		ProviderID:       prov,
		ModelID:          m,
		ClientPreference: pref,
	}
}

func splitModel(model string) (string, bool) {
	for i := 0; i < len(model); i++ {
		if model[i] == '/' {
			return model[:i], true
		}
	}
	return "", false
}

func m2(model, p string) string { return model[len(p)+1:] }

func optimizeAnalyzeCmd() *cobra.Command {
	var model, preference string
	cmd := &cobra.Command{
		Use:   "analyze --file request.json --model provider/model",
		Short: "Analyze a request: token breakdown, protected content, eligible optimizers, estimated savings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := loadNormalized(mustFlag(cmd, "file"))
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			_, _, engine, err := buildEngineForCLI()
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			if !engine.Enabled() {
				return Exit(ExitInvalidConfig, fmt.Errorf("optimization is not enabled in configuration"))
			}
			oc := optimizationContextForCLI(req, model, preference)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			optReq, res, _, err := engine.Process(ctx, req, oc)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			out := map[string]any{
				"mode_requested":           string(res.ModeRequested),
				"mode_applied":             string(res.ModeApplied),
				"input_tokens_before":      res.InputTokensBefore,
				"input_tokens_estimated":   res.InputTokensEstimated,
				"removed_tokens_estimated": res.RemovedTokensEstimated,
				"expected_cached_tokens":   res.ExpectedCachedTokens,
				"loss_class":               string(res.LossClass),
				"reversible":               res.Reversible,
				"bypassed":                 res.Bypassed,
				"bypass_reason":            res.BypassReason,
				"estimated_net_saving_usd": res.EstimatedNetSavingUSD,
				"actions":                  res.Actions,
				"warnings":                 res.Warnings,
				"lui_version":              res.LUIVersion,
			}
			_ = optReq
			return printOut(fmt.Sprintf("Optimization analysis (%s → %s)\n  tokens before: %d\n  tokens after:  %d\n  removed:       %d\n  cached est:    %d\n  loss class:    %s\n  net saving:    $%.6f",
				res.ModeRequested, res.ModeApplied, res.InputTokensBefore, res.InputTokensEstimated,
				res.RemovedTokensEstimated, res.ExpectedCachedTokens, res.LossClass, res.EstimatedNetSavingUSD), out)
		},
	}
	cmd.Flags().StringVar(&model, "model", "", "Provider/model for tokenizer and pricing (e.g. openai/gpt-4o)")
	cmd.Flags().StringVar(&preference, "mode", "", "Optimization mode preference (off|safe|balanced|aggressive)")
	cmd.Flags().String("file", "", "Path to a NormalizedRequest JSON file")
	cmd.MarkFlagRequired("file")
	return cmd
}

func optimizeDryRunCmd() *cobra.Command {
	var model, preference string
	cmd := &cobra.Command{
		Use:   "dry-run --file request.json --mode safe",
		Short: "Show the optimized request that would be sent (never calls a provider)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := loadNormalized(mustFlag(cmd, "file"))
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			_, _, engine, err := buildEngineForCLI()
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			oc := optimizationContextForCLI(req, model, preference)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			optReq, res, _, err := engine.Process(ctx, req, oc)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			b, _ := json.MarshalIndent(optReq, "", "  ")
			return printOut(fmt.Sprintf("Dry-run (%s): %d → %d tokens, loss=%s\n--- optimized request ---\n%s",
				res.ModeApplied, res.InputTokensBefore, res.InputTokensEstimated, res.LossClass, string(b)), map[string]any{
				"mode_applied": res.ModeApplied,
				"request":      optReq,
			})
		},
	}
	cmd.Flags().StringVar(&model, "model", "", "Provider/model for tokenizer and pricing")
	cmd.Flags().StringVar(&preference, "mode", "safe", "Optimization mode (off|safe|balanced|aggressive)")
	cmd.Flags().String("file", "", "Path to a NormalizedRequest JSON file")
	cmd.MarkFlagRequired("file")
	return cmd
}

func optimizeCompareCmd() *cobra.Command {
	var model string
	var modes string
	cmd := &cobra.Command{
		Use:   "compare --file request.json --modes off,safe,balanced",
		Short: "Compare estimated savings across multiple optimization modes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := loadNormalized(mustFlag(cmd, "file"))
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			_, _, engine, err := buildEngineForCLI()
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			mlist := splitComma(modes)
			type row struct {
				Mode    string  `json:"mode"`
				Before  int     `json:"before"`
				After   int     `json:"after"`
				Removed int     `json:"removed"`
				Loss    string  `json:"loss_class"`
				NetUSD  float64 `json:"net_saving_usd"`
			}
			var rows []row
			for _, mode := range mlist {
				r := cloneNormalized(req)
				oc := optimizationContextForCLI(r, model, mode)
				ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
				_, res, _, e := engine.Process(ctx2, r, oc)
				cancel2()
				if e != nil {
					return Exit(ExitGeneral, e)
				}
				rows = append(rows, row{string(res.ModeApplied), res.InputTokensBefore, res.InputTokensEstimated, res.RemovedTokensEstimated, string(res.LossClass), res.EstimatedNetSavingUSD})
			}
			var buf strings.Builder
			buf.WriteString(fmt.Sprintf("Compare modes for %s:\n  %-12s %8s %8s %8s %-10s %s\n", model, "mode", "before", "after", "removed", "loss", "net$"))
			for _, r := range rows {
				buf.WriteString(fmt.Sprintf("  %-12s %8d %8d %8d %-10s $%.6f\n", r.Mode, r.Before, r.After, r.Removed, r.Loss, r.NetUSD))
			}
			return printOut(buf.String(), rows)
		},
	}
	cmd.Flags().StringVar(&model, "model", "", "Provider/model for tokenizer and pricing")
	cmd.Flags().StringVar(&modes, "modes", "off,safe,balanced,aggressive", "Comma-separated modes to compare")
	cmd.Flags().String("file", "", "Path to a NormalizedRequest JSON file")
	cmd.MarkFlagRequired("file")
	return cmd
}

func optimizeStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show optimization configuration status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, engine, err := buildEngineForCLI()
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			o := cfg.Optimization
			out := map[string]any{
				"enabled":            engine.Enabled(),
				"default_mode":       string(o.DefaultMode),
				"aggressive_allowed": o.AggressiveAllowed,
				"prompt_cache":       o.PromptCache.Enabled,
				"compressors":        engine.Compressors().List(),
			}
			return printOut(fmt.Sprintf("Optimization: enabled=%v default_mode=%s aggressive_allowed=%v",
				engine.Enabled(), o.DefaultMode, o.AggressiveAllowed), out)
		},
	}
}

func optimizeReportCmd() *cobra.Command {
	var last string
	cmd := &cobra.Command{
		Use:   "report --last 7d",
		Short: "Report optimization savings since a duration (e.g. 7d, 24h)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, _, err := buildEngineForCLI()
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			since, err := parseDurationLast(last)
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			home, _ := homeDir()
			_, _, store, _, err := app.LoadRuntime(home)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			defer store.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			sum, err := store.OptimizationSummarySince(ctx, since)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			_ = cfg
			return printOut(fmt.Sprintf("Optimization report (since %s)\n  requests:      %d\n  tokens before: %d\n  tokens after:  %d\n  cached:        %d\n  net saving:    $%.6f\n  bypasses:      %d",
				last, sum.RequestsOptimized, sum.TokensBefore, sum.TokensAfter, sum.CachedTokens, sum.NetSavingUSD, sum.BypassCount), sum)
		},
	}
	cmd.Flags().StringVar(&last, "last", "7d", "Duration window (e.g. 7d, 24h)")
	return cmd
}

func optimizePluginsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "plugins", Short: "Manage external compressor plug-ins"}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List configured compressor plug-ins",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				_, _, engine, err := buildEngineForCLI()
				if err != nil {
					return Exit(ExitGeneral, err)
				}
				names := engine.Compressors().List()
				return printOut(fmt.Sprintf("Compressor plug-ins: %v", names), map[string]any{"plugins": names})
			},
		},
		&cobra.Command{
			Use:   "test [name]",
			Short: "Health-check a compressor plug-in",
			Args:  cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				_, _, engine, err := buildEngineForCLI()
				if err != nil {
					return Exit(ExitGeneral, err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				ok := engine.Compressors().Healthy(ctx, firstOrEmpty(args))
				status := "healthy"
				if !ok {
					status = "unhealthy-or-disabled"
				}
				return printOut(fmt.Sprintf("Plug-in %q: %s", firstOrEmpty(args), status), map[string]any{"healthy": ok})
			},
		},
	)
	return cmd
}

// --- LUI CLI ---

func luiValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate packet.json",
		Short: "Validate a LUI v0.1 envelope",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			var env lui.Envelope
			if err := json.Unmarshal(data, &env); err != nil {
				return Exit(ExitInvalidConfig, fmt.Errorf("parse envelope: %w", err))
			}
			if err := lui.Validate(&env); err != nil {
				return Exit(ExitInvalidConfig, fmt.Errorf("invalid LUI envelope: %w", err))
			}
			return printOut("LUI envelope is valid (v"+env.Version+")", map[string]any{"valid": true, "version": env.Version})
		},
	}
}

func luiRenderCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "render packet.json --format compact-json",
		Short: "Render a LUI envelope to a target format",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			var env lui.Envelope
			if err := json.Unmarshal(data, &env); err != nil {
				return Exit(ExitInvalidConfig, fmt.Errorf("parse envelope: %w", err))
			}
			if err := lui.Validate(&env); err != nil {
				return Exit(ExitInvalidConfig, fmt.Errorf("invalid LUI envelope: %w", err))
			}
			renderer := formatToRenderer(format)
			out, name, err := lui.Render(&env, renderer)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			fmt.Println(out)
			_ = name
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "compact-json", "Rendering: compact-json | human | tagged-text | native-prompt")
	return cmd
}

func luiInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect --request req_id",
		Short: "Inspect a stored optimization record (LUI metadata, no raw prompt)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reqID := mustFlag(cmd, "request")
			home, err := homeDir()
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			_, _, store, _, err := app.LoadRuntime(home)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			defer store.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			rec, err := store.GetOptimizationRecord(ctx, reqID)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			if rec == nil {
				return Exit(ExitNotFound, fmt.Errorf("no optimization record for %q", reqID))
			}
			cacheInfo := ""
			if rec.CacheStatus != "" {
				cacheInfo = fmt.Sprintf("  cache status: %s  opportunity est: %d  read actual: %d  write actual: %d  source: %s\n",
					rec.CacheStatus, rec.CacheOpportunityTokensEst, rec.CacheReadTokensActual, rec.CacheWriteTokensActual, rec.CacheSource)
			}
			return printOut(fmt.Sprintf("Optimization record %s\n  mode: %s→%s\n  tokens: %d→%d\n  %snet saving: $%.6f\n  loss: %s",
				rec.RequestID, rec.ModeRequested, rec.ModeApplied, rec.InputTokensBefore, rec.InputTokensAfterEstimated,
				cacheInfo, rec.NetSavingUSD, rec.LossClass), rec)
		},
	}
	cmd.Flags().String("request", "", "Request ID to inspect")
	cmd.MarkFlagRequired("request")
	return cmd
}

func formatToRenderer(format string) string {
	switch format {
	case "human":
		return "human"
	case "tagged-text", "tagged_text":
		return "tagged_text"
	case "native-prompt", "native_prompt":
		return "native_prompt"
	default:
		return "compact_json"
	}
}

func mustFlag(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func firstOrEmpty(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

func parseDurationLast(s string) (time.Time, error) {
	d, err := ParseLookback(s)
	if err != nil {
		return time.Time{}, err
	}
	return time.Now().UTC().Add(-d), nil
}

func cloneNormalized(req *normalization.NormalizedRequest) *normalization.NormalizedRequest {
	b, err := json.Marshal(req)
	if err != nil {
		return req
	}
	var out normalization.NormalizedRequest
	if err := json.Unmarshal(b, &out); err != nil {
		return req
	}
	return &out
}
