/**
 * Formula completion: the expression helpers (from the Go catalog) and the
 * columns in scope, offered for the word being typed. Studio never evaluates
 * expressions; this only saves typing and shows each helper's signature.
 */
import { functions } from "./stepRegistry";

export type Suggestion = {
  /** What the list shows: a helper's signature or a column title. */
  label: string;
  /** The text that replaces the typed word. */
  insert: string;
  /** A helper's one-line doc, or "column". */
  detail: string;
};

const identifier = /^[A-Za-z_][A-Za-z0-9_]*$/;
/** How a formula refers to a column: its name, or col("...") when it is not an identifier. */
export const columnReference = (name: string) =>
  identifier.test(name) ? name : `col(${JSON.stringify(name)})`;

/**
 * Suggestions for the word that ends at the caret, and where that word
 * starts. Nothing is offered inside a quoted string, after a dot, or once
 * the word is already complete.
 */
export function suggest(
  text: string,
  caret: number,
  columns: string[],
  limit = 8,
): { from: number; list: Suggestion[] } | null {
  const word = /[A-Za-z_][A-Za-z0-9_]*$/.exec(text.slice(0, caret))?.[0];
  if (!word) return null;
  const from = caret - word.length;
  const before = text.slice(0, from);
  const quotes = before.replace(/\\./g, "").match(/"/g)?.length ?? 0;
  if (quotes % 2 === 1 || /[.\w]$/.test(before)) return null;
  const prefix = word.toLowerCase();
  const list: Suggestion[] = [
    ...columns
      .filter((c) => c.toLowerCase().startsWith(prefix))
      .map((c) => ({ label: c, insert: columnReference(c), detail: "column" })),
    ...functions
      .filter((f) => f.name.toLowerCase().startsWith(prefix))
      .map((f) => ({ label: f.signature, insert: `${f.name}(`, detail: f.doc })),
  ].filter((s) => s.insert !== word);
  return list.length ? { from, list: list.slice(0, limit) } : null;
}
