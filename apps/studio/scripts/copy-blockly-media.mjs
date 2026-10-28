// Bundle Blockly's own assets locally so the desktop editor works offline.
import { cpSync, mkdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

const source = fileURLToPath(
  new URL("../node_modules/blockly/media/", import.meta.url),
);
const destination = fileURLToPath(
  new URL("../public/blockly/", import.meta.url),
);
mkdirSync(destination, { recursive: true });
cpSync(source, destination, { recursive: true });
