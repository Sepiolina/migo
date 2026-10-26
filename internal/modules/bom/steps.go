package bom

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v3"

	"miservice/internal/clients/sap"
	"miservice/internal/clients/sqlserver"
	"miservice/internal/config"
	"miservice/internal/logx"
	"miservice/internal/workflow"
)

// Job is the BOM state the steps share during one run (State.Data["bom"]).
type Job struct {
	Cfg      *config.Config
	Policy   *VendorPolicy // never nil; empty until vendor_policy runs
	Log      logx.Logger
	Rows     []*Row
	Priority PriorityMap
}

// Deps are the external systems the steps talk to. All are optional: with
// neither Lookup nor DB there is no database, a nil SAP uses the default HTTP
// client. Lookup, when set, replaces the database (tests, migo -stubdb).
type Deps struct {
	DB     *sqlserver.Pool
	Lookup LookupFunc
	SAP    *sap.Client
}

// Ops are the BOM workflow steps (see workflows/bom.yaml). Needs/After
// encode what each step reads from the earlier ones; each runs at most once.
func Ops(d Deps) workflow.Registry {
	ops := workflow.Registry{
		"read_bom":      {Compile: inputStep("bom", readBOM)},
		"read_priority": {Compile: inputStep("priority", readPriority), Needs: []string{"read_bom"}},
		"group_rows":    {Compile: rowsStep(func(_ context.Context, j *Job) { ProcessGrouping(j.Rows) }), Needs: []string{"read_bom"}},
		// Scope needs the forward-filled descriptions from group_rows.
		"pcb_scope":     {Compile: rowsStep(func(_ context.Context, j *Job) { markScope(j.Rows) }), Needs: []string{"group_rows"}},
		"pcb_reformat":  {Compile: rowsStep(func(_ context.Context, j *Job) { reformatPcbRows(j.Rows, j.Log) }), Needs: []string{"pcb_scope"}},
		"vendor_policy": {Compile: compileVendorPolicy, Needs: []string{"pcb_reformat"}},
		// material_master needs the CFT-001 verdicts (IsForbidden).
		"material_master": {
			Compile: specStep(MasterSpec.compile, func(ctx context.Context, j *Job, m *master) { m.lookupForthPNs(ctx, j, d) }),
			Needs:   []string{"vendor_policy"},
		},
		// sap_enrich needs the Forth P/Ns from material_master.
		"sap_enrich": {
			Compile: specStep(SAPSpec.compile, func(ctx context.Context, j *Job, st *sapStep) { st.enrich(ctx, j, d.SAP) }),
			Needs:   []string{"material_master"},
		},
		// CFT-002 needs priorities, vendors and stock from everything above.
		"select_priority": {
			Compile: specStep(SelectSpec.compile, func(_ context.Context, j *Job, c *Cft002) { RunCft002(j.Rows, j.Priority, c, j.Policy, j.Log) }),
			Needs:   []string{"read_priority", "pcb_scope", "vendor_policy"},
			After:   []string{"material_master", "sap_enrich"},
		},
		"write_bom": {Compile: noArg(writeBOM), Needs: []string{"read_bom"}, Writes: true},
	}
	for name, op := range ops {
		op.Once = true
		ops[name] = op
	}
	return ops
}

func job(s *workflow.State) (*Job, error) {
	j, ok := s.Data["bom"].(*Job)
	if !ok {
		return nil, fmt.Errorf("read_bom must run first")
	}
	return j, nil
}

// inputStep compiles "read_x" or "read_x: <input name>".
func inputStep(def string, run func(s *workflow.State, path string) error) func(*yaml.Node, *workflow.Scope) (workflow.Runner, error) {
	return func(arg *yaml.Node, sc *workflow.Scope) (workflow.Runner, error) {
		name := def
		if arg != nil {
			if arg.Kind != yaml.ScalarNode || arg.Value == "" {
				return nil, fmt.Errorf("expects an input name")
			}
			name = arg.Value
		}
		if err := sc.Input(name); err != nil {
			return nil, err
		}
		return func(_ context.Context, s *workflow.State) error { return run(s, s.Inputs[name]) }, nil
	}
}

func noArg(run workflow.Runner) func(*yaml.Node, *workflow.Scope) (workflow.Runner, error) {
	return func(arg *yaml.Node, _ *workflow.Scope) (workflow.Runner, error) {
		if arg != nil {
			return nil, fmt.Errorf("takes no argument")
		}
		return run, nil
	}
}

// rowsStep adapts a function of the Job that cannot fail (optional
// enrichments log and carry on) to a step.
func rowsStep(fn func(ctx context.Context, j *Job)) func(*yaml.Node, *workflow.Scope) (workflow.Runner, error) {
	return noArg(func(ctx context.Context, s *workflow.State) error {
		j, err := job(s)
		if err != nil {
			return err
		}
		fn(ctx, j)
		return nil
	})
}

// specStep compiles a step whose rules are its YAML argument (decoded into
// S and compiled to C at load time) and runs fn with them.
func specStep[S any, C any](compile func(S) (C, error), fn func(ctx context.Context, j *Job, c C)) func(*yaml.Node, *workflow.Scope) (workflow.Runner, error) {
	return func(arg *yaml.Node, _ *workflow.Scope) (workflow.Runner, error) {
		if arg == nil {
			return nil, fmt.Errorf("needs its rules as an argument")
		}
		var spec S
		if err := workflow.Decode(arg, &spec); err != nil {
			return nil, err
		}
		c, err := compile(spec)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, s *workflow.State) error {
			j, err := job(s)
			if err != nil {
				return err
			}
			fn(ctx, j, c)
			return nil
		}, nil
	}
}

func readBOM(s *workflow.State, path string) error {
	rows, err := ReadBom(path, s.Log)
	if err != nil {
		return err
	}
	s.Data["bom"] = &Job{Cfg: s.Cfg, Policy: &VendorPolicy{}, Log: s.Log, Rows: rows}
	return nil
}

func readPriority(s *workflow.State, path string) error {
	j, err := job(s)
	if err != nil {
		return err
	}
	j.Priority, err = ReadPriority(path, s.Log)
	return err
}

func writeBOM(_ context.Context, s *workflow.State) error {
	j, err := job(s)
	if err != nil {
		return err
	}
	if err := WriteOutput(j.Rows, s.Out, j.Policy); err != nil {
		return err
	}
	s.Log.Printf("Successfully processed and generated: %s", s.Out)
	return nil
}

func compileVendorPolicy(arg *yaml.Node, _ *workflow.Scope) (workflow.Runner, error) {
	var spec VendorPolicySpec
	if err := workflow.Decode(arg, &spec); err != nil {
		return nil, err
	}
	if len(spec.Banned) == 0 && len(spec.Exceptions) == 0 {
		return nil, fmt.Errorf("needs banned and/or exceptions")
	}
	policy, err := spec.Compile()
	if err != nil {
		return nil, err
	}
	return func(_ context.Context, s *workflow.State) error {
		j, err := job(s)
		if err != nil {
			return err
		}
		j.Policy = policy
		LogVendorPolicyResult(RunVendorPolicy(j.Rows, policy), policy, j.Log)
		return nil
	}, nil
}
