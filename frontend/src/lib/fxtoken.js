// fxtoken.js — message send effects, iMessage-style, as a content token.
// Same delivery trick as polls and disappearing messages: the effect rides
// INSIDE the ordinary MLS message as `[fx](concord://fx/v1/confetti)`, so it
// syncs through history, needs zero backend changes, and is authenticated like
// the words it decorates. Message.svelte strips the token from the rendered
// body and plays the burst once per session when the row first scrolls into
// view — seeded by the message id, so every peer watches the same sky.

import { confettiBurst, fireworksBurst } from "./burst.js";

export const FX_RE = /\[fx\]\(concord:\/\/fx\/v1\/([a-z]+)\)/;

// name -> { play(seed), body } — body is the default content when the command
// is sent bare, so "/confetti" alone still renders a (jumbo) message row
// rather than an empty bubble.
export const FX_EFFECTS = {
  confetti: { body: "🎉", play: (seed) => confettiBurst({ seed }) },
  fireworks: { body: "🎆", play: (seed) => fireworksBurst(seed) },
  hearts: {
    body: "❤️",
    play: (seed) =>
      confettiBurst({
        seed,
        glyphs: ["❤", "♥"],
        colors: ["#f43f5e", "#fb7185", "#f0abfc"],
        n: 18,
        size: [10, 17],
        dur: [2.2, 3.4],
        drift: 50,
      }),
  },
};

export function encodeFx(name) {
  return `[fx](concord://fx/v1/${name})`;
}

// The effect named by a message's content, or "" if none/unknown (unknown
// names — a newer client's effect — degrade to plain text, same forward-compat
// stance as the poll token's versioned path).
export function fxEffect(content) {
  const m = content?.match(FX_RE);
  return m && FX_EFFECTS[m[1]] ? m[1] : "";
}

export function stripFx(content) {
  return content ? content.replace(FX_RE, "").trim() : content;
}

// Played-this-session ledger: scrolling back through Tuesday's confetti should
// not re-rain it on every pass. Session-only on purpose — reopening the app on
// the message IS a fresh viewing.
const played = new Set();

export function playFxOnce(messageId, name) {
  if (!name || played.has(messageId)) return;
  played.add(messageId);
  FX_EFFECTS[name]?.play(messageId);
}
