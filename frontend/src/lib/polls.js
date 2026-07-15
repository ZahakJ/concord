// polls.js — polls piggyback on the message + reaction machinery, so they need
// zero backend changes and sync exactly like any message. A poll message's
// content is a single token carrying the question + options (base64url JSON);
// votes are ordinary reactions with a fixed per-option number emoji, so tallies
// travel as reaction actions and are aggregated on load like everything else.

// Option i is voted by reacting with POLL_EMOJI[i]. Cap: 10 options.
export const POLL_EMOJI = ["1️⃣", "2️⃣", "3️⃣", "4️⃣", "5️⃣", "6️⃣", "7️⃣", "8️⃣", "9️⃣", "🔟"];

export const POLL_RE = /\[poll\]\(concord:\/\/poll\/v1\/([A-Za-z0-9_-]+)\)/;

function b64urlEncode(str) {
  return btoa(unescape(encodeURIComponent(str)))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}
function b64urlDecode(s) {
  try {
    return decodeURIComponent(escape(atob(s.replace(/-/g, "+").replace(/_/g, "/"))));
  } catch {
    return "";
  }
}

// encodePoll({ q, opts, multi }) -> the message content token.
export function encodePoll(poll) {
  const clean = {
    q: String(poll.q || "").slice(0, 300),
    opts: (poll.opts || []).map((o) => String(o).slice(0, 100)).slice(0, POLL_EMOJI.length),
    multi: !!poll.multi,
  };
  return `[poll](concord://poll/v1/${b64urlEncode(JSON.stringify(clean))})`;
}

// parsePoll(content) -> { q, opts, multi } or null if it isn't a poll.
export function parsePoll(content) {
  if (!content) return null;
  const m = content.match(POLL_RE);
  if (!m) return null;
  try {
    const p = JSON.parse(b64urlDecode(m[1]));
    if (!p || typeof p.q !== "string" || !Array.isArray(p.opts) || p.opts.length < 2) return null;
    return {
      q: p.q,
      opts: p.opts.slice(0, POLL_EMOJI.length).map((o) => String(o)),
      multi: !!p.multi,
    };
  } catch {
    return null;
  }
}
