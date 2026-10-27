# Extending maiservice

Processes, endpoints and business rules are workflow files, never Go: see
[WORKFLOWS.md](WORKFLOWS.md). This page covers what needs Go: a new generic
step type, a new module, or a custom endpoint.

## The map

```
cmd/maiservice            entrypoint (flags + app.Run) -- rarely edited
internal/app             wiring. modules.go = the list of modules (1 line per module)
internal/module          what a module is (Module, Starter, Closer, Services)
internal/httpx           router, error responses, uploads, CORS/recover middleware
internal/config          config.json (typed shared keys + cfg.String/Int/Bool for your own)
internal/storage         generated files: temp -> commit, per-owner limit, retention cleanup
internal/jobs            Gate (limit concurrent heavy jobs), FreeMemory
internal/clients/sqlserver  shared lazy SQL Server pool
internal/jsx, sheet      JS-exact number/trim semantics; .xls/.xlsx readers
internal/workflow        the workflow engine: parser, steps, expressions, sql/rest, xlsx writer
internal/modules/system  /api/health   <- smallest example of a module
internal/modules/workflows  serves every workflow file at /api/<name>/{upload,download}
workflows/               built-in workflow files (embedded in the binary); bom.yaml = the BOM process
```

Rule of thumb: **handlers are thin** (parse -> call logic -> respond), logic
lives in plain functions that take and return values, and anything shared
between features goes in `internal/<name>` rather than inside a module.

## 1. Add an endpoint to an existing module

1. Write a handler in the module's `handlers.go`. It returns an `error`; never
   format error bodies yourself.

   ```go
   func (m *Module) status(w http.ResponseWriter, r *http.Request) error {
       id := r.URL.Query().Get("id")
       if id == "" {
           return httpx.Fail(http.StatusBadRequest, "Missing 'id'")   // {"error":"..."} , not logged
       }
       res, err := m.lookup(r.Context(), id)
       if err != nil {
           return err                                                 // 500 {"status":"error","message":...}, logged
       }
       return httpx.WriteJSON(w, http.StatusOK, res)
   }
   ```
2. Register it in the module's `Routes`:

   ```go
   r.GET("/api/reports/status", m.status) // GET (+HEAD)
   r.POST(...)  r.Any(...)                // POST only / every method
   ```
   Registering the same path twice panics at startup, so two features can never
   silently shadow each other. The startup banner lists the new route by itself.

Error cheat-sheet: `httpx.Fail(status, msg)` = expected client mistake;
`httpx.Status(status, err)` = failure with a status, logged; plain `error` = 500, logged.

File upload endpoint: `httpx.ParseUpload(r, httpx.UploadOptions{Dir: m.uploadDir, FieldName: "files", MaxFiles: 1, MaxFileMB: 10})`,
then `defer m.uploads.Discard(up.Paths()...)` (keep `uploads *storage.UploadStore` from `s.Uploads`).

### Upload retention: the keep registry

`uploads/` has one rule. A file **not registered** in `uploads/.keep.json` is
temporary and is deleted automatically: by `Discard` when the request ends,
and by an hourly sweep (files older than 15 min; at startup every leftover)
that catches crashes. A file **registered** with `m.uploads.Keep(path, owner, clientName)`
survives its request and is deleted by the scheduled cleanup (every 14 days,
files registered more than 14 days ago). Dotfiles (`.gitkeep`, the registry)
are never touched. If the registry is unreadable nothing is deleted.
The workflow endpoints register nothing, so their inputs are removed after
each request.

## 2. Add a new module (a new feature area)

1. `mkdir internal/modules/reports` and create `reports.go`:

   ```go
   package reports

   type Module struct{ log logx.Logger }

   func New(s module.Services) (module.Module, error) { return &Module{log: s.Log}, nil }
   func (*Module) Name() string                       { return "reports" }
   func (m *Module) Routes(r *httpx.Router)           { r.GET("/api/reports/summary", m.summary) }

   func (m *Module) summary(w http.ResponseWriter, _ *http.Request) error {
       return httpx.WriteJSON(w, http.StatusOK, map[string]int{"count": 0})
   }
   ```
