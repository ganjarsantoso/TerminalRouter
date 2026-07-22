package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/termrouter/termrouter/internal/app"
	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/execution"
	"github.com/termrouter/termrouter/internal/provider"
	panthropic "github.com/termrouter/termrouter/internal/provider/anthropic"
	"github.com/termrouter/termrouter/internal/provider/compatible"
	"github.com/termrouter/termrouter/internal/smart"
)

func newModelCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "model", Short: "Manage model capability profiles"}
	cmd.AddCommand(newModelProfileCmd())
	return cmd
}

func newModelProfileCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "profile", Short: "Inspect and override model profiles"}
	cmd.AddCommand(
		modelProfileList(),
		modelProfileShow(),
		modelProfileSet(),
		modelProfileReset(),
		modelProfileValidate(),
		modelAssessCmd(),
		modelExternalCmd(),
	)
	return cmd
}

func modelAssessCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "assess", Short: "Run or manage model self-assessments"}
	cmd.AddCommand(
		modelAssessRun(),
		modelAssessShow(),
		modelAssessApply(),
		modelAssessCancel(),
		modelAssessHistory(),
	)
	return cmd
}

func modelAssessRun() *cobra.Command {
	var depth string
	var categories string
	cmd := &cobra.Command{
		Use:   "run [provider/model]",
		Short: "Run a self-assessment for a model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, creds, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()

			id := args[0]
			d := smart.AssessmentDepth(depth)
			if d == "" {
				d = smart.DepthStandard
			}

			catList := []string{}
			if categories != "" {
				for _, c := range strings.Split(categories, ",") {
					catList = append(catList, strings.TrimSpace(c))
				}
			}

			// Real preflight checks: verify provider config, credential
			// resolution, adapter availability, and model connectivity rather
			// than claiming success unconditionally.
			reg := provider.NewRegistry()
			reg.Register(compatible.NewOpenAI())
			reg.Register(compatible.NewCompatible())
			reg.Register(panthropic.New())
			credCheck := func(providerID string) bool {
				p, ok := cfg.Providers[providerID]
				if !ok {
					return false
				}
				if p.CredentialRef == "" || p.CredentialRef == "none://" {
					return false
				}
				secret, err := creds.Resolve(p.CredentialRef)
				return err == nil && secret != ""
			}
			providerCheck := func(providerID, modelID string) (bool, bool, bool) {
				p, ok := cfg.Providers[providerID]
				if !ok {
					return false, false, false
				}
				adapter, ok := reg.Get(p.Type)
				if !ok {
					return false, false, false
				}
				secret, err := creds.Resolve(p.CredentialRef)
				if err != nil || secret == "" {
					return false, false, false
				}
				listCtx, listCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer listCancel()
				models, err := adapter.ListModels(listCtx, p, secret)
				if err != nil {
					return false, false, false
				}
				for _, m := range models {
					if m.ID == modelID {
						return true, true, p.Type != "anthropic"
					}
				}
				return false, false, false
			}
			ps := smart.NewProfileStore(smart.ProfilesFromConfig(cfg), true)
			coord := execution.New(reg, creds, store, nil)
			svc := smart.NewModelAssessmentService(store, credCheck, providerCheck, ps, coord, cfg)

			// Surface a pricing warning up front if cost cannot be estimated
			// from configured pricing (no fabricated value is returned).
			if est := svc.Estimate(mustSplitProvider(id), mustSplitModel(id), d, catList); !est.CostKnown {
				for _, w := range est.Warnings {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
				}
			}

			rec, err := svc.Start(mustSplitProvider(id), mustSplitModel(id), d, catList, nil)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("Assessment %s started (depth=%s)", rec.AssessmentID, depth), rec)
		},
	}
	cmd.Flags().StringVar(&depth, "depth", "standard", "Assessment depth: quick, standard, comprehensive")
	cmd.Flags().StringVar(&categories, "categories", "", "Comma-separated category list")
	return cmd
}

func modelAssessShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show [assessment-id]",
		Short: "Show a model assessment result",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			aid := args[0]
			data, err := store.GetAssessment(nil, aid)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			if data == nil {
				return Exit(ExitGeneral, fmt.Errorf("assessment %s not found", aid))
			}
			rec := smart.FromStorage(data)
			if flagJSON {
				return printJSON(rec)
			}
			fmt.Printf("Assessment: %s\nStatus: %s\nDepth: %s\nBenchmark: %s\nConfidence: %.2f\n",
				rec.AssessmentID, rec.Status, rec.Depth, rec.BenchmarkVersion, rec.Confidence)
			fmt.Println("\nCategories:")
			for _, cat := range rec.Categories {
				fmt.Printf("  %-22s score=%.1f confidence=%.2f (%.0f/%.0f tests)\n",
					cat.Name, cat.Score, cat.Confidence, cat.TestsPassed, cat.TestsTotal)
			}
			return nil
		},
	}
}

func modelAssessApply() *cobra.Command {
	var acceptedFields string
	// User overrides are always preserved under the layered model; an assessment
	// baseline is written to AssessmentBaseline and never overwrites UserOverrides.
	cmd := &cobra.Command{
		Use:   "apply [assessment-id]",
		Short: "Apply an assessment proposal to the model profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, paths, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			aid := args[0]

			data, err := store.GetAssessment(nil, aid)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			if data == nil {
				return Exit(ExitGeneral, fmt.Errorf("assessment %s not found", aid))
			}
			rec := smart.FromStorage(data)

			accepted := []string{}
			if acceptedFields != "" {
				for _, f := range strings.Split(acceptedFields, ",") {
					accepted = append(accepted, strings.TrimSpace(f))
				}
			}

			// Apply proposal directly to config
			if rec.ProposedProfile == nil {
				return Exit(ExitGeneral, fmt.Errorf("assessment has no proposed profile"))
			}
			prop := rec.ProposedProfile
			key := smart.ProfileKey(rec.ProviderID, rec.ModelID)

			if cfg.ModelProfiles == nil {
				cfg.ModelProfiles = map[string]config.ModelProfileConfig{}
			}
			mp := cfg.ModelProfiles[key]
			if mp.AssessmentBaseline == nil {
				mp.AssessmentBaseline = &config.ProfileBaseline{}
			}
			bl := mp.AssessmentBaseline
			bl.Version = rec.BenchmarkVersion
			if bl.Capabilities == nil {
				bl.Capabilities = map[string]float64{}
			}
			for k, v := range prop.Capabilities {
				if len(accepted) == 0 || containsString(accepted, k) {
					bl.Capabilities[k] = v
				}
			}
			cfg.ModelProfiles[key] = mp

			if err := config.Save(paths.Config, cfg); err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("Assessment %s applied to %s", aid, key), map[string]string{"applied": aid, "profile": key})
		},
	}
	cmd.Flags().StringVar(&acceptedFields, "fields", "", "Comma-separated fields to accept (empty = all)")
	return cmd
}

func modelAssessCancel() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel [assessment-id]",
		Short: "Cancel a running assessment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			aid := args[0]
			data, err := store.GetAssessment(nil, aid)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			if data == nil {
				return Exit(ExitGeneral, fmt.Errorf("assessment %s not found", aid))
			}
			data.Status = string(smart.StatusCancelled)
			if err := store.UpdateAssessment(nil, data); err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("Assessment %s cancelled", aid), map[string]string{"cancelled": aid})
		},
	}
}

func modelAssessHistory() *cobra.Command {
	return &cobra.Command{
		Use:   "history [provider/model]",
		Short: "Show assessment history for a model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			provider := mustSplitProvider(args[0])
			model := mustSplitModel(args[0])
			list, err := store.ListAssessments(nil, provider, model)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			if flagJSON {
				return printJSON(list)
			}
			if len(list) == 0 {
				fmt.Println("No assessments found for", args[0])
				return nil
			}
			for _, a := range list {
				applied := ""
				if a.AppliedAt != nil {
					applied = "applied"
				}
				fmt.Printf("%s  depth=%s  status=%s  confidence=%.2f  %s\n",
					a.AssessmentID, a.Depth, a.Status, a.OverallConfidence, applied)
			}
			return nil
		},
	}
}

