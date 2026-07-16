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
	cmd.AddCommand(keyCreate(), keyList(), keyRotate(), keyDisable(), keyRemove())
	return cmd
}

func keyCreate() *cobra.Command {
	var name string
	var aliases []string
	cmd := &cobra.Command{
		Use: "create", Short: "Create a client API key",
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
				Enabled: true, AllowedAliases: aliases, CreatedAt: time.Now().UTC(),
			}
			if err := store.InsertClientKey(cmd.Context(), k); err != nil {
				return Exit(ExitGeneral, err)
			}
			if flagJSON {
				return printJSON(map[string]any{
					"id": id, "name": name, "key": pt, "prefix": prefix,
					"note": "Save this key now; only its hash is retained.",
				})
			}
			fmt.Printf("Client key created: %s\n", pt)
			fmt.Printf("ID: %s  Name: %s\n", id, name)
			fmt.Println("Save it now; only its hash will be retained.")
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "default", "Key label")
	cmd.Flags().StringSliceVar(&aliases, "alias", nil, "Restrict to these aliases (repeatable)")
	return cmd
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
			type row struct {
				ID      string   `json:"id"`
				Name    string   `json:"name"`
				Prefix  string   `json:"prefix"`
				Enabled bool     `json:"enabled"`
				Aliases []string `json:"aliases,omitempty"`
			}
			rows := make([]row, 0, len(keys))
			for _, k := range keys {
				rows = append(rows, row{ID: k.ID, Name: k.Name, Prefix: k.KeyPrefix + "…", Enabled: k.Enabled, Aliases: k.AllowedAliases})
			}
			if flagJSON {
				return printJSON(rows)
			}
			for _, r := range rows {
				en := "enabled"
				if !r.Enabled {
					en = "disabled"
				}
				fmt.Printf("%s  %s  %s  %s\n", r.ID, r.Name, r.Prefix, en)
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
				fmt.Printf("%s  %s  model=%s provider=%s status=%d lat=%dms stream=%v %s\n",
					r.Timestamp.Format(time.RFC3339), r.ID, r.RequestedModel, r.ProviderID, r.StatusCode, r.LatencyMs, r.Stream, r.ErrorClass)
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
