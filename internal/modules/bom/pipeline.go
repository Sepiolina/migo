package bom

import (
	"context"

	"miservice/internal/config"
	"miservice/internal/logx"
)

// Pipeline runs the BOM stages in order. All I/O is injectable (Deps), so the
// logic is testable without SQL Server, SAP or a real clock.
type Pipeline struct {
	Cfg    *config.Config
	Policy *VendorPolicy // nil = DefaultVendorPolicy
	Log    logx.Logger
	Stages []Stage
}

// NewPipeline builds the standard pipeline (DefaultStages).
func NewPipeline(cfg *config.Config, log logx.Logger, d Deps) *Pipeline {
	return &Pipeline{Cfg: cfg, Log: log, Stages: DefaultStages(d)}
}

// Process ports processPipeline: read -> group -> scope -> PCB reformat ->
// CFT-001 -> DB lookup -> SAP enrichment -> CFT-002 -> write. The first stage
// that fails stops the run and its error is returned unchanged.
func (p *Pipeline) Process(ctx context.Context, bomPath, prioPath, outPath string) error {
	policy := p.Policy
	if policy == nil {
		policy = DefaultVendorPolicy
	}
	job := &Job{Cfg: p.Cfg, Policy: policy, Log: p.Log, BOMPath: bomPath, PriorityPath: prioPath, OutPath: outPath}
	for _, st := range p.Stages {
		if err := st.Run(ctx, job); err != nil {
			return err
		}
	}
	return nil
}
