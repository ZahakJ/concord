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
import { onDestroy } from "svelte";

// Innermost last. A PLAIN array, with a $state revision counter beside it for
// the readers that need to re-render when it changes (the scrims read their
// own index, the Android bridge reports the depth).
//
// It used to be the $state array itself, and that lost layers. A settings rail
// click swaps one dialog component for another in a single flush: the incoming
// dialog pushes its entry while it is constructed, the outgoing one splices
// its own entry out as it is destroyed. Measured in the running app, the
// splice saw the array as it was BEFORE the push and wrote that back, so the
// new dialog's entry vanished — on every other click, because the next swap's
// push happened to land after the previous removal. With nothing on the stack,
// Escape fell through to "mark the channel read" and Settings would not close.
// A plain array has one value, whoever reads it and whenever.
const layers = [];
let rev = $state(0);
// Bumped on a microtask, outside whatever flush the change happened in. A
// bump made from a component's teardown in the middle of a dialog swap was
// lost the same way the array write was, and readers such as "is there
// another dialog under me" kept their stale answer: on a phone the settings
// sub-panel lost its Back button.
let bumpQueued = false;
const touch = () => {
  if (bumpQueued) return;
  bumpQueued = true;
  queueMicrotask(() => {
    bumpQueued = false;
    rev++;
  });
};
const read = () => rev; // subscribe a reactive reader to the stack

let seq = 0;

// ---- focus, on the way out ----
//
// Every layer remembers what had focus when it opened, and when it goes, if
// focus went with it — dropped on <body> because the focused control was
// inside the thing that just unmounted — it is put back there. One rule for
// every layer: the emoji picker, the command palette, the profile card and
// the status popover each used to leave a keyboard user at the top of the
// document after Escape or a pick, because each was left to remember this on
// its own and four of them did not.
//
// It only ever REPAIRS: a layer that placed focus somewhere on purpose (a
// pick that returns to the composer, a menu item that opened a dialog) has
// already moved it off <body> by the time this looks, a frame later.
function openerNow() {
  if (typeof document === "undefined") return null;
  const el = document.activeElement;
  return el && el !== document.body && el !== document.documentElement ? el : null;
}

function restoreFocus(entry) {
  const back = entry.opener;
  if (!back || typeof requestAnimationFrame === "undefined") return;
  requestAnimationFrame(() => {
    const now = document.activeElement;
    if (now && now !== document.body && now !== document.documentElement && now.isConnected) return;
    if (!back.isConnected || back.closest("[inert]")) return;
    // Never behind a dialog that is still up: the opener of a dialog that was
    // swapped for another (a settings rail click) is the button on the page
    // underneath, and the dialog now on screen places its own focus.
    const dialogs = document.querySelectorAll('[role="dialog"][aria-modal="true"]');
    const top = dialogs[dialogs.length - 1];
    if (top && !top.contains(back)) return;
    back.focus({ preventScroll: true });
  });
}

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
// Registering must never make you a reader of the stack. When `layers` was a
// $state array, appending read its length on the way in, so
// `$effect(() => pushLayer(…))` — the natural way to register for as long as
// something is open — subscribed the effect to the very array it was writing
// and re-ran itself until Svelte gave up with effect_update_depth_exceeded.
// The revision bump happens on a microtask, so it never runs inside the
// registering effect either; the getters on `layer()` below are how you read
// it.
export function pushLayer(kind, close, { blocking = false } = {}) {
  const entry = { id: ++seq, kind, close, blocking, opener: openerNow() };
  layers.push(entry);
  touch();
  return () => removeLayer(entry.id);
}

export function removeLayer(id) {
  const i = layers.findIndex((l) => l.id === id);
  if (i === -1) return;
  const [gone] = layers.splice(i, 1);
  touch();
  restoreFocus(gone);
}

// popLayer dismisses the top layer, reporting whether there was one.
//
// The entry comes off BEFORE close() runs: a dismissal that animates out (every
// sheet does, for 190ms) does not clear its own state until the animation ends,
// and a second back inside that window would otherwise pop the same layer twice
// and leave the one underneath standing.
export function popLayer() {
  const top = layers[layers.length - 1];
  if (!top) return false;
  if (top.blocking) return true; // press consumed, layer stays
  layers.pop();
  touch();
  try {
    top.close();
  } catch {
    /* a layer that throws on the way out must not strand the stack */
  }
  restoreFocus(top);
  return true;
}

// How many layers are open. The Android side turns this into "should the OS
// hand me the back gesture or keep it?".
export function navDepth() {
  read();
  return layers.length;
}

export function topKind() {
  read();
  return layers.length ? layers[layers.length - 1]?.kind || "" : "";
}

// layer() registers for the lifetime of the calling component. The returned
// handle reports the layer's position, which is what gives nested scrims their
// depth: each layer's dim sits above everything below it, so a sheet raised
// over a sheet reads as two surfaces rather than one flat pile.
export function layer(kind, close) {
  const entry = { id: ++seq, kind, close, opener: openerNow() };
  layers.push(entry);
  touch();
  onDestroy(() => removeLayer(entry.id));
  return {
    id: entry.id,
    // These getters read `layers` reactively, so a template using them updates
    // when something opens above. The `l &&` guards are not defensive noise: a
    // $state array publishes its new length before the spliced slot is gone, so
    // anything reading it in the same tick as a removal — the scrim depth does,
    // on every close — walks over one undefined entry.
    get index() {
      read();
      return layers.findIndex((l) => l && l.id === entry.id);
    },
    get isTop() {
      read();
      return layers.length > 0 && layers[layers.length - 1]?.id === entry.id;
    },
    // The innermost layer of this kind — which is not the same question as
    // isTop. A dialog's Tab trap has to stay armed while a picker it opened
    // sits above it, or focus escapes onto the page the moment the picker
    // closes; but only the innermost DIALOG may hold it.
    get isInnermostOfKind() {
      read();
      for (let i = layers.length - 1; i >= 0; i--) {
        if (layers[i]?.kind === kind) return layers[i].id === entry.id;
      }
      return false;
    },
    // Is anything ELSE of this kind open? A settings panel and a confirmation
    // it raised are both dialogs, and only the panel is a rung on the trail —
    // the confirm is rendered locally precisely so that cancelling it does not
    // throw the trail away.
    //
    // Asked as "is there another one", not "am I the outermost one", because it
    // has to give the right answer from inside close(): popLayer takes the
    // entry off the stack BEFORE calling it, so a getter that looks for its own
    // id finds nothing and reports false for the very layer it is describing.
    // Excluding my own id instead is true either way.
    get hasOtherOfKind() {
      read();
      return layers.some((l) => l && l.kind === kind && l.id !== entry.id);
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
