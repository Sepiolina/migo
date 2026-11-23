import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { request, type Inspection, type RunHistoryEntry, type RunHistoryPage, type RunManifest } from "./api";
import { DataGrid, profileRows } from "./DataGrid";
import { useCurrentRequest } from "./useCurrentRequest";
import { useFileRevisions } from "./useFileRevisions";
import { WorkbookPreview } from "./WorkbookPreview";
import "./runs.css";

type CurrentInput = { name: string; path?: string; sha256?: string | null };
type CurrentWorkflow = { workflowPath: string; source: string; inputs: CurrentInput[] };
type Props = {
  workspace: string;
  onBack: () => void;
  current?: CurrentWorkflow;
  onRunCurrent?: () => void;
  canRunCurrent?: boolean;
  refreshToken?: string;
  onRefreshCurrent?: () => void;
};

const pageSize = 20;
const csvPageSize = 100;
const basename = (path: string) => path.split(/[\\/]/).at(-1) || path;

function digest(value?: string | null) {
  return value && /^[a-f\d]{64}$/i.test(value) ? value.toLowerCase() : undefined;
}

function compareDigest(before?: string | null, after?: string | null) {
  const historical = digest(before);
  const current = digest(after);
  return !historical || !current ? "Unknown" : historical === current ? "Matches" : "Changed";
}

function Hash({ value }: { value?: string | null }) {
  return digest(value) ? <code>{value}</code> : <span className="meta">Unknown</span>;
}

function RunTime({ value }: { value: string }) {
  return <time dateTime={value}>{new Date(value).toLocaleString()}</time>;
}

function Provenance({ manifest, current }: { manifest: RunManifest; current?: CurrentWorkflow }) {
  const source = current?.source;
  const subtle = globalThis.crypto?.subtle;
  const [yaml, setYaml] = useState<{ source: string; sha256?: string }>();
  useEffect(() => {
    if (source === undefined || !subtle) return;
    let active = true;
    subtle.digest("SHA-256", new TextEncoder().encode(source)).then(
      (bytes) => {
        if (!active) return;
        const sha256 = Array.from(new Uint8Array(bytes), (byte) => byte.toString(16).padStart(2, "0")).join("");
        setYaml({ source, sha256 });
      },
      () => { if (active) setYaml({ source }); },
    );
    return () => { active = false; };
  }, [source, subtle]);

  const before = manifest.inputs ?? [];
  const names = new Set(before.map((input) => input.name));
  const added = current?.inputs.filter((input) => !names.has(input.name)) ?? [];

  return <>
    <h3>Provenance</h3>
    {manifest.uncertain && <p className="runs-warning">Input provenance is uncertain. One or more inputs changed during this run.</p>}
    <p className="runs-path"><code>{manifest.workflowPath || "Unsaved workflow"}</code></p>
    {current && <>
      <h3>Current workflow comparison</h3>
      <dl className="runs-fields">
        <dt>Workflow path</dt>
        <dd>{!manifest.workflowPath || !current.workflowPath ? "Unknown" : manifest.workflowPath === current.workflowPath ? "Matches" : <span>Changed · <code>{current.workflowPath}</code></span>}</dd>
        <dt>Current YAML</dt>
        <dd>{subtle && yaml?.source !== source ? "Checking…" : compareDigest(manifest.yamlSha256, yaml?.sha256)}</dd>
      </dl>
    </>}
    <h3>Inputs</h3>
    {before.length === 0 && <p className="meta">No inputs recorded.</p>}
    {(before.length > 0 || added.length > 0) && <ul className="runs-inputs">
      {before.map((input) => {
        const binding = current?.inputs.find(({ name }) => name === input.name);
        return <li key={input.name}>
          <b>{input.name}</b>
          <dl className="runs-fields">
            <dt>Path</dt><dd><code>{input.path || "Unknown"}</code></dd>
            {current && <>
              <dt>Current binding</dt>
              <dd>{!binding ? "Removed" : !binding.path ? "Unbound" : !input.path ? "Unknown" : input.path === binding.path ? "Matches" : <span>Changed · <code>{binding.path}</code></span>}</dd>
              <dt>Current bytes</dt><dd>{compareDigest(input.sha256, binding?.sha256)}</dd>
            </>}
          </dl>
          {input.changed && <span className="runs-warning">Input changed during run. SHA-256 reflects the pre-run file.</span>}
        </li>;
      })}
      {added.map((input) => <li key={input.name}><b>{input.name}</b><p>Added since this run{input.path && <> · <code>{input.path}</code></>}</p></li>)}
    </ul>}
    <details className="runs-hashes">
      <summary>Recorded SHA-256 fingerprints</summary>
      <dl className="runs-fields">
        <dt>Workflow YAML</dt><dd><Hash value={manifest.yamlSha256} /></dd>
        <dt>Output</dt><dd><Hash value={manifest.outputSha256} /></dd>
        {before.map((input) => <div className="runs-hash-input" key={input.name}>
          <dt>{input.name}</dt><dd><Hash value={input.sha256} /></dd>
        </div>)}
      </dl>
    </details>
  </>;
}

