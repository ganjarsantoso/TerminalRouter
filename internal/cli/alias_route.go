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
	cmd.AddCommand(routeAdd(), routeList(), routeShow(), routeRemove())
	return cmd
}

func routeAdd() *cobra.Command {
	var strategy string
	var targets []string // provider:model
	cmd := &cobra.Command{
		Use: "add [name]", Short: "Add a route with ordered targets", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if len(targets) == 0 {
				return Exit(ExitInvalidConfig, fmt.Errorf("at least one --target provider:model is required"))
			}
			cfg, paths, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			if _, ok := cfg.Routes[name]; ok {
				return Exit(ExitConflict, fmt.Errorf("route %q already exists", name))
			}
			if strategy == "" {
				if len(targets) > 1 {
					strategy = "fallback"
				} else {
					strategy = "direct"
				}
			}
			var ts []config.TargetConfig
			for _, t := range targets {
				var p, m string
				for i := 0; i < len(t); i++ {
					if t[i] == ':' {
						p, m = t[:i], t[i+1:]
						break
					}
				}
				if p == "" || m == "" {
					return Exit(ExitInvalidConfig, fmt.Errorf("invalid target %q (use provider:model)", t))
				}
				ts = append(ts, config.TargetConfig{Provider: p, Model: m})
			}
			cfg.Routes[name] = config.RouteConfig{Strategy: strategy, Targets: ts}
			if err := cfg.Validate(); err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			if err := config.Save(paths.Config, cfg); err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("Route %q added", name), cfg.Routes[name])
		},
	}
	cmd.Flags().StringVar(&strategy, "strategy", "", "direct or fallback")
	cmd.Flags().StringArrayVar(&targets, "target", nil, "Target as provider:model (repeatable)")
	return cmd
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
