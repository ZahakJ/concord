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
// Both players: the privacy-enhanced domain, a title for the frame, our own
// way out, and nothing loaded until a tap (click-to-play).
const { ytEmbed, ytWatch } = await import("./embeds.js");
assert.match(ytEmbed("dQw4w9WgXcQ"), /^https:\/\/www\.youtube-nocookie\.com\/embed\/dQw4w9WgXcQ\?/, "the player is youtube-nocookie");
assert.equal(ytWatch("dQw4w9WgXcQ"), "https://www.youtube.com/watch?v=dQw4w9WgXcQ");
const card = readFileSync(join(root, "LinkPreview.svelte"), "utf8");
for (const [name, src, idExpr] of [["YouTubeEmbed", yt, "videoId"], ["LinkPreview", card, "video.id"]]) {
  const frames = [...src.matchAll(/<iframe\b[\s\S]*?>/g)].map((m) => m[0]);
  assert.ok(frames.length === 1, `${name}: one player frame`);
  assert.ok(frames[0].includes(`src={ytEmbed(${idExpr})}`), `${name}: the frame's address is rebuilt from the checked id`);
  assert.match(frames[0], /\btitle=\{/, `${name}: the frame has a title for screen readers`);
  assert.ok(new RegExp(`\\{#if [^}]*playing[^}]*\\}[\\s\\S]*?<iframe`).test(src), `${name}: the frame exists only once played`);
  assert.match(src, new RegExp(`<a\\b[^>]*href=\\{ytWatch\\(${idExpr.replace(".", "\\.")}\\)\\}[^>]*target="_blank"[^>]*rel="noopener noreferrer"[^>]*>[\\s\\S]{0,200}?Watch on YouTube`),
    `${name}: Watch on YouTube is a link of ours`);
}
const msg = readFileSync(join(root, "Message.svelte"), "utf8");
for (const m of msg.matchAll(/<YouTubeEmbed\b[^>]*>/g)) assert.match(m[0], /autoload=\{false\}/, "a message never loads Google's thumbnail by itself");
assert.ok(count >= 2, `expected the YouTube and link-card iframes, found ${count}`);
console.log(`iframesandbox: ${count} iframes sandboxed`);
