// Zero-dependency test for the profile-card frame library (`npm test` runs it).
//
// A card frame is data that becomes SVG on every viewer's screen, so the ways
// it can be wrong are all silent — nothing throws, the art just isn't there.
// The four checked here are the ones that actually happened while building it:
//
//   1. The WIRE. The id travels inside a peer's profile style blob and
//      internal/app/service.go bounds it with validID: 1–32 characters of
//      a-z/0-9/'-'. An id outside that is dropped on save, and the user is the
//      one who finds out.
//   2. The PAINTER. `a` names an animation class and `fill`/`stroke` name a
//      colour token; both are strings, and a typo produces a part that is
//      simply never animated or never painted. Every one is checked against
//      what CardFrame.svelte actually defines.
//   3. GRADIENTS. A "@name" fill that has no matching entry in `grads` paints
//      nothing at all — url(#missing) is transparent, not an error.
//   4. VISIBILITY. A `z: "back"` part draws behind the card, so it is only
//      visible where it leaves the card's box. One drawn entirely inside
//      0..272 × 0..400 is invisible by construction; that was the single most
//      common authoring mistake in the decoration library it is modelled on.
import { readFileSync } from "node:fs";
import { CARD_FRAMES, CARD_FRAME_BY_ID, CARD_FRAME_GROUPS, cardFrame } from "./cardframes.js";

let failures = 0;
const fail = (msg) => {
  console.error("FAIL:", msg);
  failures++;
};
const ok = (cond, msg) => {
  if (!cond) fail(msg);
};

// The painter is the authority on which classes and tokens exist.
const painter = readFileSync(new URL("../CardFrame.svelte", import.meta.url), "utf8");
const animClasses = new Set(
  [...painter.matchAll(/^\s{2}\.([a-z][a-z0-9-]*)\s*\{$/gm)].map((m) => m[1]),
);
const TOKENS = new Set(["c1", "c2", "ink", "light", "none"]);
const hex = /^#[0-9a-f]{3,8}$/i;

ok(CARD_FRAMES.length >= 10, `expected at least 10 frames, got ${CARD_FRAMES.length}`);

const seen = new Set();
for (const f of CARD_FRAMES) {
  const where = f.id || "(no id)";

  // 1. the wire
  ok(/^[a-z0-9-]{1,32}$/.test(f.id || ""), `${where}: id must match validID in service.go`);
  ok(!seen.has(f.id), `${where}: duplicate id`);
  seen.add(f.id);
  ok(!!f.name && !!f.group, `${where}: needs a name and a group`);
  ok(Array.isArray(f.parts) && f.parts.length > 0, `${where}: no parts`);

  const gradIds = new Set((f.grads || []).map((g) => g.id));
  let backOutside = 0;
  let backTotal = 0;
  let animated = 0;

  for (const [i, p] of f.parts.entries()) {
    const at = `${where}[${i}]`;
    ok(typeof p.d === "string" && p.d.length > 0, `${at}: empty path data`);
    ok(!/NaN|undefined/.test(p.d), `${at}: path data contains NaN/undefined`);
    ok(p.z === "back" || p.z === "front", `${at}: z must be "back" or "front"`);

    // 2. the painter
    if (p.a) {
      ok(animClasses.has(p.a), `${at}: animation "${p.a}" is not defined in CardFrame.svelte`);
      animated++;
    }
    for (const v of [p.fill, p.stroke]) {
      if (v == null) continue;
      if (v[0] === "@") {
        // 3. gradients
        ok(gradIds.has(v.slice(1)), `${at}: fill "${v}" has no matching gradient`);
      } else {
        ok(TOKENS.has(v) || hex.test(v), `${at}: "${v}" is neither a colour token nor a hex colour`);
      }
    }
    ok(p.stroke == null || p.fill == null || p.fill === "none", `${at}: a part is filled or stroked, not both`);
    if (p.op != null) ok(p.op > 0 && p.op <= 1, `${at}: opacity ${p.op} is outside (0,1]`);

    // 4. visibility
    if (p.z === "back") {
      backTotal++;
      if (leavesTheCard(p.d)) backOutside++;
    }
  }

  ok(animated > 0, `${where}: nothing moves — the library's whole premise is motion`);
  if (backTotal) {
    ok(
      backOutside > 0,
      `${where}: every back part is drawn inside the card's box, so none of them can be seen`,
    );
  }
  for (const g of f.grads || []) {
    ok(/^[a-z0-9-]+$/.test(g.id), `${where}: gradient id "${g.id}" should be plain lowercase`);
    ok(Array.isArray(g.stops) && g.stops.length >= 2, `${where}: gradient "${g.id}" needs stops`);
  }
}

// Lookup fails closed — the property that makes it safe to render an id that
// arrived on someone else's broadcast profile.
ok(cardFrame("") === null, "cardFrame('') should be null");
ok(cardFrame("no-such-frame") === null, "an unknown id must resolve to null");
ok(cardFrame(CARD_FRAMES[0].id) === CARD_FRAMES[0], "a known id must resolve to its frame");
ok(Object.keys(CARD_FRAME_BY_ID).length === CARD_FRAMES.length, "index size mismatch");
ok(
  CARD_FRAME_GROUPS.reduce((n, g) => n + g.ids.length, 0) === CARD_FRAMES.length,
  "every frame must appear in exactly one group",
);
ok(
  !CARD_FRAMES.some((f) => cardFrame(f.id).parts.some((p) => p.d.includes("Infinity"))),
  "path data contains Infinity",
);

/**
 * leavesTheCard: does any coordinate in this path fall outside the card's own
 * box? Crude on purpose — it reads every number out of the path data and asks
 * whether the bounding box of those points escapes 0..272 × 0..400. That is
 * enough to catch a back part drawn entirely under the card.
 */
function leavesTheCard(d) {
  const nums = d.match(/-?\d+(\.\d+)?/g);
  if (!nums) return false;
  for (let i = 0; i + 1 < nums.length; i += 2) {
    const x = +nums[i];
    const y = +nums[i + 1];
    if (x < -1 || x > 273 || y < -1 || y > 401) return true;
  }
  return false;
}

if (failures) {
  console.error(`cardframes.js: ${failures} failure(s)`);
  process.exit(1);
}
console.log(
  `cardframes.js: all passed (${CARD_FRAMES.length} frames, ${CARD_FRAMES.reduce((n, f) => n + f.parts.length, 0)} parts)`,
);
