// Package bom is the BOM-processor feature: upload a customer BOM plus a
// Priority workbook, get back the annotated workbook.
//
// Layout of this package (read in this order):
//
//	module.go         wiring: constructor, routes, shutdown
//	handlers.go       the HTTP endpoints (thin: parse, call pipeline, respond)
//	pipeline.go       runs the stages in order
//	stages.go         THE list of stages (add / remove / reorder features here)
//	readers.go        input workbooks -> Rows
//	grouping.go       forward fill, find-num groups, qty fix, PCB scope
//	pcb.go            PCB detection and MPN reformatting
//	vendorpolicy*.go  CFT-001: forbidden vendors (rules in the _default file)
//	materialmaster.go Forth P/N lookup in SQL Server
//	sapenrich.go      block status / vendor / stock from SAP
//	selection.go      CFT-002: MAIN / SUBn per group
//	sheetbuilder.go   Rows -> cells and colours;  layout.go / format.go: constants
//	writer.go         cells -> .xlsx file
package bom

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"sync"

	"miservice/internal/clients/sap"
	"miservice/internal/clients/sqlserver"
	"miservice/internal/httpx"
	"miservice/internal/jobs"
	"miservice/internal/logx"
	"miservice/internal/module"
	"miservice/internal/storage"
)

// Upload limits; raise MaxUploadMB if a real upload is rejected.
const (
	MaxUploadMB = 25 // per file
	uploadFiles = 2  // the BOM and the Priority workbook
)

// Module is the BOM feature.
type Module struct {
	pipeline  *Pipeline
	store     *storage.OutputStore
	gate      *jobs.Gate
	log       logx.Logger
	uploadDir string
	uploads   *storage.UploadStore
	db        *sqlserver.Pool
	profiles  map[string]*Module
	routeBase string
	profileID string

	mu     sync.Mutex
	latest string // path of the most recent output (app.locals.latestOutputFile)
}

// New is the module.Factory.
func New(s module.Services) (module.Module, error) {
	db := sqlserver.New(sqlserver.Config{
		Server: s.Config.DB.Server, Port: s.Config.DB.Port,
		User: s.Config.DB.User, Password: s.Config.DB.Password, Database: s.Config.DB.Name,
		TLSMin: s.Config.DB.TLSMin,
	})
	deps := Deps{Lookup: NewMasterDB(db).Lookup, SAP: &sap.Client{}}
	build := func(path string) (*Pipeline, error) {
		var recipe *Recipe
		var err error
		if path == "" {
			recipe, err = DefaultRecipe()
		} else {
			if !filepath.IsAbs(path) {
				path = filepath.Join(s.Config.Dir, path)
			}
			recipe, err = LoadRecipe(path)
		}
		if err != nil {
			return nil, err
		}
		return recipe.Build(s.Config, s.Log, deps)
	}
	pipeline, err := build(s.Config.BOMWorkflow)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("BOM YAML pipeline: %w", err)
	}
	m := &Module{
		pipeline:  pipeline,
		store:     s.Store,
		gate:      s.Gate,
		log:       s.Log,
		uploadDir: s.UploadDir,
		uploads:   s.Uploads,
		db:        db,
		routeBase: "/api/bom",
		profiles:  make(map[string]*Module),
	}
	names := make([]string, 0, len(s.Config.BOMPipelines))
	for name := range s.Config.BOMPipelines {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !pipelineName.MatchString(name) {
			_ = db.Close()
			return nil, fmt.Errorf("invalid BOM pipeline name %q: use lowercase letters, digits and hyphens (1-40 characters)", name)
		}
		p, err := build(s.Config.BOMPipelines[name])
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("BOM pipeline %q: %w", name, err)
		}
		store, err := storage.NewOutputStore(filepath.Join(s.Store.OutputDir, "pipelines", name))
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("BOM pipeline %q output store: %w", name, err)
		}
		m.profiles[name] = &Module{
			pipeline: p, store: store, gate: s.Gate, log: s.Log,
			uploadDir: s.UploadDir, uploads: s.Uploads,
			routeBase: "/api/pipelines/" + name, profileID: name,
		}
	}
	return m, nil
}

var pipelineName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

func (*Module) Name() string { return "bom" }

// Routes registers the BOM endpoints (contract: docs/API.md of the Node project).
func (m *Module) Routes(r *httpx.Router) {
	r.POST("/api/bom/upload", m.upload)
	r.Any("/api/bom/download", m.download)
	names := make([]string, 0, len(m.profiles))
	for name := range m.profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		profile := m.profiles[name]
		r.POST(profile.routeBase+"/upload", profile.upload)
		r.Any(profile.routeBase+"/download", profile.download)
	}
}

// Close releases the database pool.
func (m *Module) Close() error { return m.db.Close() }

var _ module.Closer = (*Module)(nil)

// Start maintains retention in each profile's physical namespace. The app
// already maintains the legacy store; its nonrecursive sweep ignores these
// directories. Sharing only a filename prefix would allow uploads on the
// original route to overwrite named pipeline outputs.
func (m *Module) Start(ctx context.Context) error {
	if len(m.profiles) == 0 {
		return nil
	}
	stop := make(chan struct{})
	go func() { <-ctx.Done(); close(stop) }()
	for _, profile := range m.profiles {
		profile.store.StartCleanupScheduler(m.log, stop)
	}
	return nil
}

var _ module.Starter = (*Module)(nil)
