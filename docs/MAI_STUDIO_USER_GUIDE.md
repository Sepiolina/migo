# Mai Studio user guide

Mai Studio builds data workflows for Excel and CSV files and publishes them as
MaiService API endpoints. A workflow reads one or more **sources** (`.xlsx`,
`.xls` or `.csv`), transforms them in ordered **steps**, and writes **outputs**
(sheets of an `.xlsx` workbook, or one `.csv` file). Studio runs every edit on
sample files with the production Go engine, so you see the table after each step
as you build it.

## Start

For Windows prerequisites and the HTTP microservice, see the
[README getting-started instructions](../README.md). From the repository root:

```sh
cd apps/studio
npm ci
npm run desktop:dev
```

`npm run dev` opens a browser preview that edits YAML only; opening folders,
previews, runs and saving need the desktop app.

## The home screen

Choose **Open folder…** and select your project folder, for example a clone of
this repository. Studio lists the workflows in its `workflows/` folder with the
route each one is served at, and offers templates:

| Template | Sources → output |
| --- | --- |
| Clean a sheet | xlsx → xlsx |
| Summarize by group | xlsx → xlsx |
| Look up and enrich | xlsx + csv → xlsx |
| Customer order totals | xlsx → xlsx (the tutorial below) |
| Excel to CSV | xlsx → csv |
| Blank workflow | xlsx → xlsx |

Templates come with sample files from `examples/`, so their table preview works
at once.

## The editor

| Area | What it holds |
| --- | --- |
| Toolbar | The workflow name, its status (valid, rows out, errors), Search (⌘K), Undo/Redo, appearance, panel switches, Save and Run |
| Left: Workflow | The **Endpoint** (`/api/<route>`), **Sources**, **Steps** in order (nested steps of `if` and `split` indented, YAML comments shown as section notes) and **Outputs** |
| Centre: the table | The data after the selected step, with a profile on each column: type, how full it is, distinct values or a histogram, blanks |
| Right: Inspector | Settings of the selected item, generated from the step registry |
| Bottom: Console | **Problems**, **Run log** (lines the workflow logs) and **YAML**, the whole file, editable |

On a phone-sized window the three areas become tabs: Steps, Table, Settings.

### Build by working on the table

Click a column header for actions: sort, keep rows that have a value or equal
a value, remove duplicates, fill blanks from above, convert to number, trim,
group by, rename and remove. Each action adds a step **after the selected one**,
selects it, and shows the new table. The step list shows each step's effect:
rows removed (−83), columns added (+1 col), log lines (☰) or an error.

Other ways to add a step:

- **+ Add step** in the step list, or ⌘⇧A, opens the step menu.
- **Search** (⌘K) finds any step type, command, step, source or workflow.

### Edit a step

Select a step to edit it in the inspector. Text fields apply when you leave them
or press Enter (⌘Enter in boxes); Escape reverts. Formula fields accept the
engine's expressions: column names directly (`Quantity * Price`),
`col("Unit price")` for names with spaces, functions such as `trim()`, `norm()`,
`prev("Col")`, `a ? b : c`. The column chips under a form insert a column name at
the cursor. **Show all** reveals rarely used settings. **Note** writes a YAML
comment above the step.

Each edit is one undo step; a toast offers Undo, and Undo also returns the
selection to where it was. Edits change only the lines they touch: comments,
formatting and settings Studio has no field for are kept, as are Windows (CRLF)
line endings.

The eye icon on a step previews the workflow **without** that step. It is not
saved; use Delete to remove a step. Move up/down, Duplicate and Delete are in
the inspector.

### Sources and outputs

Select a source to see the raw sheet with column letters and row numbers, which
is what fixed-column and header-row settings refer to. Set its **sample file**
(a path inside the folder); sample files are remembered per workflow on this
computer and are not part of the YAML. **File name** is a pattern that lets API
clients upload files in any order.

Select an output to name the sheet and choose its table. **Choose and format
columns…** switches the outputs to the detailed form, with column headers,
widths, number formats and conditional styles.

### The endpoint

Select **Endpoint** to set how MaiService serves the workflow: the route,
description, the requester-id form field and whether it is required, the upload
limit, and whether it returns an Excel workbook or a CSV file. The panel lists
the endpoints (`/run`, `/upload`, `/download`, the description and the
catalogue), the form clients send, and a `curl` example to copy.

## Run and save

