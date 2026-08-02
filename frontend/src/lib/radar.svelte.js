// radar.svelte.js — the event radar: the client-side loudness layer for the
// calendar. The backend's in-channel start announcement is a system message,
// and system messages are unread-exempt app-wide (joins/leaves must not
// badge) — so the one system line that IS time-sensitive was invisible unless
// you already stood in the channel. This module fixes that on the client,
// off state the frontend already has (lib/events.svelte.js):
//
//   OCCURRENCE  a ticking watcher spots events entering their live window in
//               any guild/DM, with a real channel destination, and fires ONCE
//               per occurrence: an in-app banner with a Join button
//               (EventNudges.svelte) + a local OS notification, plus a
//               pulsing LIVE badge on the hosting channel (ChannelList) and
//               its guild (GuildRail).
//
//   SCHEDULING  a per-guild "seen" watermark (localStorage) defines "unseen
//               new/changed events by others"; those badge the rail calendar
//               button, the guild's pill, and the DM row, and toast gently as
//               they arrive. Opening that calendar advances the watermark.
//
// Pure decisions (grace windows, signatures, watermark math) live in radar.js
// so node can test them; this file is the reactive skin: cache refresh, tick,
// dedupe persistence, and the mute/DND manners.
import { S, isMuted, guildNotifLevel, nameFor } from "./state.svelte.js";
import { on } from "./api.js";
import { EV, loadAllEvents } from "./events.svelte.js";
import { happeningNow } from "./eventtime.js";
import { localNotify } from "./scheduled.svelte.js";
import { goneLive, liveDestination, unseenCount, snapshot, pruneFired } from "./radar.js";

const FIRED_KEY = "concord.eventRadar.fired"; // occurrence key -> firedAt ms
const SEEN_KEY = "concord.eventRadar.seen"; // guildId -> { ts, sigs }

export const RADAR = $state({
  // The tick's heartbeat: LIVE badges derive from it, so they appear and
  // clear themselves within one tick of the window's edges.
  now: Math.floor(Date.now() / 1000),
  // In-app banners, capped small: { id, kind: "live"|"scheduled", title,
  // sub, guildId, channelId, voice }.
  nudges: [],
  // guildId -> count of unseen new/changed events (scheduling badges).
  unseen: {},
});

function loadJSON(key) {
  try {
    const v = JSON.parse(localStorage.getItem(key) || "{}");
    return v && typeof v === "object" && !Array.isArray(v) ? v : {};
  } catch {
    return {};
  }
}
function save(key, v) {
  try {
    localStorage.setItem(key, JSON.stringify(v));
  } catch {
    /* storage blocked — dedupe holds in memory for this session */
  }
}

let fired = pruneFired(loadJSON(FIRED_KEY), Date.now());
let seen = loadJSON(SEEN_KEY);

// DND silences every locally-decided ping app-wide (see lib/presence.js);
// the radar obeys the same switch. Badges still light — noise, not blindness.
const dnd = () => S.identity?.presence === "dnd";

// ---------------- in-app banners ----------------
let nextNudge = 1;

export function dismissNudge(id) {
  const i = RADAR.nudges.findIndex((n) => n.id === id);
  if (i >= 0) RADAR.nudges.splice(i, 1);
}

function pushNudge(n, ttl) {
  const id = nextNudge++;
  RADAR.nudges.push({ id, ...n });
  // A stack, not a wall: if a sync burst lands many at once, keep the newest.
  while (RADAR.nudges.length > 3) RADAR.nudges.shift();
  setTimeout(() => dismissNudge(id), ttl); // self-dismissing; ✕ works sooner
}

// ---------------- occurrence: the live-meeting radar ----------------
function scanLive() {
  const now = RADAR.now;
  for (const g of S.guilds) {
    for (const ev of EV.byGuild[g.id] || []) {
      if (!goneLive(ev, now)) continue;
      const ch = liveDestination(ev, g);
      if (!ch) continue; // free-text/external event — its guest flow owns it
      const key = `${g.id}:${ev.id}:${ev.startUnix}`; // startUnix: a reschedule is a new occurrence
      if (fired[key]) continue;
      // Mark BEFORE surfacing: one alert per occurrence, surviving reloads,
      // and never double-fired across the tick and the cache-update trigger.
      fired[key] = Date.now();
      save(FIRED_KEY, fired);
      if (isMuted(ch.id, g.id) || dnd()) continue; // a muted room must not shout
      if (S.activeChannelId === ch.id && !document.hidden) continue; // already in the room
      const isDM = g.kind === "dm";
      // Same rule as EventCard's Join: a guild voice channel enters its call,
      // and a DM's single channel doubles as its call.
      const voice = ch.type === "voice" || (isDM && !g.dmNotes);
      const where = isDM ? g.name : `${voice ? "🔊 " : "#"}${ch.name}`;
      pushNudge(
        { kind: "live", title: ev.title, sub: `is live in ${where}`, guildId: g.id, channelId: ch.id, voice },
        45000,
      );
      localNotify(`🔴 ${ev.title} is live`, `Happening now in ${where} — tap to join`, ch.id, `live:${ev.id}`);
    }
  }
}

