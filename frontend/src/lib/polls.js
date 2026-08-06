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

// encodePoll({ q, opts, multi, until, answer }) -> the message content token.
// `until` (epoch-seconds close time) and `answer` (correct option index, quiz
// mode) are OPTIONAL and only emitted when set. Forward-compatible by
// construction: parsePoll builds a fresh object from the keys it knows and
// drops the rest, so an old client reads a timed quiz as a plain poll — it
// still renders, still votes, just never freezes or grades.
export function encodePoll(poll) {
  const clean = {
    q: String(poll.q || "").slice(0, 300),
    opts: (poll.opts || []).map((o) => String(o).slice(0, 100)).slice(0, POLL_EMOJI.length),
    multi: !!poll.multi,
  };
  const until = Math.floor(Number(poll.until) || 0);
  if (until > 0) clean.until = until;
  if (Number.isInteger(poll.answer) && poll.answer >= 0 && poll.answer < clean.opts.length)
    clean.answer = poll.answer;
  return `[poll](concord://poll/v1/${b64urlEncode(JSON.stringify(clean))})`;
}

// parsePoll(content) -> { q, opts, multi, until?, answer? } or null if it
// isn't a poll. Unknown keys in the token are ignored, never fatal — that's
// the whole versioning story for v1.
export function parsePoll(content) {
  if (!content) return null;
  const m = content.match(POLL_RE);
  if (!m) return null;
  try {
    const p = JSON.parse(b64urlDecode(m[1]));
    if (!p || typeof p.q !== "string" || !Array.isArray(p.opts) || p.opts.length < 2) return null;
    const out = {
      q: p.q,
      opts: p.opts.slice(0, POLL_EMOJI.length).map((o) => String(o)),
      multi: !!p.multi,
    };
    // until: when the poll freezes. Same sanity window as timestamp.js's seal —
    // a close time from before Concord existed or centuries out is a corrupt
    // (or hostile) token, not a deadline: drop the FIELD, keep the poll.
    const until = Number(p.until);
    if (Number.isFinite(until) && until > 1_600_000_000 && until < 4_100_000_000)
      out.until = Math.floor(until);
    // answer: trivia's correct option. Kept only when it points at a real
    // option after the 10-cap slice, else the reveal would crown a row that
    // doesn't exist.
    if (Number.isInteger(p.answer) && p.answer >= 0 && p.answer < out.opts.length)
      out.answer = p.answer;
    return out;
  } catch {
    return null;
  }
}
