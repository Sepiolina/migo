import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
export default defineConfig({
  plugins: [react()],
  clearScreen: false,
  server: {
    watch: {
      // Rust owns native rebuilds. Watching Cargo output can hit locked .pdb
      // files on Windows and creates unnecessary frontend reloads elsewhere.
      ignored: ["**/src-tauri/**"],
    },
  },
});
