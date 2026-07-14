<script>
  // Click-to-play YouTube embed. Nothing but the thumbnail loads until the
  // user clicks; playback uses the privacy-enhanced youtube-nocookie domain.
  // All URLs are rebuilt from the validated 11-char video ID.
  import { ytThumb, ytEmbed } from "./lib/embeds.js";

  let { videoId } = $props();
  let playing = $state(false);
</script>

<div class="yt">
  {#if playing}
    <iframe
      src={ytEmbed(videoId)}
      title="YouTube video"
      allow="autoplay; fullscreen; picture-in-picture"
      allowfullscreen
    ></iframe>
  {:else}
    <button class="thumb" onclick={() => (playing = true)} aria-label="Play YouTube video">
      <img src={ytThumb(videoId)} alt="YouTube thumbnail" loading="lazy" />
      <span class="play">
        <svg width="46" height="32" viewBox="0 0 46 32" aria-hidden="true">
          <rect width="46" height="32" rx="8" fill="#f00" />
          <path d="M18 9v14l13-7z" fill="#fff" />
        </svg>
      </span>
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
