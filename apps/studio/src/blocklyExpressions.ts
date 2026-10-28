/** The deliberately small expression grammar represented by the visual editor. */
export type VisualExpression =
  | { kind: "column"; name: string; mode: "raw" | "number" | "text" }
  | { kind: "number" | "text" | "boolean" | "custom"; value: string }
  | {
      kind: "binary";
      op: string;
      left: VisualExpression;
      right: VisualExpression;
    }
  | { kind: "not"; value: VisualExpression };

const reserved = new Set([
  "index",
  "size",
  "group",
  "prev",
  "col",
  "config",
  "norm",
  "trim",
  "str",
  "num",
  "toNumber",
  "parseFloat",
  "blank",
  "truthy",
  "coalesce",
  "fields",
  "splitAny",
  "replaceRe",
  "rows",
  "nil",
]);
const priority: Record<string, number> = {
  "||": 1,
  "&&": 2,
  "==": 3,
  "!=": 3,
  ">": 3,
  ">=": 3,
  "<": 3,
  "<=": 3,
  "+": 4,
  "-": 4,
  "*": 5,
  "/": 5,
};
const tokenPattern =
  /\s*(?:("(?:[^"\\]|\\.)*")|((?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?)|([A-Za-z_][A-Za-z0-9_]*)|(&&|\|\||==|!=|>=|<=|[+\-*/!<>()]))/gy;
type Token = {
  type: "string" | "number" | "identifier" | "operator";
  text: string;
};

/** Unknown syntax returns null, so the complete expression is preserved. */
export function parseVisualExpression(source: string): VisualExpression | null {
  const tokens: Token[] = [];
  let offset = 0;
  while (offset < source.length) {
    if (!source.slice(offset).trim()) break;
    tokenPattern.lastIndex = offset;
    const match = tokenPattern.exec(source);
    if (!match) return null;
    offset = tokenPattern.lastIndex;
    const types: Token["type"][] = [
      "string",
      "number",
      "identifier",
      "operator",
    ];
    const index = match.slice(1).findIndex(Boolean);
    tokens.push({ type: types[index], text: match[index + 1] });
    if (tokens.length > 1000) return null;
  }
  let cursor = 0,
    depth = 0;
  const read = (min = 0): VisualExpression => {
    if (++depth > 100) throw new Error("Expression too deep");
    const token = tokens[cursor++];
    if (!token) throw new Error("Missing value");
    let left: VisualExpression;
    if (token.text === "(") {
      left = read();
      if (tokens[cursor++]?.text !== ")")
        throw new Error("Missing closing parenthesis");
    } else if (token.text === "!") {
      left = { kind: "not", value: read(6) };
    } else if (token.text === "-" && tokens[cursor]?.type === "number") {
      const value = `-${tokens[cursor++].text}`;
      if (!Number.isFinite(Number(value))) throw new Error("Invalid number");
      left = { kind: "number", value };
    } else if (token.type === "number") {
      if (!Number.isFinite(Number(token.text)))
        throw new Error("Invalid number");
      left = { kind: "number", value: token.text };
    } else if (token.type === "string") {
      const value: unknown = JSON.parse(token.text);
      if (typeof value !== "string") throw new Error("Invalid text");
      left = { kind: "text", value };
    } else if (token.type === "identifier") {
      const name = token.text;
      if (tokens[cursor]?.text === "(") {
        cursor++;
        if (name === "col") {
          const arg = tokens[cursor++];
          if (arg?.type !== "string") throw new Error("Unknown column call");
          const column: unknown = JSON.parse(arg.text);
          if (
            typeof column !== "string" ||
            !column.trim() ||
            /[\r\n]/.test(column) ||
            tokens[cursor++]?.text !== ")"
          )
            throw new Error("Invalid column");
          left = { kind: "column", name: column, mode: "raw" };
        } else if (name === "num" || name === "str") {
          const arg = read();
          if (
            tokens[cursor++]?.text !== ")" ||
            arg.kind !== "column" ||
            arg.mode !== "raw"
          )
            throw new Error("Unknown conversion");
          left = { ...arg, mode: name === "num" ? "number" : "text" };
        } else throw new Error("Unknown call");
      } else if (name === "true" || name === "false")
        left = { kind: "boolean", value: name };
      else if (reserved.has(name)) left = { kind: "custom", value: name };
      else left = { kind: "column", name, mode: "raw" };
    } else throw new Error("Unknown value");
    while (cursor < tokens.length) {
      const op = tokens[cursor].text,
        prec = priority[op];
      if (!prec || prec < min) break;
      cursor++;
      left = { kind: "binary", op, left, right: read(prec + 1) };
    }
    depth--;
    return left;
  };
  try {
    const result = read();
    return cursor === tokens.length ? result : null;
  } catch {
    return null;
  }
}
