<script>
  // A group DM's bubble: the members' avatars tiled inside one circle, divided
  // across the peers. Falls back to a single Avatar for one face.
  import Avatar from "./Avatar.svelte";

  let { faces = [], size = 42 } = $props();

  // Show at most 4 slices; a 4th slot becomes "+N" when there are more.
  const shown = $derived(faces.slice(0, faces.length > 4 ? 3 : 4));
  const overflow = $derived(faces.length > 4 ? faces.length - 3 : 0);
  const count = $derived(shown.length + (overflow ? 1 : 0));

  const glyph = (f) => f.emoji || (f.name || "?").slice(0, 2);
  let failed = $state({});
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
        <!-- Same fallback Avatar has: a picture that will not load must leave
             the initials behind, not a broken-image box in the middle of a
             collage. -->
        {#if f.avatar && !failed[f.avatar]}
          <img src={f.avatar} alt="" onerror={() => (failed = { ...failed, [f.avatar]: true })} />
        {:else}{glyph(f)}{/if}
      </span>
    {/each}
    {#if overflow}
      <span class="cell more">+{overflow}</span>
    {/if}
  </span>
{/if}

<style>
  /* A group DM's collage is an avatar like any other, so it follows the theme's
     silhouette. Pinned round, it stayed a circle in the squared-off themes while
     every single-person avatar beside it went square — the odd one out in the
     same list. The tiles inside stay hard-edged; the mask on the outside is what
     gives the shape. */
  .collage {
    position: relative;
    border-radius: var(--avatar-radius, 50%);
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
    color: var(--accent-fg);
    font-weight: 600;
    text-transform: uppercase;
    overflow: hidden;
    min-width: 0;
  }
  .cell img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    animation: img-in 0.18s ease;
  }
  @keyframes img-in {
    from {
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .cell img {
      animation: none;
    }
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
