import { useEffect, useReducer } from "react";
import {
  createWorkflowDocument,
  sourceTransaction,
  workflowDocumentTransition,
  type SourceEditCommand,
  type SourceTransform,
} from "./workflowDocument";
export function useDocument(initial: string) {
  const [history, dispatch] = useReducer(
    workflowDocumentTransition,
    initial,
    createWorkflowDocument,
  );
  const dirty = history.present.source !== history.savedSource;
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (dirty) {
        event.preventDefault();
        event.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", beforeUnload);
    return () => window.removeEventListener("beforeunload", beforeUnload);
  }, [dirty]);
  return {
    source: history.present.source,
    snapshot: history.present,
    dirty,
    canUndo: !!history.past.length,
    canRedo: !!history.future.length,
    setSource: (source: string) => dispatch({ type: "edit-source", source }),
    execute: (command: SourceEditCommand) => dispatch(command),
    transact: (label: string, ...edits: SourceTransform[]) =>
      dispatch(sourceTransaction(history.present.source, label, ...edits)),
    undo: () => dispatch({ type: "undo" }),
    redo: () => dispatch({ type: "redo" }),
    markSaved: (source: string | ((previous: string) => string)) =>
      dispatch({ type: "mark-saved", source }),
    load: (source: string) => dispatch({ type: "load", source }),
  };
}
