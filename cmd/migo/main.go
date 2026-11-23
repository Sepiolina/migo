// migo creates, checks, runs and tests workflow files without HTTP:
//
//	migo new order-totals -template parts-summary
//	migo validate workflows/order-totals.yaml
//	migo run -in parts=examples/parts.xlsx -out out.xlsx workflows/order-totals.yaml
//	migo test examples
//
// "migo help" lists every command; docs/CLI.md is the guide. For run, -check
// runs everything but keeps no output, -cfg takes config.json keys as JSON
// ("{}" skips DB and SAP) and -sql-stub stands in for the database.
package main

import (
	"bytes"
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
	"miservice/internal/mock"
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
	if len(args) > 0 {
		switch name := args[0]; {
		case generators[name] != nil:
			return generate(name, args[1:], stdout, stderr)
		case commands[name] != nil:
			return commands[name](ctx, args[1:], stdout, stderr)
		case name == "run":
			args = args[1:]
		}
	} else {
		fmt.Fprint(stderr, usage)
		return 2
	}
	flags := flag.NewFlagSet("migo", flag.ContinueOnError)
	flags.SetOutput(stderr)
	inputs := inputFlags{}
	flags.Var(inputs, "in", "input file as name=path (repeat per workflow input)")
	out := flags.String("out", "", "output file, .xlsx or .csv as the workflow writes (required unless -check)")
	check := flags.Bool("check", false, "run the workflow but keep no output")
	cfgJSON := flags.String("cfg", "{}", "runtime config as JSON (keys like config.json)")
	stub := flags.String("sql-stub", "", `JSON table every sql step returns: {"columns": [...], "rows": [[...]]}`)
	mocks := flags.String("mocks", "", "answer sql and rest steps from this mocks file instead of real systems")
	record := flags.String("record", "", "write the sql results and rest responses of this run to a mocks file")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: migo run [-in name=path ...] [-out file | -check] [-cfg json] [-sql-stub file | -mocks file | -record file] workflow.yaml")
		flags.PrintDefaults()
		fmt.Fprintln(stderr, "\nOther commands: migo help")
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
	if *mocks != "" && (*record != "" || *stub != "") {
		return fail(2, fmt.Errorf("-mocks replaces real systems: do not combine it with -record or -sql-stub"))
	}
	rt, closeDB, err := runtime(*cfgJSON, *stub, func(s string) { fmt.Fprintln(stdout, "LOG:", s) })
	if err != nil {
		return fail(2, err)
	}
	defer closeDB()
	if *mocks != "" {
		set, err := mock.Load(*mocks)
		if err != nil {
			return fail(2, err)
		}
		var keys map[string]any
		if err := json.Unmarshal([]byte(*cfgJSON), &keys); err != nil {
			return fail(2, fmt.Errorf("-cfg: %w", err))
		}
		log := rt.Log
		if rt, err = set.Runtime(keys); err != nil {
			return fail(2, err)
		}
		rt.Log = log
	}
	var recorded *mock.Set
	if *record != "" {
		recorded = mock.Record(&rt)
	}
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
		if !strings.EqualFold(filepath.Ext(*out), wf.Ext()) {
			return fail(2, fmt.Errorf("-out must be an %s file", wf.Ext()))
		}
		for _, p := range inputs {
			if samePath(p, *out) {
				return fail(2, fmt.Errorf("-out would overwrite input %s", p))
			}
		}
		dir = filepath.Dir(*out)
	}
	tmp, err := os.CreateTemp(dir, ".migo-*"+wf.Ext())
	if err != nil {
		return fail(1, err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	if err := wf.Run(ctx, rt, inputs, tmp.Name()); err != nil {
		return fail(1, err)
	}
	if recorded != nil {
		if err := recorded.Save(*record); err != nil {
			return fail(1, err)
		}
		fmt.Fprintf(stdout, "recorded %d sql result(s) and %d rest response(s) to %s\n", len(recorded.SQL), len(recorded.REST), *record)
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

// generators are the commands that write a file derived from the step
// descriptors: the catalog Mai Studio builds its forms from, the JSON Schema
// editors complete workflow files with, and the Markdown step reference.
var generators = map[string]func() ([]byte, error){
	"describe": func() ([]byte, error) { return indentJSON(workflow.Describe()) },
	"schema":   func() ([]byte, error) { return indentJSON(workflow.Schema()) },
	"docs":     func() ([]byte, error) { return []byte(workflow.Reference()), nil },
}

func generate(name string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("migo "+name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	out := flags.String("out", "", "write to this file instead of standard output")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "ERR %s takes no arguments\n", name)
		return 2
	}
	data, err := generators[name]()
	if err == nil {
		if *out == "" {
			_, err = stdout.Write(data)
		} else {
			err = os.WriteFile(*out, data, 0o644)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "ERR", err)
		return 1
	}
	return 0
}

func indentJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(v)
	return buf.Bytes(), err
}
