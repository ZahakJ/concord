<script>
  // Click-to-play YouTube player, for a YouTube link that came without a card.
  // Playback uses the privacy-enhanced youtube-nocookie domain, and every URL
  // is rebuilt from the validated 11-char video ID.
  //
  // autoload gates the THUMBNAIL. The thumbnail is a direct <img> to Google's
  // i.ytimg.com — a zero-click request that leaks the viewer's IP and online
  // time the moment a message scrolls into view. Messages are never allowed to
  // cause that, so callers pass autoload={false}: a poster drawn here, which
  // contacts nobody, and one tap on it is the consent and the play. (A YouTube
  // link that came with a card shows the card's own picture instead, see
  // LinkPreview.svelte.)
  //
  // The box keeps one size from poster to player, so nothing in the feed
  // moves when it starts. The player's frame may not open windows (no
  // allow-popups), so leaving for YouTube is the caption's link, ours.
  //
  // `url` rides on the box as data-link-href so holding or right-clicking it
  // offers Open link / Copy link like any other link.
  import { ytThumb, ytEmbed, ytWatch } from "./lib/embeds.js";
  import PlayGlyph from "./PlayGlyph.svelte";

  let { videoId, url = "", autoload = true, title = "" } = $props();
  const label = $derived(title || "YouTube video");
  let playing = $state(false);
  // The frame's own load event; until it fires a quiet veil covers the box.
  // The player could not be reached: our poster stays, with a retry.
  let stalled = $state(false);
  let thumbOk = $state(true);
  let box;
  // Chrome fires the frame's load event for its own "can't reach" page too,
  // and a cross-origin frame can't be looked into, so load alone never shows
  // the frame. It is shown once it loaded AND a fresh fetch of the video's
  // thumbnail from YouTube's image host came back: proof the network reaches
  // YouTube. Anything else within PLAYER_WAIT_MS, or going offline, keeps the
  // poster and says so. Chrome's error page is never what is on screen.
  const PLAYER_WAIT_MS = 8000;
  let loaded = $state(false);
  let reached = $state(false);
  let tries = 0;
  const ready = $derived(playing && loaded && reached && !stalled);

  function play() {
    playing = true;
    loaded = reached = false;
    stalled = typeof navigator !== "undefined" && navigator.onLine === false;
    if (stalled) return;
    const probe = new Image();
    const n = ++tries;
    probe.onload = () => n === tries && (reached = true);
    probe.onerror = () => n === tries && playing && !ready && (stalled = true);
    probe.src = `${ytThumb(videoId)}?r=${Date.now()}`;
  }
  function stop() {
    tries++;
    playing = false;
    loaded = reached = false;
    stalled = false;
    queueMicrotask(() => box?.querySelector("button")?.focus());
  }
  $effect(() => {
    if (!playing || ready || stalled) return;
    const t = setTimeout(() => (stalled = true), PLAYER_WAIT_MS);
    return () => clearTimeout(t);
  });
  $effect(() => {
    if (!playing || stalled) return;
    const off = () => (stalled = true);
    window.addEventListener("offline", off);
    return () => window.removeEventListener("offline", off);
  });
  function onKey(e) {
    if (e.key === "Escape" && playing) {
      e.stopPropagation();
      stop();
    }
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<figure class="ytw" aria-label={label} onkeydown={onKey}>
  <div class="yt" data-link-href={url || null} bind:this={box}>
    {#if playing && !stalled}
      <iframe
        src={ytEmbed(videoId)}
        title={label}
        allow="autoplay; fullscreen; picture-in-picture"
        allowfullscreen
        sandbox="allow-scripts allow-same-origin allow-presentation"
        class:ready
        onload={() => (loaded = true)}
      ></iframe>
      {#if !ready}
        <span class="veil" role="status"><span class="spin" aria-hidden="true"></span><span class="vh">Loading the player</span></span>
      {/if}
    {:else if stalled}
      <div class="fail" role="status">
        {#if autoload && thumbOk}
          <img src={ytThumb(videoId)} alt="" draggable="false" onerror={() => (thumbOk = false)} />
        {/if}
        <span class="fl">Can't reach YouTube</span>
        <span class="fh">You may be offline. The video plays once YouTube can be reached.</span>
        <button type="button" class="again" onclick={play}>Try again</button>
      </div>
    {:else}
      <button type="button" class="poster" onclick={play} aria-label="Play {label} (loads YouTube)">
        {#if autoload && thumbOk}
          <img src={ytThumb(videoId)} alt="" loading="lazy" draggable="false" onerror={() => (thumbOk = false)} />
        {/if}
        <span class="pmark"><PlayGlyph size={60} brand /></span>
        {#if !autoload || !thumbOk}
          <span class="pnote">Plays from YouTube · contacts Google</span>
        {/if}
      </button>
    {/if}
  </div>
  <figcaption class="cap">
    <span class="src">
      <svg width="14" height="10" viewBox="0 0 46 32" aria-hidden="true"><rect width="46" height="32" rx="8" fill="#e8202a" /><path d="M18 9v14l13-7z" fill="#fff" /></svg>
      <span>YouTube</span>
    </span>
    {#if playing}
      <button type="button" class="stop" onclick={stop}>Stop</button>
    {/if}
    <a class="watch" href={ytWatch(videoId)} target="_blank" rel="noopener noreferrer">Watch on YouTube<svg width="11" height="11" viewBox="0 0 16 16" aria-hidden="true"><path d="M6 3.5h6.5V10 M12.5 3.5 4 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg></a>
  </figcaption>
</figure>

<style>
  .ytw {
    margin: 6px 0 0;
    width: min(440px, 100%);
  }
  .yt {
    position: relative;
    width: 100%;
    aspect-ratio: 16 / 9;
    border-radius: var(--radius-md);
    overflow: hidden;
    /* Clips the player's own layers to the rounded corners in Chromium. */
    isolation: isolate;
    background: #0b0c0e;
    border: 1px solid var(--border);
  }
  iframe {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    border: 0;
    display: block;
    opacity: 0;
    transition: opacity var(--dur-standard) ease;
  }
  iframe.ready {
    opacity: 1;
  }
  /* The poster contacts nobody: a dark field lit from the top left, the
     play mark, and one line saying what a tap does. */
  .poster {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    padding: 0;
    border: 0;
    border-radius: 0;
    display: grid;
    place-items: center;
    background:
      radial-gradient(120% 90% at 18% 0%, rgb(232 32 42 / 0.22), transparent 60%),
      radial-gradient(90% 80% at 100% 100%, rgb(255 255 255 / 0.06), transparent 70%),
      linear-gradient(160deg, #1a1c20, #0b0c0e);
    color: #fff;
    cursor: pointer;
  }
  .poster img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .pmark {
    position: relative;
    display: grid;
    transition: transform var(--dur-standard) var(--ease-spring);
  }
  .pnote {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 10px;
    text-align: center;
    font-size: var(--fs-small);
    color: rgb(255 255 255 / 0.78);
    padding: 0 12px;
  }
  @media (pointer: fine) {
    .poster:hover .pmark {
      transform: scale(1.06);
    }
  }
  .poster:active .pmark {
    transform: scale(0.96);
  }
  .poster:focus-visible {
    outline: none;
  }
  .poster:focus-visible::after {
    content: "";
    position: absolute;
    inset: 0;
    border-radius: inherit;
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
    animation: yt-spin 0.8s linear infinite;
  }
  @keyframes yt-spin {
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
    isolation: isolate;
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
  /* The poster stays under the message, dimmed so the words read. */
  .fail img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
    opacity: 0.35;
    z-index: -1;
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
  .cap {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 6px;
    padding: 0 2px;
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .src {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-weight: 600;
    color: var(--text);
    margin-right: auto;
  }
  .watch,
  .stop {
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
  .watch:hover,
  .stop:hover {
    color: var(--text);
  }
  .watch:focus-visible,
  .stop:focus-visible,
  .again:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
    border-radius: 4px;
  }
  :global(:root[data-motion="reduced"]) .pmark {
    transform: none !important;
    transition: none;
  }
  :global(:root[data-motion="reduced"]) .spin {
    animation-duration: 2.4s;
  }
  :global(:root[data-motion="reduced"]) iframe {
    transition: none;
  }
</style>
