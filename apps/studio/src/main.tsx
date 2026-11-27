import React from "react";
import { createRoot } from "react-dom/client";
import App from "./App";

async function start() {
  // The browser build (vite --mode web) brings its own host: the Go engine as
  // WebAssembly on an in-memory workspace. The desktop build leaves this out.
  if (import.meta.env.MODE === "web") {
    const { installWebHost } = await import("./web/engine");
    await installWebHost();
  }
  createRoot(document.getElementById("root")!).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>,
  );
}
void start();
