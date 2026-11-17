// gallery.js — the pure half of a conversation's Media tab (MediaGallery.svelte).
//
// Items come from the core a page at a time (api.mediaPage: newest first,
// keyed by message id and the item's position in the body). The grid is
// virtual: every row has a fixed height for the column count, month headings
// are rows too, and only the rows near the viewport are mounted. Everything
// here is arithmetic on plain data so it can be tested without a page.

import { fileLabel } from "./attachments.js";

export const KIND_PHOTO = 1;
export const KIND_VIDEO = 2;
export const KIND_FILE = 3;
export const TAB_VISUAL = 1;
export const TAB_FILES = 2;

// The core's page size, and how close to the end of what is loaded the grid
// asks for the next page (in rows).
export const PAGE = 120;
export const PREFETCH_ROWS = 6;

export const itemKey = (it) => `${it.messageId}:${it.ord}`;

// mergePage appends a page to what is loaded, skipping anything already
// there (a page fetched again after a refresh overlaps the one before).
export function mergePage(items, page) {
  if (!page?.length) return items;
  const have = new Set(items.map(itemKey));
  const add = page.filter((it) => !have.has(itemKey(it)));
  return add.length ? items.concat(add) : items;
}

// withoutMessage drops every item of one message: it was deleted, hidden,
// erased or expired. The array is returned unchanged when nothing matched.
export function withoutMessage(items, messageId) {
  const out = items.filter((it) => it.messageId !== messageId);
  return out.length === items.length ? items : out;
}

// liveItems drops items whose disappearing deadline (unix seconds) has
// passed: the core stops listing them, and a grid left open must too.
export function liveItems(items, nowSec) {
  const out = items.filter((it) => !(it.expires > 0 && it.expires <= nowSec));
  return out.length === items.length ? items : out;
}

// nextExpiry is the soonest deadline among the items still live, or 0.
export function nextExpiry(items, nowSec) {
  let at = 0;
  for (const it of items) if (it.expires > nowSec && (!at || it.expires < at)) at = it.expires;
  return at;
}

