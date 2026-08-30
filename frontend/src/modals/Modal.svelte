<script module>
  // What can hold focus inside a dialog. `[tabindex="-1"]` is excluded on
  // purpose: it is how a thing is made programmatically focusable but kept out
  // of the tab order, which is exactly what the dialog container itself uses.
  const FOCUSABLE =
    'a[href],button:not([disabled]),textarea:not([disabled]),' +
    'input:not([disabled]):not([type="hidden"]),select:not([disabled]),' +
    '[tabindex]:not([tabindex="-1"])';

  const visible = (el) => !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);

  // How many dialogs are on screen. A stamp on the document is what lets the
  // app behind them read as inert (app.css, [data-modal]) without every dialog
  // having to reach for the shell element itself — and it is a COUNT because
  // panels stack: the second one mounting must not clear the flag when the
  // first unmounts underneath it. The stamp dims; it never MOVES the app.
  let openCount = 0;
  export function markOpen() {
    openCount++;
    // Only stamp on the first dialog. Re-setting data-modal while it is
    // already there restarts html[data-modal]::before's box in some engines,
    // which is a whole-screen dim pulse on every settings-rail click.
    if (openCount === 1) document.documentElement.dataset.modal = "";
  }
  export function markClosed() {
    openCount = Math.max(0, openCount - 1);
    if (!openCount) delete document.documentElement.dataset.modal;
  }
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
  //
  // `rail` is a snippet drawn down the left of the dialog, permanently, with
  // the content beside it — what the settings surface uses so that going from
  // Appearance to Connection changes the pane and nothing else. A dialog with
  // a rail has ONE size for every page it can show; the whole point is that
  // the box under the cursor stops resizing on every navigation. On a phone
  // there is no room for it and the drill-down list is the right shape anyway,
  // so the rail is dropped and this is the dialog it has always been.
  let { title, onClose, wide = false, size = "", rail = null, children } = $props();
  const railed = $derived(!!rail && !S.isMobile);
  let dialog = $state(null);
  // TalkBack/VoiceOver announce a dialog by its label; without an id to point
  // aria-labelledby at, every sheet in the app opened as an unnamed group.
  const titleId = "modal-title-" + Math.random().toString(36).slice(2, 9);

  // `me` is this dialog's place on the app-wide layer stack. Registering at
  // component init rather than in an effect keeps creation order and stack
  // order the same thing, which is what makes a confirm raised from inside a
  // settings panel — six places do it — answer Escape on its own without also
  // closing the panel that asked the question.
  //
  // What goes on the stack is `unwind`, not `onClose`: Escape and the phone's
  // back button have to mean exactly what the header's own ‹ and ✕ mean, and on
  // a settings sub-panel that is ‹, one rung at a time. Before this, Escape in
  // Settings → Privacy → confirm answered the confirm correctly and then, on
  // the next press, threw the whole dialog away and dropped you in the chat
  // pane — because a panel that replaces another leaves the layer stack one
  // deep no matter how far in you are, so the only thing left to pop was the
  // dialog itself.
  const me = layer("modal", () => unwind());

  // Back is offered whenever there's somewhere to go back TO — either a panel
  // on the stack we drilled through, or a plain `from` on a panel opened
  // directly. Only the PANEL may offer it: `S.modalStack` is global, so a
  // confirmation raised from inside Settings → Privacy used to draw a ‹ in its
  // own header that unwound the panel underneath it and took the question with
  // it. The panel is the outermost dialog; anything above it is its own.
  const isPanel = $derived(!me.hasOtherOfKind);
  const canBack = $derived(isPanel && (S.modalStack.length > 0 || !!S.modal?.from));

  // Settings and its sub-panels read as one stack you move through, not a pile
  // of unrelated dialogs: a panel opened from another slides in from the right,
  // and going back slides in from the left. Read once at mount (a CSS animation
  // only runs then) and consumed, so the next open starts from a clean slate.
  const enterDir = modalNav.dir || (S.modal?.from ? 1 : 0);
  modalNav.dir = 0;
  // A step sideways along a rail. The dialog is torn down and rebuilt (each
  // settings page is its own component), so without this the scrim re-fades and
  // the card re-pops on every rail click — the box flashing and jumping under
  // the cursor, which is the exact thing the rail exists to stop. Read once and
  // consumed, like `dir`.
  const lateral = modalNav.lateral;
  modalNav.lateral = false;
  // A rail click holds the previous dialog until this one is constructed, so
  // openCount is already > 0. Treat that the same as `lateral`: the overlay
  // must not fade and the card must not pop, or the persistent dim is the
  // only thing on screen for a beat and the click reads as "the page darkened".
  const continuing = lateral || openCount > 0;

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
      //
      // And never a help affordance. Four settings panels open with a group
      // heading that carries an ⓘ, which is therefore the first focusable thing
      // in the dialog — so "focus the first control" landed on the one button
      // whose entire job is to explain something, and the panel appeared with a
      // tooltip already up over its own first row. A dot is not a control; it
      // is an aside about one, and it is never the right place to start.
      //
      // `data-no-initial-focus` is the same idea for a field that would look
      // selected if the caret landed there on open (Members' search). It stays
      // in the Tab order; it is just not the place the panel starts.
      //
      // And never the rail. It is the navigation AROUND the page, permanently
      // on screen; opening Settings and finding the caret parked on "Account"
      // with a focus ring round it reads as a selection somebody made, not as a
      // place to start typing. Focus belongs in the page the rail points at.
      const body = S.isMobile
        ? []
        : [...dialog.querySelectorAll(FOCUSABLE)].filter(
            (el) =>
              !el.closest(".sheet-top") &&
              !el.closest("[data-nav-rail]") &&
              !el.hasAttribute("data-help-affordance") &&
              visible(el),
          );
      // A panel that names its own first field wins over DOM order. The create-
      // channel dialog leads with four channel-TYPE buttons and follows them
      // with the name box; first-in-the-tree would put the caret on "Text" and
      // leave the one thing you came here to type unfocused.
      const target =
        body.find((el) => el.hasAttribute("autofocus")) ||
        body.find((el) => !el.hasAttribute("data-no-initial-focus")) ||
        body[0] ||
        dialog;
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

  // The push-back. Announced here rather than in each dialog because "a dialog
  // is open" is a fact about the app, not about this component.
  //
  // Opened at init, closed on a microtask: a rail click destroys this dialog
  // and constructs its sibling in the same flush. Closing the stamp
  // synchronously would drop [data-modal] (and the document-level dim it
  // carries) for a frame, which is the whole-screen flash a settings click
  // used to be. The sibling increments first; the microtask then decrements,
  // and the count never hits zero.
  markOpen();
  onDestroy(() => queueMicrotask(markClosed));

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

  // What Escape and the phone's back button do. One rung of the panel trail if
  // there is one, otherwise close for real — the same two behaviours the header
  // already offers as ‹ and ✕, so the keyboard cannot disagree with the buttons.
  function unwind() {
    if (canBack) backPanel();
    else dismiss();
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
  class:lateral
  class:continuing
  class:stacked={!continuing && me.isInnermostOfKind && me.hasOtherOfKind}
  data-modal-overlay
  style:z-index={100 + me.index * 2}
  onclick={dismiss}
  role="presentation"
>
  <div
    bind:this={dialog}
    class="dialog"
    class:wide
    class:xl={size === "xl"}
    class:lg={size === "lg"}
    class:railed
    class:lateral
    class:continuing
    class:deeper={enterDir === 1}
    class:shallower={enterDir === -1}
    onclick={(e) => e.stopPropagation()}
    role="dialog"
    aria-modal="true"
    aria-labelledby={titleId}
  >
    {#snippet body()}
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
    {/snippet}
    {#if railed}
      {@render rail()}
      <!-- The scroller is the pane, not the dialog: the rail must stay put
           while a long page moves under it. The house fade goes here for the
           same reason: 3,015px of Appearance in a 658px pane ended in a razor
           cut through the middle of a theme tile, with the Done footer's
           hairline under it and nothing to say the page continued. Notifications
           was cut through its alert-word chips. One class, both pages, and
           every railed page after them. -->
      <div class="pane scroll-fade">{@render body()}</div>
    {:else}
      {@render body()}
    {/if}
  </div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    /* The dim is on this overlay again. html[data-modal]::before is only a
       gap-filler for the frame between tearing one dialog down and building
       the next — it hides the moment an overlay is in the tree, so the two
       never stack into a darker pulse. */
    background: var(--scrim);
    display: grid;
    place-items: center;
    z-index: 100;
    animation: fade var(--dur-standard) ease;
  }
  .dialog {
    width: 380px;
    max-width: calc(90 * var(--vw));
    /* Never taller than the viewport; scroll inside on short screens (laptops)
       so long content like the 24-word recovery phrase stays reachable. dvh
       tracks the viewport the soft keyboard actually leaves behind; the plain
       vh line before it is the fallback for WebViews that don't know dvh. */
    max-height: calc(90 * var(--vh));
    max-height: calc(90 * var(--dvh));
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
    gap: var(--sp-3);
    box-shadow: var(--shadow-pop);
    /* A touch of spring on entry — overshoots ~1% then settles. */
    animation: pop 0.26s var(--ease-spring);
  }
  .dialog.wide {
    width: 460px;
  }
  /* Create-a-guild: wide enough for the four template tiles in one row, short
     enough that the dialog is not 86% of the window. */
  .dialog.lg {
    width: min(860px, 94 * var(--vw));
  }
  /* A large workspace (the advanced composer): most of the viewport, and a
     fixed tall height so its inner panes get real room instead of collapsing to
     content height. */
  .dialog.xl {
    width: min(1080px, 94 * var(--vw));
    height: min(780px, 88 * var(--vh));
    height: min(780px, 88 * var(--dvh));
  }
  /* ---- railed: one constant box, a rail down the left ----------------------
     Every settings page is exactly this size. Nothing here is negotiable by
     the content: a page that needs more room scrolls inside the pane, because
     a dialog that resizes itself to fit each page is a dialog that jumps under
     the pointer on every click, and that is the complaint this answers.
     The dialog stops being the scroller — the pane is — so the rail holds
     still while a long page moves beside it. */
  .dialog.railed {
    width: min(1000px, 94 * var(--vw));
    height: min(660px, 86 * var(--vh));
    height: min(660px, 86 * var(--dvh));
    max-height: none;
    padding: 0;
    gap: 0;
    display: grid;
    grid-template-columns: 226px minmax(0, 1fr);
    overflow: hidden;
  }
  /* Below this the rail keeps its icons and drops its words rather than eating
     the page it is meant to be navigating. See SettingsRail's own query. */
  @media (max-width: 920px) {
    .dialog.railed {
      grid-template-columns: auto minmax(0, 1fr);
    }
  }
  .dialog.railed > .pane {
    min-width: 0;
    overflow-y: auto;
    overflow-x: hidden;
    overscroll-behavior: contain;
    padding: 20px;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  /* The sticky footer and the scroll fade are written against the dialog's own
     20px padding and its own scroll box. Railed, both of those belong to the
     pane, and the dialog itself has neither. */
  .dialog.railed::after {
    display: none;
  }
  .dialog.railed > .pane::after {
    content: "";
    flex: none;
    position: sticky;
    z-index: 1;
    bottom: -20px;
    height: 24px;
    margin: 0 -20px -20px;
    pointer-events: none;
    background: linear-gradient(transparent, var(--bg-elevated));
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
  /* A commit button must never be stranded below a screen of content, and that
     is as true on a laptop as on a phone. The sheet presentation below has
     pinned this row since it was written; the desktop card did not, and
     `.dialog` is max-height: 90dvh with overflow-y: auto — so at 1440x900 the
     poll composer's Cancel/Create pair was sliced by the viewport edge and Your
     profile kept Save 650px down, with nothing on screen saying either existed.
     Same selector as the sheet's, and for the same stated reason: every use of
     this class in the app IS a modal's own footer. The negative margins bleed
     it to the card's edges; the hairline reads as a footer rule on the short
     dialogs where nothing scrolls at all, and the soft lift above it is what
     says there is more on the ones that do. */
  .dialog :global(.actions) {
    position: sticky;
    bottom: -20px;
    z-index: 2;
    margin: var(--sp-2) -20px -20px;
    padding: var(--sp-3) 20px 20px;
    background: var(--bg-elevated);
    border-top: 1px solid var(--hairline);
    box-shadow: 0 -10px 16px -14px var(--scrim);
  }
  /* A disabled commit button must not be the accent at half strength. app.css
     fills a bare button with --accent and dims disabled ones to opacity .5,
     which on a footer's primary reads as a paint fault — a washed-out purple
     bleeding the dialog through itself — rather than as "not yet". A flat
     surface with faint text is the same sentence said properly. Scoped to the
     footer, which is the one place in the app where a filled primary is
     routinely disabled. */
  .dialog :global(.actions button:disabled) {
    opacity: 1;
    background: var(--bg-3);
    color: var(--text-faint);
    box-shadow: none;
  }
  /* The scroll fade, on the desktop card too. A container that ends on a
     half-drawn row reads as broken rather than as scrollable — the command
     palette sliced "Saved messages" through the middle, the forward dialog cut
     #ci-and-builds in half — and Chromium's overlay scrollbar is invisible
     until something moves, so there is nothing else to say it. It is a sticky
     pseudo-element rather than markup, it sits UNDER the footer's z-index 2 so
     a commit button is never washed out, and on a dialog with nothing to scroll
     the gradient lands on matching background and cannot be seen at all. */
  .dialog::after {
    content: "";
    flex: none;
    position: sticky;
    z-index: 1;
    bottom: -20px;
    height: 24px;
    margin: 0 -20px -20px;
    pointer-events: none;
    background: linear-gradient(transparent, var(--bg-elevated));
  }
  /* Mobile: dialogs present as full-width bottom sheets instead of floating
     cards — thumb-reachable, roomy, and keyboard-friendly. Desktop (fine
     pointer + wide viewport) is untouched. */
  @media (pointer: coarse), (max-width: 768px) {
    .overlay {
      place-items: end stretch;
      /* The bottom edge is the KEYBOARD's top edge, not the screen's. See the
         max-height note below for why dvh cannot answer this. */
      padding-bottom: var(--kb, 0px);
    }
    /* All variants collapse to the bottom-sheet presentation; higher-specificity
       selectors (`wide`, `xl`) must be listed so they can't pin a desktop size. */
    .dialog,
    .dialog.wide,
    .dialog.lg,
    .dialog.xl,
    .dialog.railed {
      width: auto;
      max-width: none;
      height: auto;
      display: flex;
      padding: 20px;
      gap: var(--sp-3);
      overflow-y: auto;
      /* dvh DOES NOT SHRINK FOR THE KEYBOARD on the phone this app ships to.
         Measured on the device with the IME up: 100vh, 100dvh, 100svh, 100lvh
         and visualViewport.height are all 915px, the full screen, while the
         native bridge reports --kb: 336px. The activity draws edge-to-edge with
         insetsHandling disabled, so the WebView is never resized and every
         viewport unit keeps describing a screen a third of which the user
         cannot see. That is the whole reason --kb exists.
         So: subtract it. Before this, opening the import wizard and tapping its
         one text field put that field 35 of its 48 pixels underneath the
         keyboard — the sheet was still claiming 92% of the WHOLE screen and
         sitting on the bottom edge of it. The vh line stays as the fallback for
         engines without dvh, where the keyboard does resize the viewport. */
      max-height: calc(92 * var(--vh));
      max-height: calc(92 * var(--dvh) - var(--kb, 0px));
      border: none;
      border-radius: var(--radius-sheet) var(--radius-sheet) 0 0;
      padding-bottom: calc(20px + var(--safe-bottom));
      animation: sheet-up 0.28s var(--ease-spring);
      touch-action: pan-y;
    }
    /* Long content ran out from under the last line and straight behind the
       gesture pill, with nothing to say it continued. This is a sticky strip at
       the foot of the scroller — a pseudo-element, so no markup and no second
       scroller — fading content into the sheet's own colour just above the
       safe-area inset. A sticky footer (.actions, below) sits at z-index 2 and
       draws over it, so a commit button is never washed out; on a sheet with
       nothing to scroll the gradient lands on matching background and cannot be
       seen at all. */
    .dialog::after {
      content: "";
      flex: none;
      position: sticky;
      z-index: 1;
      bottom: calc(-20px - var(--safe-bottom));
      height: calc(26px + var(--safe-bottom));
      margin: 0 -20px calc(-20px - var(--safe-bottom));
      pointer-events: none;
      background: linear-gradient(transparent, var(--bg-elevated));
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
    .dialog
      :global(input:not([type="checkbox"]):not([type="radio"]):not([type="color"])),
    .dialog :global(textarea),
    .dialog :global(select) {
      font-size: 16px;
    }
    /* The floor applied to buttons only, so the pickers and text fields a
       settings row hosts stayed 33-38px tall. A colour well is excluded for the
       same reason the checkbox is: it is a swatch, not a field, and 48px of it
       is a lozenge. app.css sizes it wide enough to be a tap target instead. */
    .dialog :global(button),
    .dialog
      :global(input:not([type="checkbox"]):not([type="radio"]):not([type="color"])),
    .dialog :global(select) {
      min-height: var(--tap-min);
    }
    /* …except a range slider, which app.css draws at 20px so the thumb clears
       the groove. The tap floor is still owed on a phone; everything else the
       exception used to restore is now the base rule's job. */
    .dialog :global(input[type="range"]) {
      min-height: var(--tap-min);
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
    animation: panel-in 0.28s var(--ease-out);
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
  /* A step sideways along the rail: nothing on the outside of the dialog moves.
     Only the page changes, and it says so with a short lift — enough to read as
     "this is a different page", nowhere near enough to be a transition you have
     to wait out. Declared last so it beats every entrance above it. */
  .overlay.lateral,
  .overlay.continuing,
  .dialog.lateral,
  .dialog.continuing,
  .dialog.lateral.deeper,
  .dialog.lateral.shallower,
  .dialog.continuing.deeper,
  .dialog.continuing.shallower {
    animation: none;
  }
  .dialog.lateral > .pane,
  .dialog.continuing > .pane {
    animation: none;
  }
  /* Groups on a railed page used to stagger in on every rail click — opacity 0
     plus a 6px lift, which read as the whole pane flashing. First open still
     has the dialog's own pop; after that the page is just there. */
  .dialog.railed :global(.grp) {
    animation: none;
  }
  @media (prefers-reduced-motion: reduce) {
    .overlay,
    .dialog,
    .dialog.deeper,
    .dialog.shallower,
    .dialog.lateral > .pane {
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
    border-radius: var(--radius-md);
    transition:
      color var(--dur-standard) ease,
      background var(--dur-standard) ease;
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
    padding: var(--sp-1) var(--sp-2);
    border-radius: var(--radius-md);
    transition:
      color var(--dur-standard) ease,
      background var(--dur-standard) ease,
      transform var(--dur-calm) var(--ease-spring);
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
