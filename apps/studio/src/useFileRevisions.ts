import { useCallback, useEffect, useState } from "react";
import { native } from "./api";

type Revisions = Record<string, string | null>;
type Snapshot = { workspace: string; files: Revisions; error?: string };

/** Recheck bound files on activation or explicit refresh without hashing on each YAML edit. */
export function useFileRevisions(workspace: string, paths: string[]) {
  const pathKey = JSON.stringify([...new Set(paths.filter(Boolean))].sort());
  const [snapshot, setSnapshot] = useState<Snapshot>();
  const [generation, setGeneration] = useState(0);
  const refresh = useCallback(() => setGeneration((n) => n + 1), []);
  useEffect(() => {
    if (!workspace || pathKey === "[]") return;
    window.addEventListener("focus", refresh);
    return () => window.removeEventListener("focus", refresh);
  }, [workspace, pathKey, refresh]);
  useEffect(() => {
    let current = true;
    const requested: string[] = JSON.parse(pathKey);
    if (!workspace || !requested.length) {
      setSnapshot(undefined);
      return;
    }
    native<Revisions>("file_revisions", { paths: requested }).then(
      (files) => { if (current) setSnapshot({ workspace, files }); },
      (cause) => {
        if (current) setSnapshot({ workspace, files: {}, error: String(cause instanceof Error ? cause.message : cause) });
      },
    );
    return () => { current = false; };
  }, [workspace, pathKey, generation]);
  const current = snapshot?.workspace === workspace ? snapshot : undefined;
  return { revisions: current?.files, error: current?.error, refresh };
}