function CSVPreview({ workspace, path, revision }: { workspace: string; path: string; revision: string }) {
  const [offset, setOffset] = useState(0);
  const scope = JSON.stringify([workspace, path, revision]);
  const first = useCurrentRequest(scope, scope, () => request<Inspection>("inspectWorkbook", { workspace, path, offset: 0, limit: csvPageSize + 1 }));
  const later = useCurrentRequest(offset > 0 ? JSON.stringify([scope, offset]) : null, scope,
    () => request<Inspection>("inspectWorkbook", { workspace, path, offset: offset + 1, limit: csvPageSize }));
  const page = offset === 0 ? first : later;
  const rows = (offset === 0 ? page.data?.rows.slice(1) : page.data?.rows) ?? [];
  const total = Math.max(0, (first.data?.totalRows ?? 0) - 1);
  const error = page.error;
  return <DataGrid
    columns={profileRows(first.data?.rows[0] ?? [], rows)} rows={rows} total={total} rowOffset={offset}
    header={<><b>CSV preview</b><span className="meta mono">{basename(path)}</span></>}
    controls={total > csvPageSize && <div className="runs-csv-controls">
      <button className="btn" disabled={!offset} onClick={() => setOffset(offset - csvPageSize)}>Previous rows</button>
      <button className="btn" disabled={offset + csvPageSize >= total} onClick={() => setOffset(offset + csvPageSize)}>Next rows</button>
    </div>}
    empty={error ? <div className="empty" role="alert">{error.message}<button className="btn" onClick={page.retry}>Retry CSV preview</button></div>
      : page.pending ? <div className="empty" role="status">Reading CSV…</div> : undefined}
  />;
}

function RunDetails({ workspace, run, current, revisionRefresh }: { workspace: string; run: RunHistoryEntry; current?: CurrentWorkflow; revisionRefresh: number }) {
  const [open, setOpen] = useState(false);
  const { outputPath, available } = run;
  const revisions = useFileRevisions(workspace, available && outputPath ? [outputPath] : []);
  const { refresh } = revisions;
  const previousRefresh = useRef(revisionRefresh);
  useEffect(() => {
    if (previousRefresh.current === revisionRefresh) return;
    previousRefresh.current = revisionRefresh;
    refresh();
  }, [revisionRefresh, refresh]);

  const revision = revisions.revisions?.[outputPath];
  const manifest = run.manifest?.version === 1 ? run.manifest : undefined;
  const outputStatus = compareDigest(manifest?.outputSha256, revision);
  const missing = !available || !outputPath || revision === null;
  const workbook = /\.xlsx$/i.test(outputPath);
  const csv = /\.csv$/i.test(outputPath);
  const canPreview = !missing && !!revision && !revisions.error && (workbook || csv);
  const previewVisible = open && canPreview;

  return <section className={`run-details${previewVisible ? " with-preview" : ""}`} aria-label="Run details">
    <div className="run-detail-scroll">
      <header className="run-detail-header">
        <div>
          <h2>{manifest?.workflowName || (outputPath ? basename(outputPath) : "Historical run")}</h2>
          <span className="meta"><RunTime value={run.completedAt} /></span>
        </div>
        {canPreview && <button className="btn" aria-expanded={previewVisible} onClick={() => setOpen(!open)}>{open ? "Close output" : "Open output"}</button>}
      </header>
      {missing ? <p role="status" className="runs-warning">Output file is unavailable.</p>
        : revisions.error ? <p role="alert" className="runs-warning">Couldn’t check the output file. {revisions.error} <button className="btn" onClick={refresh}>Retry file check</button></p>
        : !revision ? <p role="status" className="meta">Checking output file…</p>
        : outputStatus === "Changed" ? <p role="status" className="runs-warning">The output file has changed since this run. The preview shows its current contents.</p>
        : !workbook && !csv ? <p className="meta">Preview unavailable for this file type.</p> : null}
      <dl className="runs-fields">
        <dt>Run ID</dt><dd><code>{run.id}</code></dd>
        <dt>Output path</dt><dd>{outputPath ? <code>{outputPath}</code> : "Unknown"}</dd>
        {outputStatus === "Changed" && <><dt>Current output SHA-256</dt><dd><Hash value={revision} /></dd></>}
      </dl>
      {manifest ? <Provenance manifest={manifest} current={current} /> : <p className="meta">Run metadata is unavailable. This can happen with older runs or an unreadable manifest.</p>}
    </div>
    {previewVisible && revision && (workbook
      ? <WorkbookPreview workspace={workspace} path={outputPath} revision={revision} completedAt={Date.parse(run.completedAt)} stale={false} canRefresh={false} onRefresh={refresh} />
      : <CSVPreview key={JSON.stringify([outputPath, revision])} workspace={workspace} path={outputPath} revision={revision} />)}
  </section>;
}

