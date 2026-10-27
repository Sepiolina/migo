export type Operation = {
  operation: string;
  title: string;
  category: string;
  description: string;
  icon: string;
  defaultArgument: string;
};

export const operations: Operation[] = [
  {
    operation: "from",
    title: "Read data",
    category: "Data",
    description: "Start with an input or saved table.",
    icon: "↳",
    defaultArgument: '""',
  },
  {
    operation: "filter",
    title: "Filter rows",
    category: "Transform",
    description: "Keep rows that match a condition.",
    icon: "◇",
    defaultArgument: '"true"',
  },
  {
    operation: "set",
    title: "Calculate columns",
    category: "Transform",
    description: "Create or update columns with expressions.",
    icon: "ƒ",
    defaultArgument: "{}",
  },
  {
    operation: "select",
    title: "Select columns",
    category: "Transform",
    description: "Choose and order columns.",
    icon: "☷",
    defaultArgument: "{}",
  },
  {
    operation: "rename",
    title: "Rename columns",
    category: "Transform",
    description: "Give columns new names.",
    icon: "✎",
    defaultArgument: "{}",
  },
  {
    operation: "sort",
    title: "Sort rows",
    category: "Transform",
    description: "Order rows by columns.",
    icon: "⇅",
    defaultArgument: "{by: []}",
  },
  {
    operation: "group",
    title: "Group rows",
    category: "Transform",
    description: "Summarize rows by key.",
    icon: "⊞",
    defaultArgument: "{by: []}",
  },
  {
    operation: "join",
    title: "Join tables",
    category: "Transform",
    description: "Add columns from a saved table.",
    icon: "⋈",
    defaultArgument: "{with: '', on: []}",
  },
  {
    operation: "save",
    title: "Save table",
    category: "Data",
    description: "Keep a copy for a later step.",
    icon: "▣",
    defaultArgument: '""',
  },
];

const additional: Record<string, [string, string, string]> = {
  if: ["Condition", "Control", "◇"],
  split: ["Split rows", "Control", "⑂"],
  dedupe: ["Remove duplicates", "Transform", "≠"],
  fill: ["Fill blanks", "Transform", "↓"],
  append: ["Append tables", "Transform", "+"],
  window: ["Calculate by group", "Transform", "ƒ"],
  pick: ["Pick rows", "Transform", "✓"],
  stack: ["Stack columns", "Transform", "⇵"],
  compare: ["Compare tables", "Transform", "⇄"],
  sql: ["SQL query", "Data", "⌘"],
  rest: ["REST request", "Data", "↗"],
  log: ["Log message", "Control", "☰"],
  fail: ["Stop with error", "Control", "!"],
};
export function getOperation(operation: string): Operation {
  const known = operations.find((item) => item.operation === operation);
  if (known) return known;
  const [title, category, icon] = additional[operation] ?? [
    operation,
    "Other",
    "•",
  ];
  return {
    operation,
    title,
    category,
    icon,
    description: "Edit this step in YAML.",
    defaultArgument: "null",
  };
}
