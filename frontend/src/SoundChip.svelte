<script>
  // A sound in a message: press it to hear it, or keep it.
  //
  // Nothing is downloaded and nothing is cached, because there is nothing to
  // download — the whole sound is the couple of dozen bytes in the token, and
  // every client renders it from those numbers. That is also why keeping one
  // is free: the message already gave you the recipe.
  import Icon from "./Icon.svelte";
  import { playRecipe, soundboardEnabled, soundsEnabled, audioTrouble } from "./lib/sounds.js";
  import { recipeGlyph, recipeTotalMs, soundLength } from "./lib/sfxrecipe.js";
  import { keepSound, dropSound, onShelf, shelfFull } from "./lib/soundshelf.svelte.js";
  import { flash } from "./lib/state.svelte.js";
  import { tooltip } from "./lib/tooltip.js";
  import { onDestroy } from "svelte";

  let { recipe, payload } = $props();

  let ringing = $state(false);
  let timer = 0;
  const kept = $derived(onShelf(payload));
  const secs = $derived(soundLength(recipe));
  const ms = $derived(recipeTotalMs(recipe));

  function press() {
    // Playback can be refused for three different reasons and only one of them
    // used to be named, so two of the three read as a dead button. The
    // rate-limit case says nothing on purpose: it means somebody else's sound
    // is playing right now, which is a fifth of a second away from being fine
    // and not worth a toast.
    if (!playRecipe(recipe)) {
      if (!soundsEnabled()) flash("Sound is turned off — the volume is at zero in Settings › Notifications & sounds");
      else if (!soundboardEnabled()) flash("Sound effects are switched off in Settings › Notifications & sounds");
      return;
    }
    const why = audioTrouble();
    if (why) flash(why, "error");
    ringing = true;
    clearTimeout(timer);
    timer = setTimeout(() => (ringing = false), ms);
  }

  onDestroy(() => clearTimeout(timer));

  function keep() {
    if (kept) return dropSound(payload);
    if (shelfFull()) return flash("Your sound shelf is full — remove one first");
    keepSound(payload);
  }
</script>

<div class="sound" class:ringing style="--sfx-ms:{ms}ms">
  <button
    type="button"
    class="play"
    class:ringing
    onclick={press}
    aria-label={`Play the sound "${recipe.name || "untitled"}"`}
    title={`${secs} — synthesized here, nothing downloaded`}
  >
    <span class="glyph" aria-hidden="true">{recipeGlyph(recipe)}</span>
    <span class="meta">
      <span class="name">{recipe.name || "Untitled sound"}</span>
      <!-- The length, and nothing else. "synthesized here, nothing downloaded"
           is a claim about the app, not about this sound, and it was the string
           that SET the chip's width — five chips in one column each restating
           it, crowding out the only part that differed. It moves to the chip's
           own tooltip, where somebody who wonders can still find it. -->
      <span class="sub">{secs}</span>
    </span>
    <!-- Four bars that move while it plays. A synthesized sound has no waveform
         to draw and no file to scrub, so this is not a rendering of the audio —
         it is the only honest thing a chip can say, which is "this is happening
         now". Transforms only, on four spans, for the length of the sound. -->
    <span class="wave" aria-hidden="true">
      <span></span><span></span><span></span><span></span>
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
  /* One width for sibling chips. `fit-content` sized each chip to its caption,
     which was fine while every caption ended in the same boast — and measured
     349, 349, 349, 349 and 333px in a column of five, a ragged edge caused by
     one sound whose name happened to be shorter. With the boast gone the widths
     would have varied by the length of a name, which is worse. A card in a
     stack of cards is one shape; the name inside it ellipsises. */
  .sound {
    position: relative;
    display: inline-flex;
    align-items: stretch;
    gap: 1px;
    margin-top: var(--sp-1);
    width: min(320px, 100%);
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
  /* The glyph pulses for exactly as long as the sound lasts. */
  .play.ringing .glyph {
    animation: sound-ring var(--dur-calm) var(--ease-spring) 3;
  }
  @keyframes sound-ring {
    50% {
      transform: scale(1.22);
    }
  }
  /* Pressed. A button that plays something has to acknowledge the press before
     the sound arrives, or a slow first context reads as a click that missed. */
  .play:active .glyph {
    transform: scale(0.92);
  }
  .play:active {
    background: var(--bg-2);
  }
  /* The bars: laid out but flat and invisible until it plays, so pressing does
     not change the width of the chip under the pointer. */
  .wave {
    display: flex;
    align-items: flex-end;
    gap: 2px;
    flex: none;
    height: 15px;
    margin-left: var(--sp-1);
    opacity: 0;
    transition: opacity var(--dur-quick) ease;
  }
  .wave span {
    display: block;
    width: 2px;
    height: 100%;
    border-radius: 1px;
    background: var(--accent);
    transform: scaleY(0.22);
    transform-origin: bottom;
  }
  .play.ringing .wave {
    opacity: 1;
  }
  .play.ringing .wave span {
    animation: sound-bar 0.42s ease-in-out infinite alternate;
  }
  .play.ringing .wave span:nth-child(2) {
    animation-delay: 0.1s;
    animation-duration: 0.3s;
  }
  .play.ringing .wave span:nth-child(3) {
    animation-delay: 0.05s;
    animation-duration: 0.5s;
  }
  .play.ringing .wave span:nth-child(4) {
    animation-delay: 0.16s;
    animation-duration: 0.36s;
  }
  @keyframes sound-bar {
    to {
      transform: scaleY(1);
    }
  }
  /* How much of it is left, along the bottom edge. --sfx-ms is the recipe's own
     total, so the line finishes exactly when the sound does. */
  .sound.ringing::after {
    content: "";
    position: absolute;
    left: 0;
    bottom: 0;
    height: 2px;
    background: var(--accent);
    animation: sound-run var(--sfx-ms, 600ms) linear both;
  }
  @keyframes sound-run {
    from {
      width: 0;
    }
    to {
      width: 100%;
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
    .play.ringing .glyph,
    .play.ringing .wave span,
    .sound.ringing::after {
      animation: none;
    }
    /* Still says "playing", without moving: the bars stand up and stay up. */
    .play.ringing .wave span {
      transform: scaleY(0.7);
    }
  }
</style>
