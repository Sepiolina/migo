import { useEffect, useReducer, useState } from "react";
import { historyTransition } from "./authoring";
export function useDocument(initial: string) {
  const [history, dispatch] = useReducer(historyTransition, {
    past: [],
    present: initial,
    future: [],
  });
  const [savedSource, setSavedSource] = useState(initial);
  const dirty = history.present !== savedSource;
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
    source: history.present,
    dirty,
    canUndo: !!history.past.length,
    canRedo: !!history.future.length,
    setSource: (source: string) => dispatch({ type: "edit", source }),
    undo: () => dispatch({ type: "undo" }),
    redo: () => dispatch({ type: "redo" }),
    markSaved: setSavedSource,
    load: (source: string) => {
      dispatch({ type: "reset", source });
      setSavedSource(source);
    },
  };
}
