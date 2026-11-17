// The gallery's arithmetic (lib/gallery.js): rows and month headings, the
// virtual grid's range, paging, dropping what was deleted or expired, and
// the one-at-a-time fetch queue that never fetches what has left the screen.
import {
  buildRows,
  layout,
  visibleRange,
  needMore,
  mergePage,
  withoutMessage,
  liveItems,
  nextExpiry,
  columnsFor,
  monthKey,
  tileRatio,
  fmtDuration,
  noteDuration,
  durationOf,
  fileSub,
  itemKey,
  FetchQueue,
  PREFETCH_ROWS,
} from "./gallery.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const eq = (a, b, msg) => assert(JSON.stringify(a) === JSON.stringify(b), `${msg}: got ${JSON.stringify(a)}, want ${JSON.stringify(b)}`);

const at = (y, m, d) => new Date(y, m - 1, d, 12).toISOString();
const item = (id, sent, extra = {}) => ({ messageId: id, ord: 0, sent, kind: 1, ...extra });

// ---- rows and month headings ----
{
  const items = [
    item("a", at(2026, 11, 20)),
    item("b", at(2026, 11, 3)),
    item("c", at(2026, 11, 1)),
    item("d", at(2026, 11, 1)),
    item("e", at(2026, 10, 30)),
  ];
  const rows = buildRows(items, 3, new Date(2026, 10, 25));
  eq(rows.map((r) => r.kind), ["head", "tiles", "tiles", "head", "tiles"], "a heading per month, rows of three under it");
  eq(rows[1].items.map((x) => x.messageId), ["a", "b", "c"], "the first row fills to three");
  eq(rows[2].items.map((x) => x.messageId), ["d"], "the month's remainder is its own row");
  eq(rows[4].items.map((x) => x.messageId), ["e"], "a new month starts a new row");
  assert(/November/.test(rows[0].label) && !/2026/.test(rows[0].label), `this year's month has no year: ${rows[0].label}`);
  const old = buildRows([item("z", at(2024, 3, 9))], 3, new Date(2026, 10, 25));
  assert(/2024/.test(old[0].label), `an earlier year's month says the year: ${old[0].label}`);
  // Month bucketing is the reader's local calendar: the last minute of a
  // month and the first of the next are different headings.
  assert(monthKey(new Date(2026, 0, 31, 23, 59).toISOString()) === "2026-01", "late on the 31st is January");
  assert(monthKey(new Date(2026, 1, 1, 0, 1).toISOString()) === "2026-02", "just after midnight is February");
  const keys = new Set(rows.map((r) => r.key));
  assert(keys.size === rows.length, "row keys are unique");
}

// ---- layout and the visible range ----
{
  const rows = [{ kind: "head" }, ...Array.from({ length: 100 }, () => ({ kind: "tiles" }))];
  const lay = layout(rows, 100, 30);
  eq(lay.tops.slice(0, 3), [0, 30, 130], "a heading then fixed-height rows");
  assert(lay.height === 30 + 100 * 100, `height ${lay.height}`);
  eq(visibleRange(lay.tops, lay.height, 0, 300, 0), [0, 4], "the top of the grid");
  eq(visibleRange(lay.tops, lay.height, 1030, 300, 0), [11, 14], "scrolled to the row starting at 1030");
  eq(visibleRange(lay.tops, lay.height, 1030, 300, 300), [8, 17], "a screen of overscan each side");
  const end = visibleRange(lay.tops, lay.height, lay.height - 300, 300, 300);
  assert(end[1] === rows.length, `the bottom reaches the last row: ${end}`);
  eq(visibleRange([], 0, 0, 300, 0), [0, 0], "an empty grid mounts nothing");
  // Mounted at once stays bounded however far the grid goes: the phone's
  // 390x844 at three columns is about 130 px rows.
  const big = layout(Array.from({ length: 1700 }, () => ({ kind: "tiles" })), 131, 34);
  const r = visibleRange(big.tops, big.height, 100000, 700, 700);
  assert((r[1] - r[0]) * 3 <= 90, `tiles mounted at once: ${(r[1] - r[0]) * 3}`);
  assert(needMore([90, 95], 100, "cursor") && !needMore([90, 95], 100, ""), "asks for a page near the end, only when there is one");
  assert(!needMore([10, 20], 100, "cursor"), "not while far from the end");
  assert(needMore([0, 100 - PREFETCH_ROWS], 100, "c"), "the prefetch margin");
}

