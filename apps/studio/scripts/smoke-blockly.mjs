// Exercise Blockly-generated YAML with the actual Go parser and Excel runner.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import {
  copyFileSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";
import { transformWithEsbuild } from "vite";
import Blockly from "blockly/core";

const studio = fileURLToPath(new URL("../", import.meta.url));
const root = resolve(studio, "../..");
const directory = mkdtempSync(join(tmpdir(), "mai-blockly-"));
const codecDirectory = mkdtempSync(join(studio, "node_modules/.mai-blockly-"));
const workspace = new Blockly.Workspace();
try {
  // Compile the actual frontend codec with Vite's TypeScript transform. Keep
  // it beside node_modules so Node resolves Blockly's headless entry point.
  writeFileSync(join(codecDirectory, "package.json"), '{"type":"commonjs"}');
  for (const filename of [
    "blocklyExpressions.ts",
    "blocklyBlocks.ts",
    "blocklyWorkflow.ts",
    "workflowTemplates.ts",
    "model.ts",
    "operationCatalog.ts",
    "SimpleCondition.tsx",
  ]) {
    const { code } = await transformWithEsbuild(
      readFileSync(join(studio, "src", filename), "utf8"),
      filename,
      { format: "cjs", target: "es2022", jsx: "automatic" },
    );
    writeFileSync(
      join(codecDirectory, filename.replace(/\.tsx?$/, ".js")),
      code,
    );
  }
  const require = createRequire(import.meta.url);
  const { loadWorkflow, serializeWorkflow } = require(
    join(codecDirectory, "blocklyWorkflow.js"),
  );
  const { workflowTemplates } = require(
    join(codecDirectory, "workflowTemplates.js"),
  );
  const source = `name: Visual totals
input:
  parts: {label: Parts}
steps: []
output:
  sheets: {Result: result}
`;
  loadWorkflow(workspace, source);
  const create = (type, fields = {}) => {
    const block = workspace.newBlock(type);
    for (const [name, value] of Object.entries(fields))
      block.setFieldValue(value, name);
    return block;
  };
  const connectValue = (parent, name, child) =>
    parent.getInput(name).connection.connect(child.outputConnection);
  const read = create("mai_from", { SOURCE: "parts", SHEET: "Parts" });
  const calculate = create("mai_set", { COLUMN: "Total quantity" });
  const arithmetic = create("mai_arithmetic", { OP: "*" });
  connectValue(
    arithmetic,
    "LEFT",
    create("mai_column", { NAME: "Quantity", MODE: "number" }),
  );
  connectValue(arithmetic, "RIGHT", create("mai_number", { VALUE: "2" }));
  connectValue(calculate, "VALUE", arithmetic);
  const filter = create("mai_filter");
  const compare = create("mai_compare", { OP: ">" });
  connectValue(
    compare,
    "LEFT",
    create("mai_column", { NAME: "Total quantity", MODE: "number" }),
  );
  connectValue(compare, "RIGHT", create("mai_number", { VALUE: "0" }));
  connectValue(filter, "VALUE", compare);
  const group = create("mai_group", {
    BY: "Part Number",
    SUM: "Total quantity",
  });
  const sort = create("mai_sort", {
    COLUMNS: "Part Number",
    DIRECTION: "false",
    NUMERIC: "false",
  });
  const save = create("mai_save", { NAME: "result" });
  let connection = workspace
    .getTopBlocks(false)
    .find((block) => block.type === "mai_workflow")
    .getInput("STEPS").connection;
  for (const block of [read, calculate, filter, group, sort, save]) {
    connection.connect(block.previousConnection);
    connection = block.nextConnection;
  }
  const yaml = serializeWorkflow(workspace, source);
  const runner = join(
    directory,
    process.platform === "win32" ? "runner.exe" : "runner",
  );
  execFileSync(
    process.env.MAI_GO || "go",
    ["build", "-mod=readonly", "-o", runner, "./cmd/mai-runner"],
    { cwd: root, stdio: "inherit" },
  );
  copyFileSync(
    join(root, "examples/parts.xlsx"),
    join(directory, "parts.xlsx"),
  );
  copyFileSync(
    join(root, "examples/orders.xlsx"),
    join(directory, "orders.xlsx"),
  );
  function request(command, params) {
    const id = `blocks-${command}`;
    const output = execFileSync(runner, [], {
      input:
        JSON.stringify({
          version: 1,
          id,
          command,
          params: { workspace: directory, ...params },
        }) + "\n",
      encoding: "utf8",
      timeout: 30000,
      maxBuffer: 1024 * 1024,
    });
    const response = JSON.parse(output);
    assert.equal(response.id, id);
    assert.equal(response.ok, true, JSON.stringify(response.error));
    return response.result;
  }
  assert.deepEqual(request("validate", { yaml }).sheets, ["Result"]);
  const input = request("inspectWorkbook", {
    path: "parts.xlsx",
    sheet: "Parts",
  });
  const idIndex = input.rows[0].indexOf("Part Number"),
    quantityIndex = input.rows[0].indexOf("Quantity");
  assert.ok(idIndex >= 0 && quantityIndex >= 0);
  const expected = new Map();
  for (const row of input.rows.slice(1)) {
    const amount = Number(row[quantityIndex]) * 2;
    if (amount > 0)
      expected.set(row[idIndex], (expected.get(row[idIndex]) || 0) + amount);
  }
  const result = request("run", { yaml, inputs: { parts: "parts.xlsx" } });
  const preview = request("inspectWorkbook", { path: result.path });
  assert.deepEqual(preview.rows[0], ["Part Number", "Total quantity"]);
  assert.deepEqual(
    new Map(preview.rows.slice(1).map(([id, total]) => [id, Number(total)])),
    expected,
  );
  const artifact = readFileSync(resolve(directory, result.path));
  assert.equal(
    createHash("sha256").update(artifact).digest("hex"),
    result.sha256,
  );
  read.setFieldValue("number", "SHEET_KIND");
  read.setFieldValue("1", "SHEET");
  const indexedYaml = serializeWorkflow(workspace, source);
  assert.deepEqual(
    request("run", { yaml: indexedYaml, inputs: { parts: "parts.xlsx" } })
      .preview.rows,
    preview.rows,
  );
  // Templates must actually run against the workbook named in the user guide.
  for (const template of workflowTemplates) {
    loadWorkflow(workspace, template.source);
    if (template.id === "orders-totals") {
      assert.equal(
        workspace
          .getAllBlocks(false)
          .some((block) =>
            ["mai_preserved", "mai_custom_expression"].includes(block.type),
          ),
        false,
        "The tutorial must be fully editable with visual blocks",
      );
    }
    const yaml = serializeWorkflow(workspace, template.source);
    request("validate", { yaml });
    const inputs =
      template.id === "orders-totals"
        ? { orders: "orders.xlsx" }
        : { parts: "parts.xlsx" };
    const output = request("run", { yaml, inputs });
    assert.ok(
      output.preview.rows.length > 1,
      `${template.title} produced no data`,
    );
    if (template.id === "parts-summary") {
      assert.deepEqual(
        new Map(
          output.preview.rows
            .slice(1)
            .map(([part, quantity]) => [part, Number(quantity)]),
        ),
        new Map([
          ["00123", 5],
          ["00456", 1],
        ]),
      );
    }
    if (template.id === "orders-totals") {
      // Inspect the actual saved XLSX rather than trusting the run preview.
      const workbook = request("inspectWorkbook", {
        path: output.path,
        sheet: "Totals",
      });
      assert.deepEqual(workbook.rows[0], ["Customer", "Line total"]);
      assert.deepEqual(
        new Map(
          workbook.rows
            .slice(1)
            .map(([customer, total]) => [customer, Number(total)]),
        ),
        new Map([
          ["Acme", 50],
          ["Beta", 5],
        ]),
      );
      assert.equal(
        createHash("sha256")
          .update(readFileSync(resolve(directory, output.path)))
          .digest("hex"),
        output.sha256,
      );
      // The reusable file must match the template and have identical results.
      const savedYaml = readFileSync(
        join(root, "examples/orders-totals.yaml"),
        "utf8",
      );
      assert.equal(savedYaml, template.source);
      const savedRun = request("run", { yaml: savedYaml, inputs });
      assert.deepEqual(
        request("inspectWorkbook", { path: savedRun.path, sheet: "Totals" })
          .rows,
        workbook.rows,
      );
    }
  }
  console.log(
    `Blockly integration passed: ${expected.size} grouped rows, numeric calculations, and verified XLSX artifacts including Orders totals Acme=50, Beta=5.`,
  );
} finally {
  workspace.dispose();
  rmSync(codecDirectory, { recursive: true, force: true });
  rmSync(directory, { recursive: true, force: true });
}