**Run** (⌘↵) runs the workflow on the sample files and writes the output under
`.studio/runs/` in the folder, then shows it. Local runs are offline: SQL and
REST steps are skipped, and the Problems tab says so.

**Save** (⌘S) writes the file. A new workflow asks for a path; files in
`workflows/` become endpoints. Saves check that the file did not change on disk
since it was opened; a conflict keeps your edits so you can save them elsewhere.
Switching workflows or closing the window asks before discarding unsaved
changes.

## Worked example: customer order totals

1. On the home screen, open the repository folder and choose **Customer order
   totals**. Its source `orders` is bound to `examples/orders.xlsx`.
2. Select step 2, **Keep rows where num(Quantity) > 0**: the table shows 3 of
   the 5 rows and the step list shows −2.
3. Select step 3: **Line total** is added (green).
4. Select step 4: one row per customer, Acme 50 and Beta 5.
5. Press ⌘↵. The output **Totals** opens with the same values.
6. Press ⌘S and save as `workflows/order-totals.yaml`. MaiService then serves
   it at `/api/order-totals`.

`examples/orders-totals.yaml` is the same workflow.

## Build a Linux deployment package

Open a workspace containing the complete MaiService source checkout, including
`go.mod`, `go.sum`, and `cmd/maiservice`. Install Go on the build computer and put
it on PATH. The first build downloads missing Go modules automatically through
the Go module proxy and checksum database, so allow network access and time for
dependencies. Subsequent builds can use the local module cache. A workflow-only
folder cannot build the service. Build only a source checkout you trust.

Save workflow changes and apply or discard pending settings, then choose
**Build Linux package** in the toolbar. In the builder:

1. Choose Linux **amd64** or **arm64** to match the deployment machine.
2. Enter a saved, workspace-relative file named `config.json`, such as
   `deployment/config.json`. The builder reads configuration and workflow files
   from disk, not unsaved editor buffers.
3. Select saved **Workflow overrides**. Built-in endpoints remain available;
   selected files replace matching built-ins or add endpoints. This selection is
   not an endpoint allowlist. Override filenames must be unique lowercase
   `.yaml` names starting with a letter and using letters, numbers, and hyphens.
4. Set a new installation directory directly under `/opt`, the service name,
   PM2 restart-memory threshold, and Go memory target. Defaults use
   `/opt/maiservice` and `maiservice`. The HTTP port is not a package setting:
   it is the `"port"` value in the chosen `config.json`, which must be set
   (1–65535). The package's PM2 file does not repeat it. Memory settings are runtime
   guidance; they do not reserve memory or replace capacity testing.
5. Choose **Secure** or **Trusted local**, then choose **Build ZIP package**.
   Keep the window open; build cancellation is unavailable.

**Secure** signs the checksum inventory with Ed25519 and encrypts configuration
with age for the server. Choose **Create signing identity** once. Its private key
is retained in the build computer's OS credential store, accessed by the native
host; the frontend receives the public key and fingerprint. Unlock/configure the
credential store if access fails. There is no signing-key export, backup,
recovery, or rotation UI yet. Retain the public verification key independently;
losing the OS identity means future packages may need a new trusted signer.

Create the server's age identity once on the Linux machine with `age-keygen`.
Install age using your approved provisioning process, then, as an administrator:

```sh
sudo install -d -m 700 /etc/maiservice
sudo sh -c 'umask 077; age-keygen -o /etc/maiservice/age-identity.txt'
sudo age-keygen -y /etc/maiservice/age-identity.txt
```

Do not regenerate or overwrite an identity used for existing packages. Copy only
the printed **public** `age1…` recipient into **Deployment age public recipient**.
Keep the private identity file protected on the server and arrange its backup
through your approved secret-management process. The desktop does not need it.

**Trusted local** creates an unsigned package containing plaintext configuration,
including any secrets. Use it only where that exposure is acceptable. Studio does
not silently fall back to this mode when secure signing fails.

The result shows `.studio/packages/package-*/mai.zip`, its SHA-256, included files,
and validation status. The ZIP contains the Linux `maiservice` executable,
configuration, selected workflow overrides, PM2 settings, checksum inventory,
installation scripts, and deployment notes. Secure mode adds the signature and
uses `config.json.age`. A successful cross-build verifies the ELF architecture;
read the reported validation status to see whether a local health check actually
ran. It does not prove target-machine or live-integration behavior.

