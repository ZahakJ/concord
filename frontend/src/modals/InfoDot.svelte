<script>
  // The small ⓘ next to a setting. Hover (mouse) or tap (touch) to get the
  // paragraph that used to sit under the group as a wall of prose.
  //
  // The reasoning behind Concord's defaults is genuinely worth reading — why
  // link previews are off, why typing indicators are reciprocal — but printing
  // all of it at once turns a settings page into an essay you scroll past. This
  // keeps the explanation one gesture away from the row it explains.
  let { text, label = "Why?" } = $props();

  let open = $state(false);
  let dot = $state(null);
  let bubble = $state(null);
  let flip = $state(false); // open upward when there's no room below
  let shiftX = $state(0); // nudge back inside the panel when near an edge
  let hoverTimer;

  // Touch devices get tap-to-toggle; a hover popover there either never opens
  // or opens and won't go away.
  const coarse = window.matchMedia?.("(pointer: coarse)")?.matches ?? false;

  function show() {
    open = true;
    // Measure after paint: the bubble has no box until it exists.
    requestAnimationFrame(() => {
      if (!bubble || !dot) return;
      const b = bubble.getBoundingClientRect();
      const d = dot.getBoundingClientRect();
      flip = b.bottom > window.innerHeight - 8 && d.top > b.height + 16;
      // Re-measure horizontally against the viewport and pull it back in.
      const over = b.right - (window.innerWidth - 8);
      const under = 8 - b.left;
      shiftX = over > 0 ? -over : under > 0 ? under : 0;
    });
  }

  function hide() {
    open = false;
    flip = false;
    shiftX = 0;
  }

  function onEnter() {
    if (coarse) return;
    clearTimeout(hoverTimer);
    hoverTimer = setTimeout(show, 160); // intent delay, so a passing cursor doesn't fire it
  }
  function onLeave() {
    if (coarse) return;
    clearTimeout(hoverTimer);
    hide();
  }
  function toggle(e) {
    // The dot lives inside setting rows that are themselves buttons; without
    // this, asking what a switch does would also flip it.
    e.stopPropagation();
    e.preventDefault();
    open ? hide() : show();
  }
</script>

<svelte:window
  onkeydown={(e) => open && e.key === "Escape" && hide()}
  onpointerdown={(e) => open && coarse && !dot?.contains(e.target) && hide()}
/>

<span class="wrap" bind:this={dot} onmouseenter={onEnter} onmouseleave={onLeave} role="presentation">
  <button
    class="dot"
    class:on={open}
    aria-label={label}
    aria-expanded={open}
    onclick={toggle}
    onfocus={coarse ? undefined : show}
    onblur={coarse ? undefined : hide}
  >
    i
  </button>
  {#if open}
    <span
      class="bubble"
      class:up={flip}
      style="--shift:{shiftX}px"
      bind:this={bubble}
      role="tooltip"
    >
      {text}
    </span>
  {/if}
</span>

<style>
  .wrap {
    position: relative;
    display: inline-flex;
    vertical-align: middle;
    margin-left: 5px;
  }
  .dot {
    width: 14px;
    height: 14px;
    padding: 0;
    border-radius: 50%;
    border: 1px solid var(--text-muted);
    background: transparent;
    color: var(--text-muted);
    font-size: 9.5px;
    font-weight: 700;
    font-style: italic;
    line-height: 1;
    display: grid;
    place-items: center;
    cursor: help;
    opacity: 0.75;
    transition:
      opacity 0.12s,
      color 0.12s,
      border-color 0.12s;
    flex: 0 0 auto;
  }
  .dot:hover,
  .dot.on,
  .dot:focus-visible {
    opacity: 1;
    color: var(--accent);
    border-color: var(--accent);
  }
  .bubble {
    position: absolute;
    top: calc(100% + 7px);
    left: 50%;
    transform: translateX(calc(-50% + var(--shift, 0px)));
    z-index: 40;
    width: max-content;
    max-width: min(280px, 78vw);
    padding: 8px 10px;
    border-radius: var(--radius-sm);
    /* --bg-3 rather than --bg-1: the bubble floats over a card that is already
       --bg-1, and matching it makes the popover read as part of the row. */
    background: var(--bg-3);
    border: 1px solid var(--border);
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.45);
    color: var(--text);
    font-size: 12px;
    font-weight: 400;
    font-style: normal;
    line-height: 1.5;
    text-align: left;
    white-space: normal;
    text-transform: none;
    letter-spacing: normal;
    animation: pop 0.13s ease both;
  }
  .bubble.up {
    top: auto;
    bottom: calc(100% + 7px);
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: translateX(calc(-50% + var(--shift, 0px))) translateY(-3px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .bubble {
      animation: none;
    }
  }
</style>
