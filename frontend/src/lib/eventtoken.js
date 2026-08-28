// eventtoken.js — the message an event posts into its channel when it is
// created.
//
// Scheduling an event notified nobody. The record replicated, so it did show
// up in every member's calendar — but only if they happened to open the
// calendar, and the create dialog was meanwhile promising that the guild
// "will post a reminder in that channel when it starts". The announcement at
// CREATION is the one that fills the room, and it was the missing half.
//
// Encoded like every other card in this app: ONE token as the whole message
// body, so there is nothing new on the wire and it syncs, pins, searches and
// deletes exactly like any message. The payload is deliberately thin — an id
// and a title — because the event RECORD is already replicated on its own
// lane, and duplicating its start time into a message would be a second copy
// that goes stale the first time the event is edited. The title travels only
// so a card can still say what it is about on a peer whose event lane has not
// caught up yet.
import { b64urlEncode, b64urlDecode } from "./b64url.js";

export const EVENT_RE = /\[event\]\(concord:\/\/event\/v1\/([A-Za-z0-9_-]+)\)/;

export function encodeEventToken({ id, title }) {
  const clean = {
    id: String(id || "").slice(0, 64),
    title: String(title || "").slice(0, 200),
  };
  return `[event](concord://event/v1/${b64urlEncode(JSON.stringify(clean))})`;
}

// parseEventToken returns {id, title} only for a body that is the token and
// NOTHING else. Same rule the other cards hold: a token with prose around it
// stays prose, because the app never produces one and rendering it as a card
// would hide the words beside it.
export function parseEventToken(body) {
  const s = String(body || "").trim();
  const m = EVENT_RE.exec(s);
  if (!m || m[0] !== s) return null;
  try {
    const v = JSON.parse(b64urlDecode(m[1]));
    if (!v || typeof v.id !== "string" || !v.id) return null;
    return { id: v.id, title: typeof v.title === "string" ? v.title : "" };
  } catch {
    return null;
  }
}
