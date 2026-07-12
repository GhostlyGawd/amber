// amber — local-first, long-term memory for AI coding agents.
package main

import (
	"os"

	"github.com/ghostlygawd/amber/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
