<script>
  // Compact link-preview card. Fetches only once the card scrolls into view
  // (IntersectionObserver), so opening a link-heavy channel doesn't stampede
  // the backend. Fetched text renders via interpolation ONLY — never {@html}.
  import { loadPreview } from "./lib/embeds.js";

  let { url } = $props();
  let root = $state(null);
  let preview = $state(null);
  let attempted = $state(false);

  const hostname = $derived.by(() => {
    try {
      return new URL(url).hostname.replace(/^www\./, "");
    } catch {
      return url;
    }
  });

  $effect(() => {
    if (!root || attempted) return;
    const io = new IntersectionObserver(async (entries) => {
      if (!entries.some((e) => e.isIntersecting) || attempted) return;
      attempted = true;
      io.disconnect();
      preview = await loadPreview(url);
    });
    io.observe(root);
    return () => io.disconnect();
  });
</script>

<span bind:this={root}>
  {#if preview?.title}
    <a class="card" href={url} target="_blank" rel="noopener noreferrer">
      <span class="meta">
        <span class="site muted">{preview.siteName || hostname}</span>
        <span class="title">{preview.title}</span>
        {#if preview.description}<span class="desc muted">{preview.description}</span>{/if}
      </span>
      {#if preview.imageUrl}
        <img src={preview.imageUrl} alt="" loading="lazy" referrerpolicy="no-referrer" />
      {/if}
    </a>
  {/if}
</span>

<style>
  .card {
    display: flex;
    gap: 12px;
    margin-top: 6px;
    max-width: 460px;
    padding: 10px 12px;
    border-left: 3px solid var(--accent);
    border-radius: var(--radius-sm);
    background: var(--bg-1);
    text-decoration: none;
    color: var(--text);
  }
  .card:hover {
    background: var(--bg-3);
  }
  .meta {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
    flex: 1;
  }
  .site {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .title {
    font-weight: 600;
    font-size: 14px;
    color: var(--accent-hover);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .desc {
    font-size: 12px;
    line-height: 1.4;
    display: -webkit-box;
    -webkit-line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  img {
    width: 70px;
    height: 70px;
    object-fit: cover;
    border-radius: var(--radius-sm);
    flex-shrink: 0;
  }
</style>
