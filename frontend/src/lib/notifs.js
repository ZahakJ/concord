// notifs.js — how loud each channel is allowed to be.
//
// This is a DEVICE-LOCAL preference, like the theme and the rail layout: it
// decides what THIS machine does with a message that has already arrived and
// already been decrypted. Nothing here travels, and nothing here can be used to
// stop a message reaching you — there is no server to hold it back.
//
// Three levels, resolved down a chain:
//
//   channel setting  →  server setting  →  "all"
//
// A channel set to null follows its server; a server set to nothing is "all".
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
    if (on && !channels[id]) channels[id] = "none";
  }
  return { ...state, channels };
}

// resolve answers "how loud is this channel", walking channel → server → all.
export function resolve(state, channelId, guildId) {
  return state?.channels?.[channelId] || state?.guilds?.[guildId] || "all";
}

// setChannel pins one channel, or clears it back to following its server.
export function setChannel(state, channelId, level) {
  const channels = { ...state.channels };
  if (isLevel(level)) channels[channelId] = level;
  else delete channels[channelId];
  return { ...state, channels };
}

// setGuild sets a server's default. Clearing a server to "all" also drops any
// channel overrides that merely repeat it, so the stored map doesn't
// accumulate settings that say nothing.
export function setGuild(state, guildId, level, channelIds = []) {
  const guilds = { ...state.guilds };
  if (isLevel(level) && level !== "all") guilds[guildId] = level;
  else delete guilds[guildId];
  const channels = { ...state.channels };
  const eff = isLevel(level) ? level : "all";
  for (const id of channelIds) if (channels[id] === eff) delete channels[id];
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
