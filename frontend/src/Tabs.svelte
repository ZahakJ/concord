<script>
  // Underline tabs, for a pane with two to five views (the inspector, a
  // dialog's sub-views). The strip owns a bottom hairline and the current tab
  // draws a 2px ink underline flush with it; the current tab is also full ink
  // at 600, so the state never rests on the line alone.
  //
  //   <Tabs label="Sounds" value={tab} onChange={(id) => (tab = id)}
  //     tabs={[{ id: "shelf", label: "Shelf" }, { id: "make", label: "Make one", count: 3 }]} />
  //
  // `count` is a quiet number after the label (how many are in there), not an
  // unread badge — put a <Badge> in the panel's own rows for that. Each tab is
  // role="tab" with aria-controls `${idPrefix}-${id}` when `idPrefix` is given,
  // so the caller's panel can carry the matching id and role="tabpanel".
  // Keyboard: one Tab stop; arrow keys move and select; Home/End.
  let { tabs = [], value, onChange, label = "", idPrefix = "" } = $props();
  let el = $state(null);

  function onKey(e) {
    const step = { ArrowRight: 1, ArrowLeft: -1 }[e.key];
    let at = tabs.findIndex((t) => t.id === value);
    if (step) {
      const rtl = el && getComputedStyle(el).direction === "rtl";
      at = (at + (rtl ? -step : step) + tabs.length) % tabs.length;
    } else if (e.key === "Home") at = 0;
    else if (e.key === "End") at = tabs.length - 1;
    else return;
    e.preventDefault();
    onChange?.(tabs[at].id);
    el?.querySelectorAll('[role="tab"]')[at]?.focus();
  }
</script>

<div class="tabs" role="tablist" aria-label={label || undefined} bind:this={el}>
  {#each tabs as t (t.id)}
    {@const on = t.id === value}
    <button
      type="button"
      role="tab"
      id={idPrefix ? `${idPrefix}-tab-${t.id}` : undefined}
      aria-selected={on}
      aria-controls={idPrefix ? `${idPrefix}-${t.id}` : undefined}
      tabindex={on ? 0 : -1}
      class:on
      onclick={() => onChange?.(t.id)}
      onkeydown={onKey}
    >
      {t.label}{#if t.count != null}<span class="n">{t.count}</span>{/if}
    </button>
  {/each}
</div>

<style>
  .tabs {
    display: flex;
    align-items: stretch;
    gap: 20px;
    min-height: 40px;
    /* The hairline is an inset shadow, not a border, so the underline below can
       sit on it without being clipped by the strip's own overflow. */
    box-shadow: inset 0 -1px 0 var(--hairline);
    overflow-x: auto;
    scrollbar-width: none;
  }
  button {
    position: relative;
    display: inline-flex;
    align-items: center;
    flex: none;
    padding: 0;
    background: transparent;
    border-radius: 0;
    color: var(--text-muted);
    font-size: var(--fs-ui);
    font-weight: 500;
    white-space: nowrap;
  }
  @media (pointer: fine) {
    button:hover:not(.on) {
      color: var(--text);
    }
  }
  button.on {
    color: var(--text);
    font-weight: 600;
  }
  /* Flush with the strip's hairline: it sits ON the line rather than above it. */
  button.on::after {
    content: "";
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    height: 2px;
    border-radius: 2px;
    background: var(--text);
  }
  button:focus-visible {
    outline-offset: -2px;
  }
  .n {
    margin-left: 6px;
    font-size: var(--fs-tiny);
    font-weight: 500;
    color: var(--text-faint);
    font-variant-numeric: tabular-nums;
  }
</style>
