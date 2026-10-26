# Extending miservice

Most changes are a workflow file: a new upload/download endpoint, a new rule, a
different step order. See [WORKFLOWS.md](WORKFLOWS.md). This page covers what
needs Go: a new step type, a new module, or a custom endpoint.

## The map

```
cmd/miservice            entrypoint (flags + app.Run) -- rarely edited
internal/app             wiring. modules.go = the list of modules (1 line per module)
internal/module          what a module is (Module, Starter, Closer, Services)
internal/httpx           router, error responses, uploads, CORS/recover middleware
internal/config          config.json (typed shared keys + cfg.String/Int/Bool for your own)
internal/storage         generated files: temp -> commit, per-owner limit, retention cleanup
internal/jobs            Gate (limit concurrent heavy jobs), FreeMemory
internal/clients/sap     SAP RFC gateway client (retries)
internal/clients/sqlserver  shared lazy SQL Server pool
internal/jsx, sheet      JS-exact number/trim semantics; .xls/.xlsx readers
internal/workflow        workflow files: parser, step registry, table steps, expressions
internal/modules/system  /api/health   <- smallest example of a module
internal/modules/workflows  serves every workflow file at /api/<name>/{upload,download}
internal/modules/bom     the BOM steps used by workflows/bom.yaml (see its doc.go)
workflows/               built-in workflow files (embedded in the binary)
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
   r.GET("/api/bom/status", m.status)     // GET (+HEAD)
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
return `pool.Close()` from your `Close`. Need SAP? `(&sap.Client{}).CallRFC(ctx, endpoint, rows, retries)`.

## 3. Add a step type

Reordering, removing or reconfiguring steps is a workflow file edit. A new
kind of step needs Go.

1. Write the logic as a plain function, in its own file. For the BOM:

   ```go
   // internal/modules/bom/obsolete.go
   func flagObsolete(j *Job) {
       n := 0
       for _, r := range j.Rows {
           if r.InScope && strings.HasPrefix(r.EffectivePartType, "OBS") {
               r.IsObsolete = true    // new field: add it to Row (row.go)
               n++
           }
       }
       j.Log.Printf("Obsolete: %d row(s) flagged.", n)
   }
   ```
2. Register it in `Ops` (`internal/modules/bom/steps.go`). `Needs` lists the
   steps that must come earlier, and `After` lists steps that must come
   earlier when they are present. Both are checked when the file loads.

   ```go
   "flag_obsolete": {Compile: rowsStep(func(_ context.Context, j *Job) { flagObsolete(j) }), Needs: []string{"group_rows"}},
   ```
   A step with an argument compiles it in `Compile` (decode with
   `workflow.Decode`), so a bad argument fails at startup. A step that returns
   an error aborts the upload with that message. For optional data (like SAP),
   log a warning and return nil instead.
3. Use it: add `- flag_obsolete` to `workflows/bom.yaml`.
4. Show it in the workbook: columns and colours live in `layout.go` (indexes,
   headers, widths) and `sheetbuilder.go` (values and styling).
5. Test the function directly (see `bom_test.go`: build `[]*Row`, call, assert).

Generic table steps live in `internal/workflow/table.go`, and they follow the
same pattern via `tableStep`.

**Always run the regression check after touching a step or `bom.yaml`**:
`go test ./...` runs `TestOutputMatchesGolden`. If the output is *supposed* to
change, regenerate `testdata/golden_random7_nosap.xlsx` deliberately and say
why in the commit.

## 4. Change a business rule

| Change | Where |
| --- | --- |
| Ban a vendor outright | `banned` under `vendor_policy` in `workflows/bom.yaml` |
| Ban a brand except some parts | add an `exceptions` entry there (`brand`, `allow`, `log`) |
| A new field for rule expressions | `rowEnv` / `envOf` in `internal/modules/bom/rules.go` |
| Recognise the Priority upload by another name | `match` of the `priority` input |
| Accept another header title in customer BOMs | `headerSynonyms` in `readers.go` |
| Which rows count as ineligible for MAIN/SUB, and why | `ineligible` under `select_priority` in `workflows/bom.yaml` |
| How MAIN is chosen (conditions A-D, reasons) | `main` rules under `select_priority` |
| Material_Master query, covered rows, PCB retry suffix | `material_master` in `workflows/bom.yaml` |
| Rows sent to SAP, employee ID, SAP answer keys | `sap_enrich` in `workflows/bom.yaml` |
| SAP batch size / retries | `api_batch_size`, `api_retries` in `config.json` |
| Upload size limit | `MaxUploadMB` in `internal/modules/workflows/module.go` |
| Output retention | constants at the top of `internal/storage/outputstore.go` |

## 5. Conventions

* Log through `logx.Logger` (`log.Printf(...)`); a nil logger is safe.
* Anything that must behave like JavaScript (trim, number-to-string, parseFloat)
  goes through `internal/jsx`, not the Go standard library: output must stay
  cell-identical with the Node service.
* Inject external systems (database, SAP, clock) as function values or small
  structs so tests never need the real thing (`LookupFunc`, `sap.Client{HTTP, Sleep}`).
* `gofmt` + `go vet ./...` + `go test ./...` before every commit.
