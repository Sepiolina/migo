import { useState } from "react";

export type Condition = {
  column: string;
  operator: string;
  value: string;
  type: "text" | "number";
};
const operators = ["==", "!=", ">", ">=", "<", "<="];
const numeric = /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/;
export function conditionExpression(condition: Condition): string {
  const column = `col(${JSON.stringify(condition.column)})`;
  const value =
    condition.type === "number"
      ? condition.value
      : JSON.stringify(condition.value);
  return `${condition.type === "number" ? "num" : "str"}(${column}) ${condition.operator} ${value}`;
}
export function parseCondition(expression: string): Condition | undefined {
  const match = expression.match(
    /^\s*(num|str)\(\s*(?:col\(\s*("(?:[^"\\]|\\.)*")\s*\)|([A-Za-z_][A-Za-z0-9_]*))\s*\)\s*(==|!=|>=|<=|>|<)\s*(.*?)\s*$/,
  );
  if (!match) return;
  try {
    const type = match[1] === "num" ? "number" : "text";
    const column = match[2] ? JSON.parse(match[2]) : match[3];
    if (!column || (type === "number" && !numeric.test(match[5]))) return;
    const value = type === "text" ? JSON.parse(match[5]) : match[5];
    if (typeof value !== "string") return;
    return { column, operator: match[4], value, type };
  } catch {
    return;
  }
}

export function SimpleCondition({
  expression,
  disabled,
  onChange,
  onValidityChange,
}: {
  expression: string;
  disabled: boolean;
  onChange: (expression: string) => void;
  onValidityChange: (valid: boolean) => void;
}) {
  const parsed = parseCondition(expression);
  const [mode, setMode] = useState<"builder" | "expression">(
    parsed || expression === "true" ? "builder" : "expression",
  );
  const [condition, setCondition] = useState<Condition>(
    parsed ?? { column: "", operator: "==", value: "", type: "text" },
  );
  const [touched, setTouched] = useState(false);
  const valid =
    !!condition.column.trim() &&
    (condition.type === "text" || numeric.test(condition.value));
  function update(next: Condition) {
    setCondition(next);
    setTouched(true);
    onValidityChange(
      !!next.column.trim() &&
        (next.type === "text" || numeric.test(next.value)),
    );
    onChange(conditionExpression(next));
  }
  return (
    <div className="condition-builder">
      <div className="condition-modes">
        <button
          type="button"
          aria-pressed={mode === "builder"}
          onClick={() => {
            setMode("builder");
            if (parsed) {
              setCondition(parsed);
              setTouched(false);
              onValidityChange(true);
            } else onValidityChange(!touched || valid);
          }}
        >
          Build condition
        </button>
        <button
          type="button"
          aria-pressed={mode === "expression"}
          onClick={() => {
            setMode("expression");
            onValidityChange(true);
          }}
        >
          Expression
        </button>
      </div>
      {mode === "builder" ? (
        <>
          {!parsed && !touched && expression !== "true" && (
            <p className="hint">
              Your current expression is preserved until you change a field
              below.
            </p>
          )}
          <label>
            Column
            <input
              value={condition.column}
              disabled={disabled}
              placeholder="e.g. Quantity"
              onChange={(event) =>
                update({ ...condition, column: event.target.value })
              }
            />
          </label>
          <label>
            Comparison
            <select
              value={condition.operator}
              disabled={disabled}
              onChange={(event) =>
                update({ ...condition, operator: event.target.value })
              }
            >
              {operators.map((operator) => (
                <option key={operator} value={operator}>
                  {
                    (
                      {
                        "==": "equals",
                        "!=": "does not equal",
                        ">": "is greater than",
                        ">=": "is at least",
                        "<": "is less than",
                        "<=": "is at most",
                      } as Record<string, string>
                    )[operator]
                  }
                </option>
              ))}
            </select>
          </label>
          <label>
            Compare as
            <select
              value={condition.type}
              disabled={disabled}
              onChange={(event) =>
                update({
                  ...condition,
                  type: event.target.value as Condition["type"],
                })
              }
            >
              <option value="text">Text</option>
              <option value="number">Number</option>
            </select>
          </label>
          <label>
            Value
            <input
              value={condition.value}
              disabled={disabled}
              placeholder={
                condition.type === "number" ? "e.g. 0" : "e.g. Active"
              }
              onChange={(event) =>
                update({ ...condition, value: event.target.value })
              }
            />
          </label>
          {touched && !condition.column.trim() && (
            <p className="inline-error">
              Enter a column name to complete the condition.
            </p>
          )}
          {touched &&
            condition.type === "number" &&
            !numeric.test(condition.value) && (
              <p className="inline-error">Enter a valid number.</p>
            )}
          {!touched && expression === "true" && (
            <p className="hint">
              Choose a column and comparison to keep matching rows.
            </p>
          )}
        </>
      ) : (
        <label>
          Expression
          <input
            value={expression}
            disabled={disabled}
            onChange={(event) => onChange(event.target.value)}
          />
          <small className="hint">For example, num(Quantity) &gt; 0.</small>
        </label>
      )}
    </div>
  );
}
