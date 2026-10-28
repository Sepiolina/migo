# Mai Studio user guide

Mai Studio builds workflows with visual blocks and runs them locally with the production Go
engine. This milestone supports offline Excel fixtures. SQL and external REST
connections are disabled.

## Start and open a workspace

For Windows prerequisites, PowerShell commands, and running the HTTP microservice,
see the [README getting-started instructions](../README.md).

From the repository root:

```sh
cd apps/studio
npm ci
npm run desktop:dev
```

Install the native prerequisites listed in the
[development guide](MAI_STUDIO_DEVELOPMENT.md) first. `npm run dev` opens an editor
preview in a browser; folder access, saving, validation, workbook inspection, and
execution require the Tauri desktop app.

Open **Project & inputs**, then choose **Open workspace** and select a local
project folder. All workflow and workbook paths are relative to that folder. For the bundled example, select the
repository root. Use synthetic inputs while developing; keep private workbooks,
credentials, and generated runs out of source control.

## Open, create, and save workflows

The **Open a workflow…** list discovers `.yaml` and `.yml` files recursively inside
the workspace's `workflows/` directory. It skips symlinks. You can also enter a
workspace-relative YAML file in **Open path** and choose
**Open workflow path**, including files outside `workflows/`. The initial document
is a built-in parts-summary example. Workflow controls and input bindings are in
**Project & inputs**.

Choose **+ New workflow** and select **Copy a worksheet** or **Filter and
summarize**. The template becomes the current document and suggests a Save path.
For a new file, choose an unused workspace-relative path such as
`workflows/summary.yaml`; create the parent directory outside Studio first if
needed. Saving to a different unused path creates a copy; an existing destination
cannot be overwritten this way. `Ctrl/Cmd+S` saves to the current Save path.

A dot and **Unsaved** mark changes since the last successful save or load.
Switching workflows or workspaces, or closing the native window, asks whether to
discard unsaved work; choose **Keep editing** to retain it. Save before closing.
There is no autosave or recovery store, and input bindings are session settings rather than
part of the saved YAML.

A save checks whether the file changed on disk since it was opened or last saved.
A conflict leaves your editor contents intact and reports an error in the status
bar. Preserve those contents in a new, unused Save path, then reopen the original
to compare the external changes. Studio has no merge dialog or force-overwrite
control. Discovery is refreshed when opening a workspace; reopen the workspace if
files added externally do not appear.

## Build a workflow with blocks

**Blocks** opens by default. In **Workbook setup**, edit the workflow display
name, input identifiers and labels, and output sheet names and saved table names.
Choose **Apply workbook settings** or **Discard setup changes**. Advanced output
settings such as styles are preserved. If existing settings cannot be safely
represented, use the advanced editor to change them. Renaming an input or table
requires updating matching blocks yourself.

In **Read source**, choose a sheet **name / first** or **number**. Leave the
name blank for the first sheet; numbered sheets start at 1.

For a first workflow, choose **+ New workflow** and **Copy a worksheet**. In
Workbook setup, keep input identifier `parts` and set the output sheet's saved
table name to `result`. In the connected chain, set **Read source** to `parts`
and enter a sheet name if needed; set **Save dataset as** to `result`. Apply any
workbook setup changes. The output sheet refers to that saved dataset. Bind a
workbook and run it as described below, then save the workflow and build a Linux
package if needed.

Drag blocks from the toolbox and snap steps into the Workflow chain. Expression
blocks snap into compatible inputs for conditions and calculations. Common blocks
read, filter, set, rename and select columns, sort, group, join, remove duplicates,
and save datasets. Conditions and calculations can combine column values, text,
numbers, booleans, comparisons, arithmetic, and logic. The column value dropdown
can convert a value to a number or text; actual types and behavior are determined
by the Go runtime. List fields use comma-separated names; names containing commas
cannot be represented as individual entries there and are preserved when loaded
in unsupported shapes.

Incomplete or disconnected blocks remain as a draft. Finish connecting and filling
them, or discard block edits, before saving, running, switching views, or closing.
Likewise, apply or discard workbook setup changes; invalid settings cannot be
applied. Existing unsupported, nested, or custom steps appear as preserved blocks,
and unsupported expressions as preserved custom expression blocks. They retain
their source but are not fully editable in Blocks. Use **YAML source** for complex
branching and other advanced shapes. Go validates and executes the workflow.

**Workflow canvas** shows the ordered graph and offers the existing searchable
**+ Add node** library and guided top-level settings. Select a step, edit its
settings, and choose **Apply changes** or **Discard changes**. Canvas ports show
the flow; they are not editable connections. **Readable 100%**, **Overview**, and
**Focus selected** control the view. Use **Advanced YAML** or **YAML source** for
settings that the guided forms cannot represent.

**Undo** and **Redo** cover source edits and applied step changes. Loading a
document resets history. Outside text fields, `Ctrl/Cmd+Z` undoes and
`Ctrl/Cmd+Shift+Z` or `Ctrl/Cmd+Y` redoes. Canvas shortcuts include `Alt+Insert`
for the node library, `Delete`/`Backspace` to delete the selected top-level step,
and `Alt+Up`/`Alt+Down` to move it.

## Validate, bind inputs, and run

1. Choose **Validate** to check the current workflow with the Go parser. Errors
   appear in the status bar. Validation populates the declared input fields.
2. Open **Project & inputs**. Under **Input files**, bind each input name to an
   existing workbook inside the workspace. For the initial parts-summary example, bind `parts` to
   `examples/parts.xlsx`. Validate again after changing input declarations.
3. Choose **Run workflow**. The runner executes the current editor contents, so
   saving is a separate action. External SQL/REST operations cannot run offline.
4. Open **Excel data** to inspect the generated workbook. Choose an **Input** tab
   to inspect a bound workbook or **Output workbook** for the generated artifact.
   You can also enter a workbook path and choose **Inspect workbook**.
5. Use the sheet selector and **Previous**/**Next** to page through rows. Click a
   cell to read its complete value in the cell inspector; **Clear** dismisses it.
   **Expand data** gives the panel more space, **Dock panel** restores it, and
   **Close** returns the space to the canvas.

Outputs are written under `.studio/runs/` in the workspace. The output path and
SHA-256 identify the generated XLSX artifact. The preview reads actual workbook
cell values; styling, formula metadata, and full Excel visual fidelity are not
shown. Open the artifact in a spreadsheet application for those details.

Editing the workflow or changing input bindings marks the displayed output stale. Run
again to refresh it. Changes to workbook files made outside Studio are not
tracked automatically; rerun after changing them.

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
   HTTP port, PM2 restart-memory threshold, and Go memory target. Defaults use
   `/opt/maiservice`, `maiservice`, and port `3081`. Memory settings are runtime
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

Intermediate execution traces, cancellation controls, and live desktop
connections are planned.

Each package also includes `release.json` with the target and available build
provenance, and `sbom.cdx.json` with a CycloneDX Go dependency inventory. These
files are covered by the checksum manifest/signature. The inventory is not a
vulnerability audit. Avoid editing saved source or inputs during compilation.
