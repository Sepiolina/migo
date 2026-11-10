import { workflowTemplates, type Template } from "./workflowTemplates";

export type WorkflowEntry = { path: string; name: string; route: string };

/** The start screen: the workspace, its workflows and templates. */
export function Home({
  workspace,
  desktop,
  workflows,
  onOpenWorkspace,
  onOpen,
  onTemplate,
  onBack,
}: {
  workspace: string;
  desktop: boolean;
  workflows: WorkflowEntry[];
  onOpenWorkspace: () => void;
  onOpen: (path: string) => void;
  onTemplate: (t: Template) => void;
  onBack?: () => void;
}) {
  return (
    <main className="home" aria-label="Workflows">
      <div className="wrap">
        <div>
          <h1>Workflows</h1>
          <p className="lead">Each workflow reads Excel or CSV files, transforms them step by step, and is served by MaiService as an API endpoint.</p>
        </div>
        {!desktop ? (
          <div className="notice">This is the browser preview. Open the Mai Studio desktop app to open folders, preview data and run workflows. Templates open here so you can try the editor.</div>
        ) : (
          <div className="hero">
            <div>
              <b>{workspace ? workspace : "Open a project folder"}</b>
              <p>{workspace ? "Workflows live in its workflows/ folder; sample files can be anywhere inside it." : "Choose the folder that holds your workflows/ directory and sample files, for example a clone of the migo repository."}</p>
            </div>
            <button className={workspace ? "btn" : "primary"} onClick={onOpenWorkspace}>{workspace ? "Change folder…" : "Open folder…"}</button>
          </div>
        )}
        {workspace && (
          <section>
            <h2>In this project</h2>
            <div className="list">
              {workflows.length ? (
                workflows.map((w) => (
                  <button key={w.path} className="wf" onClick={() => onOpen(w.path)}>
                    <span>
                      <div className="n">{w.name || w.path}</div>
                      <div className="p">{w.path}</div>
                    </span>
                    <span className="route">/api/{w.route}</span>
                  </button>
                ))
              ) : (
                <div className="wf" style={{ cursor: "default" }}>
                  <span className="hint">No workflows in workflows/ yet. Start from a template below.</span>
                </div>
              )}
            </div>
          </section>
        )}
        <section>
          <h2>Start from a template</h2>
          <div className="tpls">
            {workflowTemplates.map((t) => (
              <button key={t.id} className="tpl" onClick={() => onTemplate(t)}>
                <b>{t.title}</b>
                <span>{t.description}</span>
                <span className="fmts">
                  {t.inputs.map((f, i) => <span key={i} className="fmt">{f}</span>)} → <span className="fmt">{t.output}</span>
                </span>
              </button>
            ))}
          </div>
        </section>
        {onBack && (
          <div>
            <button className="btn" onClick={onBack}>Back to the open workflow</button>
          </div>
        )}
      </div>
    </main>
  );
}
