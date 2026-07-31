// ephemeral.svelte.js — disappearing messages. A per-conversation timer (this
// device's choice, kept in localStorage) stamps outgoing messages with an
// absolute expiry token embedded in the content: [eph](concord://eph/v1/<sec>).
// Because the token rides in the (synced, MLS-authenticated) content, EVERY
// device that holds the message knows when it dies and, on a shared ticker,
// asks its own core to erase it (api.expireMessage) — so it vanishes on all
// sides with no extra coordination and no backend TTL/schema. Nothing here is a
// security boundary on its own; it's honouring an expiry the author set.
import { S } from "./state.svelte.js";
import { api } from "./api.js";

export const EPH_RE = /\[eph\]\(concord:\/\/eph\/v1\/(\d{9,12})\)/;

// TTL options offered per conversation (seconds). 0 = off.
export const TTL_OPTIONS = [
  { secs: 0, label: "Off" },
  { secs: 300, label: "5 minutes" },
  { secs: 3600, label: "1 hour" },
  { secs: 28800, label: "8 hours" },
  { secs: 86400, label: "1 day" },
  { secs: 604800, label: "1 week" },
];

export function ttlLabel(secs) {
  return (TTL_OPTIONS.find((o) => o.secs === secs) || { label: `${secs}s` }).label;
}

// Per-channel timer, persisted. Reactive so the composer banner + header button
// update the moment you change it.
const TKEY = "concord.disappear";
let timers = $state(loadTimers());
function loadTimers() {
  try {
    return JSON.parse(localStorage.getItem(TKEY) || "{}") || {};
  } catch {
    return {};
  }
}
export function channelTTL(channelId) {
  return timers[channelId] || 0;
}
export function setChannelTTL(channelId, secs) {
  if (secs) timers[channelId] = secs;
  else delete timers[channelId];
  try {
    localStorage.setItem(TKEY, JSON.stringify(timers));
  } catch {
    /* storage blocked */
  }
}

// Prepend an expiry token to content when the channel has a timer set.
export function stampEphemeral(channelId, content) {
  const ttl = channelTTL(channelId);
  if (!ttl) return content;
  const at = Math.floor(Date.now() / 1000) + ttl;
  return `[eph](concord://eph/v1/${at})${content}`;
}

// parseEphemeral -> expiry epoch (ms) or 0.
export function ephemeralExpiry(content) {
  const m = content?.match(EPH_RE);
  return m ? Number(m[1]) * 1000 : 0;
}
export function stripEphemeral(content) {
  return content ? content.replace(EPH_RE, "") : content;
}

// The sweep: erase any loaded message whose expiry has passed. Runs on a ticker
// and whenever the active channel's messages change (via startEphemeralSweep's
// caller re-invoking sweepNow is unnecessary — the ticker is frequent enough).
const firing = new Set(); // in-flight expiries, so we don't double-call
function sweepNow() {
  const now = Date.now();
  for (const m of S.messages) {
    if (m.deleted || firing.has(m.id)) continue;
    const exp = ephemeralExpiry(m.content);
    if (exp && exp <= now) {
      firing.add(m.id);
      api
        .expireMessage(m.channelId, m.id)
        .catch(() => {})
        .finally(() => firing.delete(m.id));
    }
  }
}

let timer;
export function startEphemeralSweep() {
  sweepNow();
  clearInterval(timer);
  // Paused while hidden: an expiry only matters when someone can see the
  // message, and a 5s timer running in a backgrounded phone app is pure
  // battery. The visibilitychange sweep catches up anything that lapsed.
  timer = setInterval(() => {
    if (!document.hidden) sweepNow();
  }, 5000);
  const onVis = () => {
    if (!document.hidden) sweepNow();
  };
  document.addEventListener("visibilitychange", onVis);
  return () => {
    clearInterval(timer);
    document.removeEventListener("visibilitychange", onVis);
  };
}