func mustSplitProvider(id string) string {
	provider, _ := splitProfileIDParts(id)
	return provider
}

func mustSplitModel(id string) string {
	_, model := splitProfileIDParts(id)
	return model
}

func splitProfileIDParts(id string) (string, string) {
	if i := strings.IndexByte(id, '/'); i > 0 {
		return id[:i], id[i+1:]
	}
	return "", id
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func modelProfileList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured model profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			type row struct {
				ID     string `json:"id"`
				Source string `json:"source"`
			}
			var rows []row
			ps := smart.NewProfileStoreFromConfig(cfg, true)
			for k := range cfg.ModelProfiles {
				provider, model := splitProfileID(k)
				res := ps.ResolveDetailed(provider, model, k)
				src := res.Effective.Source
				if src == "" {
					src = smart.SourceUser
				}
				rows = append(rows, row{ID: k, Source: src})
			}
			if flagJSON {
				return printJSON(rows)
			}
			for _, r := range rows {
				fmt.Printf("%-40s source=%s\n", r.ID, r.Source)
			}
			return nil
		},
	}
}

func modelProfileShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show [provider/model]",
		Short: "Show effective model profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			id := args[0]
			ps := smart.NewProfileStoreFromConfig(cfg, true)
			provider, model := splitProfileID(id)
			p, found := ps.Resolve(provider, model, id)
			if !found && p.Source == smart.SourceUnknown {
				// still show empty profile
			}
			if flagJSON {
				return printJSON(p)
			}
			fmt.Printf("Profile: %s\nSource:  %s\nVersion: %s\n\nCapabilities:\n", p.ID, p.Source, p.Version)
			for _, cap := range smart.AllCapabilities {
				if v, ok := p.Capabilities[cap]; ok {
					fmt.Printf("  %-22s %.1f\n", cap, v)
				}
			}
			fmt.Printf("\nProperties:\n")
			fmt.Printf("  vision=%v tools=%v context_window=%d cost_tier=%d latency_tier=%d privacy=%s\n",
				boolVal(p.Properties.Vision), boolVal(p.Properties.Tools),
				p.Properties.ContextWindow, p.Properties.CostTier, p.Properties.LatencyTier, p.Properties.Privacy)
			return nil
		},
	}
}

