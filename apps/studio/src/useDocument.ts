import { useRef, useState } from "react";
import {
  createWorkflowDocument,
  sourceTransaction,
  workflowDocumentTransition,
  type SourceEditCommand,
  type SourceTransform,
  type WorkflowDocumentAction,
} from "./workflowDocument";
export function useDocument(initial: string) {
  const [history, setHistory] = useState(() => createWorkflowDocument(initial));
  // Commands may run several times before React renders again. Keep their
  // latest result available synchronously, including for transaction transforms
  // whose errors must reach the caller before any changes are committed.
  const current = useRef(history);
  const dispatch = (action: WorkflowDocumentAction) => {
    const next = workflowDocumentTransition(current.current, action);
    if (next === current.current) return;
    current.current = next;
    setHistory(next);
  };
  return {
    source: history.present.source,
    savedSource: history.savedSource,
    getCurrent: () => current.current,
    snapshot: history.present,
    dirty: history.present.source !== history.savedSource,
    canUndo: !!history.past.length,
    canRedo: !!history.future.length,
    setSource: (source: string) => dispatch({ type: "edit-source", source }),
    execute: (command: SourceEditCommand) => dispatch(command),
    transact: (label: string, ...edits: SourceTransform[]) =>
      dispatch(
        sourceTransaction(current.current.present.source, label, ...edits),
      ),
    undo: () => dispatch({ type: "undo" }),
    redo: () => dispatch({ type: "redo" }),
    markSaved: (source: string | ((previous: string) => string)) =>
      dispatch({ type: "mark-saved", source }),
    load: (source: string) => dispatch({ type: "load", source }),
  };
}
