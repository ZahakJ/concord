<script>
  // A choice of two to four exclusive options, all visible at once: theme,
  // density, an agenda/month toggle, the inbox's filters on a desktop.
  //
  //   <Segmented label="Message density" value={density}
  //     options={[{ value: "compact", label: "Compact" }, …]}
  //     onChange={(v) => setDensity(v)} />
  //
  // The chosen segment is never told apart by colour alone: its thumb is
  // lighter than the track in both themes (≥1.3:1), it carries a control-edge
  // ring (--border-strong) and its label goes to full ink at 600.
  //
  // It is a radio group: one Tab stop (the chosen segment), arrow keys move
  // AND choose, Home/End jump to the ends — the behaviour of the native
  // control it stands in for. `onChange` receives the option's own value.
  let { options = [], value, onChange, label = "", size = "md", disabled = false } = $props();
  let el = $state(null);

  const same = (a, b) => String(a ?? "") === String(b ?? "");

  function onKey(e) {
    const keys = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 };
    let at = options.findIndex((o) => same(o.value, value));
    if (e.key in keys) at = (at + keys[e.key] + options.length) % options.length;
    else if (e.key === "Home") at = 0;
    else if (e.key === "End") at = options.length - 1;
    else return;
    e.preventDefault();
    // Right-to-left reverses the visual order, so it reverses the arrows too.
    if ((e.key === "ArrowRight" || e.key === "ArrowLeft") && el && getComputedStyle(el).direction === "rtl") {
      at = options.findIndex((o) => same(o.value, value)) - keys[e.key];
      at = (at + options.length) % options.length;
    }
    const next = options[at];
    if (!next || next.disabled) return;
    onChange?.(next.value);
    el?.querySelectorAll("button")[at]?.focus();
  }
</script>

<div class="seg {size}" role="radiogroup" aria-label={label || undefined} aria-disabled={disabled || undefined} bind:this={el}>
  {#each options as o (String(o.value))}
    {@const on = same(o.value, value)}
    <button
      type="button"
      role="radio"
      aria-checked={on}
      tabindex={on || (value == null && o === options[0]) ? 0 : -1}
      class:on
      disabled={disabled || o.disabled}
      title={o.title}
      onclick={() => !on && onChange?.(o.value)}
      onkeydown={onKey}>{o.label}</button
    >
  {/each}
</div>

<style>
  .seg {
    display: inline-flex;
    flex: none;
    gap: 2px;
    height: 32px;
    padding: 2px;
    border-radius: calc(var(--radius-sm) + 2px);
    background: var(--bg-3);
  }
  .seg.sm {
    height: 28px;
  }
  button {
    display: grid;
    place-items: center;
    padding: 0 var(--sp-3);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
    font-size: var(--fs-compact);
    font-weight: 500;
    white-space: nowrap;
  }
  .seg.sm button {
    padding: 0 10px;
    font-size: var(--fs-small);
  }
  @media (pointer: fine) {
    button:hover:not(.on):not(:disabled) {
      color: var(--text);
    }
  }
  /* Lighter than the track in BOTH themes: a white step over the dark track
     (and over any pack's), plain white over the paper theme's. */
  button.on {
    background: color-mix(in srgb, var(--bg-3), white 8%);
    color: var(--text);
    font-weight: 600;
    box-shadow: inset 0 0 0 1px var(--border-strong);
  }
  :global(:root[data-theme="light"]:not([data-theme-pack])) button.on {
    background: #fff;
  }
  button:focus-visible {
    outline-offset: 0;
  }
  @media (pointer: coarse), (max-width: 768px) {
    .seg {
      height: 40px;
    }
  }
</style>
