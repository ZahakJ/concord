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

{#snippet inner()}
  {#if icon}
    <span class="chip"><Icon name={icon} size={16} /></span>
  {/if}
  <span class="text">
    <span class="title">{title}{#if info}<InfoDot text={info} label="Why? {title}" />{/if}</span>
    {#if sub}<span class="sub">{sub}</span>{/if}
  </span>
{/snippet}

<!-- A switch row is a <label> for a real checkbox (role=switch) inside the
     track: the whole row toggles it, and the ⓘ inside the title stays a
     button of its own instead of a button nested in a button. -->
{#if isSwitch}
  <label class="row" class:danger class:dis={disabled}>
    {@render inner()}
    <Switch on={checked}>
      <input
        type="checkbox"
        role="switch"
        aria-label={title}
        {checked}
        {disabled}
        onchange={(e) => {
          // The parent owns the value: put the box back and let the click
          // handler decide, so a refused change never shows as made.
          e.currentTarget.checked = !!checked;
          activate();
        }}
      />
    </Switch>
  </label>
{:else}
  <svelte:element
    this={interactive ? "button" : "div"}
    class="row"
    class:danger
    class:inert={!interactive}
    disabled={interactive ? disabled : undefined}
    onclick={interactive ? activate : undefined}
  >
    {@render inner()}
    {#if isLink}
      <span class="chev" aria-hidden="true"><Icon name="chevron" size={16} /></span>
    {:else if children}
      <span class="slot">{@render children()}</span>
    {/if}
  </svelte:element>
{/if}

<style>
  /* One line in a settings pane: the label and its one-line description on
     the leading side, the control on the trailing side, 24px between them.
     Rows are separated by a hairline; there is no card around a group. */
  .row {
    display: flex;
    align-items: center;
    gap: var(--sp-5);
    width: 100%;
    min-height: 58px;
    padding: 10px 0;
    background: transparent;
    border: none;
    border-radius: 0;
    color: var(--text);
    text-align: start;
    cursor: default;
  }
  .row + :global(.row) {
    border-top: 1px solid var(--hairline);
  }
  label.row,
  button.row {
    cursor: pointer;
  }
  label.row.dis {
    cursor: not-allowed;
  }
  label.row.dis > :global(.switch) {
    opacity: 0.45;
  }
  /* A drill-in row lifts on hover. The fill bleeds a little past the text
     column so the label does not touch its own highlight. */
  @media (pointer: fine) {
    button.row:hover:not(:disabled) {
      background: color-mix(in srgb, var(--bg-3) 60%, transparent);
      box-shadow: -8px 0 0 color-mix(in srgb, var(--bg-3) 60%, transparent),
        8px 0 0 color-mix(in srgb, var(--bg-3) 60%, transparent);
    }
    button.row:hover:not(:disabled) .chev {
      color: var(--text-muted);
    }
  }
  /* app.css kills the WebView's grey tap flash on coarse pointers on the
     understanding that components draw their own pressed state. */
  button.row:active:not(:disabled) {
    background: var(--bg-3);
  }
  button.row:disabled {
    opacity: 0.45;
  }
  button.row:focus-visible {
    outline: var(--focus-ring);
    outline-offset: 2px;
  }
  /* The checkbox is invisible over the track, so the ring goes on the track. */
  :global(:root[data-kbd]) label.row:has(input:focus-visible) > :global(.switch) {
    outline: var(--focus-ring);
    outline-offset: 2px;
  }
  .row.danger .title {
    color: var(--danger-text);
  }
  .chip {
    display: grid;
    place-items: center;
    flex: none;
    width: 16px;
    margin-right: calc(-1 * var(--sp-2));
    color: var(--text-muted);
  }
  .row.danger .chip {
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
    font-weight: 500;
    line-height: 1.35;
  }
  .sub {
    font-size: var(--fs-compact);
    line-height: 1.45;
    color: var(--text-muted);
  }
  .slot {
    flex: none;
    max-width: 60%;
    display: flex;
    justify-content: flex-end;
  }
  .chev {
    flex: none;
    display: grid;
    place-items: center;
    color: var(--text-faint);
  }
  /* A row hosting a wide control (a select, a slider, a port field) cannot
     share one line on a phone: the control took 40% and the description broke
     three ways in ~110px. The control drops below the label at full width. */
  @media (max-width: 768px) {
    .row {
      min-height: 56px;
    }
    .row:has(.slot) {
      flex-wrap: wrap;
      row-gap: var(--sp-2);
    }
    .row:has(.slot) .slot {
      flex: 1 0 100%;
      max-width: none;
      justify-content: flex-start;
    }
    .chip ~ .slot {
      padding-left: var(--sp-5);
    }
    @media (pointer: fine) {
      button.row:hover:not(:disabled) {
        box-shadow: none;
      }
    }
  }
</style>
