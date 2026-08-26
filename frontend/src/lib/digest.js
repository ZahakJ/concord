// digest.js — the catch-up card, computed from what this device already knows.
//
// Coming back to a busy guild after a night away means opening every channel to
// find out whether any of it was for you. The card answers that in one place: how
// long you were gone, what is unread and where, and — first, because it is the
// only part most people need — the messages that named you or said one of your
// own alert words.
//
// Every input is local: the unread counts the sidebar already keeps, the inbox
// entries the local query already produced, and the guild's own channel list.
// Nothing here asks the network anything, and nothing here is written down.
//
// Pure functions, so the boundary rule — the thing most likely to be subtly
// wrong — is testable without a browser.

// How long "away" has to be before a summary is worth interrupting for. Four
// hours is roughly "you slept, or you had a working day": short enough to catch
// an overnight, long enough that switching guilds during a conversation never
// produces one.
export const CATCH_UP_HOURS = 4;

// The card is a summary, not a second feed. Past this many highlights it stops
// listing and starts counting, because a list of forty things is the problem the
// card exists to solve.
export const MAX_HIGHLIGHTS = 5;

// awayLongEnough is the whole gate. lastSeenMs of 0 — a guild this device has
// never read — is deliberately NOT long enough: an empty history is not an
// absence, and greeting someone's first visit with "you were away for 56 years"
// is the kind of thing that happens when a zero is treated as a date.
export function awayLongEnough(lastSeenMs, nowMs, hours = CATCH_UP_HOURS) {
  if (!lastSeenMs || lastSeenMs <= 0) return false;
  if (!nowMs || nowMs < lastSeenMs) return false;
  return nowMs - lastSeenMs >= hours * 3600_000;
}

// humanAway is the phrase the heading uses. Rounded down and never more precise
// than the unit above it: "2 days" rather than "2 days, 7 hours", because the
// number is scene-setting and the detail is in the rows below.
export function humanAway(ms) {
  const mins = Math.floor(Math.max(0, ms) / 60000);
  if (mins < 60) return `${mins} minute${mins === 1 ? "" : "s"}`;
  const hours = Math.floor(mins / 60);
  if (hours < 48) return `${hours} hour${hours === 1 ? "" : "s"}`;
  const days = Math.floor(hours / 24);
  if (days < 14) return `${days} days`;
  const weeks = Math.floor(days / 7);
  if (weeks < 9) return `${weeks} weeks`;
  const months = Math.floor(days / 30);
  return months < 24 ? `${months} months` : `${Math.floor(days / 365)} years`;
}

// guildLastSeen: when this device last read anything in a guild, taken from the
// read marks the sidebar already keeps. Deriving it rather than storing a second
// "last visited" timestamp means there is only one thing to keep true, and it is
// already kept true by the act of reading.
export function guildLastSeen(guild, lastRead) {
  let newest = 0;
  for (const c of guild?.channels || []) {
    const t = Date.parse(lastRead?.[c.id] || "");
    if (Number.isFinite(t) && t > newest) newest = t;
  }
  return newest;
}

// buildDigest assembles the card. `entries` are inbox entries (any guild); only
// the ones for this guild, newer than the mark, are kept.
//
// A channel with a level of "none" is excluded from both halves: the person
// silenced it, and a card that reports it anyway is the app arguing with a
// setting.
export function buildDigest({ guild, unread, entries, sinceMs, nowMs, muted }) {
  const isMuted = muted || (() => false);
  const channels = [];
  let total = 0;
  let mentions = 0;
  for (const c of guild?.channels || []) {
    if (isMuted(c.id)) continue;
    const u = unread?.[c.id];
    if (!u?.count) continue;
    channels.push({ id: c.id, name: c.name, count: u.count, mentions: u.mentions || 0 });
    total += u.count;
    mentions += u.mentions || 0;
  }
  // Busiest first: the reason to show counts at all is to say where to go, and
  // the alphabet does not know.
  channels.sort((a, b) => b.mentions - a.mentions || b.count - a.count);

  const highlights = (entries || [])
    .filter((e) => e.guildId === guild?.id && e.at > sinceMs && !isMuted(e.channelId))
    .slice(0, MAX_HIGHLIGHTS);

  return {
    guildId: guild?.id || "",
    awayMs: Math.max(0, (nowMs || 0) - (sinceMs || 0)),
    total,
    mentions,
    channels,
    highlights,
    // moreHighlights is what the list could not fit, so the card can say so
    // rather than silently truncating.
    moreHighlights: Math.max(
      0,
      (entries || []).filter((e) => e.guildId === guild?.id && e.at > sinceMs && !isMuted(e.channelId))
        .length - highlights.length,
    ),
  };
}

// worthShowing keeps the card off the screen when it would have nothing to say.
// An empty summary is worse than no summary: it costs a scroll position and
// tells you something you could already see.
export const worthShowing = (d) => !!d && (d.total > 0 || d.highlights.length > 0);