// monthKey buckets a send time by the reader's own calendar month.
export function monthKey(sent) {
  const d = new Date(sent);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

export function monthLabel(sent, now = new Date()) {
  const d = new Date(sent);
  const opts = d.getFullYear() === now.getFullYear() ? { month: "long" } : { month: "long", year: "numeric" };
  return d.toLocaleDateString(undefined, opts);
}

// columnsFor: three across a phone held upright, five on its side; on a
// desktop panel, as many ~110 px tiles as fit, three to six.
export function columnsFor(width, { phone = false, landscape = false } = {}) {
  if (phone) return landscape ? 5 : 3;
  return Math.max(3, Math.min(6, Math.floor((width || 0) / 110)));
}

// buildRows lays items out as rows: a heading at each new month, then that
// month's items in rows of `cols`. A row is { kind: "head", key, label } or
// { kind: "tiles", key, items }.
export function buildRows(items, cols, now = new Date()) {
  const rows = [];
  let month = "";
  let cur = null;
  for (const it of items) {
    const m = monthKey(it.sent);
    if (m !== month) {
      month = m;
      rows.push({ kind: "head", key: `h:${m}`, label: monthLabel(it.sent, now) });
      cur = null;
    }
    if (!cur || cur.items.length === cols) {
      cur = { kind: "tiles", key: `r:${itemKey(it)}`, items: [] };
      rows.push(cur);
    }
    cur.items.push(it);
  }
  return rows;
}

// layout gives each row its top offset and the grid its height. Heights are
// fixed per row kind, so this is a prefix sum and nothing is measured.
export function layout(rows, tileRowH, headH) {
  const tops = new Array(rows.length);
  let y = 0;
  for (let i = 0; i < rows.length; i++) {
    tops[i] = y;
    y += rows[i].kind === "head" ? headH : tileRowH;
  }
  return { tops, height: y };
}

// visibleRange is the half-open range of rows that intersect
// [scrollTop - overscan, scrollTop + viewH + overscan]: a binary search for
// the first, a walk for the rest.
export function visibleRange(tops, height, scrollTop, viewH, overscan = 0) {
  const n = tops.length;
  if (!n) return [0, 0];
  const lo = Math.max(0, scrollTop - overscan);
  const hi = Math.min(height, scrollTop + viewH + overscan);
  // last row whose top is <= lo
  let a = 0;
  let b = n - 1;
  while (a < b) {
    const mid = (a + b + 1) >> 1;
    if (tops[mid] <= lo) a = mid;
    else b = mid - 1;
  }
  let end = a;
  while (end < n && tops[end] < hi) end++;
  return [a, Math.max(end, a + 1)];
}

// needMore: should the grid ask for the next page? When the last mounted row
// is within PREFETCH_ROWS of the end of what is loaded, and there is a next.
export function needMore(range, rowCount, next) {
  return !!next && range[1] >= rowCount - PREFETCH_ROWS;
}

// tileRatio is the placeholder's aspect ratio from the token's dimensions,
// clamped so a panorama or a receipt still reads as a tile.
export function tileRatio(it) {
  if (!(it.w > 0 && it.h > 0)) return 1;
  return Math.max(0.5, Math.min(2, it.w / it.h));
}

// ---- videos ----------------------------------------------------------------
// No poster travels with a video, so a tile is a film glyph and, once this
// device has played or loaded the clip in this session, its length.
const durations = new Map(); // blobId -> seconds
export function noteDuration(blobId, secs) {
  if (blobId && Number.isFinite(secs) && secs > 0) durations.set(blobId, secs);
}
export const durationOf = (blobId) => durations.get(blobId) || 0;
export function fmtDuration(secs) {
  if (!(secs > 0)) return "";
  const s = Math.round(secs);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const r = String(s % 60).padStart(2, "0");
  return h ? `${h}:${String(m).padStart(2, "0")}:${r}` : `${m}:${r}`;
}

// ---- files -----------------------------------------------------------------
export const isVoice = (it) => it.kind === KIND_FILE && /^audio\//.test(it.mime || "");

// fileSub is a file row's second line: "Voice message", or "PDF · 1.2 MB".
export function fileSub(it) {
  if (isVoice(it)) return "Voice message";
  return fileLabel(it.name, it.size) || "File";
}

// ---- fetching what is not on this device ------------------------------------
// A tile whose blob this device does not hold asks the core to fetch it, only
// while the tile is on screen and one at a time (the feed's rule for a
// picture). On a billed connection the core fetches nothing until the person
// taps the tile. States: "" not asked, "asking", "local", "metered",
// "missing" (nobody reachable has it).

// FetchQueue runs `ensure(item, allowMetered)` for queued items one at a
// time, skipping any that `stillWanted` says have left the screen by the time
// their turn comes. `onState(key, state)` reports each outcome.
export class FetchQueue {
  constructor({ ensure, stillWanted, onState }) {
    this.ensure = ensure;
    this.stillWanted = stillWanted;
    this.onState = onState;
    this.queue = [];
    this.queued = new Set();
    this.busy = false;
    this.closed = false;
  }
  push(item, allowMetered = false) {
    const k = itemKey(item);
    if (this.queued.has(k) || this.closed) return;
    this.queued.add(k);
    // A tap goes to the front: the person is waiting for that one.
    if (allowMetered) this.queue.unshift({ item, allowMetered });
    else this.queue.push({ item, allowMetered });
    this.pump();
  }
  close() {
    this.closed = true;
    this.queue = [];
  }
  async pump() {
    if (this.busy) return;
    this.busy = true;
    try {
      while (this.queue.length && !this.closed) {
        const { item, allowMetered } = this.queue.shift();
        const k = itemKey(item);
        this.queued.delete(k);
        if (!allowMetered && !this.stillWanted(item)) continue;
        this.onState(k, "asking");
        let state;
        try {
          const r = await this.ensure(item, allowMetered);
          state = r === "metered" ? "metered" : "local";
        } catch {
          state = "missing";
        }
        if (!this.closed) this.onState(k, state);
      }
    } finally {
      this.busy = false;
    }
  }
}
