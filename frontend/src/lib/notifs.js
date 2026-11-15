// notifs.js — how loud each channel is allowed to be.
//
// This is a DEVICE-LOCAL preference, like the theme and the rail layout: it
// decides what THIS machine does with a message that has already arrived and
// already been decrypted. Nothing here travels, and nothing here can be used to
// stop a message reaching you — there is no server to hold it back.
//
// Three levels, resolved down a chain:
//
//   channel setting  →  guild setting  →  "all"
//
// A channel set to null follows its guild; a guild set to nothing is "all".
// The levels are deliberately few, because the honest questions are only three:
//
//   all       everything pings
//   mentions  only @you pings — the channel still reads as unread
//   none      silent, greyed, no badge at all
//
// "none" is exactly the old per-channel mute, which is what makes migrating a
// saved mute list to this model lossless (see migrateMutes).
//
// Every function is pure: state in, new state out, so the reactive layer stays
// a thin wrapper and this is testable on its own.

import { own } from "./lookup.js";

export const LEVELS = [
  { id: "all", label: "All messages", sub: "Every message pings" },
  { id: "mentions", label: "Only @mentions", sub: "Still shows unread, but stays quiet" },
  { id: "none", label: "Nothing", sub: "Silent and greyed out, no badge" },
];

const IDS = new Set(LEVELS.map((l) => l.id));
export const isLevel = (v) => IDS.has(v);
export const levelLabel = (id) => LEVELS.find((l) => l.id === id)?.label || "";

export const EMPTY = { channels: {}, guilds: {} };

// normalize accepts whatever localStorage held — including nothing, or a shape
// from a build that predates this file — and returns something safe to read.
export function normalize(raw) {
  const clean = (obj) => {
    const out = {};
    for (const [k, v] of Object.entries(obj || {})) if (isLevel(v)) out[k] = v;
    return out;
  };
  return { channels: clean(raw?.channels), guilds: clean(raw?.guilds) };
}

// migrateMutes folds the old `concord.mutes` map (channelId -> true) into the
// level model. Muted meant "no pings, no badge, greyed", which is "none"
// exactly — so an upgrading user's channels stay how they left them. Existing
// levels win, so this is safe to run every load, not just once.
export function migrateMutes(state, mutes) {
  const channels = { ...state.channels };
  for (const [id, on] of Object.entries(mutes || {})) {
    if (on && !own(channels, id)) channels[id] = "none";
  }
  return { ...state, channels };
}

// resolve answers "how loud is this channel", walking channel → guild → all.
export function resolve(state, channelId, guildId) {
  return own(state?.channels, channelId) || own(state?.guilds, guildId) || "all";
}

// setChannel pins one channel, or clears it back to following its guild.
export function setChannel(state, channelId, level) {
  const channels = { ...state.channels };
  if (isLevel(level)) channels[channelId] = level;
  else delete channels[channelId];
  return { ...state, channels };
}

// setGuild sets a guild's default. Clearing a guild to "all" also drops any
// channel overrides that merely repeat it, so the stored map doesn't
// accumulate settings that say nothing.
export function setGuild(state, guildId, level, channelIds = []) {
  const guilds = { ...state.guilds };
  if (isLevel(level) && level !== "all") guilds[guildId] = level;
  else delete guilds[guildId];
  const channels = { ...state.channels };
  const eff = isLevel(level) ? level : "all";
  for (const id of channelIds) if (own(channels, id) === eff) delete channels[id];
  return { channels, guilds };
}

// ---- what a level actually does ----

// wantsAlert: should this message make a sound or raise an OS notification?
// dnd is the user's own presence: Do Not Disturb has to mean something, and
// the only honest meaning is that nothing gets through while it's on.
export function wantsAlert(level, { mention = false, dnd = false } = {}) {
  if (dnd || level === "none") return false;
  return level === "all" || mention;
}

// showsBadge: does an unread message at this level light the sidebar? "none"
// is the only level that hides — DND is about noise, not about losing track of
// where you were, so it deliberately does NOT suppress badges.
export const showsBadge = (level) => level !== "none";

// ---- when an allowed alert becomes an OS notification ----

