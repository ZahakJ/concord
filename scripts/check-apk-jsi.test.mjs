// check-apk-jsi on dexdump-shaped fixtures (node scripts/check-apk-jsi.test.mjs).
import assert from "node:assert/strict";
import { check } from "./check-apk-jsi.mjs";

const cls = (c, s = "Ljava/lang/Object;") => `  Class descriptor  : '${c}'\n  Superclass        : '${s}'\n`;
const method = (m, ...body) => `0001:                                        |[000001] ${m}:()V\n` + body.map((b, i) => `0002: 0000                                   |${String(i).padStart(4, "0")}: ${b}\n`).join("");
const JSI = "invoke-virtual {v0, v1, v2}, Landroid/webkit/WebView;.addJavascriptInterface:(Ljava/lang/Object;Ljava/lang/String;)V";
const base = [
  cls("Lcom/getcapacitor/MessageHandler;"), method("com.getcapacitor.MessageHandler.<init>", JSI),
  cls("Lorg/apache/cordova/engine/SystemWebViewEngine;"), method("org.apache.cordova.engine.SystemWebViewEngine.exposeJsInterface", JSI),
  method("org.apache.cordova.engine.SystemWebViewEngine.<init>", "invoke-direct {v1}, Lorg/apache/cordova/engine/SystemWebViewEngine;.<init>:(Lx;)V"),
  cls("Lorg/apache/cordova/CordovaWebViewImpl;"), method("org.apache.cordova.CordovaWebViewImpl.createEngine", "const-class v0, Lorg/apache/cordova/engine/SystemWebViewEngine; // type@1"),
  cls("Lorg/apache/cordova/CordovaActivity;", "Landroid/app/Activity;"), method("org.apache.cordova.CordovaActivity.makeWebViewEngine", "invoke-static {v1, v0}, Lorg/apache/cordova/CordovaWebViewImpl;.createEngine:(Lx;Ly;)Lz;"),
  cls("Lorg/chromium/support_lib_boundary/WebViewBuilderBoundaryInterface$Config;"),
  "      name          : 'addJavascriptInterface'\n",
].join("");

let r = check(base);
assert.deepEqual(r.problems, [], r.problems.join("\n"));
assert.equal(r.callers.length, 2);

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