// Channels currently hosting a live channel-located event — ChannelList's
// pulsing LIVE badge. Reads RADAR.now, so consumers re-derive on every tick
// and the badge clears itself when the window ends.
export function liveChannelSet() {
  const now = RADAR.now;
  const set = new Set();
  for (const g of S.guilds) {
    for (const ev of EV.byGuild[g.id] || []) {
      if (happeningNow(ev, now) && liveDestination(ev, g)) set.add(ev.locationChannelId);
    }
  }
  return set;
}

// Guilds with a live meeting somewhere inside — GuildRail's subtle cue.
export function guildLiveSet() {
  const now = RADAR.now;
  const set = new Set();
  for (const g of S.guilds) {
    for (const ev of EV.byGuild[g.id] || []) {
      if (happeningNow(ev, now) && liveDestination(ev, g)) {
        set.add(g.id);
        break;
      }
    }
  }
  return set;
}

// ---------------- scheduling: unseen new/changed events ----------------
// In-memory arrival tracking for the gentle toast: `known` holds every event
// id already in the cache, and `primed` flips after the first full load — so
// a reload never re-toasts history, only events that land while running.
const known = new Set();
let primed = false;

function scanNew() {
  for (const g of S.guilds) {
    const evs = EV.byGuild[g.id];
    if (!evs) continue;
    if (!seen[g.id]) {
      // First sighting of this guild's calendar on this device: "since the
      // user last looked" is undefined, so define it as now, silently —
      // otherwise a fresh device lights up for every event ever scheduled.
      seen[g.id] = snapshot(evs, RADAR.now);
      save(SEEN_KEY, seen);
    }
    for (const ev of evs) {
      const k = `${g.id}:${ev.id}`;
      if (known.has(k)) continue;
      known.add(k);
      if (!primed) continue; // startup snapshot, not an arrival
      if (ev.createdBy === S.identity.fingerprint || g.dmNotes) continue; // own plans aren't news
      if ((ev.endUnix || ev.startUnix + 3600) <= RADAR.now) continue; // synced history
      if (guildNotifLevel(g.id) === "none" || dnd()) continue; // quiet guilds stay quiet
      pushNudge(
        {
          kind: "scheduled",
          title: ev.title,
          // In a DM the author IS the conversation — "ML6Y scheduled — ML6Y"
          // reads like a stutter, so say what it is instead.
          sub:
            g.kind === "dm"
              ? `${nameFor(ev.createdBy)} scheduled in your DM`
              : `${nameFor(ev.createdBy)} scheduled — ${g.name}`,
          guildId: g.id,
        },
        12000,
      );
    }
  }
}

// Recompute the per-guild unseen counts from the persisted watermarks.
function recount() {
  const me = S.identity?.fingerprint || "";
  const next = {};
  for (const g of S.guilds) {
    if (g.dmNotes) continue; // Notes is you scheduling at yourself
    const n = unseenCount(EV.byGuild[g.id], seen[g.id], me, RADAR.now);
    if (n) next[g.id] = n;
  }
  // Only touch state when something changed — this runs every tick and must
  // not make every badge consumer re-render for nothing.
  if (JSON.stringify(next) !== JSON.stringify(RADAR.unseen)) RADAR.unseen = next;
}

// markCalendarSeen: the user is looking at this guild's calendar — everything
// on it, in its current shape, is now "seen". App.svelte calls this while a
// calendar modal is open and when it closes.
export function markCalendarSeen(guildId) {
  const evs = EV.byGuild[guildId];
  if (!guildId || !evs) return;
  seen[guildId] = snapshot(evs, Math.floor(Date.now() / 1000));
  save(SEEN_KEY, seen);
  if (RADAR.unseen[guildId]) {
    const next = { ...RADAR.unseen };
    delete next[guildId];
    RADAR.unseen = next;
  }
}

// The blended "Your calendar" shows every source at once, so opening it sees
// everything everywhere.
export function markAllCalendarsSeen() {
  const nowUnix = Math.floor(Date.now() / 1000);
  for (const g of S.guilds) {
    if (EV.byGuild[g.id]) seen[g.id] = snapshot(EV.byGuild[g.id], nowUnix);
  }
  save(SEEN_KEY, seen);
  if (Object.keys(RADAR.unseen).length) RADAR.unseen = {};
}

// ---------------- lifecycle ----------------
let started = false;

export function startEventRadar() {
  if (started) return; // idempotent across re-logins in one page lifetime
  started = true;

  const scan = () => {
    RADAR.now = Math.floor(Date.now() / 1000);
    scanLive();
    scanNew();
    recount();
  };

  // Prime: load every calendar once, swallow the current state as "known",
  // THEN open the gates — so launch never toasts history, but a meeting
  // already live right now still pulls the user in (occurrence dedupe is
  // persistent, so only a genuinely unseen occurrence fires).
  loadAllEvents().then(() => {
    scanNew(); // populates `known` silently (primed is still false)
    primed = true;
    scan();
  });

  // The core emits guild-updated for every calendar change (create/edit/RSVP,
  // local and gossiped) — refresh the cache on it, debounced against bursts.
  let refreshT;
  on("guild-updated", () => {
    clearTimeout(refreshT);
    refreshT = setTimeout(() => loadAllEvents().then(scan), 800);
  });

  // The tick itself is cheap: no network, just arithmetic over the cache.
  setInterval(scan, 20000);
}
