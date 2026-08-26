<script>
  // One doodle, drawn in a message row.
  //
  // SVG rather than a canvas or an image, for the reason the whole format
  // exists: the strokes are geometry, so they stay crisp at any zoom and every
  // colour is a CSS token that follows the reader's theme. A doodle drawn at
  // midnight in the dark theme is readable at noon in the light one, which is
  // not true of anything raster.
  //
  // This component never validates. It is only ever handed the output of
  // decodeStrokes(), which has already refused anything outside the bounds —
  // so by the time a stroke reaches here, its colour and width resolve and its
  // points are inside the viewBox.
  import { DOODLE_W, DOODLE_H, strokePath, strokeColour, strokeWidth } from "./lib/doodle.js";
  import { plural } from "./lib/plural.js";

  let { strokes } = $props();

  // A drawing has no words, so it needs a described name of its own or it is a
  // blank hole to a screen reader. Stroke count is the only honest thing we can
  // say about a picture nobody transcribed.
  const label = $derived(`Doodle, ${plural(strokes.length, "stroke")}`);
</script>

<div class="doodle">
  <svg
    viewBox="0 0 {DOODLE_W} {DOODLE_H}"
    preserveAspectRatio="xMidYMid meet"
    role="img"
    aria-label={label}
    xmlns="http://www.w3.org/2000/svg"
  >
    {#each strokes as s, i (i)}
      <path
        d={strokePath(s)}
        stroke={strokeColour(s)}
        stroke-width={strokeWidth(s)}
        stroke-linecap="round"
        stroke-linejoin="round"
        fill="none"
      />
    {/each}
  </svg>
</div>

<style>
  /* The pad is a fixed 8:5, so the box reserves its height from the aspect
     ratio alone — no measurement, no reflow when the SVG paints. That matters
     here more than usual: the feed is windowed and sizes rows by measuring
     them, and a row that grows after it is measured moves the words. */
  .doodle {
    max-width: 420px;
    margin-top: var(--sp-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-1);
    overflow: hidden;
  }
  /* The strokes scale WITH the drawing — no non-scaling-stroke. A doodle is a
     picture, not a diagram: a line that stayed 12 screen pixels wide while the
     drawing around it shrank onto a phone would not be the drawing anyone
     made. */
  .doodle svg {
    display: block;
    width: 100%;
    aspect-ratio: 8 / 5;
  }
</style>
