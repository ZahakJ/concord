<script>
  // The one empty state. Every list, filter and pane that can be empty uses
  // this, so "nothing here" always reads the same way: an outlined mark, a
  // headline that names the place, one or two sentences saying what goes here
  // and how, up to two actions, and an optional meta line.
  //
  // It is left-aligned, like the column it sits in; anchoring it (bottom of a
  // feed, top of a pane) is the parent's job. `center` is for the one place
  // it introduces something centred: a voice room's landing, which opens into
  // the call stage's centred tiles and controls, so the headline, the Join
  // button and who is in there share that one axis rather than starting at
  // the left edge with the presence line centred under a button that is not. `compact` is the panel size
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

  let { icon = "diamond", headline, sub = "", compact = false, center = false, actions, meta } = $props();
</script>

<div class="empty" class:compact class:center>
  <div class="mark" aria-hidden="true"><Icon name={icon} size={compact ? 16 : 26} /></div>
  <h3>{headline}</h3>
  {#if sub}<p>{sub}</p>{/if}
  {#if actions}<div class="acts">{@render actions()}</div>{/if}
  {#if meta}<div class="meta">{@render meta()}</div>{/if}
</div>

<style>
  /* 0.63: the full-size empty state is a small STAGE (DESIGN-063.md): the
     mark is lit by the aurora and the lines arrive in a short staggered rise.
     The compact one (panels, popovers) stays chrome: outlined and still. */
  .empty {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    text-align: start;
    max-width: 560px;
    padding: var(--sp-5) var(--sp-4);
  }
  .empty:not(.compact) > * {
    animation: empty-rise var(--dur-reveal) var(--ease-spring) both;
  }
  .empty:not(.compact) > :nth-child(2) {
    animation-delay: var(--stagger);
  }
  .empty:not(.compact) > :nth-child(3) {
    animation-delay: calc(var(--stagger) * 2);
  }
  .empty:not(.compact) > :nth-child(n + 4) {
    animation-delay: calc(var(--stagger) * 3);
  }
  @keyframes empty-rise {
    from {
      opacity: 0;
      transform: translateY(var(--stage-rise));
    }
  }
  .compact {
    animation: empty-in var(--dur-standard) var(--ease-out) both;
  }
  @keyframes empty-in {
    from {
      opacity: 0;
    }
  }
  /* The lit mark: a tile on the stage ground with the aurora behind a frosted
     face, the glyph in stage ink. The light is painted, never animated. */
  .mark {
    position: relative;
    width: 64px;
    height: 64px;
    border-radius: var(--radius-xl);
    display: grid;
    place-items: center;
    color: var(--stage-ink);
    background: var(--aurora), var(--stage-ground);
    background-size: 220% 220%, auto;
    background-position: 30% 20%, center;
    box-shadow:
      inset 0 0 0 1px color-mix(in srgb, var(--aurora-1) 38%, transparent),
      var(--shadow-rest);
    margin-bottom: var(--sp-5);
    isolation: isolate;
  }
  /* A second, wider pool of the same light under the tile, so the mark sits
     in a glow of its own room rather than on a flat page. Static. */
  .mark::before {
    content: "";
    position: absolute;
    inset: -28px;
    z-index: -1;
    border-radius: 50%;
    background: radial-gradient(closest-side, color-mix(in srgb, var(--aurora-1) 22%, transparent), transparent);
    pointer-events: none;
  }
  .empty h3 {
    margin: 0;
    font-family: var(--display-font);
    font-size: var(--fs-headline);
    font-weight: 600;
    letter-spacing: -0.025em;
    line-height: 1.12;
    color: var(--text);
    text-wrap: balance;
  }
  .empty p {
    margin: var(--sp-2) 0 0;
    max-width: 460px;
    font-size: var(--fs-ui);
    line-height: 1.55;
    color: var(--text-muted);
    text-wrap: pretty;
  }
  .acts {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-2);
    margin-top: 20px;
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
  /* Centred in its column: the box itself (margins), every line in it, and the
     action row, whose items wrap onto centred lines. */
  .center {
    align-items: center;
    text-align: center;
    margin-inline: auto;
  }
  .center .acts,
  .center .meta {
    justify-content: center;
  }
  .compact .mark {
    width: 32px;
    height: 32px;
    border-radius: var(--radius-md);
    margin-bottom: var(--sp-3);
    color: var(--text-muted);
    background: none;
    box-shadow: inset 0 0 0 1px var(--border);
  }
  .compact .mark::before {
    content: none;
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
