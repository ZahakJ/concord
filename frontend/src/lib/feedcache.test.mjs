// feedcache.test.mjs — the switch cache stays true while you are elsewhere,
// and reconciling with the store keeps every row that did not change.
import assert from "node:assert/strict";
import { createFeedCache, reconcileFeed, sameRow } from "./feedcache.js";
import { compareSent } from "./feedorder.js";

const t = (s) => `2026-11-10T10:00:${String(s).padStart(2, "0")}.000000000Z`;
const msg = (id, s, extra = {}) => ({ id, channelId: "c1", sent: t(s), content: id, reactions: [], ...extra });
const ids = (l) => l.map((m) => m.id).join(",");

// Bounded by channel count (least recently used goes) and by rows per channel.
{
  const c = createFeedCache({ max: 2, rows: 3 });
  c.put("a", [msg("1", 1), msg("2", 2), msg("3", 3), msg("4", 4)]);
  assert.equal(ids(c.get("a")), "2,3,4");
  c.put("b", [msg("x", 1)]);
  c.get("a"); // a is now the most recent
  c.put("c", [msg("y", 1)]);
  assert.equal(c.has("b"), false);
  assert.equal(c.has("a"), true);
  // An empty channel is not worth a slot (and must not shadow a fetch).
  c.put("a", []);
  assert.equal(c.has("a"), false);
}

// Events reach a cached channel the way they reach the open feed: a new row is
// placed by sent, an edit or a delete's tombstone replaces the row in place, so
// a deleted message is never painted from the cache.
{
  const c = createFeedCache();
  c.put("c1", [msg("1", 1), msg("3", 3)]);
  c.apply(msg("2", 2));
  assert.equal(ids(c.get("c1")), "1,2,3");
  c.apply(msg("3", 3, { deleted: true, content: "" }));
  const after = c.get("c1");
  assert.equal(after[2].deleted, true);
  assert.equal(after[2].content, "");
  // Events for channels that are not cached are ignored, not cached.
  c.apply({ ...msg("9", 9), channelId: "elsewhere" });
  assert.equal(c.has("elsewhere"), false);
}

// The cache keeps its own array: the feed growing afterwards does not leak in.
{
  const c = createFeedCache();
  const live = [msg("1", 1)];
  c.put("c1", live);
  live.push(msg("2", 2));
  assert.equal(ids(c.get("c1")), "1");
}

// sameRow compares what renders, nested values included.
assert.equal(sameRow(msg("1", 1), msg("1", 1)), true);
assert.equal(sameRow(msg("1", 1), msg("1", 1, { edited: true })), false);
assert.equal(sameRow(msg("1", 1, { reactions: [{ e: "👍", n: 1 }] }), msg("1", 1, { reactions: [{ e: "👍", n: 2 }] })), false);

// Reconcile: nothing changed -> the same array back (no write, no re-render).
{
  const cur = [msg("1", 1), msg("2", 2)];
  assert.equal(reconcileFeed(cur, [msg("1", 1), msg("2", 2)], compareSent), cur);
}

// Reconcile: unchanged rows keep their objects, changed ones take the store's,
// rows the store no longer returns go (older ones included: a retention sweep
// or a purge must not survive in the cache), and a row that arrived live after
// the page was read stays.
{
  const old0 = msg("0", 0);
  const a = msg("1", 1);
  const b = msg("2", 2);
  const gone = msg("g", 3);
  const live = msg("9", 9);
  const cur = [old0, a, b, gone, live];
  const fresh = [msg("1", 1), msg("2", 2, { edited: true }), msg("4", 4)];
  const out = reconcileFeed(cur, fresh, compareSent, new Set(cur.map((m) => m.id)));
  assert.equal(ids(out), "1,2,4,9");
  assert.equal(out[0], a);
  assert.notEqual(out[1], b);
  assert.equal(out[1].edited, true);
  assert.equal(out[3], live);
}

// History paged in from the store while the fetch was in flight (scrolling up,
// a jump to an old message) is not the cache's and stays; the cached row that
// the store no longer returns still goes.
{
  const pagedIn = [msg("p1", 1), msg("p2", 2)];
  const cachedOld = msg("c", 3);
  const a = msg("1", 5);
  const cur = [...pagedIn, cachedOld, a];
  const out = reconcileFeed(cur, [msg("1", 5)], compareSent, new Set(["c", "1"]));
  assert.equal(ids(out), "p1,p2,1");
}

// An empty page from the store empties the feed.
assert.deepEqual(reconcileFeed([msg("1", 1)], [], compareSent), []);

console.log("feedcache ok");
