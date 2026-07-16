package cli

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/termrouter/termrouter/internal/app"
	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/provider"
	panthropic "github.com/termrouter/termrouter/internal/provider/anthropic"
	"github.com/termrouter/termrouter/internal/provider/compatible"
	"golang.org/x/term"
)

func newProviderCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "provider", Short: "Manage provider connections"}
	cmd.AddCommand(providerAdd(), providerList(), providerShow(), providerTest(), providerEnable(), providerDisable(), providerRemove())
	return cmd
}

func providerAdd() *cobra.Command {
	var (
		name, typ, baseURL, apiKey, envRef string
		fromStdin                          bool
	)
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a provider connection",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" || typ == "" {
				return Exit(ExitInvalidConfig, fmt.Errorf("--name and --type are required"))
			}
			cfg, paths, store, creds, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			if _, exists := cfg.Providers[name]; exists {
				return Exit(ExitConflict, fmt.Errorf("provider %q already exists", name))
			}
			switch typ {
			case "openai", "anthropic", "openai-compatible":
			default:
				return Exit(ExitInvalidConfig, fmt.Errorf("type must be openai, anthropic, or openai-compatible"))
			}
			if typ == "openai-compatible" && baseURL == "" {
				return Exit(ExitInvalidConfig, fmt.Errorf("--base-url is required for openai-compatible"))
			}
			if typ == "openai" && baseURL == "" {
				baseURL = "https://api.openai.com/v1"
			}
			if typ == "anthropic" && baseURL == "" {
				baseURL = "https://api.anthropic.com"
			}

			var credRef string
			if envRef != "" {
				credRef = "env://" + strings.TrimPrefix(envRef, "env://")
			} else {
				secret := apiKey
				if fromStdin {
					b, err := os.ReadFile("/dev/stdin")
					if err != nil {
						return Exit(ExitGeneral, err)
					}
					secret = strings.TrimSpace(string(b))
				} else if secret == "" {
					fmt.Fprint(os.Stderr, "API key (input hidden): ")
					b, err := term.ReadPassword(int(syscall.Stdin))
					fmt.Fprintln(os.Stderr)
					if err != nil {
						return Exit(ExitGeneral, fmt.Errorf("read secret: %w (use --api-key-stdin or --env)", err))
					}
					secret = strings.TrimSpace(string(b))
				}
				if secret == "" {
					// allow none for local
					credRef = "none://"
				} else {
					ref, err := creds.Store(name, secret)
					if err != nil {
						return Exit(ExitGeneral, err)
					}
					credRef = ref
				}
			}

			cfg.Providers[name] = config.ProviderConfig{
				Type:          typ,
				BaseURL:       baseURL,
				CredentialRef: credRef,
			}
			if err := config.Save(paths.Config, cfg); err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("Provider %q added (%s)", name, typ), map[string]any{
				"name": name, "type": typ, "base_url": baseURL, "credential_ref": redactRef(credRef),
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Provider connection name (slug)")
	cmd.Flags().StringVar(&typ, "type", "", "openai | anthropic | openai-compatible")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "Upstream base URL")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key (discouraged; prefer prompt or --api-key-stdin)")
	cmd.Flags().BoolVar(&fromStdin, "api-key-stdin", false, "Read API key from stdin")
	cmd.Flags().StringVar(&envRef, "env", "", "Use env var name as credential (e.g. OPENAI_API_KEY)")
	return cmd
}

func providerList() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			if flagJSON {
				out := map[string]any{}
				for k, v := range cfg.Providers {
					cp := v
					cp.CredentialRef = redactRef(cp.CredentialRef)
					out[k] = cp
				}
				return printJSON(out)
			}
			if len(cfg.Providers) == 0 {
				fmt.Println("No providers configured.")
				return nil
			}
			for name, p := range cfg.Providers {
				en := "on"
				if !p.IsEnabled() {
					en = "off"
				}
				fmt.Printf("%-20s type=%-18s enabled=%s ref=%s\n", name, p.Type, en, redactRef(p.CredentialRef))
			}
			return nil
		},
	}
}

