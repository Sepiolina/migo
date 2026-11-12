# MaiService and Mai Studio

**Mai Studio** is a desktop app (Tauri v2 and React) for building data workflows
over Excel and CSV files. Pick a source, then work on the table: each column
action or form edit becomes a step, and Studio runs the production engine on
sample files so you see the data after every step. A workflow is one YAML file,
and the same file is an API endpoint.

**MaiService** runs those workflows as an HTTP microservice. Each workflow accepts
`.xlsx`, `.xls` or `.csv` uploads and returns an Excel workbook or a CSV file. It runs on Windows for development
and on Linux under PM2 for deployment. The `migo` CLI runs workflows without HTTP.

See [current capabilities](docs/CURRENT_CAPABILITIES.md) for what works today,
validation coverage, and features still planned.

## Choose where to start

- [Run the Windows desktop app](#run-the-windows-desktop-app)
- [Run the microservice on Windows](#run-the-microservice-on-windows)
- [Run the microservice on Linux](#run-the-microservice-on-linux)
- [Build a Linux deployment ZIP in Studio](#build-a-linux-deployment-zip-in-studio)
- [Build a test folder for Linux or Windows](#build-a-test-folder-for-linux-or-windows)
- [Deploy on Linux with PM2](#deploy-on-linux-with-pm2)
- [Run a workflow without a server](#run-a-workflow-without-a-server)
- [Development checks](#development-checks)
- [Troubleshooting](#troubleshooting)

Desktop installation currently requires building from source. Studio can build
Linux `mai.zip` service packages with signing and configuration encryption, or
explicit trusted-local plaintext mode. Signed desktop installer distribution,
live desktop connection profiles, and full Excel style preview remain planned. The
current preview shows workbook cell values; the generated `.xlsx` is the actual
output of the Go engine.

## Get the source

Install Git, then open PowerShell on Windows or a terminal on Linux:

```sh
git clone https://github.com/Sepiolina/migo.git
cd migo
```

All commands below start in this repository folder unless stated otherwise.
When returning from the desktop app folder, use `cd ../..` first.

## Run the Windows desktop app

### 1. Install the prerequisites

Install these tools, then open a **new PowerShell window**:

| Tool | What to install |
| --- | --- |
| Go | Go 1.22 or newer from [go.dev](https://go.dev/dl/) |
| Node.js | Latest Node.js 22 LTS or a newer supported LTS from [nodejs.org](https://nodejs.org/) |
| Rust | Stable Rust with the MSVC toolchain from [rustup.rs](https://rustup.rs/) |
| C++ build tools | Visual Studio Build Tools 2022: select **Desktop development with C++** and a Windows SDK |
| WebView2 | Microsoft Edge WebView2 Runtime; normally present on current Windows installations |

See the [Tauri v2 Windows prerequisites](https://v2.tauri.app/start/prerequisites/#windows)
for the installers and platform details. This is a native Windows build; use
PowerShell rather than WSL for the Windows desktop app.

Check that the tools are available:

```powershell
go version
node --version
npm --version
rustc --version
cargo --version
```

### 2. Install and launch

From the repository folder:

```powershell
cd apps/studio
npm ci
npm run desktop:dev
```

The command builds the Go sidecar, starts the frontend, and opens **Mai Studio**.
The first launch downloads and compiles dependencies and may take several minutes.
Keep this PowerShell window open. Press `Ctrl+C` there to stop the development app.
A browser opened at the development URL supports editor preview only; workspace,
Excel, validation, and execution features require the native desktop window.

### 3. Create and run a workflow

1. On the home screen choose **Open folder…** and select the cloned `migo`
   folder.
2. Choose the **Customer order totals** template. Its source is bound to the
   bundled `examples/orders.xlsx`, so the table shows data at once.
3. Select the steps on the left to see the table after each one; click a column
   header to add a step such as a sort or a filter.
4. Press **Run** (⌘↵). The **Totals** output shows **Acme = 50** and
   **Beta = 5**.
5. Press **Save** (⌘S) and save it in `workflows/`. MaiService serves it at
   `/api/<file name>`; select **Endpoint** for the request format and a `curl`
   example, and **Build Linux package…** to distribute it.

Runs are offline: SQL and REST steps are skipped. Generated files are under
`.studio/runs/`; sources are read-only. See the
[Studio user guide](docs/MAI_STUDIO_USER_GUIDE.md) for the editor, shortcuts and
limitations.

### 4. Build a Windows installer

From `apps/studio`:

```powershell
npm run desktop:build
Get-ChildItem src-tauri/target/release/bundle -Recurse -File
```

Tauri places installer artifacts under `src-tauri/target/release/bundle/`, normally
in `msi` and/or `nsis` subfolders. Open the generated installer on the target
Windows machine. This builds the desktop app; it does not build the Linux
microservice package. Signing and distribution require separate release setup.

## Run the microservice on Windows

Only Go is needed to build the microservice. Node.js, Rust, and the desktop app
are not required. A prebuilt executable needs only its configuration and access
to any configured company services.

### 1. Create local configuration

From the repository folder, copy the example only if you have no config yet:

```powershell
if (-not (Test-Path config.json)) {
    Copy-Item config.example.json config.json
}
notepad config.json
```

For the first local run, change these two values and save the file:

```json
"db_enabled": false,
"sap_enabled": false
```

These are fields inside `config.json`, not a complete replacement file. Disabling
them skips company database/SAP enrichment. Enable them later only after entering
real connection settings and confirming network access. Keep credentials private.

### 2. Build and start

```powershell
go build -o maiservice.exe ./cmd/maiservice
.\maiservice.exe -config config.json
```

Leave this terminal running. The default port in the example config is **3081**.
To use a different port, stop the service with `Ctrl+C`, then run:

```powershell
$env:PORT = "3090"
.\maiservice.exe -config config.json
```

Use that port in requests too. `Remove-Item Env:PORT` restores the config-file port
for subsequent starts.

### 3. Test a real upload and download

Open a **second PowerShell window** in the repository folder:

```powershell
curl.exe --fail http://localhost:3081/api/health
curl.exe --fail -F "files=@testdata/bom_basic.xlsx" -F "files=@testdata/prio_basic.xlsx" -F "bam=113280" http://localhost:3081/api/bom/upload
curl.exe --fail -o "$env:TEMP/mai-output.xlsx" "http://localhost:3081/api/bom/download?bam=113280"
```

Expected results: health reports `"status":"online"`, upload reports
`"status":"success"`, and the downloaded workbook opens in Excel. Use `curl.exe`
explicitly: Windows PowerShell's `curl` may be a different command. Stop the server
with `Ctrl+C` in its original terminal.

## Run the microservice on Linux

Install Go 1.22 or newer on the build machine. From the repository folder:

```sh
# Copy only when config.json does not exist.
if [ ! -e config.json ]; then cp config.example.json config.json; fi
```

Edit `config.json` and set `db_enabled` and `sap_enabled` to `false` for an offline
local test, as described in the Windows section. Then:

```sh
go build -o maiservice ./cmd/maiservice
./maiservice -config config.json
```

In a second terminal in the repository folder:

```sh
curl --fail http://localhost:3081/api/health
curl --fail -F files=@testdata/bom_basic.xlsx -F files=@testdata/prio_basic.xlsx -F bam=113280 http://localhost:3081/api/bom/upload
curl --fail -o /tmp/mai-output.xlsx "http://localhost:3081/api/bom/download?bam=113280"
```

Config lookup is `-config`, then `MAISERVICE_CONFIG`, then `config.json` in the
working directory, then beside the executable. `PORT` overrides the configured
port. Use an explicit `-config` path for predictable deployments. Uploads and
outputs are stored **beside the selected config**, not necessarily beside the
binary. The loader can fall back to `config.example.json`; always supply an
intentional config for deployment.

## Build a Linux deployment ZIP in Studio

Open the full trusted source checkout as the workspace and install Go on PATH.
Missing Go modules download automatically through the module proxy and checksum
database on the first build. Save workflow edits, then choose **Build Linux
package**. Select amd64/arm64, a saved relative `config.json`, workflow overrides,
installation folder, port, and memory limits. Built-in workflow endpoints remain
available; selected overrides add or replace them rather than restrict routes.

**Secure** creates an OS-stored signing identity and encrypts configuration for
the server's age public recipient. Generate the private age identity once on the
server with `age-keygen` and copy only its public `age1…` recipient into Studio.
The signing private key remains in the build computer's OS credential store;
there is no key export/recovery/rotation UI yet. **Trusted local** is explicitly
unsigned and includes readable configuration secrets.

The result is `.studio/packages/package-*/mai.zip`, with its hash, inventory, and
validation status. Cross-compilation and ELF checks do not prove the binary runs
on the target architecture. Before extracting, obtain the verifier independently
from a trusted checkout and verify with Python 3 and OpenSSL 3:

```sh
python3 scripts/verify-mai-package.py --trusted-key 'BASE64_PUBLIC_KEY_FROM_STUDIO' mai.zip
```

Retain/confirm that public key through a trusted channel outside the ZIP. Never
execute scripts from the ZIP before signature and full inventory verification.
Unsigned trusted-local archives require explicit `--allow-unsigned` instead.

Follow the [package builder guide](docs/MAI_STUDIO_USER_GUIDE.md#build-a-linux-deployment-package)
for server age setup, verification, and installation. The installer requires a
new target and does not replace an existing `/opt/maiservice`. For a privileged
install, transfer ownership to the dedicated service user before starting PM2 as
that user. Configuration is decrypted with server-local `AGE_IDENTITY` and stored
with mode `0600`. Packages include their own `ecosystem.config.js`; the manual
PM2 example below is an alternative for directly built binaries.

## Build a deploy folder for Linux or Windows

One command builds a folder you can copy to a server and run. It works the same
in PowerShell and Linux shells:

```sh
go run ./tools/dist
```

It asks for Linux, Windows or both. To choose on the command line instead:

```sh
go run ./tools/dist -os linux
```

Use `-os windows` or `-os all` for the other targets, `-arch arm64` for ARM
servers, `-port 3090` for the port written into a new `config.json`, and `-cli`
to also build the `migo` workflow CLI. On Linux, `make dist TARGET=linux` does
the same. On Windows you can also double-click `build-dist.bat`, or run
`.\build-dist.bat windows` from a terminal.

The result is `dist/maiservice-<os>-<arch>/` with three files:

| File | Purpose |
| --- | --- |
| `maiservice` / `maiservice.exe` | The HTTP service |
| `config.json` | All settings, **including the port**. Starts with DB and SAP off. Kept when you rebuild |
| `ecosystem.config.js` | PM2 process file. Identical for Linux and Windows; it picks the right executable itself |

The port is set only in `config.json`; `ecosystem.config.js` does not repeat it.
On the server, from that folder, run either:

```sh
./maiservice                 # foreground; finds config.json beside it (maiservice.exe on Windows)
pm2 start ecosystem.config.js
```

`uploads/` and `outputs/` are created on first start. Rebuilding replaces the
binary and `ecosystem.config.js` but keeps `config.json`, `uploads/`, `outputs/`
and `workflows/`. For real use, set the DB and SAP values in `config.json`.
`config.example.json` in the repository lists every key.

## Deploy on Linux with PM2

PM2 runs the Go executable directly. Node.js and PM2 are required on the server
only for process supervision; the Go executable itself does not need Node or Go.
Use a supported Node LTS and your team's approved PM2 version.

### 1. Build the Linux executable

On Linux, from the repository folder:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/maiservice ./cmd/maiservice
```

For an ARM64 server, change `GOARCH=amd64` to `GOARCH=arm64`.
From Windows PowerShell, build the same artifact with:

```powershell
$env:CGO_ENABLED = "0"
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -trimpath -o dist/maiservice ./cmd/maiservice
Remove-Item Env:CGO_ENABLED, Env:GOOS, Env:GOARCH
```

The Linux executable cannot run directly on Windows. Test it on the target Linux
architecture before deployment.

### 2. Prepare the server folder

The following example uses `/opt/maiservice` and a dedicated `maiservice` user.
Administrator setup, on the Linux server:

```sh
sudo useradd --system --create-home --shell /bin/bash maiservice
sudo install -d -o maiservice -g maiservice /opt/maiservice
```

Skip user creation if the account already exists. Install Node/PM2 through your
approved server provisioning process. Copy `dist/maiservice` to the server as
`/tmp/maiservice`, then install it:

```sh
sudo install -o maiservice -g maiservice -m 755 /tmp/maiservice /opt/maiservice/maiservice
```

Create `/opt/maiservice/config.json` from the example with the intended DB/SAP
settings. Copy selected workflow overrides into `/opt/maiservice/workflows/` if
needed. Protect the config:

```sh
sudo chown maiservice:maiservice /opt/maiservice/config.json
sudo chmod 600 /opt/maiservice/config.json
```

### 3. Create the PM2 configuration

Create `/opt/maiservice/ecosystem.config.js` with this content. This is a file you
create on the server; there is no pre-existing deployment template to copy.

```js
module.exports = {
  apps: [{
    name: 'maiservice',
    script: '/opt/maiservice/maiservice',
    cwd: '/opt/maiservice',
    interpreter: 'none',
    args: ['-config', '/opt/maiservice/config.json'],
    exec_mode: 'fork',
    instances: 1,
    autorestart: true,
    min_uptime: '10s',
    max_restarts: 10,
    exp_backoff_restart_delay: 1000,
    max_memory_restart: '650M',
    kill_timeout: 15000,
    time: true,
    env: { GOMEMLIMIT: '400MiB' }   // the port comes from config.json
  }]
};
```

Keep `instances: 1`: file registries belong to one
process. `kill_timeout` allows time for the service's graceful shutdown. Adjust
memory limits to the server's budget.

### 4. Start and verify

Run these as the **maiservice user**, which must also have the `pm2` command on
its PATH:

```sh
sudo -iu maiservice
cd /opt/maiservice
pm2 start ecosystem.config.js
pm2 status
curl --fail http://localhost:3081/api/health
pm2 logs maiservice --lines 30
```

Exit the log viewer with `Ctrl+C`; PM2 keeps the service running. Perform the
upload/download test with representative input files before accepting deployment.
For start-on-boot, still as the service user:

```sh
pm2 startup systemd -u maiservice --hp /home/maiservice
```

Run the administrator command PM2 prints, then run `pm2 save` as the service user.
Verify startup after a controlled reboot. Configure log rotation, backups, disk
monitoring, and your reverse proxy/firewall. Health is a liveness check; it does
not prove DB/SAP access. Restrict access to trusted users until the planned
service access controls are implemented.

To update, stage a new executable under another filename, preserve the previous
version, rename the new file into place, and run `pm2 restart maiservice`. Do not
copy over a running Linux executable. Recheck health and upload/download behavior;
restore the previous executable and restart if verification fails.

## Run a workflow without a server

From the repository folder, this command works in PowerShell and Linux shells:

```sh
go run ./cmd/migo -in parts=examples/parts.xlsx -out parts-output.xlsx examples/parts-summary.yaml
```

Open `parts-output.xlsx` to inspect the result. To validate by executing without
keeping output:

```sh
go run ./cmd/migo -check -in bom=testdata/bom_basic.xlsx -in priority=testdata/prio_basic.xlsx workflows/bom.yaml
```

Default CLI configuration disables database and SAP enrichment. The
[workflow reference](docs/WORKFLOWS.md) covers SQL stubs, expressions, and steps.

## Development checks

Go checks, from the repository folder:

```sh
go vet ./...
go build ./...
go test ./...
```

With a C compiler installed, also run `go test -race ./...`. GNU Make users can
run `make help`, `make build`, or `make check`; `make check` is the Go CI gate.
Desktop checks, from `apps/studio`:

```sh
npm ci
npm test
npm run build
node scripts/smoke-runner.mjs
```

The last command needs Go and exercises the real runner/workbook protocol.
See the [desktop developer guide](docs/MAI_STUDIO_DEVELOPMENT.md) for Rust/native
checks. Windows installer builds and manual desktop interaction need verification
on Windows; Linux CI is not a substitute for that check.

## Troubleshooting

| Problem | What to do |
| --- | --- |
| A tool is not recognized | Open a new terminal after installation; rerun the version checks. |
| Desktop C++/linker error | Install the C++ workload and Windows SDK; confirm Rust uses an MSVC host toolchain. |
| Desktop opens only in a browser | Use `npm run desktop:dev`, not `npm run dev`. |
| Vite reports `EBUSY` on a Rust `.pdb` file | Update to the watcher fix, stop the dev command, and restart `npm run desktop:dev`. Native files under `src-tauri` are excluded from Vite watching. |
| Go sidecar cannot launch | Rerun `npm run desktop:dev` to rebuild it for your host. |
| Config is missing | Supply the actual path with `-config`; check that the file exists. |
| Port already in use | Stop the other process or set `PORT` and use that same port in requests. |
| Windows executable cannot be rebuilt | Stop the running executable before rebuilding it. |
| Upload fails only with company integrations enabled | Check DB/SAP settings, credentials, and connectivity from the machine running the service. |
| PM2 reports `errored` | Inspect `pm2 logs maiservice --err`; check executable architecture, paths, permissions, and config. |
| A save reports a conflict | Save your edits to a new unused path, then reopen the changed original to compare. |

## How workflows and files are organized

Each `workflows/<name>.yaml` defines one process. Built-in workflows are embedded
in the service; `<config directory>/workflows/<name>.yaml` can replace or add one.

| Endpoint | Purpose |
| --- | --- |
| `GET /api/health` | Liveness response |
| `POST /api/<name>/upload` | Multipart `files` plus employee number `bam` |
| `GET /api/<name>/download?bam=E[&file=F]` (or `?employeeId=E`) | Employee E's output F, or E's newest output; never another employee's. No owner → 400 |

BOM upload takes a BOM workbook and a Priority workbook. A filename containing
`prio` or `shortage` is recognized as Priority. Outputs live in
`outputs/<workflow>/` beside config. Upload files are normally deleted at request
completion; `.keep.json` records uploads explicitly retained by a module. Output
retention and upload cleanup are managed by the storage layer.

| Folder | Purpose |
| --- | --- |
| `apps/studio` | Desktop frontend and Tauri host |
| `cmd/maiservice` | Microservice entry point |
| `cmd/migo` | Standalone workflow CLI |
| `cmd/mai-runner` | Desktop protocol sidecar |
| `internal/workflow` | Production workflow parser and engine |
| `internal/modules` | HTTP feature modules |
| `workflows` | Built-in YAML workflows |
| `examples`, `testdata` | Example inputs and test fixtures |

### Workbook compatibility

Inputs must be real `.xlsx` or Excel 97–2003 BIFF8 `.xls` files. Password-protected
workbooks, older Excel 5/95 files, and CSV/HTML renamed to `.xls` are unsupported.
The engine reads input cells as text; output values and supported formatting come
from the workflow definition. It does not implement Excel formula recalculation.
Keep a copy of original inputs and verify representative outputs in Excel before
using a new workflow in production.

Interactive documentation: [`docs/mai-studio-guide.html`](docs/mai-studio-guide.html) is an offline field guide with platform setup tabs. Its editor screenshots and walkthrough show the previous block editor; the [Studio user guide](docs/MAI_STUDIO_USER_GUIDE.md) describes the current one.

Further reading: [Studio user guide](docs/MAI_STUDIO_USER_GUIDE.md),
[workflow reference](docs/WORKFLOWS.md), [extending MaiService](docs/EXTENDING.md),
and [implementation plan](docs/MAI_DEV_TOOL_IMPLEMENTATION_PLAN.md).

If Studio reports **Workbook saved; preview unavailable**, the workflow succeeded.
Open the generated XLSX at the displayed path in Excel; preview limits do not
delete a successful output. See [editing and preview troubleshooting](docs/MAI_STUDIO_USER_GUIDE.md#recover-from-editing-and-preview-problems).

For a step-by-step explanation of the BOM rules, open [the BOM HTML guide](docs/bom-workflow-guide.html) in a browser.
