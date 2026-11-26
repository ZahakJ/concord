#!/usr/bin/env node
// The built APK, jars included: which methods call addJavascriptInterface (an object injected into every frame,
// third-party iframes included). One caller is allowed (MessageHandler's fallbacks are patched to throw, see
// scripts/patch-capacitor.mjs):
//   - org.apache.cordova.engine.SystemWebViewEngine.exposeJsInterface, from the cordova framework jar, allowed only
//     while this proves the engine is never built: nothing outside it calls its constructor, it is named only in
//     CordovaWebViewImpl.createEngine, createEngine is called only from CordovaActivity, and no class extends,
//     builds or declares CordovaActivity (Capacitor uses MockCordovaWebViewImpl).
// androidx.webkit's WebViewBuilderBoundaryInterface$Config declares a method of that name; a declaration is no call.
// Naming either by string (const-string "addJavascriptInterface", or SystemWebViewEngine for reflection) outside
// the allowed methods fails too. Static checks against the jars' source can't see any of this; the dex can.
//
//   node scripts/check-apk-jsi.mjs <apk>        (uses $ANDROID_HOME/build-tools' dexdump and aapt2)
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

export const ALLOWED = ["org.apache.cordova.engine.SystemWebViewEngine.exposeJsInterface"];
const ENGINE = "Lorg/apache/cordova/engine/SystemWebViewEngine;";
const ACTIVITY = "Lorg/apache/cordova/CordovaActivity;";

/** dexdump -d text → { calls: [[method, line]], supers: [[class, superclass]] }. */
export function parse(text) {
  const calls = [], supers = [];
  let method = "?", cls = "?";
  for (const line of text.split("\n")) {
    let m = line.match(/\|\[[0-9a-f]+\] ([^:]+):/);
    if (m) { method = m[1]; continue; }
    m = line.match(/Class descriptor\s*: '([^']+)'/);
    if (m) { cls = m[1]; continue; }
    m = line.match(/Superclass\s*: '([^']+)'/);
    if (m) { supers.push([cls, m[1]]); continue; }
    if (/\|[0-9a-f]{4}: /.test(line)) calls.push([method, line]);
  }
  return { calls, supers };
}

/** text: every dex's dump joined; dexes: each dump on its own, each of which must hold classes. */
export function check(text, manifest = "", dexes = [text]) {
  dexes.forEach((d, i) => { if (!/Class descriptor\s*:/.test(d)) throw new Error(`dex ${i + 1} has no Class descriptor: dexdump read nothing from it`); });
  const { calls, supers } = parse(text);
  const problems = [];
  for (const [m, l] of calls) {
    const str = l.match(/const-string(?:\/jumbo)? [^,]+, "(.*)" \/\/ string@/);
    // The engine's own methods name it as their log tag; it is never built (proven below), so they never run.
    if (!str || ALLOWED.includes(m) || m.startsWith("org.apache.cordova.engine.SystemWebViewEngine.")) continue;
    if (/addJavascriptInterface/.test(str[1])) problems.push(`${m} names addJavascriptInterface`);
    else if (/SystemWebViewEngine/.test(str[1])) problems.push(`${m} names SystemWebViewEngine`);
  }
  const callers = new Set(calls.filter(([, l]) => /invoke-[a-z/]+ .*\.addJavascriptInterface:/.test(l)).map(([m]) => m));
  for (const c of callers) if (!ALLOWED.includes(c)) problems.push(`${c} calls addJavascriptInterface`);
  if (callers.has(ALLOWED[0])) {
    const why = [];
    const outside = (m) => !m.startsWith("org.apache.cordova.engine.SystemWebViewEngine.");
    for (const [m, l] of calls) {
      if (l.includes(`${ENGINE}.<init>`) && /invoke-/.test(l) && outside(m)) why.push(`${m} builds SystemWebViewEngine`);
      if ((l.includes(`new-instance`) || l.includes("const-class")) && l.includes(ENGINE) && m !== "org.apache.cordova.CordovaWebViewImpl.createEngine")
        why.push(`${m} names SystemWebViewEngine`);
      if (/invoke-.*Lorg\/apache\/cordova\/CordovaWebViewImpl;\.createEngine:/.test(l) && !m.startsWith("org.apache.cordova.CordovaActivity."))
        why.push(`${m} calls CordovaWebViewImpl.createEngine`);
      if (l.includes("new-instance") && l.includes(ACTIVITY)) why.push(`${m} builds CordovaActivity`);
    }
    for (const [c, s] of supers) if (s === ACTIVITY) why.push(`${c} extends CordovaActivity`);
    if (manifest.includes("org.apache.cordova.CordovaActivity")) why.push("the manifest declares CordovaActivity");
    for (const w of why) problems.push(`SystemWebViewEngine may be built (its addJavascriptInterface is reachable): ${w}`);
  }
  return { problems, callers: [...callers] };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const apk = process.argv[2];
  const sdk = process.env.ANDROID_HOME || process.env.ANDROID_SDK_ROOT || path.join(os.homedir(), "Android/Sdk");
  const bt = fs.readdirSync(path.join(sdk, "build-tools")).sort((a, b) => a.localeCompare(b, undefined, { numeric: true })).pop();
  const tool = (n) => path.join(sdk, "build-tools", bt, n);
  if (!apk || !fs.existsSync(apk)) { console.error("usage: check-apk-jsi.mjs <apk>"); process.exit(1); }
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "apkjsi-"));
  const dexes = [];
  try {
    execFileSync("unzip", ["-q", "-o", apk, "classes*.dex", "-d", tmp]);
    for (const f of fs.readdirSync(tmp).filter((n) => n.endsWith(".dex")).sort())
      dexes.push(execFileSync(tool("dexdump"), ["-d", path.join(tmp, f)], { maxBuffer: 1 << 30, stdio: ["ignore", "pipe", "inherit"] }).toString());
  } finally { fs.rmSync(tmp, { recursive: true, force: true }); }
  const text = dexes.join("");
  const manifest = execFileSync(tool("aapt2"), ["dump", "xmltree", "--file", "AndroidManifest.xml", apk], { maxBuffer: 1 << 26 }).toString();
  if (!dexes.length) { console.error("check-apk-jsi: no classes*.dex in the APK"); process.exit(1); }
  let r;
  try { r = check(text, manifest, dexes); } catch (e) { console.error(`check-apk-jsi: ${e.message}`); process.exit(1); }
  const { problems, callers } = r;
  if (problems.length) { for (const p of problems) console.error(`check-apk-jsi: ${p}`); process.exit(1); }
  console.log(`check-apk-jsi: addJavascriptInterface only in ${callers.join(", ") || "nothing"}; SystemWebViewEngine is never built`);
}