// shouldNotify is the last gate, after wantsAlert has said the message deserves
// attention at all: would the person see it anyway? One rule for every kind of
// message, with `direct` meaning aimed at you — an @mention, an alert word, or
// any message in a DM or group DM:
//
//   window hidden (minimised, another tab, phone screen elsewhere) → notify;
//   visible but not focused (behind another window)               → only direct;
//   focused                                                        → never; the
//     app's own sound and badges are already in front of them.
//
// A DM used to be held to the plain-message rule here while the chime treated
// it as a mention, so a DM in an unfocused window made a sound and no
// notification. Phones report hasFocus() false while you look at them, so
// `mobile` treats a visible phone as focused.
export function shouldNotify({ hidden, focused, mobile = false, direct = false }) {
  if (hidden) return true;
  if (mobile || focused) return false;
  return !!direct;
}

// ---- which sound a received message makes ----

// chimeFor names the in-app sound for a message wantsAlert has already allowed,
// or returns null for none:
//
//   in another channel or DM, or while the app is in the background: a sound,
//     "dm" for a direct message, "mention" for a mention or alert word,
//     "message" for anything else;
//   in the channel on screen: none. "Blip on every message" is a separate,
//     opt-in sound for that case.
//
// A plain message used to make no sound anywhere, even in a channel set to
// "All messages", so on a phone a busy guild stayed silent unless you were named.
//
// `background`: the window is hidden, or on a desktop not focused. A phone
// reports hasFocus() false while you look at it, so a phone caller passes
// document.hidden alone. `trayCarries`: an Android tray notification is being
// posted for this message, and the tray's channel plays the system's sound, so
// a sound from the app as well would be the same news twice.
export function chimeFor({ alert, dm = false, mention = false, onScreen = false, background = false, trayCarries = false }) {
  if (!alert) return null;
  if (onScreen && !background) return null;
  if (background && trayCarries) return null;
  return dm ? "dm" : mention ? "mention" : "message";
}

// nativePolicy is what the Android side is told before the page goes to the
// background. It posts direct messages on its own (NativeNotifier) when no
// page is running to decide, and it cannot read the levels, so it gets the
// decision instead: Do Not Disturb, and the DM channels it must leave alone.
// A DM at "Nothing" never alerts. One at "Only @mentions" alerts for a mention,
// which only the page can recognise, so the native side leaves those to the
// page as well. `dms` is [{ channelId, guildId }].
export function nativePolicy({ notifs, dms = [], dnd = false }) {
  const quiet = dms.filter(({ channelId, guildId }) => resolve(notifs, channelId, guildId) !== "all").map((d) => d.channelId);
  return { dnd: !!dnd, quiet };
}

// chimeGate decides whether a sound chimeFor named actually plays, and records
// it in `gate` ({ at, rank, byChannel }, start it as {}):
//
//   • a burst is one sound: within CHIME_GAP_MS of the last sound, only a
//     louder kind gets through (dm > mention > message), so a DM or a mention
//     arriving just after chatter is never swallowed by the quieter note;
//   • chatter is not a metronome: a plain message sounds at most once per
//     channel per CHATTER_GAP_MS, so a busy channel at "All messages" gives
//     one note when it wakes up rather than one every second and a half.
export const CHIME_GAP_MS = 1500;
export const CHATTER_GAP_MS = 10000;
const RANK = { message: 1, mention: 2, dm: 3 };
export function chimeGate(gate, kind, channelId, now) {
  const rank = RANK[kind];
  if (!rank) return false;
  gate.byChannel ||= {};
  if (kind === "message" && now - (gate.byChannel[channelId] ?? -Infinity) < CHATTER_GAP_MS) return false;
  if (now - (gate.at ?? -Infinity) < CHIME_GAP_MS && rank <= (gate.rank || 0)) return false;
  gate.at = now;
  gate.rank = rank;
  if (kind === "message") gate.byChannel[channelId] = now;
  return true;
}

// notificationTitle says who AND where, because the OS shows nothing else:
// "Ada" alone was the same for a DM and for chatter in a guild.
//   DM:        "Ada (direct message)"
//   group DM:  "Ada in Book club"
//   guild:     "Ada in #general · Dee Guild"
// An @mention keeps its "@ " lead.
export function notificationTitle({ sender, mention = false, kind = "", place = "", channel = "" }) {
  const who = sender || "Someone";
  let where = "";
  if (kind === "dm") where = " (direct message)";
  else if (kind === "group") where = place ? ` in ${place}` : " (group)";
  else if (channel) where = ` in #${channel}${place ? ` · ${place}` : ""}`;
  return `${mention ? "@ " : ""}${who}${where}`;
}