func modelProfileSet() *cobra.Command {
	var (
		general, coding, reasoning, analysis, writing, toolUse float64
		costTier, latencyTier                                  int
		privacy                                                string
		vision, tools                                          string
		contextWindow                                          int
	)
	cmd := &cobra.Command{
		Use:   "set [provider/model]",
		Short: "Set user overrides for a model profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, paths, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			id := args[0]
			if cfg.ModelProfiles == nil {
				cfg.ModelProfiles = map[string]config.ModelProfileConfig{}
			}
			mp := cfg.ModelProfiles[id]
			if mp.UserOverrides == nil {
				mp.UserOverrides = &config.ProfileBaseline{}
			}
			if mp.UserOverrides.Capabilities == nil {
				mp.UserOverrides.Capabilities = map[string]float64{}
			}
			if mp.UserOverrides.Properties == nil {
				mp.UserOverrides.Properties = &config.ModelPropertiesConfig{}
			}
			setCap := func(name string, v float64, changed bool) {
				if changed && v >= 0 {
					mp.UserOverrides.Capabilities[name] = v
				}
			}
			setCap(smart.CapGeneral, general, cmd.Flags().Changed("general"))
			setCap(smart.CapCoding, coding, cmd.Flags().Changed("coding"))
			setCap(smart.CapReasoning, reasoning, cmd.Flags().Changed("reasoning"))
			setCap(smart.CapAnalysis, analysis, cmd.Flags().Changed("analysis"))
			setCap(smart.CapWriting, writing, cmd.Flags().Changed("writing"))
			setCap(smart.CapToolUse, toolUse, cmd.Flags().Changed("tool-use"))
			if cmd.Flags().Changed("cost-tier") {
				mp.UserOverrides.Properties.CostTier = costTier
			}
			if cmd.Flags().Changed("latency-tier") {
				mp.UserOverrides.Properties.LatencyTier = latencyTier
			}
			if cmd.Flags().Changed("privacy") {
				mp.UserOverrides.Properties.Privacy = privacy
			}
			if cmd.Flags().Changed("context-window") {
				mp.UserOverrides.Properties.ContextWindow = contextWindow
			}
			if cmd.Flags().Changed("vision") {
				b := strings.EqualFold(vision, "true") || vision == "1"
				mp.UserOverrides.Properties.Vision = &b
			}
			if cmd.Flags().Changed("tools") {
				b := strings.EqualFold(tools, "true") || tools == "1"
				mp.UserOverrides.Properties.Tools = &b
			}
			prov, mod := splitProfileID(id)
			prof := smart.NewProfileStoreFromConfig(&config.Config{ModelProfiles: map[string]config.ModelProfileConfig{id: mp}}, true).ResolveDetailed(prov, mod, id)
			if err := smart.ValidateProfile(prof.Effective); err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			cfg.ModelProfiles[id] = mp
			if err := config.Save(paths.Config, cfg); err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("Profile %q saved", id), mp)
		},
	}
	cmd.Flags().Float64Var(&general, "general", -1, "General capability 1-10")
	cmd.Flags().Float64Var(&coding, "coding", -1, "Coding capability 1-10")
	cmd.Flags().Float64Var(&reasoning, "reasoning", -1, "Reasoning capability 1-10")
	cmd.Flags().Float64Var(&analysis, "analysis", -1, "Analysis capability 1-10")
	cmd.Flags().Float64Var(&writing, "writing", -1, "Writing capability 1-10")
	cmd.Flags().Float64Var(&toolUse, "tool-use", -1, "Tool-use capability 1-10")
	cmd.Flags().IntVar(&costTier, "cost-tier", 0, "Cost tier 1-5 (1=cheapest)")
	cmd.Flags().IntVar(&latencyTier, "latency-tier", 0, "Latency tier 1-5 (1=fastest)")
	cmd.Flags().StringVar(&privacy, "privacy", "", "local | private-cloud | cloud")
	cmd.Flags().StringVar(&vision, "vision", "", "true|false")
	cmd.Flags().StringVar(&tools, "tools", "", "true|false")
	cmd.Flags().IntVar(&contextWindow, "context-window", 0, "Context window tokens")
	return cmd
}

func modelProfileReset() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "reset [provider/model]",
		Short: "Remove user overrides (fall back to built-in)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return Exit(ExitInvalidConfig, fmt.Errorf("refusing without --yes"))
			}
			cfg, paths, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			mp, ok := cfg.ModelProfiles[args[0]]
			if !ok {
				return Exit(ExitInvalidConfig, fmt.Errorf("profile %q not found", args[0]))
			}
			mp.UserOverrides = nil
			if mp.ExternalBaseline == nil && mp.AssessmentBaseline == nil {
				delete(cfg.ModelProfiles, args[0])
			} else {
				cfg.ModelProfiles[args[0]] = mp
			}
			if err := config.Save(paths.Config, cfg); err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("Profile override %q removed", args[0]), map[string]string{"reset": args[0]})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm")
	return cmd
}

func modelProfileValidate() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [provider/model]",
		Short: "Validate a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			id := args[0]
			ps := smart.NewProfileStoreFromConfig(cfg, true)
			provider, model := splitProfileID(id)
			p, _ := ps.Resolve(provider, model, id)
			if err := smart.ValidateProfile(p); err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			return printOut("valid", map[string]any{"id": id, "valid": true, "source": p.Source})
		},
	}
}

func splitProfileID(id string) (provider, model string) {
	if i := strings.IndexByte(id, '/'); i > 0 {
		return id[:i], id[i+1:]
	}
	return "", id
}

func boolVal(b *bool) string {
	if b == nil {
		return "unknown"
	}
	return strconv.FormatBool(*b)
}
