import { example } from "./model";

export const ordersTotalsExample = `name: Customer order totals
input:
  orders: {label: Orders workbook}
steps:
  - from: {input: orders, sheet: Orders, trim: true}
  - filter: num(Quantity) > 0
  - set:
      Line total: num(Quantity) * num(col("Unit price"))
  - group: {by: [Customer], sum: [Line total]}
  - save: totals
output:
  sheets: {Totals: totals}
`;

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
  {
    id: "orders-totals",
    title: "Customer order totals",
    icon: "▦",
    description:
      "Try the Orders tutorial: filter quantities, calculate line totals, and total each customer's orders. Use examples/orders.xlsx.",
    source: ordersTotalsExample,
  },
];
