import { stepByOp, steps } from "./stepRegistry";

export type Operation = {
  operation: string;
  title: string;
  category: string;
  description: string;
  icon: string;
  defaultArgument: string;
  service?: boolean;
};

/** The step library, generated from the Studio step registry. */
export const operations: Operation[] = steps.map((spec) => ({
  operation: spec.op,
  title: spec.title,
  category: spec.category,
  description: spec.description,
  icon: spec.icon,
  defaultArgument: spec.op === "if" ? '"true"' : spec.example,
  service: spec.service,
}));

export function getOperation(operation: string): Operation {
  const known = operations.find((item) => item.operation === operation);
  if (known) return known;
  return {
    operation,
    title: stepByOp.get(operation)?.title ?? operation,
    category: "Other",
    icon: "•",
    description: "Edit this step in YAML.",
    defaultArgument: "null",
  };
}
