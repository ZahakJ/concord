import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// Wails serves the built assets from frontend/dist and injects its runtime
// bindings at window.go / window.runtime.
export default defineConfig({
  plugins: [svelte()],
  build: {
    // Emit assets Wails embeds; keep it self-contained.
    outDir: "dist",
    emptyOutDir: true,
    rollupOptions: {
      output: {
        // Split third-party deps into their own long-cached chunk so the app
        // chunk stays lean and a code change doesn't re-download the vendor JS.
        manualChunks(id) {
          if (id.includes("node_modules")) return "vendor";
        },
      },
    },
  },
});
