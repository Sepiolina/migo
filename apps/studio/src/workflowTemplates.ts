/** Starter workflows. Each one runs on the example files in the repository. */
export type Template = {
  id: string;
  title: string;
  description: string;
  inputs: string[];
  output: "xlsx" | "csv";
  source: string;
  /** Sample files for each input, relative to the repository root. */
  samples: Record<string, string>;
};

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

export const partsSummaryExample = `name: Parts summary
input:
  parts: {label: Parts list}
steps:
  - from: {input: parts, sheet: Parts, trim: true}
  - filter: num(Quantity) > 0
  - group: {by: [Part Number], sum: [Quantity]}
  - save: summary
output:
  sheets: {Parts: summary}
`;

export const workflowTemplates: Template[] = [
  {
    id: "clean-sheet",
    title: "Clean a sheet",
    description: "Trim cells, drop blank and duplicate rows, and write a tidy copy.",
    inputs: ["xlsx"],
    output: "xlsx",
    samples: { sheet: "examples/parts.xlsx" },
    source: `name: Clean a sheet
input:
  sheet: {label: Workbook to clean}
steps:
  - from: {input: sheet, trim: true, skip_blank: true}
  - dedupe: [Part Number]
  - save: clean
output:
  sheets: {Clean: clean}
`,
  },
  {
    id: "parts-summary",
    title: "Summarize by group",
    description: "Keep rows with a positive quantity, then total the quantity for each part.",
    inputs: ["xlsx"],
    output: "xlsx",
    samples: { parts: "examples/parts.xlsx" },
    source: partsSummaryExample,
  },
  {
    id: "lookup-enrich",
    title: "Look up and enrich",
    description: "Add each customer's region from a CSV list, then total sales per region.",
    inputs: ["xlsx", "csv"],
    output: "xlsx",
    samples: { orders: "examples/orders.xlsx", regions: "examples/regions.csv" },
    source: `name: Sales by region
input:
  orders: {label: Orders workbook}
  regions: {label: Customer regions, match: "(?i)region"}
steps:
  - from: regions
  - save: region_list
  - from: {input: orders, sheet: Orders, trim: true}
  - filter: num(Quantity) > 0
  - join: {with: region_list, on: [Customer], take: {Region: Region}}
  - set:
      Line total: num(Quantity) * num(col("Unit price"))
  - group: {by: [Region], sum: [Line total], count: [Customer]}
  - save: by_region
output:
  sheets: {By region: by_region}
`,
  },
  {
    id: "orders-totals",
    title: "Customer order totals",
    description: "The tutorial: filter quantities, calculate line totals and total each customer.",
    inputs: ["xlsx"],
    output: "xlsx",
    samples: { orders: "examples/orders.xlsx" },
    source: ordersTotalsExample,
  },
  {
    id: "excel-to-csv",
    title: "Excel to CSV",
    description: "Convert a sheet to a UTF-8 CSV file, for imports into other systems.",
    inputs: ["xlsx"],
    output: "csv",
    samples: { sheet: "examples/parts.xlsx" },
    source: `name: Excel to CSV
input:
  sheet: {label: Workbook}
steps:
  - from: {input: sheet, trim: true}
  - save: rows
output:
  format: csv
  sheets: {Rows: rows}
`,
  },
  {
    id: "blank",
    title: "Blank workflow",
    description: "One source, one output. Add steps from the table or the command menu.",
    inputs: ["xlsx"],
    output: "xlsx",
    samples: {},
    source: `name: New workflow
input:
  data: {label: Data file}
steps:
  - from: data
  - save: result
output:
  sheets: {Result: result}
`,
  },
];
