package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/termrouter/termrouter/internal/app"
	"github.com/termrouter/termrouter/internal/config"
)

func newAliasCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "alias", Short: "Manage model aliases"}
	cmd.AddCommand(aliasAdd(), aliasList(), aliasShow(), aliasRemove())
	return cmd
}

func aliasAdd() *cobra.Command {
	var route, provider, model string
	cmd := &cobra.Command{
		Use: "add [name]", Short: "Add an alias", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, paths, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			if _, ok := cfg.Aliases[name]; ok {
				return Exit(ExitConflict, fmt.Errorf("alias %q already exists", name))
			}
			a := config.AliasConfig{Route: route, Provider: provider, Model: model}
			if route == "" && (provider == "" || model == "") {
				return Exit(ExitInvalidConfig, fmt.Errorf("provide --route or --provider and --model"))
			}
			cfg.Aliases[name] = a
			if err := cfg.Validate(); err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			if err := config.Save(paths.Config, cfg); err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("Alias %q added", name), map[string]any{"alias": name, "config": a})
		},
	}
	cmd.Flags().StringVar(&route, "route", "", "Route name")
	cmd.Flags().StringVar(&provider, "provider", "", "Direct provider")
	cmd.Flags().StringVar(&model, "model", "", "Upstream model id")
	return cmd
}

func aliasList() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List aliases",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			if flagJSON {
				return printJSON(cfg.Aliases)
			}
			if len(cfg.Aliases) == 0 {
				fmt.Println("No aliases.")
				return nil
			}
			for name, a := range cfg.Aliases {
				if a.Route != "" {
					fmt.Printf("%-16s route=%s\n", name, a.Route)
				} else {
					fmt.Printf("%-16s provider=%s model=%s\n", name, a.Provider, a.Model)
				}
			}
			return nil
		},
	}
}

func aliasShow() *cobra.Command {
	return &cobra.Command{
		Use: "show [name]", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			a, ok := cfg.Aliases[args[0]]
			if !ok {
				return Exit(ExitInvalidConfig, fmt.Errorf("alias %q not found", args[0]))
			}
			return printOut(fmt.Sprintf("%+v", a), a)
		},
	}
}

func aliasRemove() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use: "remove [name]", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return Exit(ExitInvalidConfig, fmt.Errorf("refusing without --yes"))
			}
			cfg, paths, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			if _, ok := cfg.Aliases[args[0]]; !ok {
				return Exit(ExitInvalidConfig, fmt.Errorf("alias %q not found", args[0]))
			}
			delete(cfg.Aliases, args[0])
			if err := config.Save(paths.Config, cfg); err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("Alias %q removed", args[0]), map[string]any{"removed": args[0]})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm")
	return cmd
}

func newRouteCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "route", Short: "Manage routes"}
	cmd.AddCommand(routeAdd(), routeList(), routeShow(), routeRemove(), routeSmartCmd())
	return cmd
}

func routeAdd() *cobra.Command {
	var strategy, policy, def string
	var targets, candidates []string // provider:model
	var shadow bool
	cmd := &cobra.Command{
		Use: "add [name]", Short: "Add a route with ordered targets or smart candidates", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, paths, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			if _, ok := cfg.Routes[name]; ok {
				return Exit(ExitConflict, fmt.Errorf("route %q already exists", name))
			}
			if strategy == "" {
				if len(candidates) > 0 {
					strategy = "smart"
				} else if len(targets) > 1 {
					strategy = "fallback"
				} else {
					strategy = "direct"
				}
			}

			rc := config.RouteConfig{Strategy: strategy, Default: def}
			if strategy == "smart" {
				src := candidates
				if len(src) == 0 {
					src = targets
				}
				if len(src) == 0 {
					return Exit(ExitInvalidConfig, fmt.Errorf("smart route requires --candidate provider:model"))
				}
				for _, t := range src {
					p, m, err := config.ParseProviderModel(t)
					if err != nil {
						return Exit(ExitInvalidConfig, err)
					}
					rc.Candidates = append(rc.Candidates, config.CandidateConfig{Provider: p, Model: m})
				}
				mode := "live"
				if shadow || !cmd.Flags().Changed("shadow") {
					// PRD default: shadow until explicitly live; --shadow forces shadow; without flag use shadow
					mode = "shadow"
				}
				if cmd.Flags().Changed("shadow") && !shadow {
					mode = "live"
				}
				if policy == "" {
					policy = "balanced"
				}
				rc.Smart = &config.SmartConfig{Mode: mode, Policy: policy}
				if def != "" {
					rc.Smart.LowConfidenceTarget = def
				}
			} else {
				if len(targets) == 0 {
					return Exit(ExitInvalidConfig, fmt.Errorf("at least one --target provider:model is required"))
				}
				for _, t := range targets {
					p, m, err := config.ParseProviderModel(t)
					if err != nil {
						return Exit(ExitInvalidConfig, err)
					}
					rc.Targets = append(rc.Targets, config.TargetConfig{Provider: p, Model: m})
				}
			}
			cfg.Routes[name] = rc
			if err := cfg.Validate(); err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			if err := config.Save(paths.Config, cfg); err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("Route %q added", name), cfg.Routes[name])
		},
	}
	cmd.Flags().StringVar(&strategy, "strategy", "", "direct, fallback, or smart")
	cmd.Flags().StringArrayVar(&targets, "target", nil, "Target as provider:model (repeatable)")
	cmd.Flags().StringArrayVar(&candidates, "candidate", nil, "Smart candidate as provider:model (repeatable)")
	cmd.Flags().StringVar(&policy, "policy", "balanced", "Smart policy: balanced|quality|economy|fast|private")
	cmd.Flags().StringVar(&def, "default", "", "Default provider:model for smart routes")
	cmd.Flags().BoolVar(&shadow, "shadow", true, "Create smart route in shadow mode (default true)")
	return cmd
}

func routeSmartCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "smart", Short: "Enable/disable smart routing modes"}
	cmd.AddCommand(routeSmartEnable(), routeSmartDisable(), routeSmartValidate())
	return cmd
}

func routeSmartEnable() *cobra.Command {
	var shadow bool
	cmd := &cobra.Command{
		Use:   "enable [name]",
		Short: "Enable smart mode on a route (default shadow)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, paths, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			name := args[0]
			r, ok := cfg.Routes[name]
			if !ok {
				return Exit(ExitInvalidConfig, fmt.Errorf("route %q not found", name))
			}
			r.Strategy = "smart"
			if r.Smart == nil {
				r.Smart = &config.SmartConfig{Policy: "balanced"}
			}
			if shadow {
				r.Smart.Mode = "shadow"
			} else {
				r.Smart.Mode = "live"
			}
			// ensure candidates from targets if needed
			if len(r.Candidates) == 0 && len(r.Targets) > 0 {
				for _, t := range r.Targets {
					r.Candidates = append(r.Candidates, config.CandidateConfig{Provider: t.Provider, Model: t.Model})
				}
			}
			cfg.Routes[name] = r
			if err := cfg.Validate(); err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			if err := config.Save(paths.Config, cfg); err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("Route %q smart mode=%s", name, r.Smart.Mode), r)
		},
	}
	cmd.Flags().BoolVar(&shadow, "shadow", true, "Enable in shadow mode (use --shadow=false for live)")
	return cmd
}

func routeSmartDisable() *cobra.Command {
	return &cobra.Command{
		Use:   "disable [name]",
		Short: "Disable smart selection (mode=off)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, paths, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			name := args[0]
			r, ok := cfg.Routes[name]
			if !ok {
				return Exit(ExitInvalidConfig, fmt.Errorf("route %q not found", name))
			}
			if r.Smart == nil {
				r.Smart = &config.SmartConfig{}
			}
			r.Smart.Mode = "off"
			cfg.Routes[name] = r
			if err := config.Save(paths.Config, cfg); err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("Route %q smart disabled", name), r)
		},
	}
}

func routeSmartValidate() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [name]",
		Short: "Validate a smart route configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			name := args[0]
			r, ok := cfg.Routes[name]
			if !ok {
				return Exit(ExitInvalidConfig, fmt.Errorf("route %q not found", name))
			}
			if err := cfg.Validate(); err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			n := len(r.Candidates)
			if n == 0 {
				n = len(r.Targets)
			}
			return printOut(fmt.Sprintf("route %q valid (%d candidates)", name, n), map[string]any{
				"route": name, "valid": true, "candidates": n,
			})
		},
	}
}

func routeList() *cobra.Command {
	return &cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			if flagJSON {
				return printJSON(cfg.Routes)
			}
			for name, r := range cfg.Routes {
				fmt.Printf("%s strategy=%s targets=%d\n", name, r.Strategy, len(r.Targets))
			}
			return nil
		},
	}
}

func routeShow() *cobra.Command {
	return &cobra.Command{
		Use: "show [name]", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			r, ok := cfg.Routes[args[0]]
			if !ok {
				return Exit(ExitInvalidConfig, fmt.Errorf("route %q not found", args[0]))
			}
			return printOut(fmt.Sprintf("%+v", r), r)
		},
	}
}

func routeRemove() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use: "remove [name]", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return Exit(ExitInvalidConfig, fmt.Errorf("refusing without --yes"))
			}
			cfg, paths, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			delete(cfg.Routes, args[0])
			if err := config.Save(paths.Config, cfg); err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("Route %q removed", args[0]), map[string]any{"removed": args[0]})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm")
	return cmd
}
