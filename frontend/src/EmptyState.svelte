<script>
  // The house empty-state illustration, extracted from MessageList's
  // empty-channel greeting so every panel speaks the same visual language:
  // an accent-tinted badge wearing a dashed "orbit" ring and a small drifting
  // satellite dot. Drawn entirely in CSS — strict CSP means no image assets.
  // Centering is the parent's job, not ours.
  //
  // `actions` is a snippet, not a label-and-callback pair, because the buttons
  // an empty state needs are not uniform — one panel offers a single primary,
  // another offers a primary and a quiet alternative. Its absence was why five
  // panels that DO have a next action hand-rolled the whole illustration
  // instead of using this: there was nowhere to put the button, so the
  // component was only usable by the dead ends. An empty state that names the
  // thing you should do next and gives you nothing to press is a sign, not a
  // door.
  import Icon from "./Icon.svelte";

  let { icon = "diamond", headline, sub = "", actions } = $props();
</script>

<div class="empty">
  <div class="badge"><Icon name={icon} size={28} /></div>
  <h3>{headline}</h3>
  {#if sub}<p>{sub}</p>{/if}
  {#if actions}<div class="acts">{@render actions()}</div>{/if}
</div>

<style>
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: var(--sp-1);
    max-width: 400px;
    padding: var(--sp-5) var(--sp-4);
    /* Settle in gently instead of popping when the panel opens.
       (The global reduced-motion rule in app.css zeroes the duration.) */
    animation: empty-in 0.4s var(--ease-out) both;
  }
  @keyframes empty-in {
    from {
      opacity: 0;
      transform: translateY(6px);
    }
  }
  .badge {
    position: relative;
    width: 64px;
    height: 64px;
    border-radius: 22px;
    display: grid;
    place-items: center;
    background: var(--accent-soft);
    color: var(--accent-hover);
    margin-bottom: 10px;
  }
  /* A dashed "orbit" ring + a small satellite dot make it feel illustrated
     without any image assets (strict CSP: inline CSS only). */
  .badge::before {
    content: "";
    position: absolute;
    inset: -9px;
    border-radius: 28px;
    border: 1.5px dashed color-mix(in srgb, var(--accent) 38%, transparent);
  }
  .badge::after {
    content: "";
    position: absolute;
    top: -13px;
    right: -11px;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    opacity: 0.55;
    /* The satellite drifts gently, giving the illustrated badge some life. */
    animation: sat-float 4.5s ease-in-out infinite;
  }
  @keyframes sat-float {
    50% {
      transform: translateY(-4px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .badge::after {
      animation: none;
    }
  }
  .empty h3 {
    margin: 0;
    font-size: 18px;
    color: var(--text);
  }
  .empty p {
    margin: 0;
    font-size: var(--fs-ui);
    line-height: 1.55;
    color: var(--text-muted);
  }
  .acts {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: var(--sp-2);
    margin-top: var(--sp-3);
  }
  .acts :global(button) {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
</style>
