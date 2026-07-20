package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/termrouter/termrouter/internal/app"
	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/smart"
	"github.com/termrouter/termrouter/internal/smart/external"
)

func modelExternalCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "external", Short: "Independent benchmark profile import"}
	cmd.AddCommand(
		modelExternalRegistry(),
		modelExternalSearch(),
		modelExternalProposal(),
		modelExternalListProposals(),
		modelExternalApply(),
		modelExternalHistory(),
	)
	return cmd
}

// newExternalService loads the runtime once and returns the external service,
// the loaded config, and the config paths. The closer closes the single store
// handle; callers must not open a second runtime, or the extra store handle
// leaks (SQLite lock / descriptor pressure). cfg and paths are returned so
// commands that need config can reuse this single load.
func newExternalService() (*external.Service, *config.Config, config.Paths, func(), error) {
	cfg, paths, store, _, err := app.LoadRuntime(mustHome())
	if err != nil {
		return nil, nil, config.Paths{}, nil, err
	}
	searcher := external.NewWebSearcher(cfg.WebSearch)
	return external.NewService(store, searcher, nil), cfg, paths, func() { store.Close() }, nil
}

func modelExternalRegistry() *cobra.Command {
	return &cobra.Command{
		Use:   "registry",
		Short: "Show the bundled curated benchmark registry info",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, _, closer, err := newExternalService()
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer closer()
			info := svc.RegistryInfo()
			if flagJSON {
				return printJSON(info)
			}
			fmt.Printf("Registry version: %s\nUpdated:  %s\nSources:  %d\nModels:   %d\nEvidence: %d\n\n",
				info.Version, info.UpdatedAt.Format("2006-01-02"), info.SourceCount, info.ModelCount, info.EvidenceCount)
			for _, s := range info.Sources {
				fmt.Printf("  - %-26s tier=%-8s scale=%-8s %s\n", s.Name, s.TrustTier, s.NativeScale, s.URL)
			}
			return nil
		},
	}
}

func modelExternalSearch() *cobra.Command {
	return &cobra.Command{
		Use:   "search [provider/model]",
		Short: "Search the curated registry for independent benchmark consensus",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, _, closer, err := newExternalService()
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer closer()
			provider, model := splitProfileID(args[0])
			cp, ok, err := svc.Search(context.Background(), provider, model)
			if err != nil {
				return Exit(ExitGeneral, fmt.Errorf("web search failed: %w", err))
			}
			if !ok || cp == nil {
				return Exit(ExitGeneral, fmt.Errorf("no independent benchmark evidence found online for %s", args[0]))
			}
			if flagJSON {
				return printJSON(cp)
			}
			fmt.Printf("Independent consensus for %s (identity=%s)\nOverall: %.1f  Confidence: %.2f  Sources: %v\n\n",
				args[0], cp.ModelIdentity, cp.Overall, cp.Confidence, cp.Sources)
			fmt.Println("Capabilities:")
			for _, k := range external.CapabilityKeys {
				c, has := cp.Capabilities[k]
				if !has {
					continue
				}
				fmt.Printf("  %-22s %.1f  (conf %.2f, band %.1f-%.1f, n=%d, src=%s)\n",
					k, c.Estimate, c.Confidence, c.LowBand, c.HighBand, c.SourceCount, c.PrimarySource)
			}
			return nil
		},
	}
}

