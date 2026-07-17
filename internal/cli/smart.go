package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/termrouter/termrouter/internal/app"
	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/smart"
)

func newSmartCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "smart", Short: "Smart Routes utilities"}
	cmd.AddCommand(
		smartClassify(),
		smartReport(),
		smartStatus(),
	)
	return cmd
}

func smartClassify() *cobra.Command {
	var prompt string
	cmd := &cobra.Command{
		Use:   "classify",
		Short: "Classify a prompt with the local heuristic classifier",
		RunE: func(cmd *cobra.Command, args []string) error {
			if prompt == "" && len(args) > 0 {
				prompt = strings.Join(args, " ")
			}
			if prompt == "" {
				return Exit(ExitInvalidConfig, fmt.Errorf("--prompt is required"))
			}
			task := smart.ClassifyPrompt(prompt)
			if flagJSON {
				return printJSON(task)
			}
			fmt.Printf("Primary type:  %s\n", task.PrimaryType)
			fmt.Printf("Complexity:    %s\n", task.Complexity)
			fmt.Printf("Confidence:    %.2f\n", task.Confidence)
			fmt.Printf("Classifier:    %s\n", task.ClassifierVersion)
			fmt.Printf("Requirements:  coding=%d reasoning=%d analysis=%d tool_use=%d\n",
				task.Requirements[smart.CapCoding],
				task.Requirements[smart.CapReasoning],
				task.Requirements[smart.CapAnalysis],
				task.Requirements[smart.CapToolUse],
			)
			return nil
		},
	}
	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt text to classify")
	return cmd
}

func smartReport() *cobra.Command {
	var route, last string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Shadow-mode aggregate report",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			dur := 7 * 24 * time.Hour
			if last != "" {
				d, err := time.ParseDuration(last)
				if err != nil {
					// support Nd form
					if strings.HasSuffix(last, "d") {
						var n int
						if _, e := fmt.Sscanf(last, "%dd", &n); e == nil {
							d = time.Duration(n) * 24 * time.Hour
							err = nil
						}
					}
				}
				if err != nil {
					return Exit(ExitInvalidConfig, fmt.Errorf("invalid --last: %w", err))
				}
				dur = d
			}
			since := time.Now().Add(-dur)
			agg, err := store.SmartShadowStats(cmd.Context(), route, since)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			if flagJSON {
				return printJSON(agg)
			}
			fmt.Println("SMART ROUTE SHADOW REPORT")
			fmt.Println()
			if route != "" {
				fmt.Printf("Route: %s\n", route)
			}
			fmt.Printf("Period: %s\n", lastOr(last, "7d"))
			fmt.Printf("Requests analyzed: %d\n\n", agg.Total)
			fmt.Println("Recommended task distribution:")
			for k, v := range agg.ByTaskType {
				pct := 0
				if agg.Total > 0 {
					pct = v * 100 / agg.Total
				}
				fmt.Printf("  %-22s %d%%\n", k, pct)
			}
			fmt.Println()
			fmt.Println("Recommended model distribution:")
			for k, v := range agg.ByRecommendation {
				pct := 0
				if agg.Total > 0 {
					pct = v * 100 / agg.Total
				}
				fmt.Printf("  %-28s %d%%\n", k, pct)
			}
			fmt.Println()
			unc := 0
			if agg.Total > 0 {
				unc = agg.UncertainCount * 100 / agg.Total
			}
			fmt.Printf("Uncertain decisions: %d%%\n", unc)
			fmt.Println("No traffic was changed for shadow-mode decisions.")
			return nil
		},
	}
	cmd.Flags().StringVar(&route, "route", "", "Filter by route id")
	cmd.Flags().StringVar(&last, "last", "7d", "Lookback window (e.g. 7d, 24h)")
	return cmd
}

func smartStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "List smart routes and their modes",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			type row struct {
				Route    string `json:"route"`
				Strategy string `json:"strategy"`
				Mode     string `json:"mode"`
				Policy   string `json:"policy"`
				Cands    int    `json:"candidates"`
			}
			var rows []row
			for name, r := range cfg.Routes {
				if r.Strategy != "smart" && r.Smart == nil && len(r.Candidates) == 0 {
					continue
				}
				mode := "shadow"
				policy := "balanced"
				if r.Smart != nil {
					if r.Smart.Mode != "" {
						mode = r.Smart.Mode
					}
					if r.Smart.Policy != "" {
						policy = r.Smart.Policy
					}
				}
				n := len(r.Candidates)
				if n == 0 {
					n = len(r.Targets)
				}
				rows = append(rows, row{Route: name, Strategy: "smart", Mode: mode, Policy: policy, Cands: n})
			}
			if flagJSON {
				return printJSON(rows)
			}
			if len(rows) == 0 {
				fmt.Println("No smart routes configured.")
				return nil
			}
			for _, r := range rows {
				fmt.Printf("%-16s mode=%-8s policy=%-10s candidates=%d\n", r.Route, r.Mode, r.Policy, r.Cands)
			}
			return nil
		},
	}
}

func newExplainCmd() *cobra.Command {
	var prompt, requestID string
	cmd := &cobra.Command{
		Use:   "explain [alias]",
		Short: "Explain smart routing for a prompt or past request",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()

			if requestID != "" {
				rec, err := store.GetSmartDecision(cmd.Context(), requestID)
				if err != nil {
					return Exit(ExitGeneral, err)
				}
				if rec == nil {
					return Exit(ExitInvalidConfig, fmt.Errorf("no smart decision for request %q", requestID))
				}
				d := smart.DecisionFromRecord(rec)
				if flagJSON {
					return printJSON(d)
				}
				fmt.Print(smart.FormatDecision(d))
				return nil
			}

			if len(args) == 0 {
				return Exit(ExitInvalidConfig, fmt.Errorf("provide alias or --request"))
			}
			if prompt == "" {
				return Exit(ExitInvalidConfig, fmt.Errorf("--prompt is required when explaining an alias"))
			}
			alias := args[0]
			a, ok := cfg.Aliases[alias]
			if !ok {
				// case-insensitive
				for name, al := range cfg.Aliases {
					if strings.EqualFold(name, alias) {
						a, ok, alias = al, true, name
						break
					}
				}
			}
			if !ok || a.Route == "" {
				return Exit(ExitInvalidConfig, fmt.Errorf("alias %q not found or has no route", alias))
			}
			route, ok := cfg.Routes[a.Route]
			if !ok {
				return Exit(ExitInvalidConfig, fmt.Errorf("route %q missing", a.Route))
			}
			if route.Strategy != "smart" && route.Smart == nil && len(route.Candidates) == 0 {
				return Exit(ExitInvalidConfig, fmt.Errorf("route %q is not a smart route", a.Route))
			}

			eng := smart.GatewayEngine(cfg, store, nil)
			req := &normalization.NormalizedRequest{
				ID:             "explain",
				RequestedModel: alias,
				ResolvedAlias:  alias,
				Messages: []normalization.Message{{
					Role:    normalization.RoleUser,
					Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: prompt}},
				}},
			}
			rc := smart.RouteFromConfig(a.Route, route)
			// Force evaluation even if mode is off for explain
			if rc.Mode == smart.ModeOff {
				rc.Mode = smart.ModeShadow
			}
			d, err := eng.Select(req, rc, smart.Override{})
			if err != nil && d == nil {
				return Exit(ExitGeneral, err)
			}
			if flagJSON {
				return printJSON(d)
			}
			fmt.Print(smart.FormatDecision(d))
			return nil
		},
	}
	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt text to classify and route")
	cmd.Flags().StringVar(&requestID, "request", "", "Past request id")
	return cmd
}

func lastOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
