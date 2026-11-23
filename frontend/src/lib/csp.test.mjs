import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// The page carries a CSP that allows no inline script and no eval, and holds
// none itself: a script injected into the DOM has nowhere to run from.
const html = readFileSync(new URL("../../index.html", import.meta.url), "utf8");
const m = /http-equiv="Content-Security-Policy"\s+content="([^"]+)"/.exec(html);
assert.ok(m, "index.html has a CSP");
const csp = Object.fromEntries(m[1].split(";").map((d) => d.trim().split(/\s+/)).map(([k, ...v]) => [k, v]));
assert.deepEqual(csp["script-src"], ["'self'"], "scripts only from this origin");
assert.ok(!m[1].includes("unsafe-eval"), "no eval");
assert.deepEqual(csp["object-src"], ["'none'"]);
for (const [, attrs, body] of html.matchAll(/<script([^>]*)>([\s\S]*?)<\/script>/g)) {
  assert.ok(/\ssrc=/.test(attrs) && !body.trim(), `an inline script: <script${attrs}>${body.slice(0, 40)}`);
}
assert.ok(!/\son[a-z]+=/i.test(html.replace(/<!--[\s\S]*?-->/g, "")), "no inline event handlers");
console.log("csp: ok");
