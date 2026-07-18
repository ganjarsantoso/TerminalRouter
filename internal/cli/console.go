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
	"github.com/termrouter/termrouter/internal/console"
	"github.com/termrouter/termrouter/internal/observability"
)

func newConsoleCmd() *cobra.Command {
	var (
		host   string
		port   int
		noOpen bool
	)
	cmd := &cobra.Command{
		Use:   "console",
		Short: "Start the optional local TermRouter Console (management UI)",
		Long: `Start the TermRouter Console — a local-only management Web UI.

The Console binds to loopback by default (127.0.0.1:8788) and requires a
one-time bootstrap login URL printed to the terminal. It never exposes
provider secrets to the browser.

When the gateway is not already running, the Console also starts the gateway
in the same process so setup and testing work end-to-end.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := homeDir()
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			cfg, paths, store, creds, err := app.LoadRuntime(home)
			if err != nil {
				return Exit(ExitInvalidConfig, fmt.Errorf("load runtime: %w (run: termrouter init)", err))
			}
			// store is re-opened by console.New; close this temporary handle
			_ = store.Close()

			log, err := observability.New(cfg.Logging.Level, paths.LogsDir)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			defer log.Close()

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Start gateway if not already running (same process).
			var gw *app.Server
			gatewayAlreadyRunning := pidRunning(paths.PIDFile)
			if !gatewayAlreadyRunning {
				// Re-open runtime for the gateway.
				cfg2, paths2, store2, creds2, err := app.LoadRuntime(home)
				if err != nil {
					return Exit(ExitInvalidConfig, err)
				}
				gw = app.New(cfg2, paths2, store2, creds2, log)
				go func() {
					if err := gw.Start(ctx); err != nil && ctx.Err() == nil {
						fmt.Fprintf(os.Stderr, "gateway error: %v\n", err)
					}
				}()
				// Give the gateway a moment to bind.
				time.Sleep(150 * time.Millisecond)
			}

			cs, err := console.New(console.Options{
				Home:   home,
				Port:   port,
				Host:   host,
				NoOpen: noOpen,
				App:    gw,
				Creds:  creds,
				Log:    log,
			})
			if err != nil {
				return Exit(ExitGeneral, err)
			}

			if !flagJSON {
				fmt.Fprintln(os.Stderr, "TERMROUTER CONSOLE")
				fmt.Fprintln(os.Stderr)
				fmt.Fprintf(os.Stderr, "Gateway:  http://%s\n", cfg.Addr())
				fmt.Fprintf(os.Stderr, "Console:  http://%s:%d\n", cs.Host, cs.Port)
				fmt.Fprintln(os.Stderr, "Mode:     local only")
				fmt.Fprintln(os.Stderr)
				fmt.Fprintln(os.Stderr, "A one-time login URL has been generated.")
				if !noOpen {
					fmt.Fprintln(os.Stderr, "Opening the default browser...")
				}
				fmt.Fprintln(os.Stderr)
				fmt.Fprintf(os.Stderr, "Login:    %s\n", cs.BootstrapURL())
				fmt.Fprintln(os.Stderr)
				fmt.Fprintln(os.Stderr, "Press Ctrl+C to stop the Console.")
			} else {
				_ = printJSON(map[string]any{
					"gateway": cfg.Addr(),
					"console": fmt.Sprintf("%s:%d", cs.Host, cs.Port),
					"login":   cs.BootstrapURL(),
					"mode":    "local only",
				})
			}

			if err := cs.Start(ctx); err != nil {
				return Exit(ExitGeneral, err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "Console bind host (loopback only)")
	cmd.Flags().IntVar(&port, "port", 8788, "Console bind port")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Do not open the default browser")

	cmd.AddCommand(consoleStatusCmd(), consoleStopCmd())
	return cmd
}

func consoleStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Console process status",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := homeDir()
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			paths := config.ResolvePaths(root)
			pidPath := paths.RunDir + "/console.pid"
			data, err := os.ReadFile(pidPath)
			if err != nil {
				return printOut("Console is not running", map[string]any{"running": false})
			}
			pid, _ := strconv.Atoi(string(data))
			running := false
			if pid > 0 {
				if proc, err := os.FindProcess(pid); err == nil {
					if err := proc.Signal(syscall.Signal(0)); err == nil {
						running = true
					}
				}
			}
			return printOut(
				fmt.Sprintf("Console pid %d running=%v", pid, running),
				map[string]any{"running": running, "pid": pid},
			)
		},
	}
}

func consoleStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop a running Console process (via pid file)",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := homeDir()
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			paths := config.ResolvePaths(root)
			pidPath := paths.RunDir + "/console.pid"
			data, err := os.ReadFile(pidPath)
			if err != nil {
				return Exit(ExitUnavailable, fmt.Errorf("console not running (no pid file)"))
			}
			pid, err := strconv.Atoi(string(data))
			if err != nil {
				return Exit(ExitGeneral, fmt.Errorf("invalid console pid file"))
			}
			proc, err := os.FindProcess(pid)
			if err != nil {
				return Exit(ExitUnavailable, err)
			}
			if err := proc.Signal(syscall.SIGTERM); err != nil {
				return Exit(ExitUnavailable, fmt.Errorf("signal pid %d: %w", pid, err))
			}
			_ = os.Remove(pidPath)
			return printOut(fmt.Sprintf("Stopped console pid %d", pid), map[string]any{"stopped": pid})
		},
	}
}

func pidRunning(pidFile string) bool {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil || pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
