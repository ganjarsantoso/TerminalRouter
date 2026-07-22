package config

import (
	"os"
	"path/filepath"
)

// Paths holds the TermRouter filesystem layout under the config root.
type Paths struct {
	Root     string
	Config   string
	Database string
	Vault    string
	LogsDir  string
	RunDir   string
	PIDFile  string
	Socket   string
}

// DefaultRoot returns the default configuration directory (~/.termrouter).
func DefaultRoot() (string, error) {
	if v := os.Getenv("TERMROUTER_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".termrouter"), nil
}

// ResolvePaths builds the full path set for a config root.
func ResolvePaths(root string) Paths {
	return Paths{
		Root:     root,
		Config:   filepath.Join(root, "config.yaml"),
		Database: filepath.Join(root, "router.db"),
		Vault:    filepath.Join(root, "vault.db"),
		LogsDir:  filepath.Join(root, "logs"),
		RunDir:   filepath.Join(root, "run"),
		PIDFile:  filepath.Join(root, "run", "termrouter.pid"),
		Socket:   filepath.Join(root, "run", "admin.sock"),
	}
}

// EnsureDirs creates config, logs, and run directories with safe permissions.
func EnsureDirs(p Paths) error {
	for _, dir := range []string{p.Root, p.LogsDir, p.RunDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}