**Save profile** stores public, non-secret deployment settings in this computer's
localStorage, scoped to the workspace. Profiles contain paths, limits, selected
workflows, and the public age recipient, not configuration contents or private
keys. Clearing profiles does not delete the OS signing identity.

### Verify before extracting or installing

Obtain [scripts/verify-mai-package.py](../scripts/verify-mai-package.py) from an
independently trusted checkout/channel, plus Python 3 and OpenSSL 3 on the server.
Obtain the builder's base64 **Signing public key** through a trusted channel and
retain/compare it independently of the ZIP. A key bundled in the same ZIP is not
a trust anchor. Before extracting, run:

```sh
python3 scripts/verify-mai-package.py --trusted-key 'BASE64_PUBLIC_KEY_FROM_STUDIO' mai.zip
```

This verifies the signature, complete payload inventory, paths, and checksums
before extraction. Do not run any script or executable from the ZIP before this
verification succeeds. A ZIP hash alone does not establish who signed it.

For an intentionally unsigned trusted-local package, explicitly use:

```sh
python3 scripts/verify-mai-package.py --allow-unsigned mai.zip
```

That checks integrity and inventory, not authenticity. Only after successful
verification, extract into a new empty directory:

```sh
package_dir=$(mktemp -d)
unzip mai.zip -d "$package_dir"
```

The server needs Python 3, Node.js, and PM2; secure installation also needs age.
Create a dedicated service account if absent:

```sh
sudo useradd --system --create-home --shell /bin/bash maiservice
```

Skip that command if `maiservice` already exists. Do not pre-create
`/opt/maiservice`: the installer requires an absent target and refuses replacement of an existing installation. For the default profile:

```sh
sudo env AGE_IDENTITY=/etc/maiservice/age-identity.txt sh "$package_dir/mai/install.sh"
sudo chown -R maiservice:maiservice /opt/maiservice
sudo -u maiservice -H pm2 start /opt/maiservice/ecosystem.config.js
sudo -u maiservice -H pm2 save
curl --fail http://127.0.0.1:3081/api/health
```

For trusted-local mode, omit `AGE_IDENTITY`. The installer stages files beside
the target, decrypts/copies configuration with restrictive permissions
(`config/config.json` is mode `0600`), then moves the staged directory into place.
Run PM2 as the service user, not root. Follow the
[README PM2 instructions](../README.md#deploy-on-linux-with-pm2) for startup-on-boot
setup and runtime checks, using the package's generated ecosystem configuration.
Test representative uploads/downloads and integrations on the actual target.

This installer supports new targets only. Updating an existing deployment
requires a deliberate stop, backup, move-aside, install, and rollback plan; there
is no in-place upgrade, automatic rollback, or deployment/upload button.

## Workspace limits and troubleshooting

Workflow files must be UTF-8, at most 1 MiB, and end in `.yaml` or `.yml`. Absolute
paths, `..` traversal, and symlinks in workflow paths are rejected. Workbook paths
must resolve to files inside the selected workspace. A selected workspace grants
local file access to that folder; choose a folder you trust.

If native commands are disabled, start `npm run desktop:dev` rather than the
browser preview and open a workspace. If an input cannot be read, check that its
path is relative to the workspace and the file exists there. If the sidecar
cannot start, verify Go is on PATH or set `MAI_GO` to its executable. Missing
GTK/WebKit libraries or a missing desktop session prevent the Linux native host
from starting; see the development guide for dependencies.

Cancellation and live desktop connections to SQL and REST services are
planned.

Each package also includes `release.json` with the target and available build
provenance, and `sbom.cdx.json` with a CycloneDX Go dependency inventory. These
files are covered by the checksum manifest/signature. The inventory is not a
vulnerability audit. Avoid editing saved source or inputs during compilation.

## Recover from editing and preview problems

- **The YAML has an error**: the table and forms keep the last valid version.
  Fix the file in the YAML tab of the console; the error is listed under
  Problems.
- **A step failed**: its row turns red, later steps show "did not run", and the
  error is under Problems. Click the problem to select the step.
- **Add a sample file**: a source has no sample file, so nothing can be
  previewed. Select the source and enter a path inside the folder.
- **Studio has no form for a step**: the inspector shows its YAML; edit it in
  the YAML tab. The step is saved unchanged otherwise.

Preview limits apply to inputs and outputs: 25 MiB per file and 64 MiB expanded
for XLSX. The table shows the first 200 rows of a step; column profiles cover
every row.
