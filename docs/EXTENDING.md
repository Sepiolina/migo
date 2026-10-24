# Extending miservice

How to add an endpoint, a feature or a whole module without reading the rest
of the code. Every recipe says which files to touch.

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
internal/modules/system  /api/health   <- smallest example of a module
internal/modules/bom     the BOM feature (see the layout in its module.go)
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
The BOM module registers nothing, so its inputs are removed after each request
as before.

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
       bom.New,
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

## 3. Add a processing step to the BOM pipeline

The pipeline is a list of named stages: `DefaultStages` in
`internal/modules/bom/stages.go`.

1. Write the logic as a plain function on rows, in its own file:

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
2. Insert a stage where it belongs. The comments in `DefaultStages` say what
   each stage needs from the ones before it:

   ```go
   {"obsolete", func(_ context.Context, j *Job) error { flagObsolete(j); return nil }},
   ```
   A stage that returns an error aborts the upload with that message; for
   "optional" data (like SAP) log a warning and return nil instead.
3. Show it in the workbook: columns and colours live in `layout.go` (indexes,
   headers, widths) and `sheetbuilder.go` (values and styling).
4. Test the function directly (see `bom_test.go`: build `[]*Row`, call, assert).

**Always run the regression check after touching a stage**: `go test ./...`
runs `TestOutputMatchesGolden`; for the SAP path and the original Node
comparison use `tools/difftest`. If the output is *supposed* to change,
regenerate `testdata/golden_random7_nosap.xlsx` deliberately and say why in the commit.

## 4. Change a business rule

| Change | Where |
| --- | --- |
| Ban a vendor outright | `BannedUnconditional` in `vendorpolicy_default.go` |
| Ban a brand except some parts | add a `BrandException` there (see the TDK entry; helpers `brandContainsAny`, `capsMustSatisfy`, `mpnStartsWith`) |
| Accept another header title in customer BOMs | `headerSynonyms` in `readers.go` |
| MAIN/SUB selection logic | `chooseMain` in `selection.go` |
| SAP batch size / retries | `api_batch_size`, `api_retries` in `config.json` |
| Upload size limit | `MaxUploadMB` in `module.go` |
| Output retention | constants at the top of `internal/storage/outputstore.go` |

## 5. Conventions

* Log through `logx.Logger` (`log.Printf(...)`); a nil logger is safe.
* Anything that must behave like JavaScript (trim, number-to-string, parseFloat)
  goes through `internal/jsx`, not the Go standard library: output must stay
  cell-identical with the Node service.
* Inject external systems (database, SAP, clock) as function values or small
  structs so tests never need the real thing (`LookupFunc`, `sap.Client{HTTP, Sleep}`).
* `gofmt` + `go vet ./...` + `go test ./...` before every commit.
