<script>
  // The one empty state. Every list, filter and pane that can be empty uses
  // this, so "nothing here" always reads the same way: an outlined mark, a
  // headline that names the place, one or two sentences saying what goes here
  // and how, up to two actions, and an optional meta line.
  //
  // It is left-aligned, like the column it sits in; anchoring it (bottom of a
  // feed, top of a pane) is the parent's job. `compact` is the panel size
  // (32px mark, title-size headline) for inspectors, popovers and small
  // dialogs.
  //
  // `actions` is a snippet, not a label-and-callback pair, because the buttons
  // an empty state needs are not uniform — one panel offers a single primary,
  // another a quiet action and a ghost alternative. An empty state that names
  // the thing you should do next and gives you nothing to press is a sign, not
  // a door. `meta` is a snippet too (a faint line under the actions, usually
  // with an icon).
  import Icon from "./Icon.svelte";

  let { icon = "diamond", headline, sub = "", compact = false, actions, meta } = $props();
</script>

<div class="empty" class:compact>
  <div class="mark"><Icon name={icon} size={compact ? 16 : 22} /></div>
  <h3>{headline}</h3>
  {#if sub}<p>{sub}</p>{/if}
  {#if actions}<div class="acts">{@render actions()}</div>{/if}
  {#if meta}<div class="meta">{@render meta()}</div>{/if}
</div>

<style>
  .empty {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    text-align: start;
    max-width: 560px;
    padding: var(--sp-5) var(--sp-4);
    animation: empty-in var(--dur-standard) var(--ease-out) both;
  }
  @keyframes empty-in {
    from {
      opacity: 0;
    }
  }
  /* Outlined, not a tinted blob: the mark says "this is a place", the
     headline says which. */
  .mark {
    width: 48px;
    height: 48px;
    border-radius: var(--radius-lg);
    border: 1px solid var(--border);
    display: grid;
    place-items: center;
    color: var(--text-muted);
    margin-bottom: 18px;
  }
  .empty h3 {
    margin: 0;
    font-family: var(--display-font);
    font-size: var(--fs-display);
    font-weight: 600;
    letter-spacing: -0.02em;
    line-height: 1.2;
    color: var(--text);
  }
  .empty p {
    margin: var(--sp-2) 0 0;
    max-width: 460px;
    font-size: var(--fs-ui);
    line-height: 1.55;
    color: var(--text-muted);
  }
  .acts {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-2);
    margin-top: 18px;
  }
  .meta {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 20px;
    font-size: var(--fs-small);
    color: var(--text-faint);
  }
  .compact {
    padding: var(--sp-4) var(--sp-3);
  }
  .compact .mark {
    width: 32px;
    height: 32px;
    border-radius: var(--radius-md);
    margin-bottom: var(--sp-3);
  }
  .compact h3 {
    font-size: var(--fs-title);
    letter-spacing: -0.01em;
  }
  .compact p {
    font-size: var(--fs-compact);
  }
  .compact .acts {
    margin-top: var(--sp-3);
  }
</style>
