/**
 * Starter workflows. They are declared once, in Go
 * (internal/workflow/templates/*.yaml and templates.go), shared with
 * `migo new`, and reach Studio in steps.generated.json (`make describe`).
 * Each one runs on the example files in the repository.
 */
import catalog from "./steps.generated.json";

export type Template = {
  id: string;
  title: string;
  description: string;
  /** The file kind of each input, in order. */
  inputs: string[];
  output: "xlsx" | "csv";
  source: string;
  /** Sample files for each input, relative to the repository root. */
  samples: Record<string, string>;
};

export const workflowTemplates = catalog.templates as Template[];

const sourceOf = (id: string) => workflowTemplates.find((t) => t.id === id)!.source;
export const ordersTotalsExample = sourceOf("orders-totals");
export const partsSummaryExample = sourceOf("parts-summary");
