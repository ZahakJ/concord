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
import { loadOnce, want } from "./loadonce.js";
import { flashSlot } from "./state.svelte.js";

// islandFailed is the onFail the shells hand to want(): put back the state
// that asked (so asking again is a real retry), and say so once. A slot, not a
// stack — App and MobileShell both ask for the shared surfaces, and one failed
// fetch is one message.
export function islandFailed(err, putBack) {
  console.warn("island chunk failed to load", err);
  putBack?.();
  flashSlot("island", "Couldn't load that view. Try again.", "error");
}

function island(load) {
  let C = $state(null);
  const loader = loadOnce(load, (m) => (C = m.default));
  return {
    get C() {
      return C;
    },
    // Rejects on failure; for callers that await (warmIslands catches).
    load: loader,
    // For the $effects that open a surface: never rejects. A chunk that fails
    // to load (an interrupted update, a flaky WebView) used to be an unhandled
    // rejection and a surface that silently never appeared, with the state
    // that asked for it still set — so the effect never ran again and the
    // retry loadOnce allows could not happen. onFail puts that state back and
    // says so; the next ask is the retry.
    want: (onFail) => want(loader, onFail),
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
