// feedcache.js — the last few channels' rows, kept so a switch paints at once.
//
// A channel switch used to empty the feed and wait for the core before it drew
// a single row: one round trip, a sqlite page and a JSON decode between the
// click and anything to read, every time, including going straight back to the
// channel you were in a second ago. The rows of the channels you were just in
// are kept here; a switch to one of them shows those rows in the same frame,
// and the fetch that always follows reconciles them with the store.
//
// The cache is kept true while you are elsewhere rather than thrown away: every
// message event the core emits for a cached channel (a new row, an edit, a
// reaction, a delete's tombstone) is applied to it exactly as it would have
// been applied to the open feed. So a deleted message is never shown again from
// here, even for the frame before the fetch answers. Anything the events do not
// carry (a block, which filters on the way out of the core) clears the cache.

import { insertBySent } from "./feedorder.js";

export function createFeedCache({ max = 12, rows = 200 } = {}) {
  const map = new Map(); // insertion order = recency (oldest first)
  return {
    put(id, list) {
      if (!id) return;
      map.delete(id);
      if (!list?.length) return;
      map.set(id, list.length > rows ? list.slice(-rows) : list.slice());
      while (map.size > max) map.delete(map.keys().next().value);
    },
    get(id) {
      const v = map.get(id);
      if (!v) return null;
      map.delete(id);
      map.set(id, v);
      return v;
    },
    has: (id) => map.has(id),
    // apply mirrors the open feed's handling of a message event.
    apply(m) {
      const list = m && map.get(m.channelId);
      if (!list) return;
      const i = list.findIndex((x) => x.id === m.id);
      let next;
      if (i >= 0) {
        next = list.slice();
        next[i] = m;
      } else next = insertBySent(list, m);
      map.set(m.channelId, next.length > rows ? next.slice(-rows) : next);
    },
    drop: (id) => map.delete(id),
    clear: () => map.clear(),
    get size() {
      return map.size;
    },
  };
}

// sameRow: would these two views of one message render identically? Shallow
// over the fresh row's own keys, structural for the nested ones (reactions,
// attachments). A key the old row lacks counts as a change.
export function sameRow(a, b) {
  if (a === b) return true;
  if (!a || !b) return false;
  for (const k of Object.keys(b)) {
    const x = a[k];
    const y = b[k];
    if (x === y) continue;
    if (x && y && typeof x === "object" && typeof y === "object") {
      if (JSON.stringify(x) !== JSON.stringify(y)) return false;
    } else return false;
  }
  return true;
}

// reconcileFeed folds the store's first page (`fresh`, oldest first, the
// newest `rows` messages) into what the feed is already showing (`cur`).
//  - inside fresh's span the store wins: rows it no longer returns go, rows it
//    returns differently are replaced;
//  - an unchanged row keeps its existing object, so the feed does not re-render
//    it;
//  - rows newer than the page (arrived live while the fetch was in flight)
//    are kept;
//  - rows older than the page are kept only if they were paged in from the
//    store while the fetch was in flight (scrolling up, a jump to an old
//    message): anything older that came from the cache (`cachedIds`) and the
//    store no longer returns (a retention sweep, a purge) must not survive on
//    the strength of having been cached.
// Returns `cur` itself when nothing changed, so the caller can skip the write.
export function reconcileFeed(cur, fresh, cmp, cachedIds = null) {
  if (!fresh.length) return cur.length ? [] : cur;
  const first = fresh[0];
  const last = fresh[fresh.length - 1];
  const byId = new Map(cur.map((m) => [m.id, m]));
  const freshIds = new Set(fresh.map((m) => m.id));
  const older = cur.filter(
    (m) => !freshIds.has(m.id) && cmp(m, first) < 0 && !(cachedIds && cachedIds.has(m.id)),
  );
  const newer = cur.filter((m) => !freshIds.has(m.id) && cmp(m, last) > 0);
  const mid = fresh.map((m) => {
    const old = byId.get(m.id);
    return old && sameRow(old, m) ? old : m;
  });
  const out = [...older, ...mid, ...newer];
  if (out.length === cur.length && out.every((m, i) => m === cur[i])) return cur;
  return out;
}
