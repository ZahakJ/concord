<script>
  // The "your call is still running over there" indicator, in two shapes.
  //
  // On a desktop it is a compact draggable window pinned over the chat, because
  // there is spare screen to park it on and a mouse to park it with.
  //
  // On a phone it is a full-width bar docked under the top bar — the strip every
  // native OS shows during a call, where the whole thing is "tap to go back".
  // The draggable window was actively hostile there: the app is one pane at a
  // time, so this IS the call UI most of the time, yet its return gesture was a
  // double-click (a gesture phones don't have, on a header that starts a drag on
  // every touch), and it could be parked over the top bar's buttons or dragged
  // until its own Mute/Leave row was below the bottom of the screen.
  import Avatar from "./Avatar.svelte";
  import Icon from "./Icon.svelte";
  import { S, memberByFpr } from "./lib/state.svelte.js";
  import { haptic } from "./lib/touch.js";
  import { callClock } from "./lib/calltimer.svelte.js";
  import { canShareScreen } from "./lib/devices.js";

  let {
    label = "",
    away = true,
    onLeave,
    onToggleMute,
    onToggleDeafen,
    onToggleShare,
    onToggleCamera,
    onReturn,
  } = $props();

  const clock = $derived(callClock());

  // Same test the CSS phone query makes, so the two never disagree about which
  // shape is on screen.
  const phone = $derived(S.isMobile);

  // Desktop dock position, clamped to the viewport, draggable — and REMEMBERED.
  // It used to be plain component state seeded at top-right, so every drag was
  // undone by the next look at the voice channel (which unmounted the dock) and
  // by every reload. Where you put a thing that floats over your work is a
  // preference, and it is per-device, so it lives in localStorage rather than
  // in the account.
  const POS_KEY = "concord.dockPos";
  function savedPos() {
    try {
      const p = JSON.parse(localStorage.getItem(POS_KEY) || "null");
      if (p && Number.isFinite(p.x) && Number.isFinite(p.y)) return p;
    } catch {
      /* absent or corrupt — fall through to the default corner */
    }
    return null;
  }
  let pos = $state(savedPos() || { x: Math.max(12, window.innerWidth - 250), y: 70 });
  let dockEl = $state(null);
  let drag = null;
  let dragging = $state(false); // lifts the dock visually while it moves

  // Clamp against the dock's REAL height, not a guessed margin: the old
  // constant let the bottom of the widget — the row holding Mute and Leave —
  // hang below the viewport, and the only recovery was dragging it back by a
  // header you could no longer see the point of.
  function clamp(x, y) {
    const w = dockEl?.offsetWidth || 214;
    const h = dockEl?.offsetHeight || 160;
    return {
      x: Math.max(8, Math.min(window.innerWidth - w - 8, x)),
      y: Math.max(8, Math.min(window.innerHeight - h - 12, y)),
    };
  }
  function onDown(e) {
    drag = { dx: e.clientX - pos.x, dy: e.clientY - pos.y, x: e.clientX, y: e.clientY };
    dragging = true;
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    // A touch drag the browser decides is a scroll ends in pointercancel, never
    // pointerup: without this the dock stayed "held" and every later touch
    // anywhere on screen teleported it.
    window.addEventListener("pointercancel", onUp);
  }
  function onMove(e) {
    if (drag) pos = clamp(e.clientX - drag.dx, e.clientY - drag.dy);
  }
  function onUp(e) {
    // A press that didn't move is a click on the header, and a click on the
    // header means "take me back to the call" — the old double-click was both
    // undiscoverable and, on a trackpad, easy to miss.
    if (drag && e?.type === "pointerup" && Math.hypot(e.clientX - drag.x, e.clientY - drag.y) < 5) {
      onReturn?.();
    }
    if (drag) remember();
    drag = null;
    dragging = false;
    window.removeEventListener("pointermove", onMove);
    window.removeEventListener("pointerup", onUp);
    window.removeEventListener("pointercancel", onUp);
  }
  function remember() {
    try {
      localStorage.setItem(POS_KEY, JSON.stringify(pos));
    } catch {
      /* private mode or a full quota — the dock still works, it just forgets */
    }
  }
  // Keep the dock on-screen when the window is resized smaller (or rotated).
  function onResize() {
    pos = clamp(pos.x, pos.y);
  }
  // Clamp the REMEMBERED position against this window on the way in: a dock
  // parked at the right edge of a 2560px monitor is off-screen on a laptop.
  // Only written when it actually moves — `clamp` mints a fresh object every
  // call, and assigning one unconditionally in an effect that reads `pos` is an
  // effect that re-runs itself forever.
  $effect(() => {
    if (phone || !dockEl) return;
    const c = clamp(pos.x, pos.y);
    if (c.x !== pos.x || c.y !== pos.y) pos = c;
  });

  // The mobile drawers carry their own call bar and this would float over the
  // channel rows they slide in, so it steps aside (hidden, not unmounted —
  // remounting would throw away wherever the user parked it). The same applies
  // when the call's own channel is on screen: the panel there has every control
  // this does, but unmounting is what lost the parked position.
  const shelved = $derived(!away || (S.isMobile && (S.drawerOpen || S.membersOpen)));

  const roster = $derived(["self", ...S.voiceParticipants]);
  function part(pid) {
    if (pid === "self") {
      return {
        name: S.displayName || "You",
        emoji: S.identity.emoji,
        color: S.identity.color,
        image: S.identity.avatar,
        speaking: S.voiceSpeaking.includes("self"),
      };
    }
    const fpr = S.voicePeerFpr[pid];
    const m = fpr ? memberByFpr(fpr) : null;
    return {
      // Never the raw peer id: until the fingerprint lands there is no name to
      // show, and a hex string in a name's place reads as a crash rather than
      // as somebody arriving. Same rule as VoicePanel's participant().
      name: m?.name || (fpr ? fpr.slice(0, 9) : "Joining…"),
      emoji: m?.emoji || "",
      color: m?.color || "",
      image: m?.avatar || "",
      speaking: S.voiceSpeaking.includes(pid),
    };
  }

  // Where the phone bar sits: directly under the mobile top bar, measured rather
  // than assumed. The bar is 52px plus var(--safe-top), and that inset
  // is 24-48px on an edge-to-edge Android and 47-59px on iOS — the old constant
  // 70px default parked this widget squarely on top of it, covering the Members
  // and ⋯ buttons.
  let topOffset = $state(0);
  function measureTop() {
    const bar = document.querySelector(".mtopbar");
    topOffset = bar ? Math.round(bar.getBoundingClientRect().bottom) : 52;
  }
  $effect(() => {
    if (!phone) return;
    measureTop();
    // The drawer closing, a keyboard opening and an orientation change all move
    // it; re-measuring is cheap next to getting it wrong.
    const t = setTimeout(measureTop, 250);
    return () => clearTimeout(t);
  });

  const tap =
    (fn, style = "light") =>
    (e) => {
      e?.stopPropagation();
      haptic(style);
      fn?.();
    };
