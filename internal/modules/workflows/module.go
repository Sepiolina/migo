// Package workflows serves every workflow file as its own endpoint pair:
//
//	POST /api/<name>/upload    multipart "files" (one per input) + "bam"
//	ANY  /api/<name>/download  the latest result of this workflow
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
	"regexp"
	"sort"
	"strings"
	"sync"

	"miservice/internal/clients/sap"
	"miservice/internal/clients/sqlserver"
	"miservice/internal/config"
	"miservice/internal/httpx"
	"miservice/internal/jobs"
	"miservice/internal/logx"
	"miservice/internal/module"
	"miservice/internal/modules/bom"
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
	name      string
	wf        *workflow.Workflow
	cfg       *config.Config
	store     *storage.OutputStore
	gate      *jobs.Gate
	log       logx.Logger
	uploadDir string
	uploads   *storage.UploadStore

	mu     sync.Mutex
	latest string // path of the most recent output
}

var endpointName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// New is the module.Factory.
func New(s module.Services) (module.Module, error) {
	db := sqlserver.New(sqlserver.Config{
		Server: s.Config.DB.Server, Port: s.Config.DB.Port,
		User: s.Config.DB.User, Password: s.Config.DB.Password, Database: s.Config.DB.Name,
		TLSMin: s.Config.DB.TLSMin,
	})
	reg := workflow.TableOps().With(bom.Ops(bom.Deps{DB: db, SAP: &sap.Client{}}))
	files, err := workflowFiles(filepath.Join(s.Config.Dir, "workflows"))
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	m := &Module{db: db, log: s.Log}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !endpointName.MatchString(name) {
			_ = db.Close()
			return nil, fmt.Errorf("workflow file %q: name it with lowercase letters, digits and hyphens (1-40 characters)", name+".yaml")
		}
		wf, err := workflow.Parse(files[name], reg)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("workflow %s.yaml: %w", name, err)
		}
		store, err := storage.NewOutputStore(filepath.Join(s.Store.OutputDir, name))
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("workflow %s output store: %w", name, err)
		}
		m.endpoints = append(m.endpoints, &endpoint{
			name: name, wf: wf, cfg: s.Config, store: store, gate: s.Gate, log: s.Log,
			uploadDir: s.UploadDir, uploads: s.Uploads,
		})
	}
	return m, nil
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

// Routes registers /api/<name>/upload and /api/<name>/download per workflow.
func (m *Module) Routes(r *httpx.Router) {
	for _, e := range m.endpoints {
		r.POST("/api/"+e.name+"/upload", e.upload)
		r.Any("/api/"+e.name+"/download", e.download)
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
