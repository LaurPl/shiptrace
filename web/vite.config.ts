import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

// The output is consumed via //go:embed in cmd/shiptrace/main.go, which can
// only see files under its own package subtree. We render the build into
// cmd/shiptrace/web/dist so the Go binary picks it up automatically.
const goEmbedDir = path.resolve(__dirname, "../cmd/shiptrace/web/dist");

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: goEmbedDir,
    emptyOutDir: true,
    // Keep the bundle small; the dashboard is read-only and trivial.
    target: "es2020",
    sourcemap: false,
  },
  server: {
    port: 5173,
    // During dev (`npm run dev`), proxy the API to the running Go server.
    proxy: {
      "/api": "http://127.0.0.1:7777",
    },
  },
});
