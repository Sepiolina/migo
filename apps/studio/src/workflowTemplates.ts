import { example } from "./model";

export const workflowTemplates = [
  {
    id: "copy-workbook",
    title: "Copy a worksheet",
    icon: "▤",
    description:
      "Read an Excel sheet and create an output workbook. A simple place to start.",
    source: `name: Copy a worksheet
input:
  parts: {label: Source workbook}
steps:
  - from: parts
  - save: result
output:
  sheets: {Result: result}
`,
  },
  {
    id: "parts-summary",
    title: "Filter and summarize",
    icon: "▥",
    description:
      "Keep rows with a positive quantity, group by part number, and total the quantities.",
    source: example,
  },
];
