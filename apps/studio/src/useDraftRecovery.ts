import { useEffect, useLayoutEffect, useRef, useState } from "react";
import {
  createDraftId,
  draftDebounceMs,
  draftKey,
  readDrafts,
  removeDraft,
  writeDraft,
  type DraftLocation,
  type RecoveryDraft,
} from "./draftRecovery";

type DraftState = Omit<DraftLocation, "instanceId"> &
  Pick<RecoveryDraft, "baseRevision" | "source" | "savedSource"> & {
    dirty: boolean;
    enabled: boolean;
  };

type Pending = {
  key: string;
  dirty: boolean;
  baseRevision: string | null;
  draft: RecoveryDraft | null;
  timer?: ReturnType<typeof setTimeout>;
};

const localChange = "mai:workflow-draft-change";

export function useDraftRecovery({
  workspace, workflowPath, draftId, baseRevision, source, savedSource, dirty, enabled,
}: DraftState) {
  const [instanceId] = useState(createDraftId);
  const [listed, setListed] = useState<{ workspace: string; drafts: RecoveryDraft[] }>();
  const [readFailed, setReadFailed] = useState(false);
  const [writeFailed, setWriteFailed] = useState(false);
  const pending = useRef<Pending | null>(null);
  const location = { workspace, workflowPath, draftId, instanceId };
  const key = enabled && workspace && (workflowPath || draftId) ? draftKey(location) : "";

  function refresh() {
    try {
      const drafts = readDrafts(window.localStorage, workspace);
      setListed({ workspace, drafts });
      setReadFailed(false);
      return drafts;
    } catch {
      setReadFailed(true);
      return [];
    }
  }

  function mutate(action: (storage: Storage) => boolean) {
    try {
      const changed = action(window.localStorage);
      setWriteFailed(false);
      if (changed) window.dispatchEvent(new Event(localChange));
      return true;
    } catch {
      setWriteFailed(true);
      return false;
    }
  }

  function writeCurrent(
    draft: Omit<RecoveryDraft, "version" | "instanceId" | "workspace" | "timestamp">,
    destinationWorkspace = workspace,
  ) {
    if (!enabled || !destinationWorkspace || !(draft.workflowPath || draft.draftId)) return true;
    const record: RecoveryDraft = { ...draft, workspace: destinationWorkspace, instanceId, version: 1, timestamp: Date.now() };
    const written = mutate(storage => {
      writeDraft(storage, record);
      return true;
    });
    const current = pending.current;
    if (written && current?.key === draftKey(record)) {
      clearTimeout(current.timer);
      current.draft = null;
    }
    return written;
  }

  function flush() {
    const current = pending.current;
    if (!current?.draft || current.key !== key) return true;
    const draft = current.draft;
    clearTimeout(current.timer);
    return writeCurrent(draft);
  }

  function clearCurrent(savedSource?: string) {
    if (!key) return true;
    const current = pending.current;
    if (current?.key === key && savedSource !== undefined && current.draft &&
        (current.draft.source !== savedSource || current.draft.baseRevision !== baseRevision)) return true;
    const removed = mutate(storage => removeDraft(storage, location, draft =>
      savedSource === undefined || (draft.source === savedSource && draft.baseRevision === baseRevision)));
    if (removed && current?.key === key) {
      clearTimeout(current.timer);
      current.draft = null;
    }
    return removed;
  }

  function discard(draft: RecoveryDraft) {
    if (!enabled || draft.workspace !== workspace) return true;
    return mutate(storage => removeDraft(storage, draft, existing =>
      existing.timestamp === draft.timestamp && existing.source === draft.source &&
      existing.baseRevision === draft.baseRevision && existing.savedSource === draft.savedSource));
  }

  useEffect(() => {
    setReadFailed(false);
    if (!enabled || !workspace) return;
    refresh();
    window.addEventListener("storage", refresh);
    window.addEventListener(localChange, refresh);
    return () => {
      window.removeEventListener("storage", refresh);
      window.removeEventListener(localChange, refresh);
    };
  }, [enabled, workspace]);

  useLayoutEffect(() => {
    const previous = pending.current;
    if (!key) {
      pending.current = null;
      return;
    }
    const current: Pending = {
      key, dirty, baseRevision,
      draft: dirty ? { ...location, version: 1, baseRevision, source, savedSource, timestamp: 0 } : null,
    };
    pending.current = current;
    if (dirty) {
      if (previous?.key === key && previous.baseRevision !== baseRevision) flush();
      else current.timer = setTimeout(flush, draftDebounceMs);
    } else if (previous?.key === key && previous.dirty) {
      clearCurrent();
    }
    window.addEventListener("pagehide", flush);
    return () => {
      clearTimeout(current.timer);
      window.removeEventListener("pagehide", flush);
    };
  }, [key, baseRevision, dirty, source, savedSource]);

  return {
    drafts: enabled && listed?.workspace === workspace ? listed.drafts : [],
    storageUnavailable: enabled && !!workspace && (readFailed || writeFailed),
    discard,
    clearCurrent,
    flush,
    writeCurrent,
    refresh,
  };
}
