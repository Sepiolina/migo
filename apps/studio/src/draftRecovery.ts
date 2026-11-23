const prefix = "mai.workflow-draft:v1:";
export const draftRetentionMs = 7 * 24 * 60 * 60 * 1000;
export const draftDebounceMs = 750;

export type DraftLocation = {
  workspace: string;
  workflowPath: string;
  draftId: string;
  instanceId: string;
};

export type RecoveryDraft = DraftLocation & {
  version: 1;
  baseRevision: string | null;
  source: string;
  savedSource: string;
  timestamp: number;
};

export function createDraftId() {
  return globalThis.crypto.randomUUID?.() ??
    Array.from(globalThis.crypto.getRandomValues(new Uint8Array(16)), byte => byte.toString(16).padStart(2, "0")).join("");
}

export function draftKey(location: DraftLocation) {
  return prefix + JSON.stringify([
    location.workspace, location.workflowPath, location.workflowPath ? "" : location.draftId, location.instanceId,
  ]);
}

function parseDraft(raw: string | null, key: string): RecoveryDraft | null {
  if (!raw) return null;
  try {
    const value = JSON.parse(raw);
    if (!value || value.version !== 1 || typeof value.workspace !== "string" || !value.workspace ||
        typeof value.workflowPath !== "string" || typeof value.draftId !== "string" ||
        !(value.workflowPath || value.draftId) || typeof value.instanceId !== "string" || !value.instanceId ||
        (value.baseRevision !== null && typeof value.baseRevision !== "string") ||
        typeof value.source !== "string" || typeof value.savedSource !== "string" ||
        !Number.isFinite(value.timestamp) || draftKey(value) !== key) return null;
    return value;
  } catch {
    return null;
  }
}

export function readDrafts(storage: Storage, workspace: string, now = Date.now()): RecoveryDraft[] {
  const drafts: RecoveryDraft[] = [];
  const keys = Array.from({ length: storage.length }, (_, index) => storage.key(index));
  for (const key of keys) {
    if (!key?.startsWith(prefix)) continue;
    const raw = storage.getItem(key);
    const draft = parseDraft(raw, key);
    if (!draft) continue;
    if (now - draft.timestamp >= draftRetentionMs) {
      if (storage.getItem(key) === raw) storage.removeItem(key);
      continue;
    }
    if (draft.workspace === workspace) drafts.push(draft);
  }
  return drafts.sort((left, right) => right.timestamp - left.timestamp);
}

export function writeDraft(storage: Storage, draft: RecoveryDraft) {
  storage.setItem(draftKey(draft), JSON.stringify(draft));
}

export function removeDraft(storage: Storage, location: DraftLocation, matches: (draft: RecoveryDraft) => boolean) {
  const key = draftKey(location);
  const raw = storage.getItem(key);
  const existing = parseDraft(raw, key);
  if (!existing || !matches(existing) || storage.getItem(key) !== raw) return false;
  storage.removeItem(key);
  return true;
}
