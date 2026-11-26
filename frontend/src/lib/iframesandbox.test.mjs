// Every <iframe> in the app is sandboxed without top navigation (security review): an unsandboxed embed
// could POST with target=_top and load a remote page top-level in the main WebView, past
// shouldOverrideUrlLoading, which never sees a POST.
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import assert from "node:assert/strict";

const root = new URL("..", import.meta.url).pathname;
const files = [];
(function walk(d) {
  for (const n of readdirSync(d)) {
    const p = join(d, n);
    if (statSync(p).isDirectory()) walk(p);
    else if (/\.(svelte|js|mjs|html)$/.test(n) && !n.endsWith(".test.mjs")) files.push(p);
  }
})(root);

// No allow-popups: a popup from the frame (a blob: page, say) could open over the app and pass for its UI.
// "Watch on YouTube" is our own link, outside the frame (below).
const ALLOWED = ["allow-scripts", "allow-same-origin", "allow-presentation"];
let count = 0;
for (const f of files) {
  const src = readFileSync(f, "utf8");
  for (const m of src.matchAll(/<iframe\b[\s\S]*?>/g)) {
    count++;
    const s = m[0].match(/\bsandbox="([^"]*)"/);
    assert.ok(s, `${f}: an <iframe> without sandbox`);
    const tokens = s[1].trim().split(/\s+/);
    for (const t of tokens) assert.ok(ALLOWED.includes(t), `${f}: sandbox token ${t} is not allowed`);
    assert.ok(!/allow-top-navigation|allow-popups|allow-forms|allow-modals/.test(s[1]), `${f}: ${s[1]}`);
  }
  assert.ok(!/createElement\(\s*["']iframe["']\s*\)/.test(src), `${f}: an iframe made in script escapes this check`);
}
const yt = readFileSync(join(root, "YouTubeEmbed.svelte"), "utf8");
assert.match(yt, /<a\b[^>]*href=\{ytWatch\(videoId\)\}[^>]*target="_blank"[^>]*rel="noopener noreferrer"[^>]*>\s*Watch on YouTube/,
  "YouTubeEmbed: Watch on YouTube is a link of ours, outside the iframe");
assert.ok(count >= 2, `expected the YouTube and link-card iframes, found ${count}`);
console.log(`iframesandbox: ${count} iframes sandboxed`);
