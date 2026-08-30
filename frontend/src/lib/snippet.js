// snippet.js — one message, one line, no source.
//
// Three surfaces quote a message somewhere that will never render it: the
// reply strip above a reply, a search result, and the activity inbox. All
// three used to print whatever the body happened to contain, which in a chat
// app that carries polls, games, doodles, sound recipes and attachments as
// bracketed tokens means printing base64 at people:
//
//   ↩ Amina Sadiq  The fold is the whole trick — here it is in eleven lines:
//   ```js export function
//   ![image](concord://attach/v2/7ff3486b8d32b9edd1a3f4cea4231ae0fd19…
//
// The rule is: a body that IS a card is named, block content becomes a token,
// and everything else is prose with its formatting flattened. Never the
// source.
//
// This is the client half. `internal/app/snippet.go` is the same rule in Go,
// for the inbox, whose snippets are cut before they reach the browser — the
// two files carry the same table of token names deliberately, the way the
// alert-word matcher does, because a name that drifts is a row that says
// something different depending on which half wrote it.

import { previewText } from "./attachments.js";
import { stripMarkdown } from "./forum.js";
import { parsePoll, POLL_RE } from "./polls.js";
import { parseAnnounce, ANNOUNCE_RE } from "./announce.js";
import { parseEventToken, EVENT_RE } from "./eventtoken.js";
import { GAME_RE } from "./games.js";
import { DOODLE_RE } from "./doodle.js";
import { SFX_RE } from "./sfxrecipe.js";

// A fence, closed or left open by a truncated excerpt.
const FENCE_RE = /```[\s\S]*?(?:```|$)/g;

// The attachment grammar, loosely. attachments.js parses tokens STRICTLY — a
// 64-character hex id, a 75-character key — which is right for something that
// is about to fetch bytes and wrong for something that is about to print. A
// token cut in half by an excerpt, or written by a build that added a field,
// fails the strict parse and must still never reach a reader as a URI.
const LOOSE_IMG_RE = /!\[image\]\(concord:\/\/attach\/v[12]\/[^)\s]*\)/g;
const LOOSE_FILE_RE = /\[file\]\(concord:\/\/file\/v1\/[^)\s]*\)/g;

// Decorations that ride in FRONT of ordinary words — a send effect, a
// disappearing timer, a sealed timestamp. Written out here rather than
// composed from stripFx/stripEphemeral/stripTimestamp so this module stays
// importable outside a Svelte build (ephemeral.svelte.js opens with a $state),
// and so it reads as the one line its Go counterpart also is.
const RIDER_RE = /\[(?:fx|eph|ts)\]\(concord:\/\/(?:fx|eph|ts)\/v1\/[A-Za-z0-9_-]+\)/g;

// The names a quoted card answers to. Kept short: this is a label inside a
// one-line strip, not a description.
export const TOKEN_LABELS = {
  game: "🎲 game",
  doodle: "🖌 doodle",
  sound: "🔊 sound",
  code: "📄 code block",
};

// plainSnippet: the readable one-line form of a message body.
// `max` of 0 means "do not cut".
export function plainSnippet(content, max = 0) {
  let c = String(content ?? "");
  if (!c) return "";
  // Decorations that ride in FRONT of ordinary words — a send effect, a
  // disappearing timer, a sealed timestamp. They are never the message.
  c = c.replace(RIDER_RE, " ");

  // A body that is a card gets named rather than quoted. Poll and announcement
  // carry a title worth showing; the rest are their own name. Both parses can
  // fail on a payload the excerpt cut in half, which is exactly when it matters
  // that the fallback is a label and not the base64 that is left.
  const poll = parsePoll(c);
  if (poll) return cap(`📊 ${poll.q || "Poll"}`, max);
  if (POLL_RE.test(c)) return "📊 Poll";
  const ann = parseAnnounce(c);
  if (ann) return cap(`📣 ${stripMarkdown(ann.body).replace(/\s+/g, " ").trim() || "Announcement"}`, max);
  if (ANNOUNCE_RE.test(c)) return "📣 Announcement";
  const evt = parseEventToken(c);
  if (evt) return cap(`📅 ${evt.title || "Event"}`, max);
  if (EVENT_RE.test(c)) return "📅 Event";
  if (GAME_RE.test(c)) return cap(TOKEN_LABELS.game, max);
  if (DOODLE_RE.test(c)) return cap(TOKEN_LABELS.doodle, max);
  if (SFX_RE.test(c)) return cap(TOKEN_LABELS.sound, max);
  const invite = inviteSnippet(c);
  if (invite) return cap(invite, max);

  // Block content inside prose becomes a token too. A fenced block flattened
  // into one line is eleven lines of somebody's code with the newlines taken
  // out, which is not a preview of anything.
  c = c.replace(FENCE_RE, ` ${TOKEN_LABELS.code} `);

  // Attachments to a placeholder, then inline formatting to the words inside
  // it, then whitespace to single spaces.
  const shown = previewText(c)
    .replace(LOOSE_IMG_RE, " 🖼 image ")
    .replace(LOOSE_FILE_RE, " 📎 file ");
  const text = stripMarkdown(shown).replace(/\s+/g, " ").trim();
  return cap(text, max);
}

function inviteSnippet(content) {
  const raw = String(content ?? "").trim();
  if (!raw.startsWith("{") || !raw.includes('"op"')) return "";
  try {
    const n = JSON.parse(raw);
    if (!n?.op) return "";
    const name = String(n.guild || n.what || "invite").trim() || "invite";
    if (n.op === "joined") return `joined ${name}`;
    if (n.op === "offered") return `invite to ${name}`;
  } catch {
    /* not an invite card */
  }
  return "";
}

function cap(s, max) {
  if (!max || s.length <= max) return s;
  return s.slice(0, max).trimEnd() + "…";
}
