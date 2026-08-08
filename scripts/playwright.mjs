// Locating playwright-core for the browser-driving scripts in this directory.
//
// playwright-core is deliberately NOT a project dependency: the app ships no JS
// test dependencies beyond vite, and adding one would make every contributor
// pay for a harness almost none of them run. So the drivers borrow an install
// instead — from PLAYWRIGHT_CORE, or from a node_modules/ in the working
// directory or the repo root.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

// PLAYWRIGHT_CORE may name either the package directory or its index.mjs —
// both are what people end up with after `npm i playwright-core` somewhere.
function resolveEntry(candidate) {
  if (!fs.existsSync(candidate)) return null;
  const entry = fs.statSync(candidate).isDirectory()
    ? path.join(candidate, "index.mjs")
    : candidate;
  return fs.existsSync(entry) ? entry : null;
}

export async function loadChromium() {
  const candidates = [
    process.env.PLAYWRIGHT_CORE,
    path.join(process.cwd(), "node_modules", "playwright-core"),
    path.join(REPO_ROOT, "node_modules", "playwright-core"),
  ].filter(Boolean);

  for (const candidate of candidates) {
    const entry = resolveEntry(candidate);
    // pathToFileURL: a bare absolute path is not a valid import specifier.
    if (entry) return (await import(pathToFileURL(entry).href)).chromium;
  }

  console.error(
    "FAIL: playwright-core not found. Install it somewhere (`npm i playwright-core`)\n" +
      "      and either run from that directory or point PLAYWRIGHT_CORE at the\n" +
      "      playwright-core package (or its index.mjs). Tried:\n" +
      candidates.map((c) => `        ${c}`).join("\n"),
  );
  process.exit(1);
}
