// migo runs a workflow file on local files, without HTTP:
//
//	migo -in bom=Customer.xlsx -in priority=Prio.xlsx -out out.xlsx workflows/bom.yaml
//
// -check runs everything but keeps no output. -cfg takes config.json keys as
// JSON ("{}" skips DB and SAP); -stubdb stands in for Material_Master.
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

	"miservice/internal/clients/sap"
	"miservice/internal/config"
	"miservice/internal/jsx"
	"miservice/internal/modules/bom"
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
	stub := flags.String("stubdb", "", "JSON file standing in for Material_Master")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: migo [-in name=path ...] [-out file.xlsx | -check] [-cfg json] [-stubdb file] workflow.yaml")
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
	cfg, deps, err := loadConfig(*cfgJSON, *stub)
	if err != nil {
		return fail(2, err)
	}
	wf, err := workflow.Load(positional[0], workflow.TableOps().With(bom.Ops(deps)))
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
	log := func(s string) { fmt.Fprintln(stdout, "LOG:", s) }
	if err := wf.Run(ctx, cfg, log, inputs, tmp.Name()); err != nil {
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

func loadConfig(cfgJSON, stub string) (*config.Config, bom.Deps, error) {
	tmp, err := os.CreateTemp("", "cfg*.json")
	if err != nil {
		return nil, bom.Deps{}, err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.WriteString(cfgJSON)
	if err := errors.Join(writeErr, tmp.Close()); err != nil {
		return nil, bom.Deps{}, err
	}
	cfg, err := config.Load(tmp.Name())
	if err != nil {
		return nil, bom.Deps{}, err
	}
	deps := bom.Deps{SAP: &sap.Client{}}
	if stub != "" {
		b, err := os.ReadFile(stub)
		if err != nil {
			return nil, bom.Deps{}, err
		}
		var m map[string]struct{ MaterialNo, ZBlock string }
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, bom.Deps{}, err
		}
		deps.Lookup = func(_ context.Context, mpns []string) (map[string]bom.Match, error) {
			res := map[string]bom.Match{}
			for _, x := range mpns {
				if v, ok := m[jsx.Norm(x)]; ok {
					res[jsx.Norm(x)] = bom.Match{MaterialNo: v.MaterialNo, ZBlock: v.ZBlock}
				}
			}
			return res, nil
		}
	}
	return cfg, deps, nil
}
