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
  import { ytEmbed, ytWatch } from "./lib/embeds.js";
  import { cardVideo } from "./lib/linkcard.js";
  import PlayGlyph from "./PlayGlyph.svelte";

  let { card, channelId } = $props();

  const video = $derived(card.video || cardVideo(card.url));
  // A video keeps its big layout even when its picture cannot be had: the
  // poster is drawn instead, and the box is the size it was going to be.
  const big = $derived(!!card.thumb && video.kind !== "");
  let src = $state("");
  let failed = $state(false);
  // The picture has painted: it fades in over the skeleton, never pops.
  let shown = $state(false);
  let playing = $state(false);
  // The player: its frame's load, and a wait that gives up (offline, blocked).
  let ready = $state(false);
  let stalled = $state(false);
  const PLAYER_WAIT_MS = 15000;
  let box;
  function play() {
    playing = true;
    ready = false;
    stalled = typeof navigator !== "undefined" && navigator.onLine === false;
  }
  function stop() {
    playing = false;
    ready = false;
    stalled = false;
    queueMicrotask(() => box?.querySelector("button")?.focus());
  }
  $effect(() => {
    if (!playing || ready || stalled) return;
    const t = setTimeout(() => (stalled = true), PLAYER_WAIT_MS);
    return () => clearTimeout(t);
  });
  function onKey(e) {
    if (e.key === "Escape" && playing) {
      e.stopPropagation();
      stop();
    }
  }

  // Keyed on the blob, like Attachment.svelte: the token object is re-parsed on
  // every feed render, and only a different picture is a reason to refetch.
  let loadedBlob = "";
  $effect(() => {
    const tok = card.thumb;
    if (!tok || tok.blobId === loadedBlob) return;
    loadedBlob = tok.blobId;
    src = "";
    shown = false;
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

{#if big}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div class="lc video" data-link-href={card.url} onkeydown={onKey} role="group" aria-label={card.title}>
    <div class="media" class:loading={!shown && !failed} style:aspect-ratio={ratio} bind:this={box}>
      {#if playing && video.kind === "yt" && !stalled}
        <iframe
          src={ytEmbed(video.id)}
          title={card.title}
          allow="autoplay; fullscreen; picture-in-picture"
          allowfullscreen
          sandbox="allow-scripts allow-same-origin allow-presentation"
          class:ready
          onload={() => (ready = true)}
        ></iframe>
        {#if !ready}
          <span class="veil" role="status"><span class="spin" aria-hidden="true"></span><span class="vh">Loading the player</span></span>
        {/if}
      {:else if stalled}
        <div class="fail" role="status">
          <span class="fl">The player didn't load</span>
          <span class="fh">You may be offline, or YouTube is unreachable.</span>
          <button type="button" class="again" onclick={play}>Try again</button>
        </div>
      {:else}
        {#if src && !failed}<img {src} alt="" draggable="false" class:shown onload={() => (shown = true)} />{/if}
        {#if failed}<span class="poster" aria-hidden="true"></span>{/if}
        {#if video.kind === "yt"}
          <button class="play" type="button" aria-label="Play {card.title} (loads YouTube)" onclick={play}>
            <PlayGlyph size={60} />
          </button>
        {:else}
          <a class="cover" href={card.url} target="_blank" rel="noopener noreferrer" aria-label="Open {card.title}"
            ><PlayGlyph size={52} /></a
          >
        {/if}
        {#if card.duration}<span class="dur">{card.duration}</span>{/if}
      {/if}
    </div>
    <div class="foot">
      <a class="text" href={card.url} target="_blank" rel="noopener noreferrer">
        <span class="title"><bdi>{card.title}</bdi></span>
        {#if card.site}<span class="by"><bdi class="site">{card.site}</bdi></span>{/if}
        {@render domainLine()}
      </a>
      {#if playing && video.kind === "yt"}
        <span class="acts">
          <button type="button" class="act" onclick={stop}>Stop</button>
          <a class="act" href={ytWatch(video.id)} target="_blank" rel="noopener noreferrer">Watch on YouTube<svg width="11" height="11" viewBox="0 0 16 16" aria-hidden="true"><path d="M6 3.5h6.5V10 M12.5 3.5 4 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg></a>
        </span>
      {/if}
    </div>
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
      <span class="side" class:loading={!shown}>{#if src}<img {src} alt="" draggable="false" class:shown onload={() => (shown = true)} />{/if}</span>
    {/if}
  </a>
{/if}

<style>
  .lc {
    display: flex;
    margin-top: 6px;
    max-width: min(440px, 100%);
    /* Clips the player's layers, and the picture, to the rounded corners. */
    isolation: isolate;
    border-radius: var(--radius-md);
    background: var(--bg-1);
    border: 1px solid var(--border);
    color: var(--text);
    text-decoration: none;
    overflow: hidden;
  }
  .video {
    flex-direction: column;
    width: min(440px, 100%);
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
  /* The picture comes from other members, so it can take a moment: a soft
     pulse holds its place at its final size, and the picture fades in. */
  .media img,
  .side img,
  .media iframe {
    opacity: 0;
    transition: opacity var(--dur-standard) ease;
  }
  .media img.shown,
  .side img.shown,
  .media iframe.ready {
    opacity: 1;
  }
  .media.loading::before,
  .side.loading::before {
    content: "";
    position: absolute;
    inset: 0;
    background: color-mix(in srgb, var(--text) 9%, transparent);
    animation: lc-pulse 1.4s ease-in-out infinite;
  }
  .side {
    position: relative;
  }
  @keyframes lc-pulse {
    50% {
      opacity: 0.45;
    }
  }
  /* No picture to be had: a poster of our own, at the same size. */
  .poster {
    position: absolute;
    inset: 0;
    background:
      radial-gradient(120% 90% at 18% 0%, color-mix(in srgb, var(--accent) 30%, transparent), transparent 60%),
      linear-gradient(160deg, #1a1c20, #0b0c0e);
  }
  .play,
  .cover {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    padding: 0;
    border: 0;
    border-radius: 0;
    /* A scrim from the bottom only, so the picture keeps its colour. */
    background: linear-gradient(to top, rgb(0 0 0 / 0.28), transparent 55%);
    cursor: pointer;
  }
  .play :global(.pg),
  .cover :global(.pg) {
    transition: transform var(--dur-standard) var(--ease-spring);
  }
  @media (pointer: fine) {
    .play:hover :global(.pg),
    .cover:hover :global(.pg) {
      transform: scale(1.06);
    }
  }
  .play:active :global(.pg),
  .cover:active :global(.pg) {
    transform: scale(0.96);
  }
  .play:focus-visible,
  .cover:focus-visible {
    outline: none;
    box-shadow: inset 0 0 0 3px var(--accent);
  }
  .veil {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    background: #0b0c0e;
  }
  .spin {
    width: 28px;
    height: 28px;
    border-radius: 50%;
    border: 3px solid rgb(255 255 255 / 0.16);
    border-top-color: #fff;
    animation: lc-spin 0.8s linear infinite;
  }
  @keyframes lc-spin {
    to {
      transform: rotate(360deg);
    }
  }
  .vh {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  .fail {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--sp-1);
    padding: var(--sp-3);
    text-align: center;
    background: linear-gradient(160deg, #1a1c20, #0b0c0e);
    color: #fff;
  }
  .fl {
    font-weight: 600;
    font-size: var(--fs-ui);
  }
  .fh {
    font-size: var(--fs-small);
    color: rgb(255 255 255 / 0.72);
  }
  .again {
    margin-top: 6px;
    min-height: 32px;
    padding: 0 14px;
    border-radius: 999px;
    border: 1px solid rgb(255 255 255 / 0.3);
    background: rgb(255 255 255 / 0.08);
    color: #fff;
    font: inherit;
    font-size: var(--fs-small);
    font-weight: 600;
    cursor: pointer;
  }
  .foot {
    display: flex;
    flex-direction: column;
  }
  .acts {
    display: flex;
    justify-content: flex-end;
    gap: 14px;
    padding: 0 12px 8px;
    font-size: var(--fs-small);
  }
  .act {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    min-height: 28px;
    padding: 0 2px;
    font: inherit;
    color: var(--text-muted);
    text-decoration: none;
    background: none;
    border: 0;
    cursor: pointer;
  }
  .act:hover {
    color: var(--text);
  }
  .act:focus-visible,
  .again:focus-visible,
  .text:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
    border-radius: 4px;
  }
  :global(:root[data-motion="reduced"]) .play :global(.pg),
  :global(:root[data-motion="reduced"]) .cover :global(.pg) {
    transform: none !important;
    transition: none;
  }
  :global(:root[data-motion="reduced"]) .media.loading::before,
  :global(:root[data-motion="reduced"]) .side.loading::before {
    animation: none;
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
