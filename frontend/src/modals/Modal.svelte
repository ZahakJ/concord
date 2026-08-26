<script module>
  // What can hold focus inside a dialog. `[tabindex="-1"]` is excluded on
  // purpose: it is how a thing is made programmatically focusable but kept out
  // of the tab order, which is exactly what the dialog container itself uses.
  const FOCUSABLE =
    'a[href],button:not([disabled]),textarea:not([disabled]),' +
    'input:not([disabled]):not([type="hidden"]),select:not([disabled]),' +
    '[tabindex]:not([tabindex="-1"])';

  const visible = (el) => !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
</script>

<script>
  import { onDestroy, onMount, tick } from "svelte";
  import { S, modalNav, backPanel } from "../lib/state.svelte.js";
  import { layer } from "../lib/navstack.svelte.js";
  import { haptic } from "../lib/touch.js";
  import { sheetdrag, playExit } from "../lib/sheet.js";
  import Icon from "../Icon.svelte";
  // `wide` widens the desktop dialog for content that benefits from the room
  // (sectioned settings); `size="xl"` makes it a large workspace (the advanced
  // composer). The mobile sheet presentation ignores both.
  let { title, onClose, wide = false, size = "", children } = $props();
  let dialog = $state(null);
  // TalkBack/VoiceOver announce a dialog by its label; without an id to point
  // aria-labelledby at, every sheet in the app opened as an unnamed group.
  const titleId = "modal-title-" + Math.random().toString(36).slice(2, 9);

  // Back is offered whenever there's somewhere to go back TO — either a panel
  // on the stack we drilled through, or a plain `from` on a panel opened
  // directly.
  const canBack = $derived(S.modalStack.length > 0 || !!S.modal?.from);

  // Settings and its sub-panels read as one stack you move through, not a pile
  // of unrelated dialogs: a panel opened from another slides in from the right,
  // and going back slides in from the left. Read once at mount (a CSS animation
  // only runs then) and consumed, so the next open starts from a clean slate.
  const enterDir = modalNav.dir || (S.modal?.from ? 1 : 0);
  modalNav.dir = 0;

  // Focus, on open and on close.
  //
  // 34 of the 49 dialogs focused nothing at all when they appeared. That is not
  // a small omission: with focus left on whatever was behind the scrim, the Tab
  // trap below could never engage (it only fires at the first and last item),
  // so Tab walked straight out of the dialog and through the page underneath —
  // and a keyboard user had no way to reach the dialog they had just opened
  // except by tabbing all the way round. And none of the 49 gave focus BACK, so
  // closing one dropped the caret at the top of the document every time.
  //
  // `me` is this dialog's place on the app-wide layer stack. Registering at component
  // init rather than in an effect keeps creation order and stack order the same
  // thing, which is what makes a confirm raised from inside a settings panel —
  // six places do it — answer Escape on its own without also closing the panel
  // that asked the question. `dismiss` is what goes on the stack, not
  // `onClose`, so back and the ✕ play the same exit.
  const me = layer("modal", () => dismiss());
  let opener = null;

  onMount(() => {
    const active = document.activeElement;
    opener = active instanceof HTMLElement && active !== document.body ? active : null;
    // One tick, so a panel with its own opening focus (ConfirmDialog puts it on
    // Cancel, several fields carry autofocus) has already had its say. We only
    // place focus when nothing inside the dialog has claimed it.
    tick().then(() => {
      if (!dialog || dialog.contains(document.activeElement)) return;
      // Never the ✕ or the back arrow: they live in the pinned strip, and a
      // dialog whose first keystroke closes it is a dialog you can't use.
      //
      // On a phone the dialog itself takes it. There is no Tab trap to prime
      // there, and focusing the first field would raise the software keyboard
      // over the sheet that just slid up — which is precisely why every modal's
      // own autofocus is already written `autofocus={!S.isMobile}`.
      const body = S.isMobile
        ? []
        : [...dialog.querySelectorAll(FOCUSABLE)].filter(
            (el) => !el.closest(".sheet-top") && visible(el),
          );
      // A panel that names its own first field wins over DOM order. The create-
      // channel dialog leads with four channel-TYPE buttons and follows them
      // with the name box; first-in-the-tree would put the caret on "Text" and
      // leave the one thing you came here to type unfocused.
      const target = body.find((el) => el.hasAttribute("autofocus")) || body[0] || dialog;
      if (target === dialog) dialog.tabIndex = -1;
      target.focus?.({ preventScroll: true });
    });
  });

  // Closing for real drops the whole trail; navigating to another panel keeps
  // it. Which happened is simply whether a modal is still open by the time this
  // one is torn down.
  onDestroy(() => {
    if (!S.modal) S.modalStack = [];
    // Hand focus back to whatever opened us — but only if nothing else has
    // claimed it in the meantime (drilling into a settings sub-panel unmounts
    // this dialog and mounts the next one on the same frame).
    const back = opener;
    opener = null;
    if (!back) return;
    tick().then(() => {
      const now = document.activeElement;
      if (now && now !== document.body && now !== document.documentElement) return;
      if (document.contains(back)) back.focus?.({ preventScroll: true });
    });
  });

  // Mobile: the sheet can be flicked/dragged DOWN to dismiss — the native
  // gesture people expect, so they don't have to reach the tiny ✕ in the top
  // corner one-handed. The grab area is the pinned top strip (grip + title);
  // the physics is shared with every other sheet in the app (lib/sheet.js).
  let overlay = $state(null);

  // Every consumer's onClose sets S.modal = null, which unmounts this component
  // in one frame — so the sheet that slid up over 0.28s used to vanish
  // mid-swipe, with the finger still moving. Play the exit first, then unmount.
  // Desktop keeps the immediate close: there is no gesture to finish there.
  let closing = $state(false);
  function dismiss() {
    if (!S.isMobile) return onClose();
    if (closing) return;
    closing = true;
    haptic("light");
    playExit(dialog, overlay, onClose);
  }

  // Tab is trapped within the dialog so focus can't wander onto the page
  // behind. Escape is NOT handled here: it goes through the one navigation
  // stack in lib/shortcuts.js, which pops whatever is on top — and the top may
  // well be a picker or a menu this dialog raised.
  function onKeydown(e) {
    // Only the innermost dialog answers the keyboard.
    if (!me.isInnermostOfKind) return;
    if (e.key === "Tab" && dialog) {
      const f = [...dialog.querySelectorAll(FOCUSABLE)].filter(visible);
      if (!f.length) return;
      const first = f[0];
      const last = f[f.length - 1];
      const at = document.activeElement;
      // Focus sitting on the dialog container itself (a panel with nothing
      // focusable in it) or anywhere outside: the wrap has to engage from
      // there too, or Tab leaves for the page behind the scrim.
      if (!f.includes(at)) {
        e.preventDefault();
        (e.shiftKey ? last : first).focus();
      } else if (e.shiftKey && at === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && at === last) {
        e.preventDefault();
        first.focus();
      }
    }
  }
