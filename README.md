# migo / miservice (Go)

A Go microservice where every process is one YAML file in `workflows/`. Each file is its own
endpoint pair, maps its uploaded inputs to one output workbook, and holds every rule inline.
The engine is generic: sheet wrangling, SQL, REST, branching and expressions. No process
is coded in Go. `workflows/bom.yaml` is the BOM processor, ported from the Node/Express
service with the same endpoints, JSON and workbook values.

```
POST /api/<name>/upload     multipart: files (one per workflow input, any order), bam (employee number)
GET  /api/<name>/download   latest generated workbook of this workflow
GET  /api/health            {"status":"online","service":"BOM Processor Node.js API"}
```
For the BOM: `POST /api/bom/upload` takes 2 files (BOM + Priority), and `GET /api/bom/download` returns the result.
`docs/API.md` of the Node project still describes the contract (unchanged).

## Quick start
```
cp config.example.json config.json      # same keys as the Node config.json
go build -o miservice ./cmd/miservice   # or: make linux / make windows
./miservice                             # -config path | MISERVICE_CONFIG env | ./config.json
```
Config lookup order: `-config` flag, then `MISERVICE_CONFIG`, then `./config.json`, then `config.json` beside the
executable. `PORT` env overrides `"port"` (as before). `uploads/` and `outputs/` are created next to the config file.
Each workflow keeps its results in `outputs/<name>/`. Files left in the `outputs/` root by older versions are still
removed by the root's retention cleanup.
Deploy = copy the single binary + `config.json`. No Node, no node_modules (Node is only needed on the server if you
choose PM2 as the process manager, see below).

### What is kept on disk
| Folder | Contents | Lifetime |
|---|---|---|
| `outputs/<workflow>/` | generated workbooks + `.manifest.json` | newest 2 per employee number; unused files deleted after 14 days (checked daily, runs every 14 days) |
| `uploads/` | the two input files of a request | not registered in `uploads/.keep.json`: deleted at the end of the request, plus an hourly sweep for leftovers. Registered in keep: deleted on the 14-day schedule. See `docs/EXTENDING.md` |

After any request, `uploads/` should contain nothing except `.gitkeep` (and `.keep.json` if a module uses keep).

## Workflows
```yaml
# workflows/<name>.yaml  ->  /api/<name>/upload, /api/<name>/download
name: Parts summary
input:
  parts: {label: Parts list}
steps:
  - from: parts[Parts]
  - filter: num(Quantity) > 0
  - if: 'any(rows, num(#.Quantity) > 100)'
    then:
      - log: Large order
  - group: {by: [ID], sum: [Quantity]}
  - save: summary
output:
  sheets: {Parts: summary}
```
Drop a file into `<config dir>/workflows/` and restart to add an endpoint, or to replace a built-in one with the
same name. Steps (`from`, `set`, `window`, `pick`, `join`, `sql`, `rest`, `if`, `split`...), expressions
and styled output are described in [docs/WORKFLOWS.md](docs/WORKFLOWS.md). Run any workflow without HTTP:
```
go run ./cmd/migo -in bom=testdata/bom_basic.xlsx -in priority=testdata/prio_basic.xlsx -out out.xlsx workflows/bom.yaml
```

## Testing on Windows
Everything below is PowerShell, run from the project folder. None of it needs SQL Server, SAP or Node.

**1. Prerequisites (once)**
* Go 1.22 or newer: `winget install GoLang.Go` (or the installer from go.dev), then open a **new** terminal and check `go version`.
* The first build downloads the dependencies (excelize, mscfb, go-mssqldb), so it needs internet access or a company
  Go proxy (`go env -w GOPROXY=https://your-proxy,direct`). Later builds are offline.
* `curl.exe` ships with Windows 10/11 (type `curl.exe`, not `curl`: in PowerShell 5 plain `curl` is an alias of `Invoke-WebRequest`).

**2. Unit and golden tests**
```
go vet ./...
go test ./...
```
These use the files in `testdata/` with a stubbed Material_Master and a fake SAP server, so they run anywhere. To see
one package in detail: `go test -v ./internal/storage/` (retention, uploads keep registry), `./internal/workflow/`
(engine, BOM golden workbook), `./internal/modules/workflows/` (upload endpoints end to end), `./internal/sheet/` (.xls and .xlsx readers). Add `-race` if you have a
C toolchain installed; it is optional. If an antivirus quarantines the test binaries, exclude `%TEMP%\go-build*`.

