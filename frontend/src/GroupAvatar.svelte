<script>
  // A group DM's bubble: the members' avatars tiled inside one circle, divided
  // across the peers (Discord-style). Falls back to a single Avatar for one face.
  import Avatar from "./Avatar.svelte";

  let { faces = [], size = 42 } = $props();

  // Show at most 4 slices; a 4th slot becomes "+N" when there are more.
  const shown = $derived(faces.slice(0, faces.length > 4 ? 3 : 4));
  const overflow = $derived(faces.length > 4 ? faces.length - 3 : 0);
  const count = $derived(shown.length + (overflow ? 1 : 0));

  const glyph = (f) => f.emoji || (f.name || "?").slice(0, 2);
  const cellFont = $derived(Math.round(size * 0.26) + "px");
</script>

{#if faces.length <= 1}
  <Avatar
    name={faces[0]?.name || "?"}
    image={faces[0]?.avatar || ""}
    color={faces[0]?.color || ""}
    emoji={faces[0]?.emoji || ""}
    {size}
  />
{:else}
  <span
    class="collage n{count}"
    style="width:{size}px;height:{size}px;font-size:{cellFont}"
    title={faces.map((f) => f.name).join(", ")}
  >
    {#each shown as f, i (i)}
      <span
        class="cell"
        class:pending={f.pending}
        title={f.pending ? `${f.name} (invited)` : f.name}
        style={f.color ? `background:${f.color}` : ""}
      >
        {#if f.avatar}<img src={f.avatar} alt="" />{:else}{glyph(f)}{/if}
      </span>
    {/each}
    {#if overflow}
      <span class="cell more">+{overflow}</span>
    {/if}
  </span>
{/if}

<style>
  .collage {
    position: relative;
    border-radius: 50%;
    overflow: hidden;
    display: grid;
    gap: 1.5px;
    background: var(--bg-1);
    flex-shrink: 0;
    user-select: none;
  }
  /* Two peers: split down the middle. */
  .collage.n2 {
    grid-template-columns: 1fr 1fr;
  }
  /* Three: one full-height on the left, two stacked on the right. */
  .collage.n3 {
    grid-template-columns: 1fr 1fr;
    grid-template-rows: 1fr 1fr;
  }
  .collage.n3 .cell:first-child {
    grid-row: 1 / 3;
  }
  /* Four (or 3 + overflow): quadrants. */
  .collage.n4 {
    grid-template-columns: 1fr 1fr;
    grid-template-rows: 1fr 1fr;
  }
  .cell {
    display: grid;
    place-items: center;
    background: var(--accent);
    color: #fff;
    font-weight: 600;
    text-transform: uppercase;
    overflow: hidden;
    min-width: 0;
  }
  .cell img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  /* Invited-but-not-joined members show faded, so the group reads as complete
     while making clear who hasn't landed yet. */
  .cell.pending {
    opacity: 0.45;
  }
  .cell.more {
    background: var(--bg-3);
    color: var(--text-muted);
    font-size: 0.85em;
  }
</style>
