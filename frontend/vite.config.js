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
  },
});
