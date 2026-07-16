package main

import (
	"os"

	"github.com/termrouter/termrouter/internal/cli"
)

// version is set via -ldflags at release build time.
var version = "0.1.0-dev"

func main() {
	if err := cli.Execute(version); err != nil {
		os.Exit(cli.ExitCode(err))
	}
}
