// migo runs a workflow file on local files, without HTTP:
//
//	migo -in bom=Customer.xlsx -in priority=Prio.xlsx -out out.xlsx workflows/bom.yaml
//
// -check runs everything but keeps no output. -cfg takes config.json keys as
// JSON ("{}" skips DB and SAP); -sql-stub stands in for the database.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"miservice/internal/clients/sqlserver"
	"miservice/internal/config"
	"miservice/internal/modules/workflows"
	"miservice/internal/workflow"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

type inputFlags map[string]string

func (f inputFlags) String() string { return "" }
func (f inputFlags) Set(v string) error {
	name, path, ok := strings.Cut(v, "=")
	if !ok || name == "" || path == "" {
		return fmt.Errorf("use -in name=path")
	}
	if _, dup := f[name]; dup {
		return fmt.Errorf("input %q given twice", name)
	}
	f[name] = path
	return nil
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("migo", flag.ContinueOnError)
	flags.SetOutput(stderr)
	inputs := inputFlags{}
	flags.Var(inputs, "in", "input file as name=path (repeat per workflow input)")
	out := flags.String("out", "", "output .xlsx (required unless -check)")
	check := flags.Bool("check", false, "run the workflow but keep no output")
	cfgJSON := flags.String("cfg", "{}", "runtime config as JSON (keys like config.json)")
	stub := flags.String("sql-stub", "", `JSON table every sql step returns: {"columns": [...], "rows": [[...]]}`)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: migo [-in name=path ...] [-out file.xlsx | -check] [-cfg json] [-sql-stub file] workflow.yaml")
		flags.PrintDefaults()
	}
	// Accept flags before and after the workflow path.
	var positional []string
	for {
		if err := flags.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}
		if flags.NArg() == 0 {
			break
		}
		positional, args = append(positional, flags.Arg(0)), flags.Args()[1:]
	}
	fail := func(code int, err error) int {
		fmt.Fprintln(stderr, "ERR", err)
		return code
	}
	if len(positional) != 1 {
		return fail(2, fmt.Errorf("expected one workflow file, got %d", len(positional)))
	}
	if (*out == "") == !*check {
		return fail(2, fmt.Errorf("give either -out or -check"))
	}
	rt, closeDB, err := runtime(*cfgJSON, *stub, func(s string) { fmt.Fprintln(stdout, "LOG:", s) })
	if err != nil {
		return fail(2, err)
	}
	defer closeDB()
	wf, err := workflow.Load(positional[0])
	if err != nil {
		return fail(2, err)
	}
	for _, in := range wf.Inputs {
		if inputs[in.Name] == "" {
			return fail(2, fmt.Errorf("missing -in %s=<file> (%s)", in.Name, in.Label))
		}
	}
	if len(inputs) != len(wf.Inputs) {
		return fail(2, fmt.Errorf("workflow has %d input(s), got %d", len(wf.Inputs), len(inputs)))
	}

	dir := os.TempDir()
	if !*check {
		if !strings.EqualFold(filepath.Ext(*out), ".xlsx") {
			return fail(2, fmt.Errorf("-out must be an .xlsx file"))
		}
		for _, p := range inputs {
			if samePath(p, *out) {
				return fail(2, fmt.Errorf("-out would overwrite input %s", p))
			}
		}
		dir = filepath.Dir(*out)
	}
	tmp, err := os.CreateTemp(dir, ".migo-*.xlsx")
	if err != nil {
		return fail(1, err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	if err := wf.Run(ctx, rt, inputs, tmp.Name()); err != nil {
		return fail(1, err)
	}
	if *check {
		fmt.Fprintln(stdout, "workflow checked; no output kept")
		return 0
	}
	// CreateTemp makes the file 0600; give the result normal permissions.
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fail(1, err)
	}
	if err := os.Rename(tmp.Name(), *out); err != nil {
		return fail(1, err)
	}
	fmt.Fprintln(stdout, "done:", *out)
	return 0
}

func samePath(a, b string) bool {
	ai, errA := os.Stat(a)
	bi, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(ai, bi)
	}
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	return aa == bb
}

// runtime builds the run's configuration and connections. With stub, every
// sql step returns the table in that JSON file ({"columns": [...], "rows":
// [[...]]}); otherwise sql steps use the db_* settings of the config.
func runtime(cfgJSON, stub string, log func(string)) (workflow.Runtime, func(), error) {
	rt := workflow.Runtime{Log: log}
	tmp, err := os.CreateTemp("", "cfg*.json")
	if err != nil {
		return rt, nil, err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.WriteString(cfgJSON)
	if err := errors.Join(writeErr, tmp.Close()); err != nil {
		return rt, nil, err
	}
	if rt.Cfg, err = config.Load(tmp.Name()); err != nil {
		return rt, nil, err
	}
	if stub != "" {
		b, err := os.ReadFile(stub)
		if err != nil {
			return rt, nil, err
		}
		var t workflow.Table
		if err := json.Unmarshal(b, &t); err != nil {
			return rt, nil, fmt.Errorf("%s: %w", stub, err)
		}
		rt.SQL = func(context.Context, string, string, []any) (*workflow.Table, error) { return &t, nil }
		return rt, func() {}, nil
	}
	db := sqlserver.New(sqlserver.Config{
		Server: rt.Cfg.DB.Server, Port: rt.Cfg.DB.Port, User: rt.Cfg.DB.User,
		Password: rt.Cfg.DB.Password, Database: rt.Cfg.DB.Name, TLSMin: rt.Cfg.DB.TLSMin,
	})
	rt.SQL = workflows.SQL(db)
	return rt, func() { _ = db.Close() }, nil
}
