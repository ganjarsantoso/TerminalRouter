package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/termrouter/termrouter/internal/app"
	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/observability"
)

func newServeCmd() *cobra.Command {
	var host string
	var port int
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the local API gateway",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, paths, store, creds, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, fmt.Errorf("load runtime: %w (run: termrouter init)", err))
			}
			defer store.Close()

			if host != "" {
				cfg.Server.Host = host
			}
			if port > 0 {
				cfg.Server.Port = port
			}

			log, err := observability.New(cfg.Logging.Level, paths.LogsDir)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			defer log.Close()

			srv := app.New(cfg, paths, store, creds, log)
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			if !flagJSON {
				fmt.Fprintf(os.Stderr, "TermRouter listening on http://%s\n", cfg.Addr())
				fmt.Fprintf(os.Stderr, "OpenAI base:  http://%s/v1\n", cfg.Addr())
				fmt.Fprintf(os.Stderr, "Anthropic:    http://%s\n", cfg.Addr())
			} else {
				_ = printJSON(map[string]any{"listening": cfg.Addr()})
			}
			if err := srv.Start(ctx); err != nil {
				return Exit(ExitGeneral, err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "Override listen host")
	cmd.Flags().IntVar(&port, "port", 0, "Override listen port")
	return cmd
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop a running TermRouter server (via pid file)",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := homeDir()
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			paths := config.ResolvePaths(root)
			data, err := os.ReadFile(paths.PIDFile)
			if err != nil {
				return Exit(ExitUnavailable, fmt.Errorf("not running (no pid file at %s)", paths.PIDFile))
			}
			pid, err := strconv.Atoi(string(data))
			if err != nil {
				return Exit(ExitGeneral, fmt.Errorf("invalid pid file"))
			}
			proc, err := os.FindProcess(pid)
			if err != nil {
				return Exit(ExitUnavailable, err)
			}
			if err := proc.Signal(syscall.SIGTERM); err != nil {
				return Exit(ExitUnavailable, fmt.Errorf("signal pid %d: %w", pid, err))
			}
			// wait briefly
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if err := proc.Signal(syscall.Signal(0)); err != nil {
					_ = os.Remove(paths.PIDFile)
					return printOut(fmt.Sprintf("Stopped pid %d", pid), map[string]any{"stopped": pid})
				}
				time.Sleep(100 * time.Millisecond)
			}
			return printOut(fmt.Sprintf("Sent SIGTERM to pid %d", pid), map[string]any{"signaled": pid})
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show server and provider status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, paths, store, _, err := app.LoadRuntime(mustHome())
			if err != nil {
				return Exit(ExitInvalidConfig, err)
			}
			defer store.Close()

			running := false
			pid := 0
			if data, err := os.ReadFile(paths.PIDFile); err == nil {
				if p, err := strconv.Atoi(string(data)); err == nil {
					if proc, err := os.FindProcess(p); err == nil {
						if err := proc.Signal(syscall.Signal(0)); err == nil {
							running = true
							pid = p
						}
					}
				}
			}

			health, _ := store.ListProviderHealth(cmd.Context())
			healthMap := map[string]any{}
			for _, h := range health {
				healthMap[h.ProviderID] = map[string]any{
					"circuit":    h.CircuitState,
					"failures":   h.ConsecutiveFailures,
					"last_error": h.LastError,
					"latency_ms": h.LastLatencyMs,
				}
			}
			aliases := make([]string, 0, len(cfg.Aliases))
			for a := range cfg.Aliases {
				aliases = append(aliases, a)
			}
			sanitized := make(map[string]config.SanitizedProviderConfig, len(cfg.Providers))
			for name, p := range cfg.Providers {
				sanitized[name] = config.SanitizeProviderConfig(p)
			}
			out := map[string]any{
				"address":   cfg.Addr(),
				"running":   running,
				"pid":       pid,
				"providers": sanitized,
				"aliases":   aliases,
				"health":    healthMap,
				"home":      paths.Root,
			}
			if flagJSON {
				return printJSON(out)
			}
			fmt.Printf("Address:  %s\n", cfg.Addr())
			fmt.Printf("Running:  %v", running)
			if pid > 0 {
				fmt.Printf(" (pid %d)", pid)
			}
			fmt.Println()
			fmt.Printf("Home:     %s\n", paths.Root)
			fmt.Printf("Aliases:  %v\n", aliases)
			fmt.Printf("Providers: %d configured\n", len(cfg.Providers))
			for name, sp := range sanitized {
				en := "enabled"
				if sp.Enabled != nil && !*sp.Enabled {
					en = "disabled"
				}
				fmt.Printf("  - %s (%s) %s\n", name, sp.Type, en)
			}
			if len(health) > 0 {
				fmt.Println("Health:")
				for _, h := range health {
					fmt.Printf("  - %s: circuit=%s failures=%d\n", h.ProviderID, h.CircuitState, h.ConsecutiveFailures)
				}
			}
			return nil
		},
	}
}

func mustHome() string {
	h, err := homeDir()
	if err != nil {
		return ""
	}
	return h
}
