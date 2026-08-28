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
// `host` is the message row the effect belongs to. It is not optional in
// practice: a celebration that plays over the whole window paints specks on
// the channel sidebar and the member panel, which have nothing to do with the
// message that earned them.
export const FX_EFFECTS = {
  confetti: { body: "🎉", play: (seed, host) => confettiBurst({ seed, host }) },
  fireworks: { body: "🎆", play: (seed, host) => fireworksBurst(seed, host) },
  hearts: {
    body: "❤️",
    play: (seed, host) =>
      confettiBurst({
        seed,
        host,
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

// An effect celebrates a MOMENT, so it fires once and only near the moment.
// Two gates, because one of them alone was wrong in a way that shipped:
//
//   1. A PERSISTENT ledger. This was session-only, which meant every login
//      re-rained every confetti message still on screen — you logged in to a
//      fireworks display for a party three days ago.
//   2. An age window. A ledger alone still fires the first time you scroll
//      back into an old celebration, or on a fresh device syncing history.
//      Effects that arrive already-stale never play at all.
//
// The ledger is capped and pruned: it only needs to remember long enough for
// a message to age out of the window anyway.
const LEDGER_KEY = "concord.fxPlayed";
const FX_MAX_AGE_MS = 30 * 60 * 1000; // celebrate what just happened, not history
const LEDGER_CAP = 300;

function loadLedger() {
  try {
    const raw = JSON.parse(localStorage.getItem(LEDGER_KEY) || "[]");
    return Array.isArray(raw) ? raw.slice(-LEDGER_CAP) : [];
  } catch {
    return [];
  }
}
let ledger = loadLedger();
const played = new Set(ledger);

export function playFxOnce(messageId, name, sentAt, host = null) {
  if (!name || played.has(messageId)) return;
  // Unknown/unparseable timestamps are treated as old: silence is the safe
  // failure for something whose whole job is to be loud.
  const age = Date.now() - (sentAt ? new Date(sentAt).getTime() : 0);
  played.add(messageId);
  ledger = [...ledger, messageId].slice(-LEDGER_CAP);
  try {
    localStorage.setItem(LEDGER_KEY, JSON.stringify(ledger));
  } catch {
    /* private mode / quota: the in-memory set still holds for this session */
  }
  if (!(age >= 0 && age < FX_MAX_AGE_MS)) return;
  FX_EFFECTS[name]?.play(messageId, host);
}