**3. Run the service locally**
```
Copy-Item config.example.json config.json
```
Edit `config.json`. For a local test without the company systems set `"db_enabled": false` and `"sap_enabled": false`:
the Material_Master lookup and the SAP enrichment steps are then skipped. Set them to `true` with real values to
test against the real servers (the Windows machine must reach them). Then:
```
go run ./cmd/miservice -config config.json
# or, to test the real artifact:
go build -o miservice.exe ./cmd/miservice ; .\miservice.exe -config config.json
```
To use another port without editing the file: `$env:PORT = 3090` before starting. Stop with Ctrl+C: the server
finishes the running request and shuts down gracefully (up to 10 s). Windows Firewall may ask to allow the
program on first start; allow "Private networks" if colleagues should reach it.

**4. Smoke test the endpoints** (second terminal)
```
curl.exe http://localhost:3081/api/health
curl.exe -F "files=@testdata/bom_basic.xlsx" -F "files=@testdata/prio_basic.xlsx" -F "bam=113280" http://localhost:3081/api/bom/upload
curl.exe -o out.xlsx http://localhost:3081/api/bom/download
dir uploads, outputs\bom
```
Expected: health returns `{"status":"online",...}`; upload returns `"status":"success"` and a `downloadUrl`; `out.xlsx`
opens in Excel; `uploads\` holds only `.gitkeep`; `outputs\bom\` holds `bom_basic_113280.xlsx` and `.manifest.json`.
The Priority file is recognised by a file name containing `prio` or `shortage`; otherwise the first file is the BOM.
Error cases worth trying: one file only (400 "Expected 2 files"), `bam=../x` (400), a file over 25 MB (400).

**5. Run a workflow on real files, without HTTP**
```
go build -o migo.exe ./cmd/migo
.\migo.exe -in "bom=C:\data\Customer BOM.xlsx" -in "priority=C:\data\Priority.xlsx" -out out.xlsx workflows\bom.yaml
```
`-cfg` takes config.json keys as JSON; the default `{}` skips DB and SAP. Use `-sql-stub testdata/stub_master.json`
to stand in for the database, and `-check` to run without keeping the output.
Ubuntu is the production OS, so also run `go test ./...` on the Ubuntu machine (or WSL2 Ubuntu) once before a release.

**6. Cross-build the Ubuntu binary from Windows**
```
$env:CGO_ENABLED = "0"; $env:GOOS = "linux"; $env:GOARCH = "amd64"
go build -ldflags="-s -w" -o dist/miservice ./cmd/miservice
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
```
The binary is static (no cgo), so it runs on any Ubuntu without installing Go. With GNU make installed, `make linux`
does the same. Use `GOARCH=arm64` for an ARM server.

## Testing PM2 on Windows
The same `deploy/ecosystem.config.js` works on Windows (it picks `miservice.exe` there), so you can rehearse the PM2
setup before touching the server. PM2 on Windows is a test tool only; production stays Ubuntu.

**1. Install Node and PM2 (once)**
```
winget install OpenJS.NodeJS.LTS          # open a NEW terminal afterwards
npm install -g pm2
pm2 -v
```
If PowerShell says running scripts is disabled (`pm2.ps1 cannot be loaded`), run
`Set-ExecutionPolicy -Scope CurrentUser RemoteSigned`, or call `pm2.cmd` instead of `pm2`.

**2. Build the exe and prepare the config** (project folder)
```
go build -o miservice.exe ./cmd/miservice
Copy-Item config.example.json config.json        # skip if you already have one
```
For a test without the company systems set `"db_enabled": false` and `"sap_enabled": false` in `config.json`.

**3. Start under PM2**
```
$env:MISERVICE_DIR = (Get-Location).Path          # folder with miservice.exe + config.json
pm2 start deploy/ecosystem.config.js
pm2 status                                        # miservice: online
curl.exe http://localhost:3081/api/health
pm2 logs miservice --lines 30                     # banner, cleanup lines (Ctrl+C leaves the log view)
```

**4. Exercise what PM2 is there for**
```
# a) real request still works and uploads/ is cleaned
curl.exe -F "files=@testdata/bom_basic.xlsx" -F "files=@testdata/prio_basic.xlsx" -F "bam=113280" http://localhost:3081/api/bom/upload
dir uploads                                       # only .gitkeep

# b) crash recovery: kill the process, PM2 must start it again
Stop-Process -Name miservice -Force
Start-Sleep 5 ; pm2 status                        # online again, "restarts" column went up by 1
curl.exe http://localhost:3081/api/health

