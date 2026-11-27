import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
export default defineConfig(({ mode }) => ({
  plugins: [react()],
  clearScreen: false,
  // The browser build is one self-contained page: relative asset URLs, one
  // script, and the engine files (scripts/build-web.mjs) served beside it.
  ...(mode === "web" ? {
    base: "./",
    publicDir: ".web-engine",
    build: {
      outDir: "dist-web/assets",
      emptyOutDir: true,
      copyPublicDir: false,
      assetsInlineLimit: 1 << 20,
      cssCodeSplit: false,
      modulePreload: false,
      rollupOptions: { output: { inlineDynamicImports: true } },
    },
  } : {}),
  server: {
    watch: {
      // Rust owns native rebuilds. Watching Cargo output can hit locked .pdb
      // files on Windows and creates unnecessary frontend reloads elsewhere.
      ignored: ["**/src-tauri/**"],
    },
  },
}));
