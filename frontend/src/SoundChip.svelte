<script>
  // A sound in a message: press it to hear it, or keep it.
  //
  // Nothing is downloaded and nothing is cached, because there is nothing to
  // download — the whole sound is the couple of dozen bytes in the token, and
  // every client renders it from those numbers. That is also why keeping one
  // is free: the message already gave you the recipe.
  import Icon from "./Icon.svelte";
  import { playRecipe, soundboardEnabled } from "./lib/sounds.js";
  import { recipeGlyph, recipeTotalMs, soundLength } from "./lib/sfxrecipe.js";
  import { keepSound, dropSound, onShelf, shelfFull } from "./lib/soundshelf.svelte.js";
  import { flash } from "./lib/state.svelte.js";
  import { tooltip } from "./lib/tooltip.js";

  let { recipe, payload } = $props();

  let ringing = $state(false);
  let timer = 0;
  const kept = $derived(onShelf(payload));
  const secs = $derived(soundLength(recipe));

  function press() {
    // Playback can be refused — the soundboard is muted, or something else in
    // the app is already making a noise. Say so rather than looking broken.
    if (!playRecipe(recipe)) {
      if (!soundboardEnabled()) flash("Sound effects are switched off in Settings › Notifications");
      return;
    }
    ringing = true;
    clearTimeout(timer);
    timer = setTimeout(() => (ringing = false), recipeTotalMs(recipe));
  }

  function keep() {
    if (kept) return dropSound(payload);
    if (shelfFull()) return flash("Your sound shelf is full — remove one first");
    keepSound(payload);
  }
</script>

<div class="sound">
  <button type="button" class="play" class:ringing onclick={press} aria-label={`Play the sound "${recipe.name || "untitled"}"`}>
    <span class="glyph" aria-hidden="true">{recipeGlyph(recipe)}</span>
    <span class="meta">
      <span class="name">{recipe.name || "Untitled sound"}</span>
      <span class="sub">{secs} · synthesized here, nothing downloaded</span>
    </span>
  </button>
  <button
    type="button"
    class="keep"
    class:on={kept}
    onclick={keep}
    aria-pressed={kept}
    aria-label={kept ? "Remove from your sound shelf" : "Keep this sound on your shelf"}
    use:tooltip
  >
    <Icon name={kept ? "check" : "plus"} size={14} />
  </button>
</div>

<style>
  .sound {
    display: inline-flex;
    align-items: stretch;
    gap: 1px;
    margin-top: var(--sp-1);
    max-width: 320px;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-1);
    overflow: hidden;
  }
  .play {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    flex: 1;
    min-width: 0;
    padding: var(--sp-2) var(--sp-3);
    border: none;
    background: transparent;
    color: var(--text);
    text-align: left;
    cursor: pointer;
  }
  .play:hover {
    background: var(--bg-3);
  }
  .glyph {
    font-size: var(--fs-title);
    line-height: 1;
    flex: none;
  }
  /* The glyph pulses for exactly as long as the sound lasts, which is the only
     feedback there is: a synthesized sound has no waveform to scrub and no
     file to show a spinner for. */
  .play.ringing .glyph {
    animation: sound-ring var(--dur-calm) var(--ease-spring) 3;
  }
  @keyframes sound-ring {
    50% {
      transform: scale(1.22);
    }
  }
  .meta {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .name {
    font-size: var(--fs-ui);
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sub {
    font-size: var(--fs-tiny);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .keep {
    display: grid;
    place-items: center;
    padding: 0 var(--sp-2);
    border: none;
    border-left: 1px solid var(--border);
    background: var(--bg-2);
    color: var(--text-muted);
    cursor: pointer;
  }
  .keep:hover {
    color: var(--text);
  }
  .keep.on {
    color: var(--accent);
  }

  @media (prefers-reduced-motion: reduce) {
    .play.ringing .glyph {
      animation: none;
    }
  }
</style>
