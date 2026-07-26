// announce.js — published announcements, encoded the same way polls are: a
// single token in the message content, so there are zero backend changes and it
// syncs like any other message.
//
// Publishing used to just send a markdown blockquote ("> ↪ published from
// #news"), which rendered as a quotation of somebody else — the visual language
// of "here's a thing someone said", when the intent is the opposite: this IS
// the announcement, arriving in your channel. A token lets the renderer give it
// its own shape, and keeps the original author's name on it instead of the
// republisher's.

export const ANNOUNCE_RE = /\[announcement\]\(concord:\/\/announce\/v1\/([A-Za-z0-9_-]+)\)/;

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

// encodeAnnounce({ from, author, body, note }) -> the message content token.
//   from   — the channel it was published out of
//   author — fingerprint of whoever wrote it originally
//   body   — the original message text
//   note   — an optional line from the publisher, shown above the card
export function encodeAnnounce(a) {
  const clean = {
    from: String(a.from || "").slice(0, 100),
    author: String(a.author || "").slice(0, 200),
    body: String(a.body || "").slice(0, 4000),
    note: String(a.note || "").slice(0, 500),
  };
  return `[announcement](concord://announce/v1/${b64urlEncode(JSON.stringify(clean))})`;
}

// parseAnnounce returns the announcement in a message's content, or null.
export function parseAnnounce(content = "") {
  const m = content.match(ANNOUNCE_RE);
  if (!m) return null;
  try {
    const a = JSON.parse(b64urlDecode(m[1]));
    if (typeof a?.body !== "string") return null;
    return { from: a.from || "", author: a.author || "", body: a.body, note: a.note || "" };
  } catch {
    return null;
  }
}
