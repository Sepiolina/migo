// migo creates, checks, runs and tests workflow files without HTTP:
//
//	migo new order-totals -template parts-summary
//	migo validate workflows/order-totals.yaml
//	migo run -in parts=examples/parts.xlsx -out out.xlsx workflows/order-totals.yaml
//	migo test examples
//
// The command line itself is the engine module's cli package; this program
// adds the SQL Server connection MaiService uses (the db_* keys of the
// -cfg configuration) for sql steps. "migo help" lists every command;
// docs/CLI.md is the guide.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/Sepiolina/migo/engine/cli"
	"github.com/Sepiolina/migo/engine/config"
	"github.com/Sepiolina/migo/engine/workflow"
	"miservice/internal/clients/sqlserver"
	"miservice/internal/modules/workflows"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	os.Exit(cli.Main(ctx, os.Args[1:], os.Stdout, os.Stderr, cli.Options{SQL: sqlServer}))
}

// sqlServer runs sql steps on SQL Server, as MaiService does.
func sqlServer(cfg *config.Config) (workflow.SQLFunc, func(), error) {
	db := sqlserver.New(sqlserver.Config{
		Server: cfg.DB.Server, Port: cfg.DB.Port, User: cfg.DB.User,
		Password: cfg.DB.Password, Database: cfg.DB.Name, TLSMin: cfg.DB.TLSMin,
	})
	return workflows.SQL(db), func() { _ = db.Close() }, nil
}
