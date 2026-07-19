package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/credentials"
	"github.com/termrouter/termrouter/internal/storage"
)

func newInitCmd() *cobra.Command {
	var (
		backend   string
		createKey bool
		force     bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize TermRouter configuration and state",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := homeDir()
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			paths := config.ResolvePaths(root)
			if _, err := os.Stat(paths.Config); err == nil && !force {
				return Exit(ExitConflict, fmt.Errorf("already initialized at %s (use --force to reinitialize config carefully)", paths.Root))
			}
			if err := config.EnsureDirs(paths); err != nil {
				return Exit(ExitGeneral, err)
			}
			if backend == "" {
				backend = "vault"
			}
			switch backend {
			case "keyring", "vault", "env":
			default:
				return Exit(ExitInvalidConfig, fmt.Errorf("invalid --backend %q (keyring|vault|env)", backend))
			}
			cfg := config.Default()
			cfg.Credentials.Backend = backend
			if err := config.Save(paths.Config, cfg); err != nil {
				return Exit(ExitGeneral, err)
			}
			store, err := storage.Open(paths.Database)
			if err != nil {
				return Exit(ExitGeneral, err)
			}
			defer store.Close()

			// Ensure vault manager can init
			if _, err := credentials.NewManager(backend, paths.Vault, os.Getenv("TERMROUTER_VAULT_PASSPHRASE")); err != nil {
				return Exit(ExitGeneral, fmt.Errorf("credential backend init: %w", err))
			}

			result := map[string]any{
				"home":               paths.Root,
				"config":             paths.Config,
				"database":           paths.Database,
				"credential_backend": backend,
			}

			var plaintext string
			if createKey {
				pt, prefix, hash, salt, err := credentials.GenerateClientKey()
				if err != nil {
					return Exit(ExitGeneral, err)
				}
				plaintext = pt
				id := "key_" + randomHex(8)
				if err := store.InsertClientKey(cmd.Context(), storage.ClientKey{
					ID: id, Name: "default", KeyPrefix: prefix, KeyHash: hash, Salt: salt,
					Enabled: true, CreatedAt: time.Now().UTC(),
				}); err != nil {
					return Exit(ExitGeneral, err)
				}
				result["client_key_id"] = id
				result["client_key"] = plaintext
				result["client_key_note"] = "Save this key now; only its hash is retained."
			}

			if flagJSON {
				return printJSON(result)
			}
			fmt.Printf("Configuration directory: %s\n", paths.Root)
			fmt.Printf("Credential backend: %s\n", backend)
			if plaintext != "" {
				fmt.Printf("Client key created: %s\n", plaintext)
				fmt.Println("Save it now; only its hash will be retained.")
			}
			fmt.Println()
			fmt.Println("Next:")
			fmt.Println("  termrouter provider add --name openai-main --type openai")
			fmt.Println("  termrouter alias add coding --provider openai-main --model <provider>/<model>")
			fmt.Println("  termrouter serve")
			return nil
		},
	}
	cmd.Flags().StringVar(&backend, "backend", "vault", "Credential backend: keyring, vault, or env")
	cmd.Flags().BoolVar(&createKey, "create-key", true, "Create the first client API key")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing config.yaml")
	return cmd
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
