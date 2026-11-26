#!/usr/bin/env node
// Local patches to the installed @capacitor/android, applied after every `npm ci` in apps/mobile (postinstall) and
// again by `make android-app`. Each is an exact replacement: when the expected text is missing and the patch is not
// already in place, this fails, so a Capacitor upgrade can't silently drop a patch.
//
// 1. CapacitorCookies and CapacitorHttp call addJavascriptInterface in load() whatever their config says. A Java
//    object added that way is injected into every frame, the sandboxed YouTube and link-card iframes included, and
//    setCookie(domain, value) would let a third-party frame set cookies for any domain. Concord's frontend uses
//    neither (native-bridge.js checks `typeof … !== 'undefined'` first), so load() skips the call and keeps the rest.
//
//   node scripts/patch-capacitor.mjs [apps/mobile/node_modules]
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const VERSION = "8.5.2";
const MARK = "// concord-patch: no addJavascriptInterface (scripts/patch-capacitor.mjs)";
export const PATCHES = [
  ["plugin/CapacitorCookies.java", 'this.bridge.getWebView().addJavascriptInterface(this, "CapacitorCookiesAndroidInterface");'],
  ["plugin/CapacitorHttp.java", 'this.bridge.getWebView().addJavascriptInterface(this, "CapacitorHttpAndroidInterface");'],
];

export function patch(nodeModules) {
  const pkg = path.join(nodeModules, "@capacitor/android");
  const ver = JSON.parse(fs.readFileSync(path.join(pkg, "package.json"), "utf8")).version;
  if (ver !== VERSION) throw new Error(`@capacitor/android is ${ver}, the patches are for ${VERSION}: review them, then update VERSION`);
  const src = path.join(pkg, "capacitor/src/main/java/com/getcapacitor");
  const done = [];
  for (const [rel, line] of PATCHES) {
    const f = path.join(src, rel);
    const text = fs.readFileSync(f, "utf8");
    if (text.includes(line)) {
      fs.writeFileSync(f, text.replace(line, MARK));
      done.push(`${rel}: patched`);
    } else if (text.includes(MARK)) {
      done.push(`${rel}: already patched`);
    } else {
      throw new Error(`${rel}: neither the expected line nor the patch is there`);
    }
  }
  return done;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const here = path.dirname(fileURLToPath(import.meta.url));
  const nm = process.argv[2] ?? path.join(here, "../apps/mobile/node_modules");
  try {
    for (const d of patch(nm)) console.log(`patch-capacitor: ${d}`);
  } catch (e) {
    console.error(`patch-capacitor: ${e.message}`);
    process.exit(1);
  }
}
