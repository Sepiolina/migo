import { isSeq, parseDocument } from "yaml";
// Append the chosen catalog operation without rebuilding the existing document.
export function addOperation(
  source: string,
  operation: string,
  argument: string,
): string {
  const doc = parseDocument(source, { keepSourceTokens: true });
  if (doc.errors.length) throw new Error(doc.errors[0].message);
  doc.toJS({ maxAliasCount: 0 });
  const steps = doc.get("steps", true);
  if (!isSeq(steps)) throw new Error("The workflow needs a steps list.");
  const arg = parseDocument(argument);
  if (arg.errors.length) throw new Error(arg.errors[0].message);
  steps.add(doc.createNode({ [operation]: arg.toJS({ maxAliasCount: 0 }) }));
  return doc.toString();
}
