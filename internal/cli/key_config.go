package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/termrouter/termrouter/internal/app"
	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/credentials"
	"github.com/termrouter/termrouter/internal/storage"
	"gopkg.in/yaml.v3"
)

func newKeyCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "key", Short: "Manage router client API keys"}
	cmd.AddCommand(keyCreate(), keyList(), keyRotate(), keyDisable(), keyRemove(), keySetPolicy())
	return cmd
}

func keyCreate() *cobra.Command {
	var (
		name            string
		aliases         []string
		rpm             int
		maxConcurrent   int
		dailyRequests   int
		dailyInputTok   int64
		dailyOutputTok  int64
		dailyCostUSD    float64
		maxOutputTokens int
		maxRequestBody  int64
		portable        bool
		expires         string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a client API key",
		Long: `Create a client API key. Plaintext is shown exactly once; only a hash is stored.

For public VPS / shared-agent use, create a restricted portable key:

  termrouter key create --name portable-agents --portable \
    --alias coding --alias auto \
    --rpm 30 --max-concurrent 4 \
    --daily-requests 500 --daily-cost-usd 10
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				name = "default"
			}
			_, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			pt, prefix, hash, salt, err := credentials.GenerateClientKey()
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			id := "key_" + randomHex(8)
			k := storage.ClientKey{
				ID: id, Name: name, KeyPrefix: prefix, KeyHash: hash, Salt: salt,
				Enabled: true, AllowedAliases: aliases, Portable: portable,
				CreatedAt: time.Now().UTC(),
			}
			applyKeyLimitFlags(&k, rpm, maxConcurrent, dailyRequests, dailyInputTok, dailyOutputTok, dailyCostUSD, maxOutputTokens, maxRequestBody, expires)
			if err := store.InsertClientKey(cmd.Context(), k); err != nil {
				return Exit(ExitGeneral, err)
			}
			if flagJSON {
				out := map[string]any{
					"id": id, "name": name, "key": pt, "prefix": prefix,
					"portable": portable, "allowed_aliases": aliases,
					"note": "Save this key now; only its hash is retained.",
				}
				if portable {
					out["warning"] = portableKeyWarning
				}
				return printJSON(out)
			}
			fmt.Printf("Client key created: %s\n", pt)
			fmt.Printf("ID: %s  Name: %s\n", id, name)
			if len(aliases) > 0 {
				fmt.Printf("Allowed aliases: %s\n", strings.Join(aliases, ", "))
			}
			if portable {
				fmt.Println()
				fmt.Println(portableKeyWarning)
			}
			fmt.Println("Save it now; only its hash will be retained.")
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "default", "Key label")
	cmd.Flags().StringSliceVar(&aliases, "alias", nil, "Restrict to these aliases (repeatable)")
	cmd.Flags().IntVar(&rpm, "rpm", 0, "Requests-per-minute limit (0 = unlimited)")
	cmd.Flags().IntVar(&maxConcurrent, "max-concurrent", 0, "Max concurrent requests (0 = unlimited)")
	cmd.Flags().IntVar(&dailyRequests, "daily-requests", 0, "Daily request quota (0 = unlimited)")
	cmd.Flags().Int64Var(&dailyInputTok, "daily-input-tokens", 0, "Daily input-token quota (0 = unlimited)")
	cmd.Flags().Int64Var(&dailyOutputTok, "daily-output-tokens", 0, "Daily output-token quota (0 = unlimited)")
	cmd.Flags().Float64Var(&dailyCostUSD, "daily-cost-usd", 0, "Daily estimated-spend budget in USD (0 = unlimited)")
	cmd.Flags().IntVar(&maxOutputTokens, "max-output-tokens", 0, "Cap max_tokens / max_output_tokens per request (0 = unlimited)")
	cmd.Flags().Int64Var(&maxRequestBody, "max-request-body", 0, "Per-key request body size cap in bytes (0 = unlimited)")
	cmd.Flags().BoolVar(&portable, "portable", false, "Mark as shared portable key (shows security warning)")
	cmd.Flags().StringVar(&expires, "expires", "", "Optional expiration (RFC3339 timestamp)")
	return cmd
}

const portableKeyWarning = `WARNING: Portable/shared key
  • All devices share one credential; compromise affects every agent
  • Rotation must update every client at once
  • Device-specific revocation and attribution are unavailable
  • Treat this key like a password with direct financial impact`

func applyKeyLimitFlags(k *storage.ClientKey, rpm, maxConcurrent, dailyRequests int, dailyInputTok, dailyOutputTok int64, dailyCostUSD float64, maxOutputTokens int, maxRequestBody int64, expires string) {
	if rpm > 0 {
		k.RateLimitRPM = &rpm
	}
	if maxConcurrent > 0 {
		k.MaxConcurrentRequests = &maxConcurrent
	}
	if dailyRequests > 0 {
		k.DailyRequestLimit = &dailyRequests
	}
	if dailyInputTok > 0 {
		k.DailyInputTokens = &dailyInputTok
	}
	if dailyOutputTok > 0 {
		k.DailyOutputTokens = &dailyOutputTok
	}
	if dailyCostUSD > 0 {
		k.DailyEstimatedCostUSD = &dailyCostUSD
	}
	if maxOutputTokens > 0 {
		k.MaxOutputTokens = &maxOutputTokens
	}
	if maxRequestBody > 0 {
		k.MaxRequestBody = &maxRequestBody
	}
	if expires != "" {
		t, err := time.Parse(time.RFC3339, expires)
		if err == nil {
			k.ExpiresAt = &t
		}
	}
}

func keySetPolicy() *cobra.Command {
	var (
		aliases         []string
		rpm             int
		maxConcurrent   int
		dailyRequests   int
		dailyInputTok   int64
		dailyOutputTok  int64
		dailyCostUSD    float64
		maxOutputTokens int
		maxRequestBody  int64
		portable        bool
		setPortable     bool
		expires         string
		clearExpires    bool
	)
	cmd := &cobra.Command{
		Use:   "set-policy [id]",
		Short: "Update policy limits on an existing client key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			existing, err := store.GetClientKeyByID(cmd.Context(), args[0])
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			if existing == nil {
				return Exit(ExitInvalidConfig, fmt.Errorf("client key %q not found", args[0]))
			}
			k := *existing
			if cmd.Flags().Changed("alias") {
				k.AllowedAliases = aliases
			}
			if cmd.Flags().Changed("rpm") {
				if rpm > 0 {
					k.RateLimitRPM = &rpm
				} else {
					k.RateLimitRPM = nil
				}
			}
			if cmd.Flags().Changed("max-concurrent") {
				if maxConcurrent > 0 {
					k.MaxConcurrentRequests = &maxConcurrent
				} else {
					k.MaxConcurrentRequests = nil
				}
			}
			if cmd.Flags().Changed("daily-requests") {
				if dailyRequests > 0 {
					k.DailyRequestLimit = &dailyRequests
				} else {
					k.DailyRequestLimit = nil
				}
			}
			if cmd.Flags().Changed("daily-input-tokens") {
				if dailyInputTok > 0 {
					k.DailyInputTokens = &dailyInputTok
				} else {
					k.DailyInputTokens = nil
				}
			}
			if cmd.Flags().Changed("daily-output-tokens") {
				if dailyOutputTok > 0 {
					k.DailyOutputTokens = &dailyOutputTok
				} else {
					k.DailyOutputTokens = nil
				}
			}
			if cmd.Flags().Changed("daily-cost-usd") {
				if dailyCostUSD > 0 {
					k.DailyEstimatedCostUSD = &dailyCostUSD
				} else {
					k.DailyEstimatedCostUSD = nil
				}
			}
			if cmd.Flags().Changed("max-output-tokens") {
				if maxOutputTokens > 0 {
					k.MaxOutputTokens = &maxOutputTokens
				} else {
					k.MaxOutputTokens = nil
				}
			}
			if cmd.Flags().Changed("max-request-body") {
				if maxRequestBody > 0 {
					k.MaxRequestBody = &maxRequestBody
				} else {
					k.MaxRequestBody = nil
				}
			}
			if setPortable {
				k.Portable = portable
			}
			if clearExpires {
				k.ExpiresAt = nil
			} else if expires != "" {
				t, err := time.Parse(time.RFC3339, expires)
				if err != nil {
					return Exit(ExitInvalidConfig, fmt.Errorf("invalid --expires: %w", err))
				}
				k.ExpiresAt = &t
			}
			if err := store.UpdateClientKeyPolicy(cmd.Context(), k); err != nil {
				return Exit(ExitGeneral, err)
			}
			if flagJSON {
				return printJSON(keyPolicyMap(k))
			}
			fmt.Printf("Updated policy for %s (%s)\n", k.ID, k.Name)
			if k.Portable {
				fmt.Println(portableKeyWarning)
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&aliases, "alias", nil, "Replace allowed aliases (repeatable; empty clears restriction)")
	cmd.Flags().IntVar(&rpm, "rpm", 0, "Requests-per-minute limit (0 clears)")
	cmd.Flags().IntVar(&maxConcurrent, "max-concurrent", 0, "Max concurrent requests (0 clears)")
	cmd.Flags().IntVar(&dailyRequests, "daily-requests", 0, "Daily request quota (0 clears)")
	cmd.Flags().Int64Var(&dailyInputTok, "daily-input-tokens", 0, "Daily input-token quota (0 clears)")
	cmd.Flags().Int64Var(&dailyOutputTok, "daily-output-tokens", 0, "Daily output-token quota (0 clears)")
	cmd.Flags().Float64Var(&dailyCostUSD, "daily-cost-usd", 0, "Daily estimated-spend budget USD (0 clears)")
	cmd.Flags().IntVar(&maxOutputTokens, "max-output-tokens", 0, "Per-request max output tokens (0 clears)")
	cmd.Flags().Int64Var(&maxRequestBody, "max-request-body", 0, "Per-key request body size cap in bytes (0 clears)")
	cmd.Flags().BoolVar(&portable, "portable", false, "Mark as portable/shared key")
	cmd.Flags().BoolVar(&setPortable, "set-portable", false, "Apply --portable flag (required to change portable bit)")
	cmd.Flags().StringVar(&expires, "expires", "", "Expiration RFC3339")
	cmd.Flags().BoolVar(&clearExpires, "clear-expires", false, "Remove expiration")
	return cmd
}

func keyPolicyMap(k storage.ClientKey) map[string]any {
	m := map[string]any{
		"id":              k.ID,
		"name":            k.Name,
		"enabled":         k.Enabled,
		"allowed_aliases": k.AllowedAliases,
		"portable":        k.Portable,
	}
	if k.RateLimitRPM != nil {
		m["rate_limit_rpm"] = *k.RateLimitRPM
	}
	if k.MaxConcurrentRequests != nil {
		m["max_concurrent_requests"] = *k.MaxConcurrentRequests
	}
	if k.DailyRequestLimit != nil {
		m["daily_request_limit"] = *k.DailyRequestLimit
	}
	if k.DailyInputTokens != nil {
		m["daily_input_tokens"] = *k.DailyInputTokens
	}
	if k.DailyOutputTokens != nil {
		m["daily_output_tokens"] = *k.DailyOutputTokens
	}
	if k.DailyEstimatedCostUSD != nil {
		m["daily_estimated_cost_usd"] = *k.DailyEstimatedCostUSD
	}
	if k.MaxOutputTokens != nil {
		m["max_output_tokens"] = *k.MaxOutputTokens
	}
	if k.MaxRequestBody != nil {
		m["max_request_body"] = *k.MaxRequestBody
	}
	if k.ExpiresAt != nil {
		m["expires_at"] = k.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return m
}

func keyList() *cobra.Command {
	return &cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			keys, err := store.ListClientKeys(cmd.Context())
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			rows := make([]map[string]any, 0, len(keys))
			for _, k := range keys {
				row := keyPolicyMap(k)
				row["prefix"] = k.KeyPrefix + "…"
				rows = append(rows, row)
			}
			if flagJSON {
				return printJSON(rows)
			}
			for _, r := range rows {
				en := "enabled"
				if e, ok := r["enabled"].(bool); ok && !e {
					en = "disabled"
				}
				portable := ""
				if p, ok := r["portable"].(bool); ok && p {
					portable = " portable"
				}
				fmt.Printf("%s  %s  %s  %s%s\n", r["id"], r["name"], r["prefix"], en, portable)
			}
			return nil
		},
	}
}

func keyRotate() *cobra.Command {
	return &cobra.Command{
		Use: "rotate [id]", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			pt, prefix, hash, salt, err := credentials.GenerateClientKey()
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			if err := store.UpdateClientKeyHash(cmd.Context(), args[0], prefix, hash, salt); err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			if flagJSON {
				return printJSON(map[string]any{"id": args[0], "key": pt})
			}
			fmt.Printf("Rotated key %s\nNew secret: %s\n", args[0], pt)
			return nil
		},
	}
}

func keyDisable() *cobra.Command {
	return &cobra.Command{
		Use: "disable [id]", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			if err := store.DisableClientKey(cmd.Context(), args[0]); err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			return printOut(fmt.Sprintf("Disabled %s", args[0]), map[string]any{"disabled": args[0]})
		},
	}
}

func keyRemove() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use: "remove [id]", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return Exit(ExitInvalidConfig, fmt.Errorf("refusing without --yes"))
			}
			_, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			if err := store.RemoveClientKey(cmd.Context(), args[0]); err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			return printOut(fmt.Sprintf("Removed %s", args[0]), map[string]any{"removed": args[0]})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm")
	return cmd
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Inspect and manage configuration"}
	cmd.AddCommand(
		&cobra.Command{Use: "path", Short: "Print config directory", RunE: func(cmd *cobra.Command, args []string) error {
			h, err := homeDir()
			if err != nil {
				return err
			}
			return printOut(h, map[string]string{"home": h})
		}},
		&cobra.Command{Use: "show", Short: "Show config (secrets redacted)", RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			san := cfg.ExportSanitized()
			if flagJSON {
				return printJSON(san)
			}
			b, _ := yaml.Marshal(san)
			fmt.Print(string(b))
			return nil
		}},
		&cobra.Command{Use: "check", Short: "Validate configuration", RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			if err := cfg.Validate(); err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			return printOut("Configuration OK", map[string]any{"ok": true})
		}},
		&cobra.Command{Use: "export", Short: "Export sanitized configuration", RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			b, err := yaml.Marshal(cfg.ExportSanitized())
			if err != nil {
				return err
			}
			// Ensure no secret-looking material
			if strings.Contains(string(b), "sk-") || strings.Contains(string(b), "tr_live_") {
				return Exit(ExitGeneral, fmt.Errorf("export aborted: possible secret in output"))
			}
			fmt.Print(string(b))
			return nil
		}},
	)
	return cmd
}

func newLogsCmd() *cobra.Command {
	var errorsOnly bool
	var limit int
	var requestID string
	cmd := &cobra.Command{
		Use: "logs", Short: "Show recent request metadata logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, paths, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			recs, err := store.RecentRequests(cmd.Context(), limit, errorsOnly)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			if requestID != "" {
				filtered := recs[:0]
				for _, r := range recs {
					if r.ID == requestID {
						filtered = append(filtered, r)
					}
				}
				recs = filtered
			}
			if flagJSON {
				return printJSON(recs)
			}
			for _, r := range recs {
				label := ""
				if r.ClientLabel != "" {
					label = " client=" + r.ClientLabel
				}
				fmt.Printf("%s  %s  model=%s provider=%s status=%d lat=%dms stream=%v %s%s\n",
					r.Timestamp.Format(time.RFC3339), r.ID, r.RequestedModel, r.ProviderID, r.StatusCode, r.LatencyMs, r.Stream, r.ErrorClass, label)
			}
			if len(recs) == 0 {
				fmt.Printf("No request logs. File logs: %s/termrouter.log\n", paths.LogsDir)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&errorsOnly, "errors", false, "Only errors")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max rows")
	cmd.Flags().StringVar(&requestID, "request", "", "Filter by request id")
	return cmd
}

func newUsageCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "usage", Short: "Usage summaries"}
	cmd.AddCommand(
		&cobra.Command{Use: "today", RunE: usageSince(0)},
		&cobra.Command{Use: "summary", RunE: usageSince(7 * 24 * time.Hour)},
	)
	return cmd
}

func usageSince(d time.Duration) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		_, _, store, _, err := app.LoadRuntime(mustHome())
		if err != nil {
			return Exit(ExitInvalidConfig, err)
		}
		defer store.Close()
		since := time.Now().UTC().Truncate(24 * time.Hour)
		if d > 0 {
			since = time.Now().UTC().Add(-d)
		}
		sum, err := store.UsageSince(cmd.Context(), since)
		if err != nil {
			return Exit(ExitGeneral, err)
		}
		if flagJSON {
			return printJSON(sum)
		}
		fmt.Printf("Since %s\n", since.Format(time.RFC3339))
		fmt.Printf("Requests: %d  success=%d  errors=%d\n", sum.TotalRequests, sum.SuccessCount, sum.ErrorCount)
		fmt.Printf("Tokens:   in=%d out=%d\n", sum.InputTokens, sum.OutputTokens)
		fmt.Printf("Avg lat:  %.1f ms\n", sum.AvgLatencyMs)
		for p, n := range sum.ByProvider {
			fmt.Printf("  %s: %d\n", p, n)
		}
		return nil
	}
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use: "doctor", Short: "Run diagnostics (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := homeDir()
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			paths := config.ResolvePaths(root)
			issues := []string{}
			ok := []string{}

			if _, err := os.Stat(paths.Config); err != nil {
				issues = append(issues, "config.yaml missing — run termrouter init")
			} else {
				ok = append(ok, "config.yaml present")
				cfg, err := config.Load(paths.Config)
				if err != nil {
					issues = append(issues, "config invalid: "+err.Error())
				} else {
					ok = append(ok, fmt.Sprintf("config valid (%d providers, %d aliases)", len(cfg.Providers), len(cfg.Aliases)))
					if !cfg.Server.AuthRequired {
						issues = append(issues, "auth_required is false — only use for local development")
					}
					if cfg.PublicHosting.Enabled {
						ok = append(ok, "public_hosting enabled (loopback + reverse proxy expected)")
						if cfg.PublicHosting.ConsolePublic {
							issues = append(issues, "public_hosting.console_public must be false")
						}
						if !cfg.Server.AuthRequired {
							issues = append(issues, "public hosting requires auth_required: true")
						}
					}
				}
			}
			if _, err := os.Stat(paths.Database); err != nil {
				issues = append(issues, "router.db missing")
			} else {
				store, err := storage.Open(paths.Database)
				if err != nil {
					issues = append(issues, "database open failed: "+err.Error())
				} else {
					keys, _ := store.ListClientKeys(cmd.Context())
					ok = append(ok, fmt.Sprintf("database OK (%d client keys)", len(keys)))
					for _, k := range keys {
						if k.Portable && len(k.AllowedAliases) == 0 {
							issues = append(issues, fmt.Sprintf("portable key %s has no alias restriction", k.ID))
						}
						if k.Portable && k.MaxRequestBody == nil {
							issues = append(issues, fmt.Sprintf("portable key %s has no per-key request body limit", k.ID))
						}
						if k.Portable && k.DailyEstimatedCostUSD == nil {
							issues = append(issues, fmt.Sprintf("portable key %s has no daily estimated-spend budget", k.ID))
						}
					}
					store.Close()
				}
			}
			for _, d := range []string{paths.LogsDir, paths.RunDir} {
				if st, err := os.Stat(d); err != nil || !st.IsDir() {
					issues = append(issues, "missing dir "+d)
				}
			}

			out := map[string]any{"ok": ok, "issues": issues, "home": paths.Root}
			if flagJSON {
				return printJSON(out)
			}
			fmt.Printf("TermRouter doctor — %s\n", paths.Root)
			for _, s := range ok {
				fmt.Println("  ✓", s)
			}
			for _, s := range issues {
				fmt.Println("  ✗", s)
			}
			if len(issues) > 0 {
				return Exit(ExitPartial, fmt.Errorf("%d issue(s) found", len(issues)))
			}
			fmt.Println("All checks passed.")
			return nil
		},
	}
}

func newTestCmd() *cobra.Command {
	var stream bool
	var prompt string
	cmd := &cobra.Command{
		Use: "test [alias-or-model]", Short: "Send a test completion through the local server",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// This tests resolution + execute path in-process (no need for running server)
			cfg, paths, store, creds, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()
			_ = paths
			log, _ := observabilityQuiet()
			srv := app.New(cfg, paths, store, creds, log)
			_ = srv

			// Use HTTP against running server if possible, else in-process via coordinator
			model := args[0]
			if prompt == "" {
				prompt = "Reply with exactly: pong"
			}

			// Prefer live HTTP if server is up
			addr := cfg.Addr()
			keys, _ := store.FindEnabledKeys(cmd.Context())
			if len(keys) == 0 {
				return Exit(ExitAuth, fmt.Errorf("no client keys; run termrouter key create"))
			}
			// We don't have plaintext keys stored — user must pass TERMROUTER_TEST_KEY
			apiKey := os.Getenv("TERMROUTER_TEST_KEY")
			if apiKey == "" {
				return Exit(ExitAuth, fmt.Errorf("set TERMROUTER_TEST_KEY to a client key (tr_live_…) to run live tests against the server at %s", addr))
			}

			url := fmt.Sprintf("http://%s/v1/chat/completions", addr)
			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":%q}],"stream":%v,"max_tokens":64}`, model, prompt, stream)
			req, err := httpNewRequest(cmd.Context(), "POST", url, body)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			req.Header.Set("Authorization", "Bearer "+apiKey)
			req.Header.Set("Content-Type", "application/json")
			resp, err := httpDo(req)
			if err != nil {
				return Exit(ExitUnavailable, fmt.Errorf("request failed (is the server running?): %w", err))
			}
			defer resp.Body.Close()
			b, _ := readAll(resp.Body)
			if resp.StatusCode >= 400 {
				return Exit(ExitUnavailable, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b)))
			}
			if flagJSON {
				fmt.Print(string(b))
				return nil
			}
			fmt.Printf("OK HTTP %d\n%s\n", resp.StatusCode, string(b))
			return nil
		},
	}
	cmd.Flags().BoolVar(&stream, "stream", false, "Use streaming")
	cmd.Flags().StringVar(&prompt, "prompt", "", "User prompt")
	return cmd
}
