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
	return &Module{
		pipeline:  NewPipeline(s.Config, s.Log, deps),
		store:     s.Store,
		gate:      s.Gate,
		log:       s.Log,
		uploadDir: s.UploadDir,
		uploads:   s.Uploads,
		db:        db,
	}, nil
}

func (*Module) Name() string { return "bom" }

// Routes registers the BOM endpoints (contract: docs/API.md of the Node project).
func (m *Module) Routes(r *httpx.Router) {
	r.POST("/api/bom/upload", m.upload)
	r.Any("/api/bom/download", m.download)
}

// Close releases the database pool.
func (m *Module) Close() error { return m.db.Close() }

var _ module.Closer = (*Module)(nil)
