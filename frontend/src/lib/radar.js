// radar.js — pure reasoning for the event radar (lib/radar.svelte.js): which
// events just went live somewhere joinable, and which are new/changed since
// the user last opened a calendar. Split from the reactive glue so plain node
// can test the boundaries (grace windows, watermarks, change signatures) —
// same pattern as eventtime.js.

// eventSig fingerprints the parts of an event whose change is worth a nudge:
// WHEN it happens and WHERE. Deliberately excludes RSVPs, title typo-fixes and
// details edits — updatedAt bumps on every RSVP, so watching it would turn
// each "going" click in a busy guild into a false "event changed" badge.
export const eventSig = (ev) =>
  `${ev.startUnix}:${ev.endUnix || 0}:${ev.locationChannelId || ""}:${ev.location || ""}`;

// goneLive: the event is inside its live window AND entered it recently.
// The grace bound (default 10 min) is what keeps a device that was off all
// afternoon from shouting about a meeting that started three hours ago — the
// per-occurrence dedupe alone can't help a device that never saw it fire.
// The end fallback mirrors eventtime.js: open-ended events count for an hour.
export function goneLive(ev, now, grace = 600) {
  const end = ev.endUnix || ev.startUnix + 3600;
  return ev.startUnix <= now && now - ev.startUnix < grace && now < end;
}

// liveDestination resolves an event's location to a REAL channel of its own
// guild — the same receive-side rule EventCard and the backend announcer
// hold: a record naming a foreign or vanished channel is a caption, not a
// door. Free-text/external events return null and never trigger the radar
// (they have their own guest flow).
export function liveDestination(ev, g) {
  if (!ev?.locationChannelId || !g) return null;
  return (g.channels || []).find((c) => c.id === ev.locationChannelId) || null;
}

// unseenCount: how many of this guild's events are news to the user — created
// by someone else since the watermark, or whose time/place moved since the
// calendar was last opened. `entry` is the persisted per-guild watermark
// { ts, sigs: {eventId: sig} }; no entry means "never measured" and counts
// nothing (the reactive layer initializes silently rather than lighting every
// old event at once on a fresh device).
export function unseenCount(events, entry, selfFpr, now) {
  if (!entry) return 0;
  let n = 0;
  for (const ev of events || []) {
    if (ev.createdBy === selfFpr) continue; // your own scheduling is not news
    if ((ev.endUnix || ev.startUnix + 3600) <= now) continue; // over — too late to matter
    const prior = entry.sigs?.[ev.id];
    if (prior === undefined) {
      // New to the watermark. The createdAt guard covers the one hole a pure
      // membership test has: a calendar marked "seen" before its cache loaded
      // would otherwise resurface its whole history as "new".
      if ((ev.createdAt || 0) > entry.ts) n++;
    } else if (prior !== eventSig(ev)) {
      n++; // time or place moved since the user last looked
    }
  }
  return n;
}

// snapshot builds a fresh watermark from the events currently on screen —
// "seen as of now, in this exact shape".
export function snapshot(events, nowUnix) {
  const sigs = {};
  for (const ev of events || []) sigs[ev.id] = eventSig(ev);
  return { ts: nowUnix, sigs };
}

// pruneFired drops occurrence-dedupe markers old enough that their event can
// never re-enter a grace window, so localStorage doesn't accrete one key per
// meeting forever. 7 days comfortably outlives any grace period.
export function pruneFired(map, nowMs, maxAgeMs = 7 * 86400000) {
  const out = {};
  for (const [k, at] of Object.entries(map || {})) {
    if (typeof at === "number" && nowMs - at < maxAgeMs) out[k] = at;
  }
  return out;
}
