// The patch and the check together (node scripts/check-capacitor-jsi.test.mjs), on a fixture tree.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import assert from "node:assert/strict";
import { check } from "./check-capacitor-jsi.mjs";
import { patch, PATCHES, VERSION } from "./patch-capacitor.mjs";

function fixture(version = VERSION) {
  const nm = fs.mkdtempSync(path.join(os.tmpdir(), "capjsi-"));
  const src = path.join(nm, "@capacitor/android/capacitor/src/main/java/com/getcapacitor");
  fs.mkdirSync(path.join(src, "plugin"), { recursive: true });
  fs.writeFileSync(path.join(nm, "@capacitor/android/package.json"), JSON.stringify({ version }));
  fs.writeFileSync(path.join(src, "MessageHandler.java"), "class M { void a() { webView.addJavascriptInterface(this, \"androidBridge\"); } }");
  for (const [rel, line] of PATCHES) fs.writeFileSync(path.join(src, rel), `class P {\n    public void load() {\n        ${line}\n        super.load();\n    }\n}\n`);
  return nm;
}

// Unpatched: both plugins fail the check; MessageHandler alone is allowed.
let nm = fixture();
let r = check(nm);
assert.equal(r.problems.length, 2, r.problems.join("\n"));
assert.equal(r.allowedSeen, 1);

// Patched: the check passes, the rest of load() stays, and patching again is a no-op.
assert.deepEqual(patch(nm).map((d) => d.endsWith(": patched")), [true, true]);
r = check(nm);
assert.deepEqual(r.problems, []);
for (const [rel] of PATCHES) {
  const t = fs.readFileSync(path.join(nm, "@capacitor/android/capacitor/src/main/java/com/getcapacitor", rel), "utf8");
  assert.ok(t.includes("super.load();") && t.includes("concord-patch"), rel);
}
assert.deepEqual(patch(nm).map((d) => d.endsWith("already patched")), [true, true]);

// A plugin elsewhere that adds one fails, Kotlin included; a comment mentioning it doesn't.
const other = path.join(nm, "some-plugin/android/src/main/java/x");
fs.mkdirSync(other, { recursive: true });
fs.writeFileSync(path.join(other, "Y.java"), "// addJavascriptInterface( is only mentioned here\nclass Y {}");
assert.deepEqual(check(nm).problems, []);
fs.writeFileSync(path.join(other, "Z.kt"), "class Z { fun f() { wv.addJavascriptInterface(this, \"z\") } }");
assert.equal(check(nm).problems.length, 1);

// Another Capacitor version, or upstream text that changed, fails the patch loudly.
assert.throws(() => patch(fixture("9.0.0")), /review them/);
nm = fixture();
fs.writeFileSync(path.join(nm, "@capacitor/android/capacitor/src/main/java/com/getcapacitor", PATCHES[0][0]), "class P {}");
assert.throws(() => patch(nm), /neither the expected line nor the patch/);

console.log("check-capacitor-jsi.test: ok");
