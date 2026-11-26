#!/usr/bin/env node
// addJavascriptInterface injects a Java object into every frame of the WebView, third-party iframes included. The
// only one allowed in the installed Android sources is Capacitor's MessageHandler fallback for a WebView without
// WEB_MESSAGE_LISTENER, which WebViewGate makes unreachable (Concord refuses to start on such a WebView). This fails
// on any other, in @capacitor/android or any plugin's android sources, so an upgrade or a new plugin can't bring
// one back unseen. Run after `npm ci` (which applies scripts/patch-capacitor.mjs).
//
//   node scripts/check-capacitor-jsi.mjs [apps/mobile/node_modules]
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const ALLOWED = ["@capacitor/android/capacitor/src/main/java/com/getcapacitor/MessageHandler.java"];

function* files(dir) {
  if (!fs.existsSync(dir)) return;
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) yield* files(p);
    else if (/\.(java|kt)$/.test(e.name)) yield p;
  }
}

// Every package's android/ (scoped or not) and @capacitor/android itself.
function androidRoots(nm) {
  const roots = [];
  for (const e of fs.existsSync(nm) ? fs.readdirSync(nm) : []) {
    if (e.startsWith(".")) continue;
    const pkgs = e.startsWith("@") ? fs.readdirSync(path.join(nm, e)).map((n) => path.join(e, n)) : [e];
    for (const p of pkgs) {
      for (const sub of ["android", "capacitor"]) {
        const d = path.join(nm, p, sub);
        if (fs.existsSync(d)) roots.push(d);
      }
    }
  }
  return roots;
}

export function check(nm) {
  const problems = [];
  let allowedSeen = 0;
  for (const root of androidRoots(nm)) {
    for (const f of files(root)) {
      const text = fs.readFileSync(f, "utf8");
      if (!/addJavascriptInterface\s*\(/.test(text.replace(/\/\/.*$/gm, ""))) continue;
      const rel = path.relative(nm, f).split(path.sep).join("/");
      if (ALLOWED.includes(rel)) allowedSeen++;
      else problems.push(`${rel} calls addJavascriptInterface`);
    }
  }
  return { problems, allowedSeen };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const here = path.dirname(fileURLToPath(import.meta.url));
  const nm = process.argv[2] ?? path.join(here, "../apps/mobile/node_modules");
  if (!fs.existsSync(path.join(nm, "@capacitor/android"))) {
    console.error(`check-capacitor-jsi: no @capacitor/android under ${nm} (run npm ci first)`);
    process.exit(1);
  }
  const { problems } = check(nm);
  if (problems.length) {
    for (const p of problems) console.error(`check-capacitor-jsi: ${p}`);
    process.exit(1);
  }
  console.log("check-capacitor-jsi: only MessageHandler's old-WebView fallback (unreachable: WebViewGate)");
}
