#!/usr/bin/env node
// Who may read Concord's cards (jaybos CONTRACTS.md §1) is a list of signing
// certificates: @array/card_readers, which also feeds the READ_CARDS
// permission's knownCerts. The shared Android debug key proves nothing (eight
// of the owner's apps carry it), so it may appear only in a debug build.
// This fails when any resource outside src/debug names it, when src/main's list
// is empty, or (with --merged) when a release build's merged resources do.
//
//   node scripts/check-card-readers.mjs <android app dir>
//   node scripts/check-card-readers.mjs --merged <android app dir>
import fs from "node:fs";
import path from "node:path";

export const DEBUG_CERT = "5a6ef046fc93fe8d30b032a80188a5455556b3e6ae2d229adca9d5a6fdee8cb8";

function* files(dir) {
  if (!fs.existsSync(dir)) return;
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) yield* files(p);
    else if (e.name.endsWith(".xml")) yield p;
  }
}

export function problems(appDir, { merged = false } = {}) {
  const out = [];
  const src = path.join(appDir, "src");
  for (const f of files(src)) {
    const rel = path.relative(src, f);
    if (rel.split(path.sep)[0] === "debug") continue;
    if (fs.readFileSync(f, "utf8").toLowerCase().includes(DEBUG_CERT)) out.push(`the debug key is listed in src/${rel}`);
  }
  const main = path.join(src, "main/res/values/card_readers.xml");
  const xml = fs.existsSync(main) ? fs.readFileSync(main, "utf8").replace(/<!--[\s\S]*?-->/g, "") : "";
  if (!/<item>\s*[0-9a-fA-F]{64}\s*<\/item>/.test(xml)) out.push("src/main/res/values/card_readers.xml lists no reader");
  if (merged) {
    const dir = path.join(appDir, "build/intermediates");
    let seen = 0;
    for (const f of files(dir)) {
      if (!/release/i.test(f) || !/values/.test(f)) continue;
      seen++;
      if (fs.readFileSync(f, "utf8").toLowerCase().includes(DEBUG_CERT)) out.push(`a release build's resources list the debug key: ${path.relative(appDir, f)}`);
    }
    if (!seen) out.push("no merged release resources under app/build/intermediates");
  }
  return out;
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const merged = process.argv[2] === "--merged";
  const dir = process.argv[merged ? 3 : 2];
  if (!dir) {
    console.error("usage: check-card-readers.mjs [--merged] <android app dir>");
    process.exit(2);
  }
  const p = problems(dir, { merged });
  for (const x of p) console.error(`FAIL: ${x}`);
  if (p.length) process.exit(1);
  console.log("card readers: ok");
}
