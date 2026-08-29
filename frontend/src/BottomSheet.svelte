<script module>
  // Sheets currently on screen. Module scope, not state: it is only read at
  // creation, to stack a nested sheet's scrim above the sheet it was raised
  // from.
  let openSheets = 0;
</script>

<script>
  // BottomSheet — the mobile action-sheet primitive: a panel that slides up from
  // the bottom edge under a scrim, with a grab handle you can drag or fling down
  // to dismiss. Content renders via the children snippet and scrolls
  // independently; only the handle/header region drives the drag, so a
  // scrollable list inside never fights the gesture. Desktop never mounts
  // this — it's the touch counterpart of popovers and context menus.
  //
  // The gesture itself is not implemented here: lib/sheet.js owns the numbers
  // and the animation, shared with the dialog sheets and the two profile cards.
  // This file used to carry its own — touch events, a 40% threshold, a 0.55
  // px/ms fling, and no exit animation at all, so a sheet dismissed by a flick
  // vanished under a still-moving finger while a dialog dismissed the same way
  // slid out over 190ms.
  import { onDestroy } from "svelte";
  import { sheetdrag } from "./lib/sheet.js";

  // The keyboard is subtracted EXPLICITLY, because no viewport unit knows about
  // it here. Measured on the device with the IME up: 100vh, 100dvh, 100svh,
  // 100lvh and visualViewport.height all report the full 915px screen while the
  // native bridge reports --kb: 336px — the activity is edge-to-edge with
  // insetsHandling disabled, so the WebView is never resized. dvh was chosen for
  // this on the belief that it shrank; it does not, and a sheet holding an input
  // was measuring itself against a screen a third of which was covered. The
  // .bs-sheet rule carries a vh value as the fallback for engines that drop the
  // unit (where the viewport really does resize and --kb is 0).
  let { title = "", onClose, maxHeight = "calc(72 * var(--dvh))", children } = $props();

  let sheetEl = $state(null);
  let scrimEl = $state(null);
  let bodyEl = $state(null);

  // How many sheets are already up. A second sheet raised over the first used
  // to be drawn at the same z-index with a single scrim under both, so the two
  // read as one flat pile and nothing said which surface a tap would reach.
  // Each one now brings its own dim, above everything below it.
  const depth = openSheets++;
  onDestroy(() => openSheets--);
</script>

<button
  bind:this={scrimEl}
  class="bs-scrim"
  style:z-index={400 + depth * 2}
  aria-label="Close"
  onclick={onClose}
></button>
<div
  bind:this={sheetEl}
  class="bs-sheet"
  style="max-height:min({maxHeight}, calc(100 * var(--dvh) - var(--kb, 0px) - 8px))"
  style:z-index={401 + depth * 2}
  role="dialog"
  aria-label={title || "Sheet"}
>
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="bs-grab"
    use:sheetdrag={{
      sheet: () => sheetEl,
      scrim: () => scrimEl,
      scroller: () => bodyEl,
      onDismiss: () => onClose?.(),
    }}
  >
    <span class="bs-handle"></span>
    {#if title}<div class="bs-title">{title}</div>{/if}
  </div>
  <div class="bs-body" bind:this={bodyEl}>
    {@render children?.()}
  </div>
</div>

<style>
  .bs-scrim {
    position: fixed;
    inset: 0;
    background: var(--scrim);
    z-index: 400;
    border: none;
    animation: bs-fade var(--dur-standard) ease;
    /* Springs the dim back in sync with the sheet on a released half-swipe;
       lib/sheet.js suppresses it for the duration of a tracked drag. */
    transition: opacity var(--dur-standard) ease;
  }
  .bs-sheet {
    position: fixed;
    left: 0;
    right: 0;
    /* Rest on the keyboard's top edge when there is one. */
    bottom: var(--kb, 0px);
    z-index: 401;
    display: flex;
    flex-direction: column;
    background: var(--bg-elevated, var(--bg-1));
    border-radius: var(--radius-sheet) var(--radius-sheet) 0 0;
    /* Fallback for engines without dvh: the inline max-height above carries the
       dvh value and is simply dropped there, leaving this one standing. */
    max-height: calc(72 * var(--vh));
    box-shadow: var(--shadow-pop);
    animation: bs-up 0.22s var(--ease-out);
  }
  .bs-grab {
    flex-shrink: 0;
    /* The pill itself is 4px tall; a thumb aims at the strip, not the pill, so
       the strip has to be worth aiming at. */
    padding: var(--sp-3) var(--sp-4) var(--sp-2);
    cursor: grab;
    /* The grab zone owns its touches — without this the browser treats the
       drag as a scroll/refresh gesture and the sheet stutters. */
    touch-action: none;
    /* A slow grab-and-pull starts WebView text selection on the title
       otherwise, popping the Android selection toolbar over the sheet and
       abandoning the drag. */
    user-select: none;
    -webkit-user-select: none;
    -webkit-touch-callout: none;
  }
  .bs-handle {
    display: block;
    width: 38px;
    height: 4px;
    margin: 0 auto;
    border-radius: 2px;
    background: var(--border);
  }
  /* This is the title of every sheet in the app, and it names a real thing —
     a guild, a person, a channel. It was set as a micro-label: 13px, uppercase
     and tracked out, the least legible combination in the vocabulary. Let a
     title read as a title. */
  .bs-title {
    padding: 8px 4px 6px;
    font-size: var(--fs-body);
    font-weight: 600;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .bs-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    -webkit-overflow-scrolling: touch;
    /* Momentum that reaches the end of the sheet stops there. Without this the
       remainder is handed to the message feed behind the scrim, which is the
       classic "the wrong thing moved" feeling. */
    overscroll-behavior: contain;
    padding: 0 10px calc(12px + var(--safe-bottom));
  }
  /* The same fade the dialog sheets get: long content should look like it is
     running out under the gesture pill, not like it was cut with scissors. */
  .bs-body::after {
    content: "";
    display: block;
    position: sticky;
    bottom: calc(-12px - var(--safe-bottom));
    height: calc(22px + var(--safe-bottom));
    margin: 0 -10px calc(-12px - var(--safe-bottom));
    pointer-events: none;
    background: linear-gradient(transparent, var(--bg-elevated, var(--bg-1)));
  }
  @keyframes bs-fade {
    from {
      opacity: 0;
    }
  }
  @keyframes bs-up {
    from {
      transform: translateY(100%);
    }
  }
</style>