# c) manual control and inspection
pm2 restart miservice
pm2 describe miservice                            # cwd, args, env (PORT, GOMEMLIMIT), restarts, uptime
pm2 monit                                         # live CPU / memory (q to quit)
pm2 stop miservice ; pm2 start miservice
```
Optional memory-limit test: set `max_memory_restart: "20M"` in a copy of the ecosystem file, start it, upload a BOM,
and PM2 should log a restart because the process went over the limit. Do not copy that value to production.

**5. Clean up**
```
pm2 delete miservice
pm2 kill                                          # stops the PM2 daemon too
Remove-Item Env:MISERVICE_DIR
```
PM2 keeps its logs in `%USERPROFILE%\.pm2\logs\miservice-out.log` and `miservice-error.log`.

**What differs from Ubuntu** (so a pass here is not a full sign-off)
* Windows has no SIGTERM: `pm2 stop/restart` kills the process at once, so a request in flight is cut. On Ubuntu the
  service drains it first (`kill_timeout`). Test graceful shutdown on Ubuntu or WSL2.
* `pm2 startup` (start on boot) is not supported on Windows; test it on the Ubuntu server.
* A running `miservice.exe` cannot be overwritten: `pm2 stop miservice` before rebuilding with `go build`.
* File permissions, the `miservice` service user, ufw and `GOARCH` problems only show up on Ubuntu.

## Deploying on Ubuntu with PM2
PM2 can supervise any executable, so it runs the Go binary directly (`interpreter: "none"`). Use it if the company's
"1 service slot" means a PM2 slot; otherwise `deploy/miservice.service` (systemd) does the same without Node.
Paths below assume `/opt/miservice` and a service user `miservice`; adjust to taste.

**1. One-time server setup**
```
sudo useradd --system --create-home --shell /bin/bash miservice
sudo mkdir -p /opt/miservice && sudo chown miservice:miservice /opt/miservice
# Node + PM2 (only needed for PM2; any Node LTS works)
sudo apt update && sudo apt install -y nodejs npm
sudo npm install -g pm2
```
The service user needs a login shell (or run the `pm2` commands via `sudo -iu miservice`), because PM2 keeps its
process list in that user's home.

**2. Copy the files** (from the Windows or build machine)
```
scp dist/miservice deploy/ecosystem.config.js you@server:/tmp/
# on the server:
sudo install -o miservice -g miservice -m 755 /tmp/miservice /opt/miservice/miservice
sudo install -o miservice -g miservice -m 644 /tmp/ecosystem.config.js /opt/miservice/ecosystem.config.js
```
Create `/opt/miservice/config.json` from `config.example.json` with the real DB/SAP values and protect it, because it
holds the DB password: `sudo chmod 600 /opt/miservice/config.json && sudo chown miservice:miservice /opt/miservice/config.json`.
`uploads/` and `outputs/` are created next to it on first start.

**3. Start**
```
sudo -iu miservice
cd /opt/miservice
pm2 start ecosystem.config.js
pm2 status                      # miservice should be "online"
curl http://localhost:3081/api/health
pm2 logs miservice --lines 50   # startup banner, cleanup lines, request logs
```
`ecosystem.config.js` pins the working directory to the folder the file sits in (`/opt/miservice`; override with the
`MISERVICE_DIR` env var), passes `-config`, sets `PORT=3081`, restarts on
crash (max 10 quick restarts), restarts above 650 MB, and sets `GOMEMLIMIT=400MiB` so the Go garbage collector works
harder before memory grows (the equivalent of Node's `--max-old-space-size`). Keep `exec_mode: "fork"` and
`instances: 1`: the "latest download" is per process and the service is built for a single instance.

**4. Start on boot**
```
pm2 startup systemd -u miservice --hp /home/miservice     # prints one sudo command: run it
pm2 save
```
Reboot once to prove it (`sudo reboot`, then `pm2 status`). Run `pm2 save` again whenever you add or remove apps.

**5. Open the port** (only if ufw is on): `sudo ufw allow 3081/tcp`. If you put nginx in front, allow bodies of at least
60 MB (`client_max_body_size 60m;`, two files of up to 25 MB each) and a generous `proxy_read_timeout` (large BOMs with
SAP lookups can take minutes).

**6. Logs**
PM2 writes `~/.pm2/logs/miservice-out.log` and `miservice-error.log`. Add rotation once:
`pm2 install pm2-logrotate` (defaults: 10 MB, 30 files).

**7. Update to a new version**
```
# build + copy the new binary to /tmp/miservice, then on the server:
sudo install -o miservice -g miservice -m 755 /tmp/miservice /opt/miservice/miservice.new
sudo -iu miservice bash -c 'cd /opt/miservice && mv miservice.new miservice && pm2 restart miservice'
```
(Renaming over the file avoids "text file busy"; plain copying over a running binary can fail.) PM2 sends SIGTERM, the
service finishes the running request and exits, then PM2 starts the new binary. `config.json`, `outputs/` and
`uploads/` are untouched. Roll back by keeping the previous binary as `miservice.prev` and moving it back.

**8. Verify after every deploy**
```
pm2 status                                   # online, restarts count not climbing
curl -s http://localhost:3081/api/health
curl -F "files=@bom.xlsx" -F "files=@prio.xlsx" -F "bam=113280" http://localhost:3081/api/bom/upload
ls -la /opt/miservice/uploads                # only .gitkeep (and .keep.json) after the request
```

**Troubleshooting**
| Symptom | Likely cause |
|---|---|
| `errored`, log says `no config.json found` | `config.json` is missing in `/opt/miservice`, or `cwd`/`-config` in the ecosystem file is wrong |
| `listen tcp 0.0.0.0:3081: bind: address already in use` | another process (or an old PM2 app) holds the port: `sudo ss -ltnp \| grep 3081` |
| `permission denied` on `uploads/` or `outputs/` | the folder is owned by root; `sudo chown -R miservice:miservice /opt/miservice` |
| `exec format error` | binary built for the wrong architecture (`GOARCH=amd64` vs `arm64`) |
| restarts again and again | `pm2 logs miservice --err`; also check `max_memory_restart` vs the size of the BOM files |
| works locally, 500 on the server | DB or SAP unreachable from the server: check `db_server`/`sap_host`, firewall, and the DB credentials in `config.json` |

systemd alternative: `sudo cp deploy/miservice.service /etc/systemd/system/ && sudo systemctl enable --now miservice`,
logs with `journalctl -u miservice -f`. Use one supervisor or the other, never both at once.

## Layout
```
workflows/             built-in workflow files (embedded); bom.yaml = the BOM processor
cmd/miservice          server entrypoint (flags + app.Run)
cmd/migo               run a workflow file on local files without HTTP (-check; -sql-stub for tests)
internal/app           wiring; modules.go is the list of modules the service runs
internal/module        what a module is (Module / Starter / Closer / Services)
internal/workflow      the engine: strict parser, steps, expressions, sql/rest, branching, styled xlsx writer
internal/modules/workflows  serves each workflow at /api/<name>/{upload,download}
internal/modules/system     GET /api/health (the smallest example module)
internal/httpx         router, error responses, multipart upload, CORS + panic-recovery middleware
internal/config        config.json (typed shared keys; cfg.String/Int/Bool for a module's own keys)
internal/storage       generated files: atomic commit, newest-N per owner, retention cleanup
internal/jobs          concurrency gate for heavy jobs, memory release
internal/clients/sqlserver  shared lazy SQL Server pool
internal/jsx           JavaScript-exact trim / Number toString / parseFloat
internal/sheet         .xlsx streaming reader + BIFF8 .xls reader (typed cells, SheetJS range semantics)
examples/              a generic table workflow and its input
docs/WORKFLOWS.md      the workflow file reference
docs/EXTENDING.md      how to add a step type, a module or a custom endpoint
```

## Known differences
* Upload order: an uploaded file whose name contains `prio` or `shortage` is always taken as the Priority file
  (`match` in `workflows/bom.yaml`). Node also required the other file's name to contain `bom` or `.xls`. The two
  rules differ only for files uploaded without an Excel extension.
* Output is **value identical**: every cell value matches the Node/former Go output. Colours, fonts and borders
  come from the `output` styles in `workflows/bom.yaml` and differ in detail (e.g. fonts, debug-cell borders).
* A BOM whose first sheet has no BOM rows at all is rejected with "The selected BOM file is empty." (before:
  only a sheet without any cells was; one with only blank rows gave an empty workbook).
* The SAP request body is built in `bom.yaml`: keys are sent in alphabetical order, and `tcode` is sent as `null`
  when `sap_tcode` is not configured (before: omitted). Log wording differs slightly (e.g. header detection).
* Node's Zod "NOTE: unexpected field values" log lines are gone (static types make that check moot; output unaffected).
* Unknown routes return plain `Cannot GET /x` instead of Express' HTML error page.
* Input must be a real .xlsx or Excel 97-2003 (BIFF8) .xls. Excel 5/95 .xls, password-protected files and
  HTML/CSV files saved with an .xls extension are rejected with a clear error (SheetJS would try to guess).
* `JS toUpperCase` special cases (e.g. "ß" -> "SS") are not reproduced; irrelevant for part numbers/vendor names.