</script>

<svelte:window onkeydown={onKeydown} />

<!-- Depth comes from the stack, not from a constant. Two sheets used to be
     drawn at the same z-index with a single scrim under both, so a profile
     sheet raised over Settings read as one flat pile with no dim between the
     layers and no clue which of them a tap would reach. Each layer's scrim now
     sits above everything below it. -->
<div
  bind:this={overlay}
  class="overlay"
  style:z-index={100 + me.index * 2}
  onclick={dismiss}
  role="presentation"
>
  <div
    bind:this={dialog}
    class="dialog"
    class:wide
    class:xl={size === "xl"}
    class:deeper={enterDir === 1}
    class:shallower={enterDir === -1}
    onclick={(e) => e.stopPropagation()}
    role="dialog"
    aria-modal="true"
    aria-labelledby={titleId}
  >
    <!-- Grip and title travel together as one pinned strip: the grip used to
         scroll away with the content, leaving the sheet's own drag handle out
         of reach on exactly the tall panels that need it most. -->
    <div
      class="sheet-top"
      use:sheetdrag={{
        enabled: S.isMobile && !closing,
        sheet: () => dialog,
        scrim: () => overlay,
        scroller: () => dialog,
        onDismiss: onClose,
      }}
      role="presentation"
    >
      <div class="grip"></div>
      <div class="head">
        {#if canBack}
          <button class="back" onclick={backPanel} aria-label="Back" title="Back">
            <Icon name="chevron" size={16} />
          </button>
        {/if}
        <h3 id={titleId}>{title}</h3>
        <button class="x" onclick={dismiss} aria-label="Close">✕</button>
      </div>
    </div>
    {@render children()}
  </div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    /* Frosted scrim: the app dims AND recedes, so the dialog reads as the
       only in-focus surface. */
    background: rgba(0, 0, 0, 0.55);
    display: grid;
    place-items: center;
    z-index: 100;
    animation: fade 0.16s ease;
  }
  .dialog {
    width: 380px;
    max-width: 90vw;
    /* Never taller than the viewport; scroll inside on short screens (laptops)
       so long content like the 24-word recovery phrase stays reachable. dvh
       tracks the viewport the soft keyboard actually leaves behind; the plain
       vh line before it is the fallback for WebViews that don't know dvh. */
    max-height: 90vh;
    max-height: 90dvh;
    overflow-y: auto;
    /* No modal may pan the sheet sideways. A single wide child (ModalStats'
       peer rows) otherwise turns the whole surface, sticky header included,
       into a horizontal scroller that jitters under a thumb. */
    overflow-x: hidden;
    /* Hitting either end of this scroller must not start scrolling the app
       behind it (or trigger the WebView's pull-to-refresh). */
    overscroll-behavior: contain;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 20px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    box-shadow: var(--shadow-pop);
    /* A touch of spring on entry — overshoots ~1% then settles. */
    animation: pop 0.26s cubic-bezier(0.34, 1.4, 0.5, 1);
  }
  .dialog.wide {
    width: 460px;
  }
  /* A large workspace (the advanced composer): most of the viewport, and a
     fixed tall height so its inner panes get real room instead of collapsing to
     content height. */
  .dialog.xl {
    width: min(1080px, 94vw);
    height: min(780px, 88vh);
    height: min(780px, 88dvh);
  }
  @keyframes fade {
    from {
      opacity: 0;
    }
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: translateY(14px) scale(0.95);
    }
  }
  .grip {
    display: none; /* desktop: no drag handle */
  }
  /* Mobile: dialogs present as full-width bottom sheets instead of floating
     cards — thumb-reachable, roomy, and keyboard-friendly. Desktop (fine
     pointer + wide viewport) is untouched. */
  @media (pointer: coarse), (max-width: 768px) {
    .overlay {
      place-items: end stretch;
    }
    /* All variants collapse to the bottom-sheet presentation; higher-specificity
       selectors (`wide`, `xl`) must be listed so they can't pin a desktop size. */
    .dialog,
    .dialog.wide,
    .dialog.xl {
      width: auto;
      max-width: none;
      height: auto;
      /* vh resolves against the LARGE viewport in Android WebView and does not
         shrink when the keyboard opens — a sheet with a text field would keep
         claiming 92% of the full screen and push its own confirm button behind
         the keyboard. dvh is the height actually available right now. */
      max-height: 92vh;
      max-height: 92dvh;
      border: none;
      border-radius: 18px 18px 0 0;
      padding-bottom: calc(20px + var(--safe-bottom));
      animation: sheet-up 0.28s cubic-bezier(0.22, 1.1, 0.36, 1);
      touch-action: pan-y;
    }
    /* The drag, the spring-back and the slide-out are all driven from
       lib/sheet.js as inline styles, so the transform and its transition live
       there rather than being split between a rule here and an element there
       (which is what needed an !important to settle). */
    .sheet-top {
      /* touch-action is written by lib/sheet.js, which keeps it in step with
         the body's scroll position: `none` at the top so the strip owns the
         drag, `pan-y` once scrolled so the header scrolls like any other area.
         The static `pan-y` that used to be here meant the browser claimed every
         downward drag as a scroll and cancelled the gesture on its second move,
         which is why no dialog could actually be swiped away. */
      touch-action: none;
      /* A slow grab-and-pull — the exact gesture the grip invites — otherwise
         starts WebView text selection on the title and pops the Android
         selection toolbar over the sheet, abandoning the drag. */
      user-select: none;
      -webkit-user-select: none;
      -webkit-touch-callout: none;
    }
    /* The pill grip — the universal "grab me and pull down" affordance. */
    .grip {
      display: block;
      width: 40px;
      height: 5px;
      margin: -8px auto 6px;
      border-radius: 999px;
      background: var(--border);
      flex: none;
      cursor: grab;
    }
    /* ≥16px inputs stop iOS auto-zoom on focus; ≥44px buttons are the
       touch-target floor. Reaches into each modal's own markup. */
    .dialog :global(input:not([type="checkbox"]):not([type="radio"])),
    .dialog :global(textarea),
    .dialog :global(select) {
      font-size: 16px;
    }
    /* The floor applied to buttons only, so the pickers and text fields a
       settings row hosts stayed 33-38px tall. */
    .dialog :global(button),
    .dialog :global(input:not([type="checkbox"]):not([type="radio"])),
    .dialog :global(select) {
      min-height: var(--tap-min);
    }
    /* …except a range slider, which app.css's text-input chrome had been
       giving a border, an inset shadow and 14px of side padding — the floor
       then stretched that box while the track inside stayed short. */
    .dialog :global(input[type="range"]) {
      min-height: var(--tap-min);
      padding: 0;
      border: none;
      background: transparent;
      box-shadow: none;
    }
    /* A commit button must never be stranded below a screen of content. Every
       modal's footer row shares this class — the profile sheet alone runs well
       past 1200px with Save at the very bottom — so pinning it here fixes all
       of them at once. The negative margins let the bar bleed to the sheet's
       edges and sit flush on the safe-area inset; they assume the row is the
       modal's own footer rather than something inside a padded card, which is
       what every use of this class in the app is. */
    .dialog :global(.actions) {
      position: sticky;
      bottom: calc(-20px - var(--safe-bottom));
      z-index: 2;
      margin: 8px -20px calc(-20px - var(--safe-bottom));
      padding: 10px 20px calc(10px + var(--safe-bottom));
      background: var(--bg-elevated);
      border-top: 1px solid var(--border);
    }
    /* …with one exception: the ⓘ beside a setting is a 14px glyph sitting IN a
       line of text, not a control on its own row. The floor stretched it into a
       14×44 ellipse straddling the lines above and below — every info dot in
       every settings panel looked broken on a phone. It still needs a thumb-
       sized target, so give it the area without the height: a transparent pad
       that reaches 44px in both directions and disturbs no layout. */
    .dialog :global(button.dot) {
      min-height: 0;
      position: relative; /* the pad below measures from the dot, not its wrapper */
    }
    .dialog :global(button.dot)::after {
      content: "";
      position: absolute;
      /* Horizontally it can reach the full 44px (14 + 2×15) — there is nothing
         beside it but its own label. Vertically it must NOT: a settings card
         separates rows by a 1px hairline, so a symmetric pad hung 15px into the
         rows above and below and stole taps meant for those switches. 12px
         keeps the pad inside a 44px row. */
      inset: -12px -15px;
    }
    /* Dismiss and go-back are the two controls every sheet has; they were the
       only ones that floor didn't reach (the close button opted out of
       min-height, and .back is sized by its padding). */
    .head .x {
      min-height: var(--tap-min);
      width: var(--tap-min);
      display: grid;
      place-items: center;
    }
    .head .back {
      min-width: var(--tap-min);
      display: grid;
      place-items: center;
    }
  }
  @keyframes sheet-up {
    from {
      transform: translateY(100%);
    }
  }
  /* Navigating the settings stack: a panel opened from another slides in from
     the right, going back slides in from the left, so depth reads as movement
     between places instead of one dialog blinking into another. Declared after
     both the desktop and mobile entrances so it wins over either — on a phone a
     stack should push sideways too, which is what the sheet is already doing
     underneath. */
  .dialog.deeper,
  .dialog.shallower {
    animation: panel-in 0.28s cubic-bezier(0.22, 1, 0.36, 1);
  }
  .dialog.deeper {
    --panel-from: 42px;
  }
  .dialog.shallower {
    --panel-from: -42px;
  }
  @keyframes panel-in {
    from {
      opacity: 0;
      transform: translateX(var(--panel-from)) scale(0.99);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .overlay,
    .dialog,
    .dialog.deeper,
    .dialog.shallower {
      animation: none;
    }
  }
  /* Keep the grip, title and close button visible while the body scrolls. */
  .sheet-top {
    position: sticky;
    top: -20px;
    margin: -20px -20px 0;
    padding: 20px 20px 8px;
    background: var(--bg-elevated);
    /* Comfortably above anything a modal's body might layer internally — at
       z-index 1 it tied with ordinary content and lost on DOM order, letting
       scrolled content slide over the title. */
    z-index: 3;
    flex: none;
  }
  .head {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  h3 {
    margin: 0;
    font-size: var(--fs-title);
    font-weight: 700;
    letter-spacing: 0.01em;
  }
  /* Back arrow sits before the title; title takes the slack so ✕ stays right. */
  .head h3 {
    margin-right: auto;
  }
  .back {
    display: grid;
    place-items: center;
    background: transparent;
    color: var(--text-muted);
    padding: 4px 6px;
    margin: 0 4px 0 -4px;
    border-radius: 8px;
    transition:
      color 0.15s ease,
      background 0.15s ease;
  }
  .back :global(svg) {
    transform: rotate(180deg);
  }
  .back:hover {
    color: var(--text);
    background: var(--bg-input);
  }
  .x {
    background: transparent;
    color: var(--text-muted);
    padding: 4px 8px;
    border-radius: 8px;
    transition:
      color 0.15s ease,
      background 0.15s ease,
      transform 0.2s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  .x:hover {
    color: var(--text);
    background: var(--bg-input);
    transform: rotate(90deg);
  }
  @media (prefers-reduced-motion: reduce) {
    .x {
      transition: none;
    }
  }
</style>
