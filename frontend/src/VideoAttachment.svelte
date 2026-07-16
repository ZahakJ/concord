<script>
  // An inline video attachment (mp4/webm/…): a click-to-play card that fetches +
  // decrypts the blob on demand (so a channel full of clips doesn't download
  // them all on scroll), then swaps to a native <video> player. Rides the same
  // encrypted-file path as any attachment — nothing new server-side.
  import Icon from "./Icon.svelte";
  import { api } from "./lib/api.js";
  import { flash } from "./lib/state.svelte.js";

  let { channelId, tok } = $props();

  let src = $state(""); // decrypted object/data URL, once loaded
  let loading = $state(false);

  const sizeLabel = $derived(fmtSize(tok.size));
  function fmtSize(n) {
    if (!n) return "";
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
    return `${(n / 1024 / 1024).toFixed(1)} MB`;
  }

  async function load() {
    if (loading || src) return;
    loading = true;
    try {
      src = await api.fetchFile(channelId, tok.blobId, tok.keys, tok.mime);
    } catch (err) {
      flash(err);
    } finally {
      loading = false;
    }
  }
</script>

{#if src}
  <!-- svelte-ignore a11y_media_has_caption -->
  <video class="vid" controls autoplay {src}></video>
{:else}
  <button class="vid-card" onclick={load} title="Play {tok.name}">
    <span class="vid-play">
      {#if loading}<span class="vid-spin"></span>{:else}<Icon name="play" size={22} />{/if}
    </span>
    <span class="vid-meta">
      <span class="vid-name">{tok.name}</span>
      <span class="vid-sub muted">{sizeLabel} · click to play</span>
    </span>
  </button>
{/if}

<style>
  .vid {
    display: block;
    margin-top: 4px;
    max-width: min(440px, 100%);
    max-height: 360px;
    border-radius: var(--radius-md);
    background: #000;
    outline: 1px solid var(--border);
  }
  .vid-card {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 4px;
    width: min(340px, 100%);
    padding: 14px 16px;
    text-align: left;
    color: var(--text);
    /* A filmic dark tile so it reads as "video", distinct from a file card. */
    background: linear-gradient(135deg, #1a1d24, #0e1014);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    transition:
      border-color 0.13s ease,
      transform 0.1s ease;
  }
  .vid-card:hover {
    border-color: var(--accent);
  }
  .vid-card:active {
    transform: scale(0.99);
  }
  .vid-play {
    flex-shrink: 0;
    width: 44px;
    height: 44px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: var(--accent);
    color: #fff;
  }
  .vid-meta {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .vid-name {
    font-size: 13px;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .vid-sub {
    font-size: 11px;
  }
  .vid-spin {
    width: 20px;
    height: 20px;
    border: 2px solid rgba(255, 255, 255, 0.4);
    border-top-color: #fff;
    border-radius: 50%;
    animation: vid-rot 0.8s linear infinite;
  }
  @keyframes vid-rot {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .vid-card:active {
      transform: none;
    }
  }
</style>
