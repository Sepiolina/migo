import { useLayoutEffect, useRef, useState } from "react";
import { request } from "./api";
import { asRunnerError, useCurrentRequest } from "./useCurrentRequest";
import "./workflow-tests.css";

type Mismatch = {
  kind: string;
  sheet?: string;
  cell?: string;
  line?: number;
  expected?: string;
  actual?: string;
  expectedSheets?: string[];
  actualSheets?: string[];
};

type TestCase = {
  name: string;
  expectedError?: string;
  outcome?: "passed" | "failed";
  durationMs?: number;
  error?: string;
  actualError?: string;
  mismatch?: Mismatch;
};

type TestSuite = { path: string; name: string; cases: TestCase[] };
type TestReport = {
  workflowPath: string;
  revision: string;
  suites: TestSuite[];
  diagnostics?: { path: string; message: string }[];
};
type Target = { path?: string };
type Props = {
  workspace: string;
  workflowPath?: string;
  dirty: boolean;
  onSave: () => Promise<string | null>;
  onBack: () => void;
  busy?: boolean;
};

function Comparison({ mismatch }: { mismatch: Mismatch }) {
  return <dl className="workflow-tests-fields">
    {mismatch.sheet !== undefined && <><dt>Sheet</dt><dd><code>{mismatch.sheet}</code></dd></>}
    {mismatch.cell !== undefined && <><dt>Cell</dt><dd><code>{mismatch.cell}</code></dd></>}
    {mismatch.line !== undefined && <><dt>First differing line</dt><dd>{mismatch.line}</dd></>}
    {mismatch.kind !== "sheets" && mismatch.expected !== undefined && <><dt>{mismatch.line === undefined ? "Expected value" : "Expected line"}</dt><dd><code>{JSON.stringify(mismatch.expected)}</code></dd></>}
    {mismatch.kind !== "sheets" && mismatch.actual !== undefined && <><dt>{mismatch.line === undefined ? "Actual value" : "Actual line"}</dt><dd><code>{JSON.stringify(mismatch.actual)}</code></dd></>}
    {mismatch.expectedSheets && <><dt>Expected sheets</dt><dd><code>{mismatch.expectedSheets.join(", ")}</code></dd></>}
    {mismatch.actualSheets && <><dt>Actual sheets</dt><dd><code>{mismatch.actualSheets.join(", ")}</code></dd></>}
  </dl>;
}

function CaseResult({ result }: { result: TestCase }) {
  const { name, outcome, durationMs, error, actualError, expectedError, mismatch } = result;
  return <li className="workflow-tests-case">
    <div className="workflow-tests-case-title">
      <b>{name}</b>
      {outcome && <span className={`workflow-tests-outcome ${outcome}`}>{outcome === "passed" ? "Passed" : "Failed"}</span>}
      {durationMs !== undefined && <span className="meta">{durationMs.toLocaleString()} ms</span>}
    </div>
    {expectedError && <dl className="workflow-tests-fields">
      <dt>Expected error fragment</dt><dd><code>{expectedError}</code></dd>
      {outcome && <><dt>Actual error</dt><dd>{actualError ? <code>{actualError}</code>
        : outcome === "passed" ? "Matched expected error" : "No workflow error was reported"}</dd></>}
    </dl>}
    {mismatch && <Comparison mismatch={mismatch} />}
    {error && <p className="workflow-tests-error">{error}</p>}
  </li>;
}

function SuiteResult({ suite, disabled, onRun }: { suite: TestSuite; disabled: boolean; onRun: () => void }) {
  const passed = suite.cases.filter(({ outcome }) => outcome === "passed").length;
  const failed = suite.cases.filter(({ outcome }) => outcome === "failed").length;
  const duration = suite.cases.reduce((total, { durationMs }) => total + (durationMs ?? 0), 0);

  return <section className="workflow-tests-suite" aria-label={suite.name}>
    <header>
      <div>
        <h2>{suite.name}</h2>
        <code className="meta">{suite.path}</code>
        <div className="meta" aria-live="polite">
          {passed + failed > 0 && <>{passed} passed · {failed} failed · {duration.toLocaleString()} ms total</>}
        </div>
      </div>
      <button type="button" className="btn" aria-label={`Run suite ${suite.name}`} disabled={disabled} onClick={onRun}>Run suite</button>
    </header>
    {!suite.cases.length && <p className="meta">No cases in this suite.</p>}
    {suite.cases.length > 0 && <ul>{suite.cases.map((result, index) =>
      <CaseResult key={JSON.stringify([result.name, index])} result={result} />,
    )}</ul>}
  </section>;
}

export function WorkflowTests(props: Props) {
  return <SavedWorkflowTests key={JSON.stringify([props.workspace, props.workflowPath])} {...props} />;
}

