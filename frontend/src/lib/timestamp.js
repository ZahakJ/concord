// timestamp.js — a sealed timestamp a message carries with it.
//
// Ordinary message times are a rendering detail: they come from `sent`, they are
// formatted to the reader's locale and clock preference, and on a grouped run of
// messages they are hidden behind a hover — which is to say, unreachable on a
// phone. That is fine for chat and useless the moment a time is the POINT of the
// message: "I'm leaving now", a handover, a bet, the moment something happened.
//
// A sealed timestamp is the author's deliberate mark. It rides in the content, so
// it is covered by the same MLS authentication as the words and cannot drift or
// be re-derived by a reader whose clock disagrees. It renders as a chip that says
// the time out loud instead of hiding it behind a pointer.
//
// The token is shaped like the disappearing-message one (ephemeral.svelte.js) —
// a markdown link the renderer strips — because that shape is already proven to
// survive the round trip through the store and the wire.
export const TS_RE = /\[ts\]\(concord:\/\/ts\/v1\/(\d{9,12})\)/;

// stampTimestamp seals the current moment into the content. Epoch SECONDS, to
// match the ephemeral token and to keep the marker short — a message is not
// timed to the millisecond and pretending otherwise would be false precision.
export function stampTimestamp(content, nowMs = Date.now()) {
  if (!content) return content;
  if (TS_RE.test(content)) return content; // already sealed; never stack them
  return `[ts](concord://ts/v1/${Math.floor(nowMs / 1000)})${content}`;
}

// sealedAt -> epoch ms, or 0 when the message carries no seal.
export function sealedAt(content) {
  const m = content?.match(TS_RE);
  if (!m) return 0;
  const secs = Number(m[1]);
  // A seal from the far future or before Concord existed is a corrupt or
  // hostile token, not a time. Ignore it rather than rendering nonsense.
  if (!Number.isFinite(secs) || secs < 1_600_000_000 || secs > 4_100_000_000) return 0;
  return secs * 1000;
}

export function stripTimestamp(content) {
  return content ? content.replace(TS_RE, "") : content;
}

// A short label for the chip itself: the time only, because the chip sits beside
// a message whose day is already established by the feed's date divider.
export function sealShort(ms, clock = {}) {
  try {
    return new Date(ms).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", ...clock });
  } catch {
    return "";
  }
}

// The full reading, for the hover card / tap sheet: weekday, date, and seconds,
// since a sealed time is one someone may need to quote exactly.
export function sealFull(ms, clock = {}) {
  try {
    return new Date(ms).toLocaleString([], {
      weekday: "short",
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      ...clock,
    });
  } catch {
    return "";
  }
}

// How long ago, in words. Recomputed by the caller on a ticker so a fresh seal
// counts up while you watch it.
export function sealAgo(ms, nowMs = Date.now()) {
  const s = Math.max(0, Math.round((nowMs - ms) / 1000));
  if (s < 10) return "just now";
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.round(h / 24);
  return d === 1 ? "yesterday" : `${d}d ago`;
}