export function RunsPanel(props: Props) {
  return <WorkspaceRuns key={props.workspace} {...props} />;
}

function WorkspaceRuns({ workspace, current, onBack, onRunCurrent, canRunCurrent, refreshToken, onRefreshCurrent }: Props) {
  const [snapshot, setSnapshot] = useState<RunHistoryPage>();
  const [intent, setIntent] = useState({ offset: 0, generation: 0 });
  const [selectedId, setSelectedId] = useState<string>();
  const heading = useRef<HTMLHeadingElement>(null);
  useLayoutEffect(() => heading.current?.focus(), []);
  const refreshKey = JSON.stringify([refreshToken, intent.generation]);
  const previousRefresh = useRef(refreshKey);
  const refreshStarted = previousRefresh.current !== refreshKey;
  const offset = refreshStarted ? 0 : intent.offset;

  const listing = useCurrentRequest<RunHistoryPage>(
    workspace ? JSON.stringify([workspace, refreshKey, offset]) : null,
    workspace,
    async () => {
      const page = await request<RunHistoryPage>("listRuns", { workspace, offset, limit: pageSize });
      if (offset > 0) return page;
      // Refresh the loaded range so a selection from an older page stays visible.
      const runs = [...page.runs];
      let nextOffset = page.nextOffset;
      while (nextOffset != null && runs.length < (snapshot?.runs.length ?? 0)) {
        const prior = nextOffset;
        const next = await request<RunHistoryPage>("listRuns", { workspace, offset: nextOffset, limit: pageSize });
        if (!next.runs.length || (next.nextOffset != null && next.nextOffset <= prior)) break;
        runs.push(...next.runs);
        nextOffset = next.nextOffset;
      }
      return { runs, nextOffset };
    },
    0,
    (page) => {
      setSnapshot((previous) => {
        if (!offset || !previous) return page;
        const ids = new Set(previous.runs.map(({ id }) => id));
        return { runs: [...previous.runs, ...page.runs.filter(({ id }) => !ids.has(id))], nextOffset: page.nextOffset };
      });
    },
  );

  function refreshList() {
    onRefreshCurrent?.();
    setIntent(({ generation }) => ({ offset: 0, generation: generation + 1 }));
  }

  function loadMore(nextOffset: number) {
    previousRefresh.current = refreshKey;
    setIntent(({ generation }) => ({ offset: nextOffset, generation }));
  }

  const runs = snapshot?.runs ?? [];
  const selected = runs.find(({ id }) => id === selectedId) ?? runs[0];
  return <section className="runs-panel" aria-labelledby="runs-title">
    <header className="runs-toolbar">
      <button className="tb" onClick={onBack}>← {current ? "Back to workflow" : "Back to workspace"}</button>
      <h1 ref={heading} tabIndex={-1} id="runs-title">Runs</h1>
      <div className="runs-actions">
        <button className="btn" onClick={refreshList} disabled={!workspace || listing.pending}>Refresh runs</button>
        {onRunCurrent && <button className="primary" disabled={!canRunCurrent} onClick={onRunCurrent}>Run current workflow</button>}
      </div>
    </header>
    {!workspace ? <p className="runs-status">Open a project folder to see its runs.</p> :
      <div className="runs-body">
        <section className="runs-list" aria-label="Run history">
          {listing.pending && <p className="runs-status" role="status">Loading runs…</p>}
          {listing.error && <div className="runs-status" role="alert"><p>Couldn’t load runs. {listing.error.message}</p><button className="btn" onClick={listing.retry}>Retry</button></div>}
          {snapshot && !runs.length && <p className="runs-status">No runs yet.</p>}
          {runs.length > 0 && <ul aria-label="Recent runs">
            {runs.map((run) => <li key={run.id}>
              <button type="button" aria-current={selected?.id === run.id ? "true" : undefined} onClick={() => setSelectedId(run.id)}>
                <b title={run.manifest?.workflowName || run.id}>{run.manifest?.workflowName || (run.outputPath ? basename(run.outputPath) : "Historical run")}</b>
                {run.manifest?.workflowName && run.outputPath && <span title={run.outputPath}>{basename(run.outputPath)}</span>}
                {run.manifest?.version !== 1 && <span>Earlier run · metadata unavailable</span>}
                <span title={run.id}>{run.id.slice(-8)} · <RunTime value={run.completedAt} />{!run.available && " · Unavailable"}</span>
              </button>
            </li>)}
          </ul>}
          {snapshot?.nextOffset != null && !listing.error && <button className="btn runs-load-more" disabled={listing.pending} onClick={() => loadMore(snapshot.nextOffset!)}>Load more runs</button>}
        </section>
        {selected && <RunDetails key={selected.id} workspace={workspace} run={selected} current={current} revisionRefresh={intent.generation} />}
      </div>}
  </section>;
}