2. Register it -- the only edit outside your package -- in `internal/app/modules.go`:

   ```go
   var DefaultModules = []module.Factory{
       system.New,
       workflows.New,
       reports.New,   // <- new
   }
   ```
3. Needs shared things? They come in `module.Services`: `Config`, `Log`, `Store`
   (generated files), `UploadDir`, `Gate` (limits heavy jobs: `Acquire(ctx)` / `Release()`).
4. Optional hooks, just implement the method:
   * `Start(ctx) error` -- background work (launch goroutines, stop on `ctx.Done()`).
   * `Close() error` -- release connections; called once at shutdown.
5. Test it like `internal/app/app_test.go`: build `app.New(cfg, logx.Nop, []module.Factory{reports.New})`
   and call `a.Handler()` with `httptest`.

New config keys need no change to `internal/config`:
`s.Config.String("reports_url")`, `.Int("reports_timeout")`, `.Bool("reports_enabled")`
(add the keys to `config.example.json`).

Need SQL Server? Take a pool: `sqlserver.New(sqlserver.Config{...})`, call `pool.DB()`,
return `pool.Close()` from your `Close`. Calling an HTTP API from a process is a `rest` step, not module code.

## 3. Add a generic step type

First check whether expressions already do it: `set` with `prev()`, `window`
with `group`, and `pick` cover most row logic. A step type belongs in Go only
when it is generic, meaning not tied to one process.

1. Write the compiler in `internal/workflow` (table steps: `table.go` /
   `group.go`). A table step decodes its argument with `strictDecode`,
   compiles any expressions with `compileExpr`, and returns a `tableFn`:

   ```go
   // - upper: [Col, ...] upper-cases the listed columns.
   func compileUpper(arg *yaml.Node, _ *scope) (tableFn, error) {
       names, err := nameList(arg)
       if err != nil {
           return nil, err
       }
       return func(t *Table, _ *State) (*Table, error) {
           ix, err := t.cols(names)
           if err != nil {
               return nil, err
           }
           for _, r := range t.Rows {
               for _, n := range ix {
                   r[n] = strings.ToUpper(text(r[n]))
               }
           }
           return t, nil
       }, nil
   }
   ```
2. Register it in `ops` (`workflow.go`): `"upper": tableStep(compileUpper)`.
   Everything is compiled when a file loads, so check the argument there.
3. Test it in `internal/workflow/workflow_test.go` (`TestTableSteps` runs a
   small workflow on `testdata/parts.xls`), and document it in `docs/WORKFLOWS.md`.

**After touching the engine or `bom.yaml`, run `go test ./...`.** It includes
`TestBOMMatchesGolden`. If the BOM output is *supposed* to change, regenerate
`testdata/golden_random7_nosap.xlsx` deliberately and say why in the commit.

## 4. Change a business rule

Every BOM rule is in `workflows/bom.yaml`:

| Change | Where in `bom.yaml` |
| --- | --- |
| Accept another header title in customer BOMs | `header.columns` of the first `from` |
| Recognise the Priority upload by another name | `match` of the `priority` input |
| Ban a vendor, or a brand except some parts | `Exception`, `Exempt` and `Forbidden` in the CFT-001 `set` |
| Material_Master query or which rows are looked up | the `sql` step and `Covered` |
| SAP call, employee ID, fields | the `rest` step and the `sap` steps after it |
| CFT-002 eligibility, MAIN conditions A-D, reasons | `Eligible` and the `pick` rules |
| Output columns, widths, colours | `output.sheets` |

Elsewhere:

| Change | Where |
| --- | --- |
| SAP / DB connection, batch size, retries | `config.json` (read with `config("...")`) |
| Upload size limit | `MaxUploadMB` in `internal/modules/workflows/module.go` |
| Output retention | constants at the top of `internal/storage/outputstore.go` |

## 5. Conventions

* Log through `logx.Logger` (`log.Printf(...)`); a nil logger is safe.
* Anything that must behave like JavaScript (trim, number-to-string, parseFloat)
  goes through `internal/jsx`, not the Go standard library: output must stay
  cell-identical with the Node service.
* Inject external systems (database, HTTP, clock) as function values or small
  structs so tests never need the real thing (`workflow.Runtime{SQL, HTTP, Sleep}`).
* `gofmt` + `go vet ./...` + `go test ./...` before every commit.
