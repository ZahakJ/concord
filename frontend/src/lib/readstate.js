// readstate.js — when a channel counts as read, and through what.
//
// THE RULE. A channel is being read only while ALL of these hold:
//
//   1. it is the open channel AND its message list is actually on screen
//      (on a phone: the conversation page, not Home/the channel list parked
//      over it; anywhere: not hidden behind a full-screen takeover);
//   2. the window is visible, and on a desktop also focused — a window behind
//      another one is not being read. Phones skip the focus test: a mobile
//      WebView reports hasFocus() false while you are looking straight at it;
//   3. the feed is scrolled to the newest messages, so a message arriving
//      below someone reading history is not something they have seen.
//
// While a channel is being read, every message in it is read the moment it
// lands. While it is NOT, a message arriving in the open channel counts as
// unread exactly as it would anywhere else (badge, title, inbox), and the
// moment the rule becomes true again (window back, page pushed, scrolled to
// the end) the channel is marked read.
//
// THROUGH WHAT. The read mark is the newest message that was on screen, by
// that message's own timestamp — never this device's "now". A device whose
// clock runs hours fast used to stamp its marks hours in the future, and since
// a mark only ever moves forward that made everything said in the meantime
// read on every device of the account. The core additionally refuses to store a
// mark further ahead of its own clock than readSkew allows (readstate.go), so a
// skewed sender's message can only push a mark that far either.

// isReading answers rule 1–3. Every input is plain data so the rule can be
// tested without a DOM; state.svelte.js supplies the live values.
export function isReading({ open, onScreen, hidden, focused, mobile, atBottom }) {
  if (!open || !onScreen || hidden) return false;
  if (!mobile && !focused) return false;
  return !!atBottom;
}

// readThrough is the mark for a channel whose rows are on screen: the newest
// `sent` among them (ISO string, "" for an empty channel). Every message
// counts, ours included: ours never count as unread, so marking past them
// costs nothing, and stopping short of them would leave a later message from
// someone else that was ALSO seen looking unread.
export function readThrough(messages) {
  let newest = "";
  let newestMs = -Infinity;
  for (const m of messages || []) {
    const t = Date.parse(m?.sent);
    if (Number.isFinite(t) && t > newestMs) {
      newestMs = t;
      newest = m.sent;
    }
  }
  return newest ? new Date(newestMs).toISOString() : "";
}

// attention is what a badge NUMBER means: things aimed at you. In a guild that
// is mentions (and alert words, which are counted as mentions); in a DM or a
// group DM every message is addressed to you, so all of them count. Plain
// chatter in a guild is not a number, it is a quiet dot (see hasUnread).
export function attention(u, isDM) {
  if (!u) return 0;
  return isDM ? u.count || 0 : u.mentions || 0;
}

// addressedToYou: does every message in this place count as aimed at you?
// True for a DM or group DM — unless it was set to "Only @mentions", which
// then means exactly that, as it does in a guild. Muting ("none") is handled
// before this question is ever asked.
export function addressedToYou(isDM, level) {
  return !!isDM && level !== "mentions";
}

export const hasUnread = (u) => !!u && (u.count || 0) > 0;

// badgeText caps a count for a small round badge.
export const badgeText = (n) => (n > 99 ? "99+" : String(n));

// windowTitle: "(N) <where> — Concord". N is attention summed across the app
// (mentions + DM messages) — the title is the only unread signal a minimised
// window has, and someone writing only to you is exactly what it is for.
// <where> names the place the way a person would: "#general" in a guild, the
// person's or group's name in a DM, "Notes" for the self-DM — never the
// channel's internal name ("dm", "notes").
export function windowTitle({ attention: n = 0, channelName = "", guild = null } = {}) {
  let where = "";
  if (guild?.kind === "dm") where = guild.dmNotes ? "Notes" : guild.name || "Direct message";
  else if (guild?.kind === "meeting") where = guild.name || channelName;
  else if (channelName) where = `#${channelName}`;
  return `${n > 0 ? `(${n > 99 ? "99+" : n}) ` : ""}${where ? `${where} — ` : ""}Concord`;
}
