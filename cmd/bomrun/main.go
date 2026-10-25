// bomrun executes a declarative workflow or the original two-file BOM pipeline.
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

	"miservice/internal/clients/sap"
	"miservice/internal/config"
	"miservice/internal/jsx"
	"miservice/internal/modules/bom"
	"miservice/internal/workflow"

	"gopkg.in/yaml.v3"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("bomrun", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bomF := flags.String("bom", "", "customer BOM (.xls/.xlsx)")
	prioF := flags.String("prio", "", "priority workbook")
	out := flags.String("out", "out.xlsx", "legacy output .xlsx")
	cfgJSON := flags.String("cfg", "{}", "BOM runtime config as JSON (keys like config.json)")
	stub := flags.String("stubdb", "", "JSON file standing in for Material_Master")
	recipe := flags.String("workflow", "", "declarative YAML workflow file")
	check := flags.Bool("check", false, "validate and evaluate -workflow without publishing outputs")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(code int, err error) int {
		fmt.Fprintln(stderr, "ERR", err)
		return code
	}
	if flags.NArg() != 0 {
		return fail(2, fmt.Errorf("unexpected arguments: %v", flags.Args()))
	}
	if *recipe != "" {
		engine, err := recipeEngine(*recipe)
		if err != nil {
			return fail(2, err)
		}
		var conflicts []string
		flags.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "bom", "prio", "out":
				conflicts = append(conflicts, "-"+f.Name)
			case "cfg", "stubdb":
				if engine != "bom" {
					conflicts = append(conflicts, "-"+f.Name)
				}
			}
		})
		if len(conflicts) > 0 {
			return fail(2, fmt.Errorf("-workflow cannot be combined with legacy options %v; use workflow input/output settings", conflicts))
		}
		if engine == "bom" {
			spec, err := bom.LoadRecipe(*recipe)
			if err != nil {
				return fail(2, err)
			}
			cfg, deps, err := loadBOMConfig(*cfgJSON, *stub)
			if err != nil {
				return fail(2, err)
			}
			log := func(s string) { fmt.Fprintln(stdout, "LOG:", s) }
			if *check {
				err = spec.Check(ctx, cfg, log, deps)
			} else {
				err = spec.Run(ctx, cfg, log, deps)
			}
			if err != nil {
				return fail(1, err)
			}
			if *check {
				fmt.Fprintln(stdout, "workflow checked; no outputs published")
			} else {
				fmt.Fprintln(stdout, "done")
			}
			return 0
		}
		cfg, err := workflow.Load(*recipe)
		if err != nil {
			return fail(2, err)
		}
		if *check {
			if err := workflow.Check(ctx, cfg); err != nil {
				return fail(1, err)
			}
			fmt.Fprintln(stdout, "workflow checked; no outputs published")
		} else {
			if err := workflow.Run(ctx, cfg); err != nil {
				return fail(1, err)
			}
			fmt.Fprintln(stdout, "done")
		}
		return 0
	}
	if *check {
		return fail(2, fmt.Errorf("-check requires -workflow"))
	}

	cfg, deps, err := loadBOMConfig(*cfgJSON, *stub)
	if err != nil {
		return fail(2, err)
	}

	p := bom.NewPipeline(cfg, func(s string) { fmt.Fprintln(stdout, "LOG:", s) }, deps)
	if err := p.Process(ctx, *bomF, *prioF, *out); err != nil {
		return fail(1, err)
	}
	fmt.Fprintln(stdout, "done")
	return 0
}

// The engine selector only dispatches; each engine then strictly validates the
// complete document, including duplicates, unknown fields and extra documents.
func recipeEngine(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var header struct {
		Engine string `yaml:"engine"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		return "", err
	}
	if header.Engine != "" && header.Engine != "bom" {
		return "", fmt.Errorf("unknown workflow engine %q", header.Engine)
	}
	return header.Engine, nil
}

func loadBOMConfig(cfgJSON, stub string) (*config.Config, bom.Deps, error) {

	tmp, err := os.CreateTemp("", "cfg*.json")
	if err != nil {
		return nil, bom.Deps{}, err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.WriteString(cfgJSON)
	closeErr := tmp.Close()
	if writeErr != nil {
		return nil, bom.Deps{}, writeErr
	}
	if closeErr != nil {
		return nil, bom.Deps{}, closeErr
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
