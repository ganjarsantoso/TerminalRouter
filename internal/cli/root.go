package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	flagHome    string
	flagJSON    bool
	appVersion  string
)

// Execute runs the root command.
func Execute(version string) error {
	appVersion = version
	root := &cobra.Command{
		Use:           "termrouter",
		Short:         "Terminal-only multi-provider AI API gateway and protocol router",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().StringVar(&flagHome, "home", "", "TermRouter home directory (default: $TERMROUTER_HOME or ~/.termrouter)")
	root.PersistentFlags().BoolVar(&flagJSON, "json", false, "Machine-readable JSON output")

	root.AddCommand(
		newInitCmd(),
		newServeCmd(),
		newStopCmd(),
		newStatusCmd(),
		newDoctorCmd(),
		newProviderCmd(),
		newAliasCmd(),
		newRouteCmd(),
		newModelCmd(),
		newSmartCmd(),
		newExplainCmd(),
		newKeyCmd(),
		newConfigCmd(),
		newLogsCmd(),
		newUsageCmd(),
		newTestCmd(),
		newConsoleCmd(),
		newVersionCmd(),
	)
	return root.Execute()
}

func homeDir() (string, error) {
	if flagHome != "" {
		return flagHome, nil
	}
	if v := os.Getenv("TERMROUTER_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".termrouter"), nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printOut(human string, v any) error {
	if flagJSON {
		return printJSON(v)
	}
	fmt.Fprintln(os.Stdout, human)
	return nil
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagJSON {
				return printJSON(map[string]string{"version": appVersion})
			}
			fmt.Println(appVersion)
			return nil
		},
	}
}
