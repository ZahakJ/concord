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

  // Who opened the panel, carried across a panel swap. A rail click or a
  // drill-down replaces one dialog component with another, so the incoming one
  // finds focus on a button inside the outgoing one (about to be detached) and
  // would remember THAT as its opener — and closing Settings after visiting a
  // second page dropped focus on <body>. The outgoing panel leaves its own
  // opener here; the incoming one takes it if it arrives within the same swap.
  let handoff = null;
</script>

<script>
  import { onDestroy, onMount, tick } from "svelte";
  import { S, modalNav, backPanel } from "../lib/state.svelte.js";
  import { layer } from "../lib/navstack.svelte.js";
  import { Z } from "../lib/layers.js";
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
  //
  // Sizes are the four of the one dialog frame: S 420 (a question, a rename),
  // M 560 (most forms), L 720 (lists and tables) and XL 900 (a workspace, and
  // every railed page). `wide` and `lg` are the older names for M and L, kept
  // so a caller that says them still gets the size it meant.
  let { title, subtitle = "", onClose, wide = false, size = "", rail = null, children } = $props();
  const railed = $derived(!!rail && !S.isMobile);
  const sz = $derived(
    size === "xl" ? "xl" : size === "lg" || size === "l" ? "l" : size === "m" || wide ? "m" : "s",
  );
  // A railed page on a phone is a page, not a sheet: Settings is somewhere you
  // go and come back from, and a 92% sheet over the shell read as a dialog
  // you could half-dismiss by brushing its top edge.
  const page = $derived(!!rail && S.isMobile);
  let dialog = $state(null);
  let titleEl = $state(null);
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
    const own = active instanceof HTMLElement && active !== document.body ? active : null;
    opener = own;
    // One tick, so a panel with its own opening focus (ConfirmDialog puts it on
    // Cancel, several fields carry autofocus) has already had its say. We only
    // place focus when nothing inside the dialog has claimed it.
    tick().then(() => {
      // By now the dialog this one replaced (if any) has been torn down and
      // has left its opener in `handoff`; the button we found focused at
      // mount was inside it and is gone.
      if (handoff && performance.now() - handoff.at < 1000 && (!opener || !opener.isConnected)) opener = handoff.el;
      handoff = null;
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
      //
      // With nothing to focus, the TITLE takes it, not the dialog box: the
      // box drew the focus ring around the entire dialog (a third of the
      // dialogs opened framed in accent, the rest did not), and a heading is
      // what a screen reader should read first anyway.
      const target =
        body.find((el) => el.hasAttribute("autofocus")) ||
        body.find((el) => !el.hasAttribute("data-no-initial-focus")) ||
        body[0] ||
        titleEl ||
        dialog;
      if (target === dialog) dialog.tabIndex = -1;
      target.focus?.({ preventScroll: true });
    });
  });

  // The S.modal this dialog was raised for. If a different one is current
  // when we are torn down, we were replaced (a rail click, a drill-down); if
  // the same one is, we were a confirm closing over the panel that stays.
  const shownFor = S.modal;

  // Closing for real drops the whole trail; navigating to another panel keeps
  // it. Which happened is simply whether a modal is still open by the time this
  // one is torn down.
  onDestroy(() => {
    if (!S.modal) S.modalStack = [];
    // Being replaced rather than closed: hand the opener to whoever replaces
    // us. A confirm closing over its panel is not a swap: S.modal is unchanged.
    const replaced = !!S.modal && S.modal !== shownFor;
    if (replaced && opener) handoff = { el: opener, at: performance.now() };
    // Hand focus back to whatever opened us — but only if nothing else has
    // claimed it in the meantime, and never on a swap: the replacement is
    // about to place focus inside itself, and restoring to the button behind
    // the scrim first would leave a keyboard user on the page underneath.
    const back = opener;
    opener = null;
    if (!back || replaced) return;
    tick().then(() => {
      const now = document.activeElement;
      if (now && now !== document.body && now !== document.documentElement) return;
      if (document.contains(back)) back.focus?.({ preventScroll: true });
    });
  });

  // The footer's hairline is a statement that there is more above it. On a
  // dialog whose body fits, a rule over the buttons is a line that separates
  // nothing, so it is drawn only while the scroller actually overflows.
  let scrolls = $state(false);
  $effect(() => {
    const box = railed ? dialog?.querySelector(":scope > .pane") : dialog;
    if (!box) return;
    const check = () => (scrolls = box.scrollHeight > box.clientHeight + 1);
    const ro = new ResizeObserver(check);
    const watch = () => {
      ro.disconnect();
      ro.observe(box);
      for (const el of box.children) ro.observe(el);
      check();
    };
    const mo = new MutationObserver(watch);
    mo.observe(box, { childList: true });
    watch();
    return () => {
      ro.disconnect();
      mo.disconnect();
    };
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

  // Focus that dies with the control holding it — a Back button that swaps
  // an editor for a list, a row deleted from under the caret — falls to
  // <body>, outside the dialog, where Tab starts from the top of the page and
  // the dialog's own keys stop answering. Catch it and keep it in the dialog.
  function onFocusOut(e) {
    if (e.relatedTarget) return;
    const lost = e.target;
    // Two frames: a layer closing inside the dialog (a picker) hands focus
    // back to its own opener one frame after it goes (lib/navstack), and that
    // is a better place than the title.
    requestAnimationFrame(() => requestAnimationFrame(() => {
      if (!dialog?.isConnected || lost.isConnected || !me.isInnermostOfKind) return;
      const now = document.activeElement;
      if (now && now !== document.body && now !== document.documentElement) return;
      (titleEl || dialog).focus({ preventScroll: true });
    }));
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
  style:z-index={Z.layer + me.index * 2}
  onclick={dismiss}
  role="presentation"
>
  <div
    bind:this={dialog}
    class="dialog {sz}"
    class:railed
    class:page
    class:scrolls
    class:lateral
    class:continuing
    class:deeper={enterDir === 1}
    class:shallower={enterDir === -1}
    onclick={(e) => e.stopPropagation()}
    onfocusout={onFocusOut}
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
          enabled: S.isMobile && !closing && !page,
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
            <button class="iconbtn back" onclick={backPanel} aria-label="Back" title="Back">
              <Icon name="chevron" size={16} />
            </button>
          {/if}
          <h3 id={titleId} tabindex="-1" bind:this={titleEl}>{title}</h3>
          <button class="iconbtn x" onclick={dismiss} aria-label="Close" title="Close · Esc">
            <Icon name="close" size={16} />
          </button>
        </div>
        {#if subtitle}<p class="subtitle">{subtitle}</p>{/if}
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
  /* ---- the one dialog frame -------------------------------------------------
     Every dialog in the app is this box: an opaque raised surface with one
     hairline edge and the one floating shadow, a 60px header (title left, ×
     right), a body inset by --dlg-pad, and — on a form — a footer pinned to
     the bottom with the actions on the trailing side. Four widths and nothing
     else, so seventy dialogs read as one family rather than seventy answers. */
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
    --dlg-pad: var(--sp-5);
    width: 420px;
    max-width: calc(100 * var(--vw) - var(--sp-6));
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
    border-radius: var(--radius-lg);
    padding: 0 var(--dlg-pad) var(--dlg-pad);
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    box-shadow: var(--shadow-pop);
    /* Never a ring round the whole dialog: focus goes to the first field, the
       primary or the title, never the box. */
    outline: none;
    animation: rise var(--dur-standard) var(--ease-spring);
  }
  .dialog.m {
    width: 560px;
  }
  .dialog.l {
    width: 720px;
  }
  /* A workspace (the advanced composer, the meme maker, a new post): the XL
     box, and a fixed height so its inner panes get real room instead of
     collapsing to content height. */
  .dialog.xl {
    --dlg-pad: var(--sp-6);
    width: 900px;
    height: min(700px, calc(90 * var(--vh)));
    height: min(700px, calc(90 * var(--dvh)));
  }
  /* ---- railed: one constant box, a rail down the left ----------------------
     Every settings page is exactly this size. Nothing here is negotiable by
     the content: a page that needs more room scrolls inside the pane, because
     a dialog that resizes itself to fit each page is a dialog that jumps under
     the pointer on every click, and that is the complaint this answers.
     The dialog stops being the scroller — the pane is — so the rail holds
     still while a long page moves beside it. */
  .dialog.railed {
    --dlg-pad: var(--sp-6);
    width: 900px;
    height: min(700px, calc(90 * var(--vh)));
    height: min(700px, calc(90 * var(--dvh)));
    max-height: none;
    padding: 0;
    gap: 0;
    display: grid;
    grid-template-columns: 224px minmax(0, 1fr);
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
    padding: 0 var(--dlg-pad) var(--dlg-pad);
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  /* The sticky footer and the scroll fade are written against the scroll box.
     Railed, that box is the pane, and the dialog itself has neither. */
  .dialog.railed::after {
    display: none;
  }
  .dialog.railed > .pane::after {
    content: "";
    flex: none;
    position: sticky;
    z-index: 1;
    bottom: calc(-1 * var(--dlg-pad));
    height: var(--sp-5);
    margin: 0 calc(-1 * var(--dlg-pad)) calc(-1 * var(--dlg-pad));
    pointer-events: none;
    background: linear-gradient(transparent, var(--bg-elevated));
  }
  @keyframes fade {
    from {
      opacity: 0;
    }
  }
  /* Arrives: a fade and an 8px rise, decelerating. No scale, no overshoot. */
  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(8px);
    }
  }
  .grip {
    display: none; /* desktop: no drag handle */
  }

  /* ---- header ---------------------------------------------------------------
     60px, pinned while the body scrolls: the title on the leading side in the
     display face, the × on the trailing side (and ‹ before the title when there
     is a pane to go back to). No icons and no art in a dialog's header. */
  .sheet-top {
    position: sticky;
    top: 0;
    margin: 0 calc(-1 * var(--dlg-pad));
    padding: 0 var(--sp-3) 0 var(--dlg-pad);
    background: var(--bg-elevated);
    /* Comfortably above anything a modal's body might layer internally — at
       z-index 1 it tied with ordinary content and lost on DOM order, letting
       scrolled content slide over the title. */
    z-index: 3;
    flex: none;
  }
  .head {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
    min-height: 60px;
  }
  /* Focus parks on the title only when a dialog has nothing else to focus or
     the control holding it went away; it is a place, not a control. */
  h3:focus {
    outline: none;
  }
  h3 {
    margin: 0 auto 0 0;
    min-width: 0;
    font-family: var(--display-font);
    font-size: calc(var(--fs-title) + 1px);
    font-weight: 600;
    letter-spacing: -0.01em;
    line-height: 1.25;
    overflow-wrap: anywhere;
  }
  /* An empty title (What's new names itself in its body) must not leave the
     × floating over a blank band of the same height. */
  h3:empty {
    flex: 1;
  }
  .subtitle {
    margin: -14px 0 var(--sp-3);
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  .back {
    margin-left: calc(-1 * var(--sp-2));
  }
  .back :global(svg) {
    transform: rotate(180deg);
  }
  .x {
    flex: none;
  }

  /* ---- footer ---------------------------------------------------------------
     A form's actions: pinned to the bottom of the scroller so a commit button
     is never stranded below a screen of content (at 1440x900 the poll
     composer's Create used to be sliced by the viewport edge), on the trailing
     side, Cancel before the one primary. The hairline above it appears only
     while the body actually scrolls; on a dialog that fits it would separate
     nothing. Every use of this class in the app is a modal's own footer. */
  .dialog :global(.actions) {
    position: sticky;
    bottom: calc(-1 * var(--dlg-pad));
    z-index: 2;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: var(--sp-2);
    margin: var(--sp-2) calc(-1 * var(--dlg-pad)) calc(-1 * var(--dlg-pad));
    padding: var(--sp-4) var(--dlg-pad);
    background: var(--bg-elevated);
    border-top: 1px solid transparent;
    border-radius: 0 0 var(--radius-lg) var(--radius-lg);
  }
  .dialog.scrolls :global(.actions) {
    border-top-color: var(--hairline);
  }
  /* Footer buttons are the md control: 32px, the same in every dialog. */
  .dialog :global(.actions > button) {
    min-height: 32px;
    padding-top: 0;
    padding-bottom: 0;
  }
  /* The scroll fade. A container that ends on a half-drawn row reads as broken
     rather than as scrollable, and Chromium's overlay scrollbar is invisible
     until something moves. It is a sticky pseudo-element rather than markup,
     it sits UNDER the footer's z-index 2 so a commit button is never washed
     out, and on a dialog with nothing to scroll the gradient lands on matching
     background and cannot be seen at all. */
  .dialog::after {
    content: "";
    flex: none;
    position: sticky;
    z-index: 1;
    bottom: calc(-1 * var(--dlg-pad));
    height: var(--sp-5);
    margin: 0 calc(-1 * var(--dlg-pad)) calc(-1 * var(--dlg-pad));
    pointer-events: none;
    background: linear-gradient(transparent, var(--bg-elevated));
  }

  /* ---- phone: every size is a sheet ------------------------------------------
     Full width from the bottom edge, thumb-reachable, with a grip; a railed
     dialog (Settings, the guild hub) is a full page instead. */
  @media (max-width: 768px) {
    .overlay {
      background: var(--scrim-sheet);
      place-items: end stretch;
      /* The bottom edge is the KEYBOARD's top edge, not the screen's. See the
         max-height note below for why dvh cannot answer this. */
      padding-bottom: var(--kb, 0px);
    }
    /* Every size collapses to the sheet; the size classes are listed so none
       of them can pin a desktop width. */
    .dialog,
    .dialog.m,
    .dialog.l,
    .dialog.xl,
    .dialog.railed {
      --dlg-pad: var(--sp-4);
      width: auto;
      max-width: none;
      height: auto;
      display: flex;
      padding: 0 var(--dlg-pad) var(--dlg-pad);
      gap: var(--sp-3);
      overflow-y: auto;
      /* dvh DOES NOT SHRINK FOR THE KEYBOARD on the phone this app ships to.
         Measured on the device with the IME up: 100vh, 100dvh, 100svh, 100lvh
         and visualViewport.height are all 915px, the full screen, while the
         native bridge reports --kb: 336px. The activity draws edge-to-edge with
         insetsHandling disabled, so the WebView is never resized and every
         viewport unit keeps describing a screen a third of which the user
         cannot see. That is the whole reason --kb exists. The vh line stays
         as the fallback for engines without dvh. */
      max-height: calc(92 * var(--vh));
      max-height: calc(92 * var(--dvh) - var(--kb, 0px));
      border: none;
      border-radius: var(--radius-sheet) var(--radius-sheet) 0 0;
      padding-bottom: calc(var(--dlg-pad) + var(--safe-bottom));
      animation: sheet-up 0.24s var(--ease-spring);
      touch-action: pan-y;
    }
    /* A page: the whole screen, under the status bar, no grip and no rounded
       top — it is pushed, and ‹ / × / the system back are the ways out. */
    .dialog.page {
      height: calc(100 * var(--dvh) - var(--kb, 0px));
      max-height: none;
      border-radius: 0;
      padding-top: var(--safe-top);
    }
    .dialog.page .grip {
      display: none;
    }
    .dialog.page .sheet-top {
      top: calc(-1 * var(--safe-top));
      margin-top: calc(-1 * var(--safe-top));
      padding-top: var(--safe-top);
    }
    /* Long content ran out from under the last line and straight behind the
       gesture pill, with nothing to say it continued. */
    .dialog::after {
      bottom: calc(-1 * var(--dlg-pad) - var(--safe-bottom));
      height: calc(var(--sp-5) + var(--safe-bottom));
      margin: 0 calc(-1 * var(--dlg-pad)) calc(-1 * var(--dlg-pad) - var(--safe-bottom));
    }
    /* The drag, the spring-back and the slide-out are all driven from
       lib/sheet.js as inline styles. */
    .sheet-top {
      /* touch-action is written by lib/sheet.js, which keeps it in step with
         the body's scroll position: `none` at the top so the strip owns the
         drag, `pan-y` once scrolled so the header scrolls like any other area. */
      touch-action: none;
      /* A slow grab-and-pull — the exact gesture the grip invites — otherwise
         starts WebView text selection on the title and pops the Android
         selection toolbar over the sheet, abandoning the drag. */
      user-select: none;
      -webkit-user-select: none;
      -webkit-touch-callout: none;
      padding-right: var(--sp-1);
    }
    .head {
      min-height: 52px;
    }
    /* The pill grip — the universal "grab me and pull down" affordance. */
    .grip {
      display: block;
      width: 36px;
      height: 4px;
      margin: var(--sp-2) auto 0;
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
    /* A colour well is excluded for the same reason the checkbox is: it is a
       swatch, not a field, and 48px of it is a lozenge. */
    .dialog :global(button),
    .dialog
      :global(input:not([type="checkbox"]):not([type="radio"]):not([type="color"])),
    .dialog :global(select) {
      min-height: var(--tap-min);
    }
    .dialog :global(input[type="range"]) {
      min-height: var(--tap-min);
    }
    /* The footer on a sheet: flush on the safe-area inset, the full width. */
    .dialog :global(.actions) {
      bottom: calc(-1 * var(--dlg-pad) - var(--safe-bottom));
      margin: var(--sp-2) calc(-1 * var(--dlg-pad)) calc(-1 * var(--dlg-pad) - var(--safe-bottom));
      padding: 10px var(--dlg-pad) calc(10px + var(--safe-bottom));
      border-radius: 0;
    }
    .dialog :global(.actions > button) {
      min-height: var(--tap-min);
    }
    /* …with one exception: the ⓘ beside a setting is a 14px glyph sitting IN a
       line of text, not a control on its own row. It still needs a thumb-
       sized target, so give it the area without the height: a transparent pad
       that reaches 44px across and stays inside a 44px row. */
    .dialog :global(button.dot) {
      min-height: 0;
      position: relative; /* the pad below measures from the dot, not its wrapper */
    }
    .dialog :global(button.dot)::after {
      content: "";
      position: absolute;
      inset: -12px -15px;
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
     both the desktop and mobile entrances so it wins over either. */
  .dialog.deeper,
  .dialog.shallower {
    animation: panel-in var(--dur-standard) var(--ease-out);
  }
  .dialog.deeper {
    --panel-from: 24px;
  }
  .dialog.shallower {
    --panel-from: -24px;
  }
  @keyframes panel-in {
    from {
      opacity: 0;
      transform: translateX(var(--panel-from));
    }
  }
  /* A step sideways along the rail: nothing on the outside of the dialog moves.
     Declared last so it beats every entrance above it. */
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
</style>
