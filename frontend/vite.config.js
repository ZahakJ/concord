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
        //
        // The QR libraries are the exception. They are large, they are reachable
        // from exactly two screens (the login scanner and device linking), and
        // most sessions open neither — but naming them "vendor" would pull them
        // back out of the dynamic imports at those two call sites and straight
        // into the boot chunk, which is where they are today. Returning nothing
        // for them lets the bundler give each dynamic import its own chunk.
        manualChunks(id) {
          if (/node_modules[/\\](jsqr|qrcode|encode-utf8|dijkstrajs|pngjs)[/\\]/.test(id)) return;
          if (id.includes("node_modules")) return "vendor";
        },
      },
    },
  },
});
