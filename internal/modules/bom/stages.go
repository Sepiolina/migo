package bom

import (
	"context"

	"miservice/internal/clients/sap"
	"miservice/internal/config"
	"miservice/internal/logx"
)

// Job is the state one upload carries through the pipeline. A stage reads
// what earlier stages produced and writes what later stages need.
type Job struct {
	Cfg    *config.Config
	Policy *VendorPolicy // never nil
	Log    logx.Logger

	// Inputs / output (set by Pipeline.Process).
	BOMPath, PriorityPath, OutPath string

	// Produced by the read-inputs stage.
	Rows     []*Row
	Priority PriorityMap
}

// Stage is one step of the pipeline. Run mutates the Job; returning an error
// aborts the whole request (use it for "the input is unusable", not for
// "an optional enrichment was unavailable" -- log and carry on for those).
type Stage struct {
	Name string
	Run  func(ctx context.Context, j *Job) error
}

// Deps are the external systems the stages talk to. Both are optional: a nil
// Lookup means "no database", a nil SAP uses the default HTTP client.
type Deps struct {
	Lookup LookupFunc
	SAP    *sap.Client
}

// DefaultStages is THE pipeline, in execution order. To add a feature, write
// a Stage and insert it here; to disable one, remove it. Each stage's
// ordering constraint is noted next to it.
func DefaultStages(d Deps) []Stage {
	return []Stage{
		{"read-inputs", readInputs},
		{"group-rows", func(_ context.Context, j *Job) error { ProcessGrouping(j.Rows); return nil }},
		// Scope needs the forward-filled descriptions from group-rows.
		{"pcb-scope", func(_ context.Context, j *Job) error { markScope(j.Rows); return nil }},
		{"pcb-reformat", func(_ context.Context, j *Job) error { reformatPcbRows(j.Rows, j.Log); return nil }},
		{"cft-001-vendor-policy", func(_ context.Context, j *Job) error {
			LogVendorPolicyResult(RunVendorPolicy(j.Rows, j.Policy), j.Policy, j.Log)
			return nil
		}},
		// material-master needs the CFT-001 verdicts (IsForbidden).
		{"material-master", func(ctx context.Context, j *Job) error { lookupForthPNs(ctx, j, d.Lookup); return nil }},
		// sap-enrich needs the Forth P/Ns from material-master.
		{"sap-enrich", func(ctx context.Context, j *Job) error { enrichFromSAP(ctx, j, d.SAP); return nil }},
		// cft-002 needs priorities, vendors and stock from everything above.
		{"cft-002-priority", func(_ context.Context, j *Job) error {
			RunCft002(j.Rows, j.Priority, j.Policy, j.Log)
			return nil
		}},
		{"write-workbook", writeWorkbook},
	}
}

func readInputs(_ context.Context, j *Job) error {
	rows, err := ReadBom(j.BOMPath, j.Log)
	if err != nil {
		return err
	}
	pmap, err := ReadPriority(j.PriorityPath, j.Log)
	if err != nil {
		return err
	}
	j.Rows, j.Priority = rows, pmap
	return nil
}

func writeWorkbook(_ context.Context, j *Job) error {
	if err := WriteOutput(j.Rows, j.OutPath, j.Policy); err != nil {
		return err
	}
	j.Log.Printf("Successfully processed and generated: %s", j.OutPath)
	return nil
}
