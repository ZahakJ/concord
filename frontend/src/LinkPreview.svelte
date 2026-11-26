<script>
  // A link card: the title, picture, channel or site and length that the
  // SENDER's device fetched once, at send time, and sent inside the message
  // (lib/linkcard.js). `card` comes from parseCard, never from anything this
  // device fetched.
  // Drawing it contacts nobody: the picture is an ordinary encrypted
  // attachment, fetched from other members like any other. The only things
  // that reach outside are taps — the title opens the link, and a YouTube
  // picture's play button loads YouTube's player in place.
  //
  // What the card says about itself (title, channel) is the author's, so it is
  // printed as plain text, isolated so its direction cannot run into the line
  // beside it. Where the link goes is NOT the author's to say: the domain is
  // computed from the URL (lib/regdomain.js) and printed on its own line, in
  // its own style, never cut short — a long channel name could otherwise push
  // it off the edge of a phone, leaving only the name the page chose. The site
  // line has already lost any address that is not this one (parseCard).
  //
  // Only a link whose real host is YouTube or Vimeo is drawn as a video: big
  // picture, length badge, play button. A page that calls itself a video is
  // drawn as a page.
  import { loadAttachment } from "./lib/attachments.js";
  import { ytEmbed } from "./lib/embeds.js";
  import { cardVideo } from "./lib/linkcard.js";

  let { card, channelId } = $props();

  const video = $derived(card.video || cardVideo(card.url));
  const big = $derived(!!card.thumb && video.kind !== "");
  let src = $state("");
  let failed = $state(false);
  let playing = $state(false);

  // Keyed on the blob, like Attachment.svelte: the token object is re-parsed on
  // every feed render, and only a different picture is a reason to refetch.
  let loadedBlob = "";
  $effect(() => {
    const tok = card.thumb;
    if (!tok || tok.blobId === loadedBlob) return;
    loadedBlob = tok.blobId;
    src = "";
    failed = false;
    loadAttachment(channelId, tok).then(
      (s) => {
        if (loadedBlob === tok.blobId) src = s;
      },
      () => {
        if (loadedBlob === tok.blobId) failed = true;
      },
    );
  });

  const ratio = $derived(card.thumb?.w && card.thumb?.h ? `${card.thumb.w} / ${card.thumb.h}` : "16 / 9");
</script>

