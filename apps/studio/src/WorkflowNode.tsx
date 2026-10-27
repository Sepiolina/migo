import { memo, type CSSProperties } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import type { WorkflowGraphNode } from "./model";
import "./workflow-node.css";

const categoryColors: Record<string, string> = {
  Data: "#6dacf9",
  Transform: "#b899ff",
  Other: "#f2b66d",
};
const operationColors: Record<string, string> = {
  from: "#6dacf9",
  filter: "#efb36d",
  set: "#c2a0ff",
  select: "#77c4e5",
  rename: "#89cbd2",
  sort: "#f5a1c6",
  group: "#84d0b3",
  join: "#d6b3ff",
  save: "#7eb4ff",
};
const WorkflowNode = memo(function WorkflowNode({
  data,
  selected,
}: NodeProps<WorkflowGraphNode>) {
  const color = data.branch
    ? "#e8bc69"
    : operationColors[data.operation] ||
      categoryColors[data.category] ||
      "#80cbb4";
  return (
    <article
      className={`workflow-node${selected ? " is-selected" : ""}`}
      style={{ "--node-color": color } as CSSProperties}
      aria-label={`${data.title}: ${data.summary}`}
    >
      <Handle
        type="target"
        position={Position.Left}
        id="input"
        aria-label="Input flow"
      />
      <header className="workflow-node__header">
        <span className="workflow-node__icon" aria-hidden="true">
          {data.icon}
        </span>
        <span className="workflow-node__category">
          {data.branch ? "Control" : data.category}
        </span>
        <span className="workflow-node__number">
          {String(data.stepIndex + 1).padStart(2, "0")}
        </span>
      </header>
      <div className="workflow-node__body">
        <h3>{data.title}</h3>
        <p title={data.summary}>{data.summary}</p>
      </div>
      <footer className="workflow-node__footer">
        <span>IN</span>
        <span>{data.branch ? "THEN / ELSE" : "OUT"}</span>
      </footer>
      {data.branch ? (
        <>
          <Handle
            type="source"
            position={Position.Right}
            id="then"
            style={{ top: "42%" }}
            aria-label="Then flow"
          />
          <Handle
            type="source"
            position={Position.Right}
            id="else"
            style={{ top: "76%" }}
            aria-label="Else flow"
          />
        </>
      ) : (
        <Handle
          type="source"
          position={Position.Right}
          id="output"
          aria-label="Output flow"
        />
      )}
    </article>
  );
});

export const workflowNodeTypes = { workflow: WorkflowNode };
