// Package workflows serves every workflow file as its own set of endpoints:
//
//	GET  /api/workflows        catalog: every workflow, its inputs and routes
//	GET  /api/<name>           this workflow's description
//	POST /api/<name>/upload    multipart "files" (one per input) + "bam"
//	ANY  /api/<name>/download  the caller's result: ?bam=E[&file=F] or ?employeeId=E
//	POST /api/<name>/run       same form; responds with the workbook itself
//
// <name> is the file name, or the workflow's service.route. Adding a
// workflow file adds its endpoints; no Go change is needed.
//
// Files come from the built-in set (repo folder workflows/, embedded in the
// binary) and from <config dir>/workflows/*.yaml, which override built-ins
// of the same name.
package workflows

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"miservice/internal/clients/sqlserver"
	"miservice/internal/httpx"
	"miservice/internal/jobs"
	"miservice/internal/logx"
	"miservice/internal/module"
	"miservice/internal/storage"
	"miservice/internal/workflow"
	builtin "miservice/workflows"
)

// MaxUploadMB is the per-file upload limit; raise it if a real upload is rejected.
const MaxUploadMB = 25

// Module serves all workflows.
type Module struct {
	db        *sqlserver.Pool
	log       logx.Logger
	endpoints []*endpoint
}

// endpoint is one workflow file and its routes.
type endpoint struct {
	name      string // the URL segment
	file      string // the workflow file name without .yaml
	wf        *workflow.Workflow
	rt        workflow.Runtime
	store     *storage.OutputStore
	gate      *jobs.Gate
	log       logx.Logger
	uploadDir string
	uploads   *storage.UploadStore
}

// New is the module.Factory.
func New(s module.Services) (module.Module, error) {
	db := sqlserver.New(sqlserver.Config{
		Server: s.Config.DB.Server, Port: s.Config.DB.Port,
		User: s.Config.DB.User, Password: s.Config.DB.Password, Database: s.Config.DB.Name,
		TLSMin: s.Config.DB.TLSMin,
	})
	files, err := workflowFiles(filepath.Join(s.Config.Dir, "workflows"))
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	m := &Module{db: db, log: s.Log}
	rt := workflow.Runtime{Cfg: s.Config, Log: s.Log, SQL: SQL(db)}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	routes := map[string]string{} // route -> file
	for _, file := range names {
		if !workflow.ValidRoute(file) {
			_ = db.Close()
			return nil, fmt.Errorf("workflow file %q: name it with lowercase letters, digits and hyphens (1-40 characters)", file+".yaml")
		}
		wf, err := workflow.Parse(files[file])
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("workflow %s.yaml: %w", file, err)
		}
		name := file
		if wf.Service.Route != "" {
			name = wf.Service.Route
		}
		if other, dup := routes[name]; dup || name == "workflows" {
			_ = db.Close()
			if name == "workflows" {
				return nil, fmt.Errorf("workflow %s.yaml: the route \"workflows\" is reserved for the catalog", file)
			}
			return nil, fmt.Errorf("workflows %s.yaml and %s.yaml both use the route %q", other, file, name)
		}
		routes[name] = file
		store, err := storage.NewOutputStore(filepath.Join(s.Store.OutputDir, name))
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("workflow %s output store: %w", name, err)
		}
		m.endpoints = append(m.endpoints, &endpoint{
			name: name, file: file, wf: wf, rt: rt, store: store, gate: s.Gate, log: s.Log,
			uploadDir: s.UploadDir, uploads: s.Uploads,
		})
	}
	return m, nil
}

// SQL runs sql steps on the shared SQL Server pool (connection "db", the
// db_* keys of config.json).
func SQL(db *sqlserver.Pool) workflow.SQLFunc {
	return func(ctx context.Context, connection, query string, args []any) (*workflow.Table, error) {
		if connection != "db" {
			return nil, fmt.Errorf("unknown connection %q (configured: db)", connection)
		}
		cols, rows, err := db.Query(ctx, query, args)
		if err != nil {
			return nil, err
		}
		return &workflow.Table{Columns: cols, Rows: rows}, nil
	}
}

// workflowFiles returns name -> YAML for the built-ins plus dir (if present).
func workflowFiles(dir string) (map[string][]byte, error) {
	files := map[string][]byte{}
	add := func(fsys fs.FS, where string) error {
		paths, err := fs.Glob(fsys, "*.yaml")
		if err != nil {
			return err
		}
		for _, p := range paths {
			data, err := fs.ReadFile(fsys, p)
			if err != nil {
				return fmt.Errorf("%s: %w", filepath.Join(where, p), err)
			}
			files[strings.TrimSuffix(p, ".yaml")] = data
		}
		return nil
	}
	if err := add(builtin.FS, "built-in"); err != nil {
		return nil, err
	}
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		if err := add(os.DirFS(dir), dir); err != nil {
			return nil, err
		}
	}
	return files, nil
}

func (*Module) Name() string { return "workflows" }

// Routes registers the catalog and, per workflow, its description,
// upload, download and run endpoints.
func (m *Module) Routes(r *httpx.Router) {
	r.GET("/api/workflows", m.catalog)
	for _, e := range m.endpoints {
		r.GET("/api/"+e.name, e.describe)
		r.POST("/api/"+e.name+"/upload", e.upload)
		r.Any("/api/"+e.name+"/download", e.download)
		r.POST("/api/"+e.name+"/run", e.run)
	}
}

// Start runs each workflow's output retention.
func (m *Module) Start(ctx context.Context) error {
	stop := make(chan struct{})
	go func() { <-ctx.Done(); close(stop) }()
	for _, e := range m.endpoints {
		e.store.StartCleanupScheduler(m.log, stop)
	}
	return nil
}

// Close releases the database pool.
func (m *Module) Close() error { return m.db.Close() }

var (
	_ module.Starter = (*Module)(nil)
	_ module.Closer  = (*Module)(nil)
)
