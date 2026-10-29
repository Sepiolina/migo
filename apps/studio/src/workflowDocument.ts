import { parseDocument } from "yaml";

/** Source bytes are authoritative, including while a YAML draft is invalid. */
export type WorkflowDocument = {
  readonly source: string;
  readonly cst: ReturnType<typeof parseDocument>;
  readonly revision: number;
};

export type WorkflowDocumentState = {
  readonly past: readonly WorkflowDocument[];
  readonly present: WorkflowDocument;
  readonly future: readonly WorkflowDocument[];
  readonly savedSource: string;
};

export type SourceEditCommand = {
  type: "edit-source";
  source: string;
  label?: string;
};

export type WorkflowDocumentAction =
  | SourceEditCommand
  | { type: "undo" | "redo" }
  | { type: "load"; source: string }
  | { type: "mark-saved"; source: string | ((previous: string) => string) };

export type SourceTransform = (source: string) => string;

/** Reuse existing authoring transforms; a transaction yields one undo entry.
 * Compute before dispatch so a failed edit can be reported by its caller.
 */
export function sourceTransaction(
  source: string,
  label: string,
  ...edits: SourceTransform[]
): SourceEditCommand {
  return {
    type: "edit-source",
    label,
    source: edits.reduce((draft, edit) => edit(draft), source),
  };
}

function snapshot(source: string, revision: number): WorkflowDocument {
  return {
    source,
    cst: parseDocument(source, { keepSourceTokens: true }),
    revision,
  };
}

export function createWorkflowDocument(source: string): WorkflowDocumentState {
  return {
    past: [],
    present: snapshot(source, 0),
    future: [],
    savedSource: source,
  };
}

/** The only history transition boundary. Never mutate a previous CST snapshot. */
export function workflowDocumentTransition(
  state: WorkflowDocumentState,
  action: WorkflowDocumentAction,
): WorkflowDocumentState {
  if (action.type === "mark-saved") {
    const savedSource =
      typeof action.source === "function"
        ? action.source(state.savedSource)
        : action.source;
    return savedSource === state.savedSource
      ? state
      : { ...state, savedSource };
  }
  if (action.type === "load") {
    return {
      past: [],
      present: snapshot(action.source, state.present.revision + 1),
      future: [],
      savedSource: action.source,
    };
  }
  if (action.type === "edit-source") {
    if (action.source === state.present.source) return state;
    return {
      ...state,
      past: [...state.past, state.present].slice(-200),
      present: snapshot(action.source, state.present.revision + 1),
      future: [],
    };
  }
  if (action.type === "undo") {
    const previous = state.past.at(-1);
    if (!previous) return state;
    return {
      ...state,
      past: state.past.slice(0, -1),
      present: { ...previous, revision: state.present.revision + 1 },
      future: [state.present, ...state.future],
    };
  }
  const next = state.future[0];
  if (!next) return state;
  return {
    ...state,
    past: [...state.past, state.present].slice(-200),
    present: { ...next, revision: state.present.revision + 1 },
    future: state.future.slice(1),
  };
}
