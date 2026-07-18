<script>
  // Click-to-play YouTube embed. Playback uses the privacy-enhanced
  // youtube-nocookie domain. All URLs are rebuilt from the validated 11-char
  // video ID.
  //
  // autoload gates the THUMBNAIL. The thumbnail is a direct <img> to Google's
  // i.ytimg.com — a zero-click request that leaks the viewer's IP and online
  // time the moment a message scrolls into view. When link previews are off we
  // must not fire it: show a neutral placeholder and only reach out to Google
  // once the user explicitly clicks.
  import { ytThumb, ytEmbed } from "./lib/embeds.js";

  let { videoId, autoload = true } = $props();
  let playing = $state(false);
  // Reveal the (network-touching) thumbnail only when previews are allowed, or
  // after the user opts in by clicking the placeholder.
  let revealed = $state(autoload);
</script>

<div class="yt">
  {#if playing}
    <iframe
      src={ytEmbed(videoId)}
      title="YouTube video"
      allow="autoplay; fullscreen; picture-in-picture"
      allowfullscreen
    ></iframe>
  {:else if revealed}
    <button class="thumb" onclick={() => (playing = true)} aria-label="Play YouTube video">
      <img src={ytThumb(videoId)} alt="YouTube thumbnail" loading="lazy" />
      <span class="play">
        <svg width="46" height="32" viewBox="0 0 46 32" aria-hidden="true">
          <rect width="46" height="32" rx="8" fill="#f00" />
          <path d="M18 9v14l13-7z" fill="#fff" />
        </svg>
      </span>
    </button>
  {:else}
    <button class="placeholder" onclick={() => (revealed = true)} aria-label="Load YouTube preview">
      <span class="pi" aria-hidden="true">
        <svg width="40" height="28" viewBox="0 0 46 32"><rect width="46" height="32" rx="8" fill="#f00" /><path d="M18 9v14l13-7z" fill="#fff" /></svg>
      </span>
      <span class="pl">Load YouTube preview</span>
      <span class="ph muted">Previews are off · this contacts Google</span>
    </button>
  {/if}
</div>

<style>
  .yt {
    margin-top: 6px;
    width: min(420px, 100%);
    aspect-ratio: 16 / 9;
    border-radius: var(--radius-md);
    overflow: hidden;
    background: var(--bg-0);
    border: 1px solid var(--border);
  }
  iframe {
    width: 100%;
    height: 100%;
    border: 0;
    display: block;
  }
  .placeholder {
    width: 100%;
    height: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    padding: 12px;
    background: var(--bg-1);
    text-align: center;
    transition: background 0.12s ease;
  }
  .placeholder:hover {
    background: var(--bg-2);
  }
  .pi {
    opacity: 0.9;
  }
  .pl {
    font-size: 13px;
    font-weight: 600;
    color: var(--text);
  }
  .ph {
    font-size: 11px;
  }
  .thumb {
    position: relative;
    width: 100%;
    height: 100%;
    padding: 0;
    background: transparent;
    display: block;
  }
  .thumb:hover {
    background: transparent;
  }
  .thumb img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  .play {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    background: rgba(0, 0, 0, 0.25);
    transition: background 0.15s ease;
  }
  .thumb:hover .play {
    background: rgba(0, 0, 0, 0.05);
  }
  .play svg {
    filter: drop-shadow(0 2px 6px rgba(0, 0, 0, 0.45));
    transition:
      transform 0.15s cubic-bezier(0.34, 1.56, 0.64, 1),
      filter 0.15s ease;
  }
  /* The red button swells slightly under the cursor — the whole thumbnail
     feels like a play control. */
  .thumb:hover .play svg {
    transform: scale(1.12);
    filter: drop-shadow(0 3px 10px rgba(0, 0, 0, 0.5));
  }
  .thumb:active .play svg {
    transform: scale(0.96);
  }
  @media (prefers-reduced-motion: reduce) {
    .thumb:hover .play svg,
    .thumb:active .play svg {
      transform: none;
    }
  }
</style>
