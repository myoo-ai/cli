// Command myoo bundles linters, refactoring helpers and renderers for AI
// agents into one binary.
package main

import (
	"os"

	"github.com/myoo-ai/cli/internal/app"
	"github.com/myoo-ai/cli/internal/command"
)

// version gets set at build time with -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	os.Exit(app.Run(os.Args[1:], &command.Env{
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Dir:     dir,
		Version: version,
	}))
}
