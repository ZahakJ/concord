// check-apk-jsi on dexdump-shaped fixtures (node scripts/check-apk-jsi.test.mjs).
import assert from "node:assert/strict";
import { check } from "./check-apk-jsi.mjs";

const cls = (c, s = "Ljava/lang/Object;") => `  Class descriptor  : '${c}'\n  Superclass        : '${s}'\n`;
const method = (m, ...body) => `0001:                                        |[000001] ${m}:()V\n` + body.map((b, i) => `0002: 0000                                   |${String(i).padStart(4, "0")}: ${b}\n`).join("");
const JSI = "invoke-virtual {v0, v1, v2}, Landroid/webkit/WebView;.addJavascriptInterface:(Ljava/lang/Object;Ljava/lang/String;)V";
const base = [
  cls("Lcom/getcapacitor/MessageHandler;"), method("com.getcapacitor.MessageHandler.<init>", 'const-string v0, "concord-patch: no bridge for a WebView without WEB_MESSAGE_LISTENER (WebViewGate)" // string@9'),
  cls("Lorg/apache/cordova/engine/SystemWebViewEngine;"), method("org.apache.cordova.engine.SystemWebViewEngine.exposeJsInterface", JSI),
  method("org.apache.cordova.engine.SystemWebViewEngine.<init>", "invoke-direct {v1}, Lorg/apache/cordova/engine/SystemWebViewEngine;.<init>:(Lx;)V"),
  cls("Lorg/apache/cordova/CordovaWebViewImpl;"), method("org.apache.cordova.CordovaWebViewImpl.createEngine", "const-class v0, Lorg/apache/cordova/engine/SystemWebViewEngine; // type@1"),
  cls("Lorg/apache/cordova/CordovaActivity;", "Landroid/app/Activity;"), method("org.apache.cordova.CordovaActivity.makeWebViewEngine", "invoke-static {v1, v0}, Lorg/apache/cordova/CordovaWebViewImpl;.createEngine:(Lx;Ly;)Lz;"),
  cls("Lorg/chromium/support_lib_boundary/WebViewBuilderBoundaryInterface$Config;"),
  "      name          : 'addJavascriptInterface'\n",
].join("");

let r = check(base);
assert.deepEqual(r.problems, [], r.problems.join("\n"));
assert.equal(r.callers.length, 1);

// MessageHandler calling it again (an unpatched build) fails.
r = check(base + cls("Lcom/getcapacitor/MessageHandler;") + method("com.getcapacitor.MessageHandler.<init>", JSI));
assert.deepEqual(r.problems, ["com.getcapacitor.MessageHandler.<init> calls addJavascriptInterface"]);

// Reaching either by name, for reflection, fails outside the allowed methods.
for (const str of ["addJavascriptInterface", "org.apache.cordova.engine.SystemWebViewEngine"]) {
  r = check(base + cls("Lapp/R;") + method("app.R.go", `const-string v0, "${str}" // string@7`));
  assert.equal(r.problems.length, 1, str);
  assert.match(r.problems[0], /^app\.R\.go names /);
}

// The engine naming itself (its log tag) is not a way in.
assert.deepEqual(check(base + method("org.apache.cordova.engine.SystemWebViewEngine.destroy", 'const-string v0, "SystemWebViewEngine" // string@3')).problems, []);

// Every dex file must have read as classes.
assert.throws(() => check(base, "", [base, "nothing here"]), /dex 2 has no Class descriptor/);
assert.deepEqual(check(base, "", [base, base]).problems, []);

// A plugin calling it fails.
r = check(base + cls("Lcom/x/Plugin;") + method("com.x.Plugin.load", JSI));
assert.deepEqual(r.problems, ["com.x.Plugin.load calls addJavascriptInterface"]);

// Anything that could build the Cordova engine makes its call reachable, and fails.
for (const extra of [
  cls("Lapp/Main;", "Lorg/apache/cordova/CordovaActivity;"),
  cls("Lapp/A;") + method("app.A.go", "new-instance v0, Lorg/apache/cordova/CordovaActivity; // type@2"),
  cls("Lapp/B;") + method("app.B.go", "invoke-static {v1, v0}, Lorg/apache/cordova/CordovaWebViewImpl;.createEngine:(Lx;Ly;)Lz;"),
  cls("Lapp/C;") + method("app.C.go", "invoke-direct {v1}, Lorg/apache/cordova/engine/SystemWebViewEngine;.<init>:(Lx;)V"),
  cls("Lapp/D;") + method("app.D.go", "const-class v0, Lorg/apache/cordova/engine/SystemWebViewEngine; // type@1"),
]) {
  r = check(base + extra);
  assert.equal(r.problems.length, 1, extra);
  assert.match(r.problems[0], /SystemWebViewEngine may be built/);
}
assert.equal(check(base, 'A: android:name="org.apache.cordova.CordovaActivity"').problems.length, 1);

console.log("check-apk-jsi.test: ok");