{#snippet domainLine()}
  <span class="dom"
    ><svg width="12" height="12" viewBox="0 0 16 16" aria-hidden="true"
      ><path
        d="M6.5 9.5l3-3M7 4.5l1-1a2.8 2.8 0 014 4l-1 1M9 11.5l-1 1a2.8 2.8 0 01-4-4l1-1"
        fill="none"
        stroke="currentColor"
        stroke-width="1.6"
        stroke-linecap="round"
      /></svg
    ><span dir="ltr">{card.domain}</span></span
  >
{/snippet}

{#if big && !failed}
  <div class="lc video" data-link-href={card.url}>
    <div class="media" style:aspect-ratio={ratio}>
      {#if playing && video.kind === "yt"}
        <iframe
          src={ytEmbed(video.id)}
          title={card.title}
          allow="autoplay; fullscreen; picture-in-picture"
          allowfullscreen
          sandbox="allow-scripts allow-same-origin allow-presentation allow-popups"
        ></iframe>
      {:else}
        {#if src}<img {src} alt="" draggable="false" />{/if}
        {#if video.kind === "yt"}
          <button
            class="play"
            type="button"
            aria-label="Play {card.title} (loads YouTube)"
            onclick={() => (playing = true)}
          >
            <svg width="56" height="40" viewBox="0 0 46 32" aria-hidden="true">
              <rect width="46" height="32" rx="8" fill="#f00" />
              <path d="M18 9v14l13-7z" fill="#fff" />
            </svg>
          </button>
        {:else}
          <a class="cover" href={card.url} target="_blank" rel="noopener noreferrer" aria-label="Open {card.title}"></a>
        {/if}
        {#if card.duration}<span class="dur">{card.duration}</span>{/if}
      {/if}
    </div>
    <a class="text" href={card.url} target="_blank" rel="noopener noreferrer">
      <span class="title"><bdi>{card.title}</bdi></span>
      {#if card.site}<span class="by"><bdi class="site">{card.site}</bdi></span>{/if}
      {@render domainLine()}
    </a>
  </div>
{:else}
  <a class="lc page" href={card.url} target="_blank" rel="noopener noreferrer" data-link-href={card.url}>
    <span class="text">
      <span class="title"><bdi>{card.title}</bdi></span>
      {#if card.site || card.duration}
        <span class="by"
          >{#if card.site}<bdi class="site">{card.site}</bdi>{/if}{#if card.site && card.duration}<span
              class="dot"
              aria-hidden="true">·</span
            >{/if}{#if card.duration}<span>{card.duration}</span>{/if}</span
        >
      {/if}
      {@render domainLine()}
    </span>
    {#if card.thumb && !failed}
      <span class="side">{#if src}<img {src} alt="" draggable="false" />{/if}</span>
    {/if}
  </a>
{/if}

<style>
  .lc {
    display: flex;
    margin-top: 6px;
    max-width: min(420px, 100%);
    border-radius: var(--radius-md);
    background: var(--bg-1);
    border: 1px solid var(--border);
    color: var(--text);
    text-decoration: none;
    overflow: hidden;
  }
  .video {
    flex-direction: column;
  }
  .page {
    gap: var(--sp-3);
    padding: 10px 12px;
    min-height: var(--tap-min);
    align-items: center;
    transition: background var(--dur-quick) ease;
  }
  /* Pointer-only: Chromium latches :hover onto the last-tapped element. */
  @media (pointer: fine) {
    .page:hover,
    .video .text:hover {
      background: var(--bg-2);
    }
  }
  .page:active,
  .video .text:active {
    background: var(--bg-2);
  }
  .media {
    position: relative;
    width: 100%;
    background: var(--bg-0);
  }
  .media img,
  .media iframe {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    border: 0;
    display: block;
    object-fit: cover;
  }
  .play,
  .cover {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    padding: 0;
    background: rgb(0 0 0 / 0.18);
    border: 0;
    transition: background var(--dur-standard) ease;
  }
  .play svg {
    filter: drop-shadow(0 2px 6px rgb(0 0 0 / 0.45));
    transition: transform var(--dur-standard) var(--ease-spring);
  }
  @media (pointer: fine) {
    .play:hover {
      background: rgb(0 0 0 / 0.04);
    }
    .play:hover svg {
      transform: scale(1.1);
    }
  }
  .play:active svg {
    transform: scale(0.96);
  }
  :global(:root[data-motion="reduced"]) .play svg {
    transform: none !important;
  }
  .dur {
    position: absolute;
    right: 8px;
    bottom: 8px;
    padding: 1px 6px;
    border-radius: var(--radius-sm);
    background: rgb(0 0 0 / 0.78);
    color: #fff;
    font-size: var(--fs-small);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    pointer-events: none;
  }
  .text {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
    flex: 1;
    color: inherit;
    text-decoration: none;
  }
  .video .text {
    padding: 9px 12px 10px;
  }
  .title {
    font-weight: 600;
    font-size: var(--fs-ui);
    line-height: 1.3;
    color: var(--text);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    overflow-wrap: anywhere;
  }
  .by {
    font-size: var(--fs-small);
    color: var(--text-muted);
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dot {
    margin: 0 5px;
  }
  /* Where the link goes. Its own line, its own colour and weight, and it
     wraps rather than ever being cut: it is the one thing on the card the
     sender's page did not choose. */
  .dom {
    display: flex;
    align-items: flex-start;
    gap: 5px;
    margin-top: 1px;
    font-size: var(--fs-small);
    font-weight: 600;
    line-height: 1.35;
    color: var(--text);
    overflow-wrap: anywhere;
    unicode-bidi: isolate;
  }
  .dom svg {
    flex-shrink: 0;
    margin-top: 2px;
    color: var(--text-muted);
  }
  .side {
    width: 72px;
    height: 72px;
    flex-shrink: 0;
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: var(--bg-0);
  }
  .side img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
</style>
