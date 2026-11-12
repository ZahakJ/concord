<script>
  // The face of a guild that has no icon (lib/rail.js guildMarks): one letter
  // in the display face, or a glyph when an earlier guild has that letter.
  // The tile around it paints the ground; this is only the mark.
  import Icon from "./Icon.svelte";
  import { S } from "./lib/state.svelte.js";
  import { guildMarks } from "./lib/rail.js";

  let { g, size = 14 } = $props();
  const marks = $derived(guildMarks(S.guilds));
  const mark = $derived(marks.get(g?.id) || { letter: ([...String(g?.name || "")][0] || "?").toUpperCase(), glyph: "" });
</script>

{#if mark.glyph}<Icon name={mark.glyph} {size} />{:else}<span class="gm-letter">{mark.letter}</span>{/if}

<style>
  .gm-letter {
    font-family: var(--display-font);
    font-weight: 600;
    line-height: 1;
  }
</style>
