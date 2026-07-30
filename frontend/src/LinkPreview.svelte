<script>
  // Compact link-preview card. Fetches only once the card scrolls into view
  // (IntersectionObserver), so opening a link-heavy channel doesn't stampede
  // the backend. Fetched text renders via interpolation ONLY — never {@html}.
  import { loadPreview } from "./lib/embeds.js";
  import { S } from "./lib/state.svelte.js";

  let { url } = $props();
  let root = $state(null);
  let preview = $state(null);
  let attempted = $state(false);
  // A preview image that 404s / is blocked would otherwise render the browser's
  // broken-image glyph inside the card — hide it and keep the text-only card.
  let imgFailed = $state(false);

  const hostname = $derived.by(() => {
    try {
      return new URL(url).hostname.replace(/^www\./, "");
    } catch {
      return url;
    }
  });

  $effect(() => {
    // Privacy gate: fetching a preview discloses the viewer's IP to the link's
    // host, so it only happens when the user has opted into link previews.
    if (!root || attempted || !S.prefs.linkPreviews) return;
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
      {#if preview.imageUrl && !imgFailed}
        <img
          src={preview.imageUrl}
          alt=""
          loading="lazy"
          referrerpolicy="no-referrer"
          onerror={() => (imgFailed = true)}
        />
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
    min-height: var(--tap-min);
    padding: 10px 12px;
    border-left: 3px solid var(--accent);
    border-radius: var(--radius-sm);
    background: var(--bg-1);
    text-decoration: none;
    color: var(--text);
    transition:
      background 0.14s ease,
      box-shadow 0.14s ease,
      transform 0.14s ease;
  }
  /* Pointer-only: Chromium latches :hover onto the last-tapped element, which
     left a link card you had merely tapped floating above the feed for good. */
  @media (pointer: fine) {
    .card:hover {
      background: var(--bg-3);
      box-shadow: 0 2px 10px rgb(0 0 0 / 0.14);
      transform: translateY(-1px);
    }
  }
  .card:active {
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
    font-size: var(--fs-small);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .title {
    font-weight: 600;
    font-size: var(--fs-ui);
    color: var(--accent-hover);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .desc {
    font-size: var(--fs-compact);
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
