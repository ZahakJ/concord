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
  // Fixed viewport coordinates, because the bubble is moved OUT of the modal to
  // be positioned (see portal below).
  let pos = $state({ left: 0, top: 0 });
  let hoverTimer;

  // Touch devices get tap-to-toggle; a hover popover there either never opens
  // or opens and won't go away.
  const coarse = window.matchMedia?.("(pointer: coarse)")?.matches ?? false;

  // Position against the VIEWPORT, then keep it there.
  //
  // The first version placed the bubble absolutely inside the row and only
  // clamped against the window. That is the wrong box: the settings dialog
  // clips its own content, so a dot on the last row opened a bubble that was
  // inside the viewport and still half-cut by the panel. Being fixed and
  // portalled to <body> means the only edges that can clip it are the screen's,
  // which is what the clamping below actually measures.
  const M = 8; // margin from the screen edge
  function place() {
    if (!bubble || !dot) return;
    const d = dot.getBoundingClientRect();
    const b = bubble.getBoundingClientRect();
    // Below by default; above when that would run off the bottom and there is
    // genuinely room up there.
    const below = d.bottom + 7;
    const above = d.top - b.height - 7;
    const top = below + b.height > window.innerHeight - M && above >= M ? above : below;
    // Centred on the dot, then pulled back inside whichever edge it crosses.
    let left = d.left + d.width / 2 - b.width / 2;
    left = Math.max(M, Math.min(left, window.innerWidth - b.width - M));
    pos = { left, top: Math.max(M, top) };
  }

  function show() {
    open = true;
    // Measure after paint: the bubble has no box until it exists.
    requestAnimationFrame(place);
  }

  function hide() {
    open = false;
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
  // Move the bubble to <body>. position:fixed alone is not enough: any ancestor
  // with a transform or filter becomes the containing block for fixed children,
  // and the modal animates with one. Out here nothing can clip or re-anchor it.
  function portal(node) {
    document.body.appendChild(node);
    return { destroy: () => node.remove() };
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
  onresize={() => open && place()}
/>

<span class="wrap" bind:this={dot} onmouseenter={onEnter} onmouseleave={onLeave} role="presentation">
  <!-- tap-hit, not a bigger dot: this is the app's answer to a tooltip and it
       sits inline beside a setting's label, where a 44px circle would shove the
       row apart. The glyph stays 14px; only the hit box grows, and only on
       touch. -->
  <button
    class="dot tap-hit"
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
      style="left:{pos.left}px; top:{pos.top}px"
      bind:this={bubble}
      use:portal
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
      opacity var(--dur-quick),
      color var(--dur-quick),
      border-color var(--dur-quick);
    flex: 0 0 auto;
  }
  .dot:hover,
  .dot.on,
  .dot:focus-visible {
    opacity: 1;
    color: var(--accent);
    border-color: var(--accent);
  }
  /* Fixed and living on <body>: `left`/`top` are set from JS in viewport
     coordinates, so nothing here may re-anchor it. The z-index clears the modal
     it escaped from. */
  .bubble {
    position: fixed;
    z-index: 200;
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
    font-size: var(--fs-compact);
    font-weight: 400;
    font-style: normal;
    line-height: 1.5;
    text-align: left;
    white-space: normal;
    text-transform: none;
    letter-spacing: normal;
    animation: pop var(--dur-quick) ease both;
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: translateY(-3px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .bubble {
      animation: none;
    }
  }
</style>
