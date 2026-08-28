<script>
  // The app's own <select>.
  //
  // A native select drops OS chrome — its own radius, its own chevron, its own
  // type size, and on Linux its own font — into the middle of a sheet where
  // every neighbouring control is drawn by this codebase. G-E replaced the
  // three in the audio dialog by hand and G-F the import wizard's, each with a
  // Menu and a bespoke trigger; the remaining eleven live in seven files, which
  // is where hand-rolling stops being cheaper than a component.
  //
  // This is that trigger, factored out. It wraps Menu, so it inherits the whole
  // dropdown-on-desktop / bottom-sheet-on-a-phone split, the outside-click and
  // Escape handling, the navstack layer and the arrow-key walk — none of which
  // a native select gives you on a phone either.
  //
  // `options` is `[{ value, label, sub? }]`. Values are compared as strings, so
  // a numeric option list bound to a numeric variable still matches; `onPick`
  // receives the option's ORIGINAL value, not the stringified one, because a
  // caller that stored numbers wants numbers back.
  import Menu from "./Menu.svelte";
  import Icon from "./Icon.svelte";

  let {
    options = [],
    value = "",
    onPick,
    label = "Choose",
    placeholder = "",
    disabled = false,
    align = "left",
    wide = true,
  } = $props();

  const same = (a, b) => String(a ?? "") === String(b ?? "");
  const current = $derived(options.find((o) => same(o.value, value)));
</script>

{#if disabled}
  <!-- A disabled Menu trigger would still open. The control keeps its shape and
       loses its behaviour, which is what "disabled" has to mean here: the
       birthday day list is unusable until a month is picked, and an empty
       dropdown that opens is worse than one that plainly will not. -->
  <span class="pick off" class:wide aria-disabled="true">
    <span class="pick-name faint">{placeholder || label}</span>
    <span class="pick-chev"><Icon name="chevron" size={12} /></span>
  </span>
{:else}
  <Menu {label} {wide} {align}>
    {#snippet trigger()}
      <span class="pick" class:wide>
        <span class="pick-name" class:faint={!current}>{current?.label ?? placeholder ?? label}</span>
        <span class="pick-chev"><Icon name="chevron" size={12} /></span>
      </span>
    {/snippet}
    {#each options as o (String(o.value))}
      <button class="menu-item" class:active={same(o.value, value)} onclick={() => onPick?.(o.value)}>
        <span class="opt-txt">
          {o.label}
          {#if o.sub}<span class="opt-sub">{o.sub}</span>{/if}
        </span>
        {#if same(o.value, value)}<span class="opt-tick" aria-hidden="true">✓</span>{/if}
      </button>
    {/each}
  </Menu>
{/if}

<style>
  /* The filled well app.css draws for a text input, so a picker and the field
     beside it read as the same species of control. */
  .pick {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-height: 38px;
    padding: 8px 10px 8px 12px;
    background: var(--bg-input);
    color: var(--text);
    border: 1px solid color-mix(in srgb, var(--border) 62%, transparent);
    border-radius: var(--radius-md);
    font-size: var(--fs-ui);
    text-align: left;
    transition:
      border-color var(--dur-standard) ease,
      background var(--dur-standard) ease;
  }
  .pick.wide {
    width: 100%;
  }
  .pick.off {
    opacity: 0.5;
  }
  @media (pointer: fine) {
    :global(.trigger.own:hover) .pick {
      border-color: color-mix(in srgb, var(--border) 100%, transparent);
      background: var(--bg-3);
    }
  }
  :global(.trigger.own[aria-expanded="true"]) .pick {
    border-color: var(--accent);
  }
  .pick-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .faint {
    color: var(--text-faint);
  }
  /* The shared chevron points right; a dropdown's points down. Rotating at the
     call site is the app's existing idiom for direction (MobileShell's back and
     down chevrons do the same), so there is one glyph and no second path to
     keep in sync. */
  .pick-chev {
    display: grid;
    place-items: center;
    color: var(--text-muted);
    transform: rotate(90deg);
  }
  .opt-txt {
    display: flex;
    flex-direction: column;
    gap: 1px;
    flex: 1;
    min-width: 0;
  }
  .opt-sub {
    font-size: var(--fs-tiny);
    color: var(--text-muted);
  }
  .opt-tick {
    color: var(--accent-hover);
    font-weight: 700;
  }
  @media (pointer: coarse), (max-width: 768px) {
    .pick {
      min-height: var(--tap-min);
    }
  }
</style>
