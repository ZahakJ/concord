// islands.svelte.js — the surfaces that are not on the first screen, loaded
// the first time they are asked for.
//
// The boot chunk carried every component the shell could ever render: the
// voice stage (the largest file in the tree), the forum board, the search
// takeover, the command palette and the parked call window — 200KB of parsed
// JavaScript for surfaces most sessions open once, or never, on a phone whose
// cold start was measured to be forty-five percent JS parse. The dialogs had
// already been split (App.svelte's MODAL_LOADERS); these are the non-dialog
// islands, on the same idea: a $state slot that holds the component once its
// chunk lands, and a loader the shell calls the moment the surface is wanted.
//
// warmIslands() fetches all of them in the first idle moment after boot, so
// the first tap on Study Hall or a forum still finds the code on the machine
// — the split moves the bytes off the boot path, not out of the session.
import { untrack } from "svelte";

function island(load) {
  let C = $state(null);
  let pending = null;
  return {
    get C() {
      return C;
    },
    load() {
      if (untrack(() => C)) return Promise.resolve();
      if (!pending) {
        pending = load().then(
          (m) => {
            C = m.default;
          },
          (err) => {
            // A failed fetch must not poison the slot: the next ask retries.
            pending = null;
            throw err;
          },
        );
      }
      return pending;
    },
  };
}

export const voiceStage = island(() => import("../VoicePanel.svelte"));
export const forumBoard = island(() => import("../ForumView.svelte"));
export const searchTakeover = island(() => import("../SearchPanel.svelte"));
export const commandPalette = island(() => import("../QuickSwitcher.svelte"));
export const parkedCall = island(() => import("../FloatingCall.svelte"));
export const selfView = island(() => import("../SelfView.svelte"));
export const callMiniControls = island(() => import("../CallMiniControls.svelte"));

export function warmIslands() {
  for (const i of [voiceStage, forumBoard, searchTakeover, commandPalette, parkedCall, selfView, callMiniControls]) {
    i.load().catch(() => {});
  }
}