func modelExternalProposal() *cobra.Command {
	return &cobra.Command{
		Use:   "proposal [provider/model]",
		Short: "Build a reviewable external-consensus proposal for a model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cfg, _, closer, err := newExternalService()
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer closer()
			provider, model := splitProfileID(args[0])
			current := map[string]float64{}
			ps := smart.NewProfileStoreFromConfig(cfg, false)
			prov, mod := splitProfileID(args[0])
			if eff, ok := ps.Resolve(prov, mod, args[0]); ok {
				for k, v := range eff.Capabilities {
					current[k] = v
				}
			}
			p, ok, err := svc.BuildProposal(context.Background(), provider, model, current)
			if err != nil {
				return Exit(ExitGeneral, fmt.Errorf("web search failed: %w", err))
			}
			if !ok || p == nil {
				return Exit(ExitGeneral, fmt.Errorf("no independent benchmark evidence found online for %s", args[0]))
			}
			if err := svc.SaveProposal(*p); err != nil {
				return Exit(ExitGeneral, err)
			}
			if flagJSON {
				return printJSON(p)
			}
			fmt.Printf("Proposal %s for %s (identity=%s)\n\n", p.ID, args[0], p.ModelIdentity)
			for _, f := range p.Fields {
				cur := "(unset)"
				if f.Current != nil {
					cur = fmt.Sprintf("%.1f", *f.Current)
				}
				fmt.Printf("  %-22s current=%-7s proposed=%.1f\n", f.Capability, cur, f.Proposed)
			}
			fmt.Printf("\nSaved. Apply with: termrouter model external apply %s\n", p.ID)
			return nil
		},
	}
}

func modelExternalListProposals() *cobra.Command {
	var status string
	cmd := &cobra.Command{
		Use:   "list-proposals",
		Short: "List saved external-consensus proposals",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, _, closer, err := newExternalService()
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer closer()
			list, err := svc.ListProposals(status)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			if flagJSON {
				return printJSON(list)
			}
			if len(list) == 0 {
				fmt.Println("No proposals.")
				return nil
			}
			for _, p := range list {
				fmt.Printf("%s  %s/%s  status=%s  fields=%d\n", p.ID, p.ProviderID, p.ModelID, p.Status, len(p.Fields))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (pending|applied|dismissed)")
	return cmd
}

func modelExternalApply() *cobra.Command {
	return &cobra.Command{
		Use:   "apply [proposal-id]",
		Short: "Apply an external-consensus proposal to the model profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cfg, paths, closer, err := newExternalService()
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer closer()
			p, ok, err := svc.GetProposal(args[0])
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			if !ok {
				return Exit(ExitGeneral, fmt.Errorf("proposal %s not found", args[0]))
			}
			caps, err := svc.ApplyProposal(p)
			if err != nil {
				return Exit(ExitGeneral, err)
			}

			key := p.ProviderID + "/" + p.ModelID
			if cfg.ModelProfiles == nil {
				cfg.ModelProfiles = map[string]config.ModelProfileConfig{}
			}
			mp := cfg.ModelProfiles[key]
			if mp.ExternalBaseline == nil {
				mp.ExternalBaseline = &config.ProfileBaseline{}
			}
			mp.ExternalBaseline.Version = p.RegistryVersion
			if mp.ExternalBaseline.Capabilities == nil {
				mp.ExternalBaseline.Capabilities = map[string]float64{}
			}
			for k, v := range caps {
				mp.ExternalBaseline.Capabilities[k] = v
			}
			// External consensus sits below user overrides and local assessment;
			// per-field resolution keeps higher layers' values for shared fields,
			// so we must not copy or overwrite user/assessment values here.
			cfg.ModelProfiles[key] = mp
			if err := config.Save(paths.Config, cfg); err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("External consensus applied to %s", key), map[string]any{
				"profile": key, "proposal": p.ID, "capabilities": caps,
			})
		},
	}
}

func modelExternalHistory() *cobra.Command {
	return &cobra.Command{
		Use:   "history",
		Short: "Show external-consensus import history",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, _, closer, err := newExternalService()
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer closer()
			hist, err := svc.ImportHistory(50)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			if flagJSON {
				return printJSON(hist)
			}
			if len(hist) == 0 {
				fmt.Println("No imports yet.")
				return nil
			}
			for _, h := range hist {
				caps := []string{}
				for k, v := range h.Capabilities {
					caps = append(caps, fmt.Sprintf("%s=%.1f", k, v))
				}
				fmt.Printf("%s  %s  %s\n", h.AppliedAt.Format("2006-01-02 15:04"), h.ProfileID, strings.Join(caps, " "))
			}
			return nil
		},
	}
}
