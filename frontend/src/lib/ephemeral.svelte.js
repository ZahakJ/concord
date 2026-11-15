// ephemeral.svelte.js — disappearing messages. A per-conversation timer (this
// device's choice, kept in localStorage) stamps outgoing messages with an
// absolute expiry token embedded in the content: [eph](concord://eph/v1/<sec>).
// Because the token rides in the (synced, MLS-authenticated) content, EVERY
// device that holds the message knows when it dies and, on a shared ticker,
// asks its own core to erase it (api.expireMessage) — so it vanishes on all
// sides with no extra coordination and no backend TTL/schema. Nothing here is a
// security boundary on its own; it's honouring an expiry the author set.
import { S } from "./state.svelte.js";
import { api, setSendStamp } from "./api.js";
import { own } from "./lookup.js";

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

// Per-chat timer: kept by the core as a chat setting, synced between this
// account's own devices (app/chattimer.go), and mirrored here in S.chatPrefs.
// The core stamps it on everything sent into the chat; the stamp below is a
// second line for content built in this window.
export function channelTTL(channelId) {
  return own(S.chatPrefs, channelId)?.t || 0;
}
export async function setChannelTTL(channelId, secs) {
  await api.setChatTimer(channelId, secs);
  S.chatPrefs = (await api.chatPrefs()) || {};
}

// Older builds kept the timers in this window's localStorage. Once the core
// answers, each is handed to it, unless the core already holds a choice for
// that chat (made on another device, or since), and the old copy goes.
const TKEY = "concord.disappear";
async function adoptLocalTimers() {
  let old;
  try {
    old = JSON.parse(localStorage.getItem(TKEY) || "null");
  } catch {
    return;
  }
  if (!old || typeof old !== "object") return;
  try {
    const prefs = (await api.chatPrefs()) || {};
    for (const [ch, secs] of Object.entries(old)) {
      if (own(prefs, ch)?.ta) continue;
      await api.setChatTimer(ch, Number(secs) || 0).catch(() => {});
    }
    localStorage.removeItem(TKEY);
    S.chatPrefs = (await api.chatPrefs()) || {};
  } catch {
    /* core not answering yet: try again next start */
  }
}
$effect.root(() => {
  let done = false;
  $effect(() => {
    if (S.ready && !done) {
      done = true;
      adoptLocalTimers();
    }
  });
});

// Prepend an expiry token to content when the channel has a timer set.
export function stampEphemeral(channelId, content) {
  const ttl = channelTTL(channelId);
  if (!ttl) return content;
  const at = Math.floor(Date.now() / 1000) + ttl;
  return `[eph](concord://eph/v1/${at})${content}`;
}

// Every send in lib/api.js carries the channel's timer, whichever screen made
// it: pictures, files, GIFs, doodles, sounds and polls used to go out bare and
// outlive the words around them. Content already stamped is left as it is.
setSendStamp((channelId, content) => (EPH_RE.test(content || "") ? content : stampEphemeral(channelId, content || "")));

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