func providerShow() *cobra.Command {
	return &cobra.Command{
		Use: "show [name]", Short: "Show provider details", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			p, ok := cfg.Providers[args[0]]
			if !ok {
				return Exit(ExitInvalidConfig, fmt.Errorf("provider %q not found", args[0]))
			}
			p.CredentialRef = redactRef(p.CredentialRef)
			if flagJSON {
				return printJSON(p)
			}
			fmt.Printf("Name:     %s\nType:     %s\nBase URL: %s\nCred:     %s\nEnabled:  %v\n",
				args[0], p.Type, p.BaseURL, p.CredentialRef, p.IsEnabled())
			return nil
		},
	}
}

func providerTest() *cobra.Command {
	return &cobra.Command{
		Use: "test [name]", Short: "Test provider connectivity", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, creds, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			p, ok := cfg.Providers[args[0]]
			if !ok {
				return Exit(ExitInvalidConfig, fmt.Errorf("provider %q not found", args[0]))
			}
			adapter := adapterFor(p.Type)
			if adapter == nil {
				return Exit(ExitGeneral, fmt.Errorf("no adapter for type %s", p.Type))
			}
			secret, err := creds.Resolve(p.CredentialRef)
			if err != nil {
				return Exit(ExitAuth, fmt.Errorf("resolve credential: %w", err))
			}
			// never log secret
			if err := adapter.Validate(cmd.Context(), p, secret); err != nil {
				return Exit(ExitUnavailable, fmt.Errorf("provider %s test failed: %w\nNext: check credential_ref and network", args[0], err))
			}
			return printOut(fmt.Sprintf("Provider %q OK", args[0]), map[string]any{"provider": args[0], "ok": true})
		},
	}
}

func providerEnable() *cobra.Command {
	return enableDisable(true)
}
func providerDisable() *cobra.Command {
	return enableDisable(false)
}

func enableDisable(enable bool) *cobra.Command {
	use := "disable"
	if enable {
		use = "enable"
	}
	return &cobra.Command{
		Use: use + " [name]", Args: cobra.ExactArgs(1),
		Short: use + " a provider",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, paths, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			p, ok := cfg.Providers[args[0]]
			if !ok {
				return Exit(ExitInvalidConfig, fmt.Errorf("provider %q not found", args[0]))
			}
			p.Enabled = &enable
			cfg.Providers[args[0]] = p
			if err := config.Save(paths.Config, cfg); err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("Provider %q %sd", args[0], use), map[string]any{"provider": args[0], "enabled": enable})
		},
	}
}

func providerRemove() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use: "remove [name]", Short: "Remove a provider", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return Exit(ExitInvalidConfig, fmt.Errorf("refusing to remove without --yes"))
			}
			cfg, paths, store, creds, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			p, ok := cfg.Providers[args[0]]
			if !ok {
				return Exit(ExitInvalidConfig, fmt.Errorf("provider %q not found", args[0]))
			}
			_ = creds.Remove(p.CredentialRef)
			delete(cfg.Providers, args[0])
			if err := config.Save(paths.Config, cfg); err != nil {
				return Exit(ExitGeneral, err)
			}
			return printOut(fmt.Sprintf("Provider %q removed", args[0]), map[string]any{"removed": args[0]})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm removal")
	return cmd
}

func adapterFor(typ string) provider.Adapter {
	switch typ {
	case "openai":
		return compatible.NewOpenAI()
	case "openai-compatible":
		return compatible.NewCompatible()
	case "anthropic":
		return panthropic.New()
	}
	return nil
}

func redactRef(ref string) string {
	if ref == "" {
		return ""
	}
	if i := strings.Index(ref, "://"); i >= 0 {
		return ref[:i+3] + "[redacted]"
	}
	return "[redacted]"
}
