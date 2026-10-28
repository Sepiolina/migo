import {
  isAlias,
  isMap,
  isNode,
  isScalar,
  isSeq,
  parseAllDocuments,
} from "yaml";

export type SetupInput = { key: string; id: string; label: string };
export type SetupOutput = { key: string; name: string; table: string };
export type WorkbookSettings = {
  name: string;
  inputs: SetupInput[];
  outputs: SetupOutput[];
};

const str = (node: unknown): string | null =>
  isScalar(node) && typeof node.value === "string" ? node.value : null;
const get = (map: any, key: string) =>
  isMap(map) ? map.get(key, true) : undefined;

function documentFor(source: string) {
  const docs = parseAllDocuments(source, {
    keepSourceTokens: true,
    uniqueKeys: true,
  });
  if (
    docs.length !== 1 ||
    docs[0].errors.length ||
    docs[0].warnings.length ||
    !isMap(docs[0].contents)
  )
    throw new Error("Unsupported document");
  const visit = (node: any): void => {
    if (isAlias(node)) throw new Error("Aliases are unsupported");
    if (isNode(node) && (node.anchor || node.tag))
      throw new Error("Unsupported YAML annotation");
    if (isMap(node))
      node.items.forEach((pair: any) => {
        if (str(pair.key) === null || str(pair.key) === "<<")
          throw new Error("Unsupported key");
        visit(pair.value);
      });
    else if (isSeq(node)) node.items.forEach(visit);
  };
  visit(docs[0].contents);
  return docs[0];
}

/** Return null when the settings cannot be safely edited. */
export function readWorkbookSettings(source: string): WorkbookSettings | null {
  try {
    const doc = documentFor(source);
    const root = doc.contents;
    const name = str(get(root, "name"));
    const input = get(root, "input");
    const output = get(root, "output");
    const sheets = get(output, "sheets");
    if (
      name === null ||
      !isMap(input) ||
      !isMap(output) ||
      (!isMap(sheets) && !isSeq(sheets))
    )
      return null;
    const inputs: SetupInput[] = input.items.map((pair, i) => {
      const id = str(pair.key);
      if (id === null || !isMap(pair.value))
        throw new Error("Unsupported input");
      const labelNode = get(pair.value, "label");
      const label = labelNode === undefined ? id : str(labelNode);
      if (label === null) throw new Error("Unsupported label");
      return { key: `input-${i}`, id, label };
    });
    const outputs: SetupOutput[] = isMap(sheets)
      ? sheets.items.map((pair, i) => {
          const name = str(pair.key),
            table = str(pair.value);
          if (name === null || table === null)
            throw new Error("Unsupported sheet");
          return { key: `output-${i}`, name, table };
        })
      : (sheets as any).items.map((item: any, i: number) => {
          const name = str(get(item, "name")),
            table = str(get(item, "from"));
          if (!isMap(item) || name === null || table === null)
            throw new Error("Unsupported sheet");
          return { key: `output-${i}`, name, table };
        });
    return { name, inputs, outputs };
  } catch {
    return null;
  }
}

export function validateWorkbookSettings(settings: WorkbookSettings): string[] {
  const errors: string[] = [];
  if (!settings.name.trim()) errors.push("Enter a workflow display name.");
  if (!settings.inputs.length) errors.push("Add at least one input.");
  if (!settings.outputs.length) errors.push("Add at least one output sheet.");
  const ids = new Set<string>();
  for (const input of settings.inputs) {
    if (!/^[A-Za-z_][A-Za-z0-9_-]*$/.test(input.id))
      errors.push(
        "Input identifiers must start with a letter or underscore and contain only letters, numbers, underscores, or hyphens.",
      );
    if (ids.has(input.id)) errors.push("Input identifiers must be unique.");
    ids.add(input.id);
    if (!input.label.trim()) errors.push("Enter a label for each input.");
  }
  const names = new Set<string>();
  for (const output of settings.outputs) {
    if (
      !output.name ||
      output.name.trim() !== output.name ||
      output.name.startsWith("'") ||
      output.name.endsWith("'") ||
      [...output.name].length > 31 ||
      /[:\\/?*\[\]]/.test(output.name)
    )
      errors.push(
        "Sheet names must be 1–31 characters, with no leading or trailing spaces or apostrophes, and no : \\ / ? * [ ].",
      );
    const key = output.name.toLowerCase();
    if (names.has(key))
      errors.push("Output sheet names must be unique, ignoring case.");
    names.add(key);
    if (!output.table.trim())
      errors.push("Enter a saved table name for each output sheet.");
  }
  return [...new Set(errors)];
}

export function writeWorkbookSettings(
  source: string,
  settings: WorkbookSettings,
): string {
  const original = readWorkbookSettings(source);
  if (!original || validateWorkbookSettings(settings).length)
    throw new Error("Workbook settings cannot be applied.");
  const doc = documentFor(source);
  const root: any = doc.contents;
  const input: any = get(root, "input"),
    sheets: any = get(get(root, "output"), "sheets");
  const put = (map: any, key: string, value: string) => {
    const existing = get(map, key);
    if (isScalar(existing)) existing.value = value;
    else map.set(key, value);
  };
  put(root, "name", settings.name);
  const oldInputs = new Map(
    original.inputs.map((row, i) => [row.key, input.items[i]]),
  );
  input.items = settings.inputs.map((row) => {
    const pair: any =
      oldInputs.get(row.key) ?? doc.createPair(row.id, { label: row.label });
    if (isScalar(pair.key)) pair.key.value = row.id;
    put(pair.value, "label", row.label);
    return pair;
  });
  const oldOutputs = new Map(
    original.outputs.map((row, i) => [row.key, sheets.items[i]]),
  );
  sheets.items = settings.outputs.map((row) => {
    if (isMap(sheets)) {
      const pair: any =
        oldOutputs.get(row.key) ?? doc.createPair(row.name, row.table);
      if (isScalar(pair.key)) pair.key.value = row.name;
      if (isScalar(pair.value)) pair.value.value = row.table;
      return pair;
    }
    const item: any =
      oldOutputs.get(row.key) ??
      doc.createNode({ name: row.name, from: row.table });
    put(item, "name", row.name);
    put(item, "from", row.table);
    return item;
  });
  return doc.toString();
}
