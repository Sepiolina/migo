import { Document, isMap, isScalar, isSeq, parseDocument, type Node } from "yaml";

/**
 * Re-use the original text of every collection that did not change.
 *
 * `yaml` re-prints a whole document from its tree, which reflows folded
 * scalars, flow spacing and trailing comments. After an edit, this keeps the
 * hand-written text of unchanged steps, sheets and other mappings, so a
 * one-field change produces a one-line diff. The result is checked: if it
 * does not parse to the same data, the freshly printed text is used.
 */
export function preserveText(original: string, printed: string, commentsChanged = false): string {
  const before = parseDocument(original, { keepSourceTokens: true });
  const after = parseDocument(printed, { keepSourceTokens: true });
  if (before.errors.length || after.errors.length) return printed;
  const data = (n: unknown) =>
    JSON.stringify((n as { toJSON?: () => unknown })?.toJSON?.() ?? n);
  // When comments changed, nodes are equal only if their comments are too.
  const json = (n: unknown) =>
    commentsChanged && n && typeof n === "object" ? new Document(n as Node).toString() : data(n);
  if (!commentsChanged && data(before.contents) === data(after.contents)) return original;

  const edits: [start: number, end: number, text: string][] = [];
  const column = (text: string, at: number) => at - text.lastIndexOf("\n", at - 1) - 1;
  const trimEnd = (text: string, start: number, end: number) => {
    while (end > start && /\s/.test(text[end - 1])) end--;
    return end;
  };
  const replace = (next: Node, old: Node): boolean => {
    if (!next.range || !old.range) return false;
    if (column(printed, next.range[0]) !== column(original, old.range[0])) return false;
    // A mapping written as a list item keeps its comments inside its range.
    const end = trimEnd(printed, next.range[0], next.range[1]);
    const oldEnd = trimEnd(original, old.range[0], old.range[1]);
    edits.push([next.range[0], end, original.slice(old.range[0], oldEnd)]);
    return true;
  };
  const firstKey = (n: unknown) =>
    isMap(n) && isScalar(n.items[0]?.key) ? String(n.items[0].key.value) : undefined;
  const visit = (next: unknown, old: unknown): void => {
    if (isMap(next) && isMap(old)) {
      for (const pair of next.items) {
        if (!isScalar(pair.key)) continue;
        const match = old.items.find(
          (p) => isScalar(p.key) && p.key.value === (pair.key as { value: unknown }).value,
        );
        if (!match) continue;
        const value = pair.value as Node | null;
        const previous = match.value as Node | null;
        if (!value || !previous) continue;
        if (json(value) === json(previous) && replace(value, previous)) continue;
        if (!(isMap(value) || isSeq(value))) continue;
        visit(value, previous);
      }
    } else if (isSeq(next) && isSeq(old)) {
      let j = 0;
      for (const item of next.items) {
        if (!(isMap(item) || isSeq(item))) continue;
        let k = j;
        while (k < old.items.length && json(old.items[k]) !== json(item)) k++;
        if (k < old.items.length) {
          if (replace(item as Node, old.items[k] as Node)) {
            j = k + 1;
            continue;
          }
        }
        const candidate = old.items[j];
        if (firstKey(candidate) !== undefined && firstKey(candidate) === firstKey(item)) {
          visit(item, candidate);
          j++;
        }
      }
    }
  };
  visit(after.contents, before.contents);
  if (!edits.length) return printed;
  let text = printed;
  for (const [start, end, value] of edits.sort((a, b) => b[0] - a[0]))
    text = text.slice(0, start) + value + text.slice(end);
  const check = parseDocument(text);
  return !check.errors.length && data(check.contents) === data(after.contents) ? text : printed;
}
