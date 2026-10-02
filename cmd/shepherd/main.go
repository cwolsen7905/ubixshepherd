// Command shepherd is uBixShepherd: the daemon, its CLI, and (later) the MCP server, in
// one binary.
package main

import (
	"os"

	"github.com/ubixsys/ubixshepherd/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args[1:])) }
