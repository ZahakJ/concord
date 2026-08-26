// navstack.svelte.js — one stack of dismissible layers, and the only answer to
// "what is open right now?".
//
// Before this there were three answers, and they disagreed. App.svelte held a
// thirteen-rung if/else ladder for the Android back button, lib/shortcuts.js
// held a near-identical ladder for desktop Escape, and state.svelte.js held a
// flat list of overlay closers that both ladders consulted at a fixed position
// — so an overlay opened AFTER a modal still closed before it, and two things
// (the profile card, the context menu) were hardcoded rungs rather than
// registrants and could never be anywhere but the bottom. Seven visible layers
// were registered nowhere at all, which is how back walked straight past the
// search row and out of the app.
//
// The model is the one every window manager uses: a layer is pushed when it
// appears and removed when it goes, back/Escape pops whatever is on top, and
// nothing consults a priority table. Open order IS the order, so a sheet raised
// from inside a modal closes before the modal without anybody writing that
// down.
//
// What is NOT a layer: places. A conversation, a forum board, the phone's
// channel list — those are somewhere you are, not something covering what you
// were looking at, and back moves between them only once the stack is empty.
// Keeping the two apart is what makes the phone's back behaviour monotone (see
// handleBack in App.svelte).
import { onDestroy, untrack } from "svelte";

// Innermost last. $state so depth is reactive: the scrims read their own index
// out of it, and the Android bridge reports its length to the OS.
const layers = $state([]);
let seq = 0;

// pushLayer registers a dismissible layer and returns the remover. Callers that
// live in a component should prefer `layer()` (below), which removes itself.
//
// `close` must be idempotent and must be the SAME dismissal the layer's own ✕
// performs — a sheet that plays an exit animation should hand over the function
// that plays it, not a raw state assignment, or back and the button disagree.
//
// `blocking` marks a layer that absorbs back without going away — the join
// veil is the only one. It covers the screen while a join is in flight and
// nothing about it is cancellable, so back must neither dismiss it (the join
// completes anyway and drops you somewhere with no explanation) nor fall
// through to the app-exit rung underneath it.
//
// Every mutation runs untracked. `layers` is $state, and appending to a $state
// array reads its length on the way in — so `$effect(() => pushLayer(…))`, the
// natural way to register for as long as something is open, subscribed the
// effect to the very array it was writing and re-ran itself until Svelte gave
// up with effect_update_depth_exceeded. Registering must never make you a
// reader of the stack; the getters on `layer()` below are how you read it.
export function pushLayer(kind, close, { blocking = false } = {}) {
  const entry = { id: ++seq, kind, close, blocking };
  untrack(() => layers.push(entry));
  return () => removeLayer(entry.id);
}

export function removeLayer(id) {
  untrack(() => {
    const i = layers.findIndex((l) => l && l.id === id);
    if (i !== -1) layers.splice(i, 1);
  });
}

// popLayer dismisses the top layer, reporting whether there was one.
//
// The entry comes off BEFORE close() runs: a dismissal that animates out (every
// sheet does, for 190ms) does not clear its own state until the animation ends,
// and a second back inside that window would otherwise pop the same layer twice
// and leave the one underneath standing.
export function popLayer() {
  const top = untrack(() => layers[layers.length - 1]);
  if (!top) return false;
  if (top.blocking) return true; // press consumed, layer stays
  untrack(() => layers.pop());
  try {
    top.close();
  } catch {
    /* a layer that throws on the way out must not strand the stack */
  }
  return true;
}

// How many layers are open. The Android side turns this into "should the OS
// hand me the back gesture or keep it?".
export function navDepth() {
  return layers.length;
}

export function topKind() {
  return layers.length ? layers[layers.length - 1]?.kind || "" : "";
}

// layer() registers for the lifetime of the calling component. The returned
// handle reports the layer's position, which is what gives nested scrims their
// depth: each layer's dim sits above everything below it, so a sheet raised
// over a sheet reads as two surfaces rather than one flat pile.
export function layer(kind, close) {
  const entry = { id: ++seq, kind, close };
  untrack(() => layers.push(entry));
  onDestroy(() => removeLayer(entry.id));
  return {
    id: entry.id,
    // These getters read `layers` reactively, so a template using them updates
    // when something opens above. The `l &&` guards are not defensive noise: a
    // $state array publishes its new length before the spliced slot is gone, so
    // anything reading it in the same tick as a removal — the scrim depth does,
    // on every close — walks over one undefined entry.
    get index() {
      return layers.findIndex((l) => l && l.id === entry.id);
    },
    get isTop() {
      return layers.length > 0 && layers[layers.length - 1]?.id === entry.id;
    },
    // The innermost layer of this kind — which is not the same question as
    // isTop. A dialog's Tab trap has to stay armed while a picker it opened
    // sits above it, or focus escapes onto the page the moment the picker
    // closes; but only the innermost DIALOG may hold it.
    get isInnermostOfKind() {
      for (let i = layers.length - 1; i >= 0; i--) {
        if (layers[i]?.kind === kind) return layers[i].id === entry.id;
      }
      return false;
    },
  };
}

// syncLayer keeps an entry in step with a piece of reactive state, for layers
// that have no component of their own to register from (a reply being composed,
// an incoming call banner, the member panel). Call it from a component's script.
export function syncLayer(kind, isOpen, close, opts) {
  $effect(() => {
    if (!isOpen()) return;
    return pushLayer(kind, close, opts);
  });
}

// ---- what back means when the stack is empty ----
//
// The phone's places, outermost first: the channel list (the drawer), a
// conversation, a post inside a forum board. Back walks OUT of them one step at
// a time and then leaves the app. The rule that matters is that no press may
// ever undo the press before it: the old ladder had one rung that opened the
// drawer and another that closed it, so back oscillated between the two forever
// and the "press back again to exit" toast was a promise nothing kept.
//
// The drawer is a place here, not a layer. It is the phone's channel list —
// there is no other screen that shows one — so backing out of a conversation
// reveals it, and backing out of IT leaves the app. That is why it does not
// register: a layer would be popped, and popping it is precisely the move that
// made back run in circles. Closing a drawer you opened on purpose is the
// scrim, the swipe, or picking a channel, exactly as it was.
//
// Which places exist, and how to leave them, is the shell's business and not
// this module's — see handleBack in App.svelte. All this file promises is that
// when popLayer() returns false there is genuinely nothing covering the screen.
