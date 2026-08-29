<script>
  // One line in a settings panel. Three shapes, one look:
  //   • a switch      (`checked` given)  — flips something on the spot
  //   • a drill-in    (`to` given)       — opens a sub-panel
  //   • an action     (neither)          — runs `onclick`, or hosts `children`
  //     as its own control (a <select>, a slider, whatever the row needs).
  //
  // `sub` is the one-line "what does this do". Anything longer belongs INSIDE
  // the panel this row opens, not stacked on the row — a list where every
  // entry carries a paragraph is the wall of text we're trying not to build.
  import Icon from "../Icon.svelte";
  import InfoDot from "./InfoDot.svelte";
  import Switch from "../Switch.svelte";
  import { openPanel } from "../lib/state.svelte.js";
  import { haptic } from "../lib/touch.js";

  let {
    icon = "",
    title,
    sub = "",
    checked = undefined,
    to = "",
    from = "",
    disabled = false,
    danger = false,
    // `info` is the long version: the reasoning, one click away, instead of a
    // paragraph printed under every group.
    info = "",
    onclick = undefined,
    children = undefined,
  } = $props();

  const isSwitch = $derived(checked !== undefined);
  const isLink = $derived(!!to);
  // A row with its own control isn't clickable itself — the control is.
  const interactive = $derived(isSwitch || isLink || !!onclick);

  function activate() {
    if (disabled) return;
    // A switch commits a change in place with nothing else to confirm it — the
    // 0.18s knob slide is the only feedback, and a thumb usually covers it.
    if (isSwitch) haptic("light");
    if (isLink) openPanel(to, from);
    else onclick?.();
  }
</script>

<svelte:element
  this={interactive ? "button" : "div"}
  class="row"
  class:danger
  class:inert={!interactive}
  role={isSwitch ? "switch" : undefined}
  aria-checked={isSwitch ? checked : undefined}
  disabled={interactive ? disabled : undefined}
  onclick={interactive ? activate : undefined}
>
  {#if icon}
    <span class="chip"><Icon name={icon} size={16} /></span>
  {/if}
  <span class="text">
    <span class="title">{title}{#if info}<InfoDot text={info} label="Why? {title}" />{/if}</span>
    {#if sub}<span class="sub">{sub}</span>{/if}
  </span>
  {#if isSwitch}
    <Switch on={checked} />
  {:else if isLink}
    <span class="chev">›</span>
  {:else if children}
    <span class="slot">{@render children()}</span>
  {/if}
</svelte:element>

<style>
  .row {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    width: 100%;
    padding: 11px 14px;
    background: transparent;
    border: none;
    color: var(--text);
    text-align: left;
    transition: background var(--dur-quick) ease;
  }
  .row + :global(.row) {
    border-top: 1px solid var(--border);
  }
  button.row:hover:not(:disabled) {
    background: var(--bg-3);
  }
  /* app.css kills the WebView's grey tap flash on coarse pointers on the
     understanding that components draw their own pressed state. This one never
     did, so every row in Settings, Privacy and Notifications was inert to the
     touch until the panel it opens actually appeared. */
  button.row:active:not(:disabled) {
    background: var(--bg-3);
  }
  button.row:disabled {
    opacity: 0.5;
  }
  .row.danger .title {
    color: var(--danger-text);
  }
  .chip {
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    flex: none;
    border-radius: var(--radius-md);
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .row.danger .chip {
    background: color-mix(in srgb, var(--danger) 15%, transparent);
    color: var(--danger-text);
  }
  .text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    margin-right: auto;
  }
  .title {
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .sub {
    font-size: var(--fs-compact);
    line-height: 1.45;
    color: var(--text-muted);
  }
  .slot {
    flex: none;
    max-width: 55%;
  }
  .chev {
    flex: none;
    color: var(--text-faint);
    font-size: var(--fs-title);
    transition: transform var(--dur-standard) var(--ease-spring);
  }
  button.row:hover .chev {
    transform: translateX(3px);
    color: var(--text-muted);
  }
  /* A row hosting its own control (a select, a port field) can't share one line
     on a phone: the control took 40% and the description was left breaking
     three ways in ~110px. Give the control its own line, aligned under the
     text rather than under the icon chip. Switches and chevrons are narrow and
     stay put. */
  @media (pointer: coarse), (max-width: 768px) {
    .row:has(.slot) {
      flex-wrap: wrap;
      row-gap: var(--sp-2);
    }
    .row:has(.slot) .slot {
      flex: 1 0 100%;
      max-width: none;
    }
    /* Indent with PADDING, not margin. A margin on a full-width flex item is
       added outside the box, so the row overhangs by exactly that much and the
       Save button on the fixed-port row was pushed past the card edge and
       rendered as "Sav". Padding is inside the box under the global
       border-box, so the same visual indent costs no width. */
    .chip ~ .slot {
      padding-left: 42px; /* 30px chip + the row's 12px gap */
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .chev {
      transition: none;
    }
    button.row:hover .chev {
      transform: none;
    }
  }
</style>
