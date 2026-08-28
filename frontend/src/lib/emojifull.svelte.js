// emojifull.svelte.js — the full Unicode emoji table, on its own chunk.
//
// Same shape as lib/cosmetics.svelte.js: a $state box, a once-started dynamic
// import, and a getter that both triggers the fetch and subscribes whoever read
// it. The generated table is ~35KB of source and nothing on a cold start needs
// it, so it must not sit in the boot bundle beside the login screen.
//
// The install is the point, not the return value: lib/emoji.js is on the
// synchronous send path and cannot await anything, so it keeps working off the
// 379 curated shortcodes and simply gets better once this lands. Readers who
// want to re-render at that moment — the picker's tab strip and grid — read
// `emojiTable()` inside a $derived.
import { installFullEmoji } from "./emoji.js";

const box = $state({ ready: false });
let started = false;

export function emojiTable() {
  if (!started) {
    started = true;
    import("./emojitable.js").then(
      (m) => {
        installFullEmoji(m.EMOJI_TABLE);
        box.ready = true;
      },
      () => {
        /* A chunk that will not load leaves the curated set in place. */
      },
    );
  }
  return box.ready;
}

// Warm it during idle, beside the cosmetics tables: by the time anyone opens a
// picker or types a colon it is already in, and a cold start still paid nothing
// for it.
export function precacheEmoji() {
  emojiTable();
}
