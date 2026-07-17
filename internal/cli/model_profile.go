package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/termrouter/termrouter/internal/app"
	"github.com/termrouter/termrouter/internal/config"
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
	)
	return cmd
}

func modelProfileList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List built-in and user model profiles",
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
			for _, k := range smart.ListBuiltinKeys() {
				rows = append(rows, row{ID: k, Source: smart.SourceBuiltin})
			}
			for k, mp := range cfg.ModelProfiles {
				src := mp.Source
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
			ps := smart.NewProfileStore(smart.ProfilesFromConfig(cfg), true)
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
					fmt.Printf("  %-22s %d\n", cap, v)
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
		general, coding, reasoning, analysis, writing, toolUse int
		costTier, latencyTier                                   int
		privacy                                                 string
		vision, tools                                           string
		contextWindow                                           int
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
			if mp.Capabilities == nil {
				mp.Capabilities = map[string]int{}
			}
			mp.Source = smart.SourceUser
			setCap := func(name string, v int, changed bool) {
				if changed && v >= 0 {
					mp.Capabilities[name] = v
				}
			}
			setCap(smart.CapGeneral, general, cmd.Flags().Changed("general"))
			setCap(smart.CapCoding, coding, cmd.Flags().Changed("coding"))
			setCap(smart.CapReasoning, reasoning, cmd.Flags().Changed("reasoning"))
			setCap(smart.CapAnalysis, analysis, cmd.Flags().Changed("analysis"))
			setCap(smart.CapWriting, writing, cmd.Flags().Changed("writing"))
			setCap(smart.CapToolUse, toolUse, cmd.Flags().Changed("tool-use"))
			if cmd.Flags().Changed("cost-tier") {
				mp.Properties.CostTier = costTier
			}
			if cmd.Flags().Changed("latency-tier") {
				mp.Properties.LatencyTier = latencyTier
			}
			if cmd.Flags().Changed("privacy") {
				mp.Properties.Privacy = privacy
			}
			if cmd.Flags().Changed("context-window") {
				mp.Properties.ContextWindow = contextWindow
			}
			if cmd.Flags().Changed("vision") {
				b := strings.EqualFold(vision, "true") || vision == "1"
				mp.Properties.Vision = &b
			}
			if cmd.Flags().Changed("tools") {
				b := strings.EqualFold(tools, "true") || tools == "1"
				mp.Properties.Tools = &b
			}
			prof := smart.ProfilesFromConfig(&config.Config{ModelProfiles: map[string]config.ModelProfileConfig{id: mp}})[id]
			if err := smart.ValidateProfile(prof); err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			cfg.ModelProfiles[id] = mp
			if err := config.Save(paths.Config, cfg); err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("Profile %q saved", id), mp)
		},
	}
	cmd.Flags().IntVar(&general, "general", -1, "General capability 1-5")
	cmd.Flags().IntVar(&coding, "coding", -1, "Coding capability 1-5")
	cmd.Flags().IntVar(&reasoning, "reasoning", -1, "Reasoning capability 1-5")
	cmd.Flags().IntVar(&analysis, "analysis", -1, "Analysis capability 1-5")
	cmd.Flags().IntVar(&writing, "writing", -1, "Writing capability 1-5")
	cmd.Flags().IntVar(&toolUse, "tool-use", -1, "Tool-use capability 1-5")
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
			delete(cfg.ModelProfiles, args[0])
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
			ps := smart.NewProfileStore(smart.ProfilesFromConfig(cfg), true)
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
