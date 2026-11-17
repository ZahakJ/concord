// Parser parity: the gallery's items come from the core's token parser
// (internal/mediatok), the feed's pictures from lib/attachments.js. Both read
// internal/mediatok/testdata/tokens.json, so a body the feed draws as a
// picture is a gallery item and a body it leaves as text is not.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { ATTACH_RE, FILE_RE, parseAttachTokens, parseFileTokens } from "./attachments.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

const here = path.dirname(fileURLToPath(import.meta.url));
const fx = JSON.parse(fs.readFileSync(path.join(here, "../../../internal/mediatok/testdata/tokens.json"), "utf8"));

// The feed's view of a body, in body order: each token the combined pattern
// finds, read by the parser the feed uses for it.
function feedItems(body) {
  const any = new RegExp(`${ATTACH_RE.source}|${FILE_RE.source}`, "g");
  const out = [];
  let ord = 0;
  for (const m of body.matchAll(any)) {
    const text = m[0];
    const img = parseAttachTokens(text)[0];
    if (img) out.push({ ord, kind: 1, ...img });
    else {
      const f = parseFileTokens(text)[0];
      out.push({ ord, kind: f.mime.startsWith("video/") ? 2 : 3, ...f });
    }
    ord++;
  }
  return out;
}

assert(fx.cases.length >= 10, "the fixture has its cases");
for (const c of fx.cases) {
  // A link card's thumbnail (flag bit 2) is a feed picture but never a gallery
  // item: the fixture names those ordinals in `cards`.
  const want = c.items;
  const got = feedItems(c.body).filter((it) => !c.cards.includes(it.ord));
  assert(got.length === want.length, `${c.name}: feed sees ${got.length} items, fixture ${want.length}`);
  for (let i = 0; i < Math.min(got.length, want.length); i++) {
    const g = got[i];
    const w = want[i];
    const same =
      g.ord === w.ord &&
      g.kind === w.kind &&
      g.blobId === w.blobId &&
      g.keys === fx.keys &&
      (w.kind === 1
        ? g.subtype === w.subtype && g.w === w.w && g.h === w.h && g.spoiler === ((w.flags & 1) !== 0) && g.name === w.name && g.desc === w.desc
        : g.size === w.size && g.mime === w.mime && g.name === w.name);
    assert(same, `${c.name}: item ${i} feed ${JSON.stringify(g)} vs fixture ${JSON.stringify(w)}`);
  }
}

if (failures) {
  console.error(`mediatok: ${failures} failure(s)`);
  process.exit(1);
}
console.log("mediatok: parity with the core parser ok");