</script>

<svelte:window onresize={phone ? measureTop : onResize} />

{#if phone}
  <div class="callbar" class:shelved style="top:{topOffset}px">
    <button class="cb-open" onclick={onReturn} aria-label="Return to the call">
      <span class="live"></span>
      <span class="cb-text">
        <span class="cb-lbl">{label || "In call"}</span>
        <span class="cb-hint">{clock ? `${clock} · tap to return` : "Tap to return"}</span>
      </span>
    </button>
    <button
      class="ico"
      class:on={S.muted}
      title={S.muted ? "Unmute" : "Mute"}
      aria-label={S.muted ? "Unmute" : "Mute"}
      aria-pressed={S.muted}
      onclick={tap(onToggleMute)}
    >
      <Icon name={S.muted ? "micOff" : "mic"} size={17} />
    </button>
    <button
      class="ico"
      class:on={S.deafened}
      title={S.deafened ? "Undeafen" : "Deafen"}
      aria-label={S.deafened ? "Undeafen" : "Deafen"}
      aria-pressed={S.deafened}
      onclick={tap(onToggleDeafen)}
    >
      <Icon name={S.deafened ? "deafened" : "speaker"} size={17} />
    </button>
    <button class="ico hang" title="Leave call" aria-label="Leave call" onclick={tap(onLeave, "heavy")}>
      <Icon name="door" size={17} />
    </button>
  </div>
{:else}
  <div
    class="dock"
    class:dragging
    class:shelved
    bind:this={dockEl}
    style="left:{pos.x}px; top:{pos.y}px"
  >
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="head" onpointerdown={onDown} title="Drag to move · click to open">
      <span class="live"></span>
      <span class="lbl">{label || "In call"}</span>
      {#if clock}<span class="clock">{clock}</span>{/if}
      <button
        class="ico expand"
        title="Return to call"
        aria-label="Return to call"
        onpointerdown={(e) => e.stopPropagation()}
        onclick={onReturn}
      >
        <Icon name="chevron" size={14} />
      </button>
    </div>

    <div class="faces">
      {#each roster as pid (pid)}
        {@const p = part(pid)}
        <div class="face" class:speaking={p.speaking} title={p.name}>
          <Avatar name={p.name} emoji={p.emoji} color={p.color} image={p.image} size={30} />
        </div>
      {/each}
    </div>

    <!-- The two you most often reach for mid-conversation live here too. Before
         this the dock could only mute, deafen and hang up, so wandering into
         another channel to paste a link and then wanting to put your screen up
         meant navigating back to the call first. -->
    <div class="ctl">
      <button class="ico" class:on={S.muted} title={S.muted ? "Unmute" : "Mute"} aria-label={S.muted ? "Unmute" : "Mute"} aria-pressed={S.muted} onclick={onToggleMute}>
        <Icon name={S.muted ? "micOff" : "mic"} size={15} />
      </button>
      <button class="ico" class:on={S.deafened} title={S.deafened ? "Undeafen" : "Deafen"} aria-label={S.deafened ? "Undeafen" : "Deafen"} aria-pressed={S.deafened} onclick={onToggleDeafen}>
        <Icon name={S.deafened ? "deafened" : "speaker"} size={15} />
      </button>
      <button
        class="ico"
        class:lit={S.cameraOn}
        title={S.cameraOn ? "Turn off camera" : "Turn on camera"}
        aria-label={S.cameraOn ? "Turn off camera" : "Turn on camera"}
        aria-pressed={S.cameraOn}
        onclick={onToggleCamera}
      >
        <Icon name={S.cameraOn ? "cameraOff" : "camera"} size={15} />
      </button>
      {#if canShareScreen}
        <button
          class="ico"
          class:lit={S.sharing}
          title={S.sharing ? "Stop sharing" : "Share screen"}
          aria-label={S.sharing ? "Stop sharing" : "Share screen"}
          aria-pressed={S.sharing}
          onclick={onToggleShare}
        >
          <Icon name={S.sharing ? "screenOff" : "screen"} size={15} />
        </button>
      {/if}
      <button class="ico hang" title="Leave call" aria-label="Leave call" onclick={onLeave}>
        <Icon name="door" size={15} />
      </button>
    </div>
  </div>
{/if}

<style>
  /* ---- phone: a docked call strip ---- */
  .callbar {
    position: fixed;
    left: 0;
    right: 0;
    z-index: 90; /* above chat, but BELOW modals (100) so dialogs aren't covered */
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: 6px var(--sp-2);
    background: var(--ok-soft);
    border-bottom: 1px solid color-mix(in srgb, var(--ok) 35%, transparent);
    box-shadow: var(--shadow-pop);
    user-select: none;
  }
  .cb-open {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    /* The whole strip is the way back, so it has to be the target: the old
       widget's return button was the SMALLEST thing in it. */
    min-height: var(--tap-min);
    padding: 0 var(--sp-1);
    background: transparent;
    border: none;
    text-align: left;
  }
  .cb-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    line-height: 1.25;
  }
  .cb-lbl {
    font-size: var(--fs-compact);
    font-weight: 600;
    color: var(--ok-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .cb-hint {
    font-size: var(--fs-tiny);
    color: color-mix(in srgb, var(--ok-text) 75%, transparent);
  }

  /* ---- desktop: the draggable dock ---- */
  .dock {
    position: fixed;
    width: 214px;
    z-index: 90;
    background: var(--bg-elevated, var(--bg-1));
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    overflow: hidden;
    user-select: none;
    /* The dock breathes softly while the call is live — a low green ambience
       that says "this is running" without demanding attention. */
    animation: dock-breathe 4s ease-in-out infinite;
    transition: transform var(--dur-standard) ease, box-shadow var(--dur-standard) ease;
  }
  .dock.shelved,
  .callbar.shelved {
    display: none;
  }
  /* Lifted while dragged: bigger shadow + a slight grow under the pointer. */
  .dock.dragging {
    transform: scale(1.03);
    box-shadow:
      var(--shadow-pop),
      0 18px 44px rgb(0 0 0 / 0.4);
    animation: none;
  }
  @keyframes dock-breathe {
    0%,
    100% {
      box-shadow: var(--shadow-pop);
    }
    50% {
      box-shadow:
        var(--shadow-pop),
        0 0 18px color-mix(in srgb, var(--ok) 22%, transparent);
    }
  }
  .head {
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 7px 8px 7px 10px;
    background: var(--ok-soft);
    cursor: move;
    /* Ours, not the scroller's: with the default the browser claimed any
       vertical touch drag and cancelled the pointer mid-move. */
    touch-action: none;
  }
  .live {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--ok);
    flex-shrink: 0;
    animation: blink 1.4s ease-in-out infinite;
  }
  @keyframes blink {
    50% {
      opacity: 0.3;
    }
  }
  .lbl {
    flex: 1;
    min-width: 0;
    font-size: var(--fs-compact);
    font-weight: 600;
    color: var(--ok-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* Elapsed time, between the room's name and the way back into it. Tabular so
     it doesn't shuffle the header every second. */
  .clock {
    flex: none;
    font-size: var(--fs-tiny);
    font-variant-numeric: tabular-nums;
    font-weight: 600;
    color: color-mix(in srgb, var(--ok-text) 78%, transparent);
  }
  .faces {
    display: flex;
    flex-wrap: wrap;
    justify-content: center; /* same axis as the control row below it */
    gap: 6px;
    padding: 10px;
    max-height: 120px; /* ~3 rows; a big call scrolls instead of growing off-screen */
    overflow-y: auto;
    overscroll-behavior: contain; /* don't hand the leftover flick to the feed */
  }
  .face :global(.avatar) {
    border: 2px solid transparent;
    transition: border-color 0.1s ease;
  }
  /* Speaking: same layered ring-and-glow as the big call tiles, mini-sized. */
  .face.speaking :global(.avatar) {
    border-color: var(--ok);
    animation: fc-glow 1.6s ease-in-out infinite;
  }
  @keyframes fc-glow {
    0%,
    100% {
      box-shadow:
        0 0 0 2px var(--ok-soft),
        0 0 3px 0 color-mix(in srgb, var(--ok) 30%, transparent);
    }
    50% {
      box-shadow:
        0 0 0 2px color-mix(in srgb, var(--ok) 28%, transparent),
        0 0 8px 2px color-mix(in srgb, var(--ok) 45%, transparent);
    }
  }
  .ctl {
    display: flex;
    justify-content: center;
    gap: 10px;
    padding: 0 10px 10px;
  }
  .ico {
    width: 34px;
    height: 34px;
    padding: 0;
    border-radius: 50%;
    display: grid;
    place-items: center;
    background: var(--bg-3);
    color: var(--text);
    border: 1px solid var(--border);
    flex-shrink: 0;
  }
  .ico:hover {
    background: var(--bg-1);
  }
  /* Active mute/deafen: a filled danger state, so "I'm muted/deafened" reads at
     a glance rather than needing to be looked for. */
  .ico.on {
    background: var(--danger);
    color: var(--danger-fg);
    border-color: transparent;
  }
  .ico.on:hover {
    background: color-mix(in srgb, var(--danger) 85%, #000);
  }
  /* Camera and share are ON states, not alarm states: an engaged toggle here
     means something is going out, which is worth seeing but is not the red
     "nobody can hear me" that mute and deafen are. */
  .ico.lit {
    background: var(--ok);
    color: var(--ok-fg);
    border-color: transparent;
  }
  .ico.lit:hover {
    background: color-mix(in srgb, var(--ok) 85%, #000);
  }
  /* Five 34px circles no longer fit the 214px dock at a 10px gap. */
  .ctl {
    flex-wrap: wrap;
    gap: var(--sp-2);
  }
  .ico.expand {
    width: 24px;
    height: 24px;
    background: transparent;
    border: none;
    color: var(--ok-text);
    transform: rotate(180deg); /* chevron points back at the call, not away */
  }
  .ico.hang {
    background: var(--danger);
    color: var(--danger-fg);
    border-color: transparent;
  }
  .ico.hang:hover {
    background: color-mix(in srgb, var(--danger) 85%, #000);
  }

  @media (prefers-reduced-motion: reduce) {
    .dock,
    .live,
    .face.speaking :global(.avatar) {
      animation: none;
    }
  }

  @media (pointer: coarse), (max-width: 768px) {
    .ico {
      width: var(--tap-min);
      height: var(--tap-min);
    }
  }
</style>
