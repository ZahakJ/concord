<script>
  // An inline video attachment (mp4/webm/…). It renders as a real embedded
  // player — a proper video frame with the native centered play control — but
  // only fetches + decrypts the (encrypted) blob once it scrolls into view, so
  // a channel full of clips doesn't download them all at once. Rides the same
  // encrypted-file path as any attachment; nothing new server-side.
  import { onMount } from "svelte";
  import { api } from "./lib/api.js";
  import { flash } from "./lib/state.svelte.js";

  let { channelId, tok } = $props();

  let wrap = $state(null);
  let src = $state(""); // decrypted data URL, once loaded
  let loading = $state(false);
  let failed = $state(false);

  // Keep the frame from collapsing before the video loads (16:9 default), then
  // snap to the clip's real aspect once its metadata arrives so it isn't
  // letterboxed.
  let realRatio = $state("");
  const ratio = $derived(realRatio || (tok.w && tok.h ? `${tok.w} / ${tok.h}` : "16 / 9"));
  function onMeta(e) {
    const v = e.currentTarget;
    if (v.videoWidth && v.videoHeight) realRatio = `${v.videoWidth} / ${v.videoHeight}`;
  }

  async function load() {
    if (loading || src || failed) return;
    loading = true;
    try {
      src = await api.fetchFile(channelId, tok.blobId, tok.keys, tok.mime);
    } catch (err) {
      failed = true;
      flash(err);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    if (!("IntersectionObserver" in window)) {
      load(); // no observer support → just load it
      return;
    }
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          io.disconnect();
          load();
        }
      },
      { rootMargin: "200px" }, // start a touch before it's fully on screen
    );
    if (wrap) io.observe(wrap);
    return () => io.disconnect();
  });
</script>

<div class="vid-embed" bind:this={wrap} style="aspect-ratio:{ratio}">
  {#if src}
    <!-- svelte-ignore a11y_media_has_caption -->
    <!-- playsinline: without it iOS/WKWebView yanks the clip into the system
         fullscreen player the instant it starts, throwing the reader out of the
         conversation for a two-second video — and defeating the point of an
         embedded frame. -->
    <video
      class="vid"
      controls
      playsinline
      webkit-playsinline
      preload="metadata"
      onloadedmetadata={onMeta}
      {src}
    ></video>
  {:else}
    <div class="vid-ph" class:failed>
      {#if loading}
        <span class="vid-spin"></span>
      {:else if failed}
        <button class="vid-retry" onclick={() => ((failed = false), load())}>Couldn't load — retry</button>
      {:else}
        <span class="vid-idle"></span>
      {/if}
    </div>
  {/if}
</div>

<style>
  .vid-embed {
    position: relative;
    margin-top: 4px;
    width: min(420px, 100%);
    max-height: 360px;
    border-radius: var(--radius-md);
    overflow: hidden;
    background: #000;
    border: 1px solid var(--border);
  }
  .vid {
    display: block;
    width: 100%;
    height: 100%;
    max-height: 360px;
    object-fit: contain;
    background: #000;
  }
  .vid-ph {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    background: linear-gradient(135deg, #1a1d24, #0e1014);
  }
  .vid-spin {
    width: 26px;
    height: 26px;
    border: 3px solid rgba(255, 255, 255, 0.35);
    border-top-color: #fff;
    border-radius: 50%;
    animation: vid-rot 0.8s linear infinite;
  }
  .vid-retry {
    min-height: var(--tap-min);
    padding: 8px 14px;
    background: rgba(255, 255, 255, 0.12);
    color: #fff;
    border-radius: var(--radius-sm);
    font-size: var(--fs-ui);
  }
  .vid-retry:hover,
  .vid-retry:active {
    background: rgba(255, 255, 255, 0.2);
  }
  @keyframes vid-rot {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .vid-spin {
      animation: none;
    }
  }
</style>