function SavedWorkflowTests({ workspace, workflowPath, dirty, onSave, onBack, busy }: Props) {
  const heading = useRef<HTMLHeadingElement>(null);
  const choices = useRef<HTMLButtonElement>(null);
  const active = useRef<AbortController | null>(null);
  const [target, setTarget] = useState<Target>();
  const [phase, setPhase] = useState<"saving" | "testing" | "cancelled">();
  const [report, setReport] = useState<TestReport>();
  const [error, setError] = useState("");
  useLayoutEffect(() => {
    heading.current?.focus();
    return () => active.current?.abort();
  }, []);
  useLayoutEffect(() => { if (target) choices.current?.focus(); }, [target]);

  const scope = JSON.stringify([workspace, workflowPath]);
  const params = { workspace, workflowPath };
  const discovery = useCurrentRequest<TestReport>(
    workspace && workflowPath ? scope : null,
    scope,
    () => request("discoverTests", params),
  );
  const catalog = discovery.data ?? discovery.previous;
  const discovered = new Map(catalog?.suites.map((suite) => [suite.path, suite]));
  const suites = new Map(discovered);
  for (const suite of report?.suites ?? []) suites.set(suite.path, suite);
  const diagnostics = new Map(catalog?.diagnostics?.map((entry) => [entry.path, entry]));
  for (const entry of report?.diagnostics ?? []) diagnostics.set(entry.path, entry);
  const locked = !!busy || phase === "saving" || phase === "testing";

  function chooseRun(next: Target) {
    if (locked || active.current || target) return;
    setError("");
    if (dirty) setTarget(next);
    else void run(next, false);
  }

  async function run(next: Target, save: boolean) {
    if (active.current || locked || !workflowPath) return;
    const controller = new AbortController();
    active.current = controller;
    setTarget(undefined);
    setPhase(save ? "saving" : "testing");
    try {
      const revision = save ? await onSave() : undefined;
      if (controller.signal.aborted) return;
      if (revision === null) {
        setError("The workflow could not be saved. Tests did not run.");
        return;
      }
      setPhase("testing");
      const result = await request<TestReport>("runTests", {
        ...params,
        ...(next.path ? { path: next.path } : {}),
        ...(revision ? { revision } : {}),
      }, controller.signal);
      if (controller.signal.aborted) return;
      setReport((previous) => {
        if (!next.path || previous?.revision !== result.revision) return result;
        const untouched = previous.suites.filter(({ path }) => !result.suites.some((suite) => suite.path === path));
        return { ...result, suites: [...untouched, ...result.suites] };
      });
    } catch (failure) {
      if (!controller.signal.aborted) setError(asRunnerError(failure).message);
    } finally {
      active.current = null;
      setPhase(controller.signal.aborted ? "cancelled" : undefined);
    }
  }

  return <section className="workflow-tests" aria-labelledby="workflow-tests-title">
    <header className="workflow-tests-toolbar">
      <button className="tb" type="button" onClick={onBack} disabled={phase === "saving"}>← Back to workflow</button>
      <h1 id="workflow-tests-title" ref={heading} tabIndex={-1}>Workflow tests</h1>
      <div className="workflow-tests-actions">
        <button type="button" className="btn" disabled={!workflowPath || locked || discovery.pending || !!target}
          onClick={discovery.retry}>Refresh suites</button>
        <button type="button" className="primary" disabled={!catalog?.suites.length || locked || discovery.pending || !!target}
          onClick={() => chooseRun({})}>Run all tests</button>
      </div>
    </header>
    <div className="workflow-tests-body">
      {!workflowPath && <p>Save this workflow to find and run its test suites.</p>}
      {discovery.pending && <p role="status">Finding test suites…</p>}
      {discovery.error && <p className="workflow-tests-error" role="alert">Couldn’t find test suites. {discovery.error.message}</p>}
      {catalog && !catalog.suites.length && <p>No test suites found for this workflow.</p>}
      {target && <section className="workflow-tests-choice" aria-label="Choose workflow revision">
        <p>This workflow has unsaved changes. Which version should the tests use?</p>
        <div className="workflow-tests-actions">
          <button className="primary" type="button" ref={choices} disabled={locked} onClick={() => void run(target, true)}>Save and run tests</button>
          <button className="btn" type="button" disabled={locked} onClick={() => void run(target, false)}>Test saved version</button>
          <button className="btn" type="button" onClick={() => setTarget(undefined)}>Cancel</button>
        </div>
      </section>}
      {phase === "saving" && <p role="status">Saving workflow…</p>}
      {phase === "testing" && <div className="workflow-tests-actions">
        <p role="status">Running workflow tests…</p>
        <button type="button" className="btn" onClick={() => active.current?.abort()}>Cancel tests</button>
      </div>}
      {phase === "cancelled" && <p role="status">Workflow tests cancelled.</p>}
      {error && <p className="workflow-tests-error" role="alert">{error}</p>}
      {report && <dl className="workflow-tests-fields" aria-live="polite">
        <dt>Tested workflow</dt><dd><code>{report.workflowPath}</code></dd>
        <dt>Tested revision</dt><dd><code>{report.revision}</code></dd>
      </dl>}
      {diagnostics.size > 0 && <section className="workflow-tests-diagnostics" aria-label="Suite discovery issues">
        <h2>Unreadable test suites</h2>
        <ul>{Array.from(diagnostics.values(), ({ path, message }) =>
          <li key={path}><code>{path}</code><p className="workflow-tests-error">{message}</p></li>,
        )}</ul>
      </section>}
      {Array.from(suites.values(), (suite) => <SuiteResult key={suite.path} suite={suite}
        disabled={!discovered.has(suite.path) || locked || discovery.pending || !!target}
        onRun={() => chooseRun({ path: suite.path })} />)}
    </div>
  </section>;
}
