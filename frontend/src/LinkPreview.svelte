<script>
  // Link-preview card, drawn ONLY from what the sender embedded in the message.
  // Nothing here touches the network on its own: the picture is an encrypted
  // attachment blob (same path as any image), and the card is a plain link to
  // the very URL the message text shows. All text renders via interpolation,
  // never {@html}. `card` has already been validated by parseLinkEmbeds.
  import { loadAttachment } from "./lib/attachments.js";

  let { card, channelId } = $props();
  let img = $state("");

  const hostname = $derived.by(() => {
    try {
      return new URL(card.url).hostname.replace(/^www\./, "");
    } catch {
      return card.url;
    }
  });

  $effect(() => {
    const tok = card.image;
    img = "";
    if (!tok) return;
    let live = true;
    loadAttachment(channelId, tok).then(
      (d) => live && (img = d),
      () => {}, // no picture: the text card stands on its own
    );
    return () => (live = false);
  });
</script>

<a class="card" href={card.url} target="_blank" rel="noopener noreferrer">
  <span class="meta">
    <span class="site muted">{card.site || hostname}</span>
    {#if card.title}<span class="title">{card.title}</span>{/if}
    {#if card.desc}<span class="desc muted">{card.desc}</span>{/if}
  </span>
  {#if img}
    <img src={img} alt="" />
  {/if}
</a>

<style>
  .card {
    display: flex;
    gap: var(--sp-3);
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
      background var(--dur-quick) ease,
      box-shadow var(--dur-quick) ease,
      transform var(--dur-quick) ease;
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
