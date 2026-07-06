<script>
  // The one avatar. Renders, in priority order: uploaded image, profile emoji,
  // name/fingerprint initials — tinted by the member's accent color, with an
  // optional presence dot. Replaces five copy-pasted implementations.
  let {
    name = "",
    emoji = "",
    color = "",
    image = "",
    size = 32,
    online = null, // null hides the dot; true/false shows presence
  } = $props();

  const glyph = $derived(emoji || (name || "?").slice(0, 2));
</script>

<span
  class="avatar"
  style="width:{size}px;height:{size}px;font-size:{Math.round(size * 0.38)}px;{color
    ? `background:${color}`
    : ''}"
>
  {#if image}
    <img src={image} alt="" />
  {:else}
    {glyph}
  {/if}
  {#if online !== null}
    <span class="dot" class:online></span>
  {/if}
</span>

<style>
  .avatar {
    position: relative;
    border-radius: 50%;
    background: var(--accent);
    color: white;
    display: grid;
    place-items: center;
    font-weight: 600;
    text-transform: uppercase;
    flex-shrink: 0;
    user-select: none;
  }
  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    border-radius: 50%;
  }
  .dot {
    position: absolute;
    bottom: -1px;
    right: -1px;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--text-faint);
    border: 2px solid var(--bg-1);
  }
  .dot.online {
    background: var(--ok);
  }
</style>
