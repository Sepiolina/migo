// migo creates, checks, runs and tests workflow files without HTTP. It is
// the repository's migo without the SQL Server driver: sql steps are
// answered by -mocks or -sql-stub (or fail when they run). See
// docs/CLI.md and "migo help".
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/Sepiolina/migo/engine/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	os.Exit(cli.Main(ctx, os.Args[1:], os.Stdout, os.Stderr, cli.Options{}))
}