// ---- columns ----
assert(columnsFor(390, { phone: true }) === 3 && columnsFor(844, { phone: true, landscape: true }) === 5, "phone: 3 upright, 5 on its side");
assert(columnsFor(300) === 3 && columnsFor(500) === 4 && columnsFor(2000) === 6, "desktop: as many ~110 px tiles as fit, 3 to 6");

// ---- paging and freshness ----
{
  const a = [item("a", at(2026, 1, 3)), item("b", at(2026, 1, 2))];
  const merged = mergePage(a, [item("b", at(2026, 1, 2)), item("c", at(2026, 1, 1))]);
  eq(merged.map(itemKey), ["a:0", "b:0", "c:0"], "an overlapping page adds only what is new");
  assert(mergePage(a, []) === a, "an empty page changes nothing");
  const two = [item("m", at(2026, 1, 3)), { ...item("m", at(2026, 1, 3)), ord: 1 }, item("n", at(2026, 1, 2))];
  eq(withoutMessage(two, "m").map(itemKey), ["n:0"], "a deleted message takes all its items");
  assert(withoutMessage(two, "zz") === two, "an unknown message changes nothing");
  const now = 1000;
  const exp = [item("x", at(2026, 1, 3), { expires: 999 }), item("y", at(2026, 1, 2), { expires: 1001 }), item("z", at(2026, 1, 1))];
  eq(liveItems(exp, now).map((x) => x.messageId), ["y", "z"], "an item past its deadline leaves the grid");
  assert(nextExpiry(exp, now) === 1001, "the next deadline is the soonest still ahead");
  assert(nextExpiry([item("z", at(2026, 1, 1))], now) === 0, "no deadline, no timer");
}

// ---- tiles ----
assert(tileRatio({ w: 400, h: 200 }) === 2 && tileRatio({ w: 100, h: 1000 }) === 0.5 && tileRatio({}) === 1, "placeholder ratio from the token, clamped");
assert(fmtDuration(75) === "1:15" && fmtDuration(3725) === "1:02:05" && fmtDuration(0) === "", "video lengths");
assert(durationOf("v1") === 0, "an unplayed video has no length");
noteDuration("v1", 12.4);
noteDuration("v2", NaN);
assert(durationOf("v1") === 12.4 && durationOf("v2") === 0, "a length is known once the clip was loaded");
assert(fileSub({ kind: 3, mime: "audio/webm", name: "v.webm", size: 9 }) === "Voice message", "voice messages are named as such");
assert(fileSub({ kind: 3, mime: "application/pdf", name: "a.pdf", size: 1258291 }) === "PDF · 1.2 MB", "a file's kind and size");

// ---- the fetch queue ----
{
  const calls = [];
  const states = {};
  let release;
  const visible = new Set(["a:0", "b:0", "c:0"]);
  const q = new FetchQueue({
    ensure: (it, allow) => {
      calls.push(`${itemKey(it)}${allow ? "!" : ""}`);
      return new Promise((res, rej) => {
        release = (v) => (v === "fail" ? rej(new Error("not found")) : res(v));
      });
    },
    stillWanted: (it) => visible.has(itemKey(it)),
    onState: (k, s) => (states[k] = s),
  });
  const tick = () => new Promise((r) => setTimeout(r, 0));
  q.push(item("a"));
  q.push(item("b"));
  q.push(item("b"));
  q.push(item("c"));
  await tick();
  eq(calls, ["a:0"], "one fetch at a time, duplicates ignored");
  assert(states["a:0"] === "asking", "the tile says it is asking");
  visible.delete("b:0"); // scrolled away before its turn
  release("fetched");
  await tick();
  await tick();
  eq(calls, ["a:0", "c:0"], "a tile that left the screen is skipped");
  assert(states["a:0"] === "local", "a fetched blob is local");
  q.push(item("b"), true); // tapped: allowed on metered, and first in line
  release("metered");
  await tick();
  await tick();
  assert(states["c:0"] === "metered", "the core's metered answer reaches the tile");
  eq(calls, ["a:0", "c:0", "b:0!"], "a tap is fetched even off screen, with the metered go-ahead");
  release("fail");
  await tick();
  await tick();
  assert(states["b:0"] === "missing", "nobody reachable: the tile says so");
  q.close();
  q.push(item("a"));
  await tick();
  assert(calls.length === 3, "a closed queue fetches nothing");
}

if (failures) {
  console.error(`gallery: ${failures} failure(s)`);
  process.exit(1);
}
console.log("gallery: ok");
