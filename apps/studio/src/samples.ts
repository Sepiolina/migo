import type { InputItem } from "./pipeline";

/** A file imported by the native host: picked or dropped. */
export type ImportedFile = { name: string; path?: string; copied?: boolean; error?: string };

/**
 * The input's file-name pattern as a JavaScript RegExp. Workflows use Go
 * syntax, where a leading (?i) means "ignore case".
 */
export function matcher(pattern: string): RegExp | null {
  if (!pattern.trim()) return null;
  const ci = /^\(\?i\)/.test(pattern);
  try {
    return new RegExp(ci ? pattern.slice(4) : pattern, ci ? "i" : "");
  } catch {
    return null;
  }
}

/**
 * Decides which source each imported file becomes the sample of.
 *
 * Files whose name matches a source's pattern go to that source (and never
 * to another one). Every other file goes to `preferred` (the source whose import button was used),
 * else to a source that has no sample yet, else to a source without a
 * pattern. Each source takes at most one file per import; files that find no
 * source are returned as `unassigned`.
 */
export function assignSamples(
  inputs: InputItem[],
  bindings: Record<string, string>,
  files: ImportedFile[],
  preferred?: string,
): { bind: Record<string, string>; unassigned: string[] } {
  const bind: Record<string, string> = {};
  const unassigned: string[] = [];
  const taken = (id: string) => id in bind;
  const usable = files.filter((f): f is ImportedFile & { path: string } => !!f.path);
  const matches = (f: ImportedFile) => inputs.filter((i) => matcher(i.match)?.test(f.name));
  const ordered = [...usable.filter((f) => matches(f).length), ...usable.filter((f) => !matches(f).length)];
  for (const f of ordered) {
    // A file named for a source never lands on another one.
    const pick = matches(f).length
      ? matches(f).find((i) => !taken(i.id))
      : [
        inputs.find((i) => i.id === preferred && !taken(i.id)),
        inputs.find((i) => !bindings[i.id] && !taken(i.id) && !matcher(i.match)),
        inputs.find((i) => !bindings[i.id] && !taken(i.id)),
        inputs.find((i) => !matcher(i.match) && !taken(i.id)),
      ].find(Boolean);
    if (pick) bind[pick.id] = f.path;
    else unassigned.push(f.name);
  }
  return { bind, unassigned };
}
