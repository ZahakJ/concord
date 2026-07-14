<script>
  // A non-image file attachment: a compact card with icon, name, size, and a
  // download button that fetches + decrypts the blob on demand.
  import Icon from "./Icon.svelte";
  import { api } from "./lib/api.js";
  import { flash } from "./lib/state.svelte.js";

  let { channelId, tok } = $props();

  let busy = $state(false);

  const sizeLabel = $derived(fmtSize(tok.size));
  // Only show an extension badge when the name actually has one (so "README"
  // doesn't render a bogus "READM" badge).
  const ext = $derived(
    tok.name.includes(".") ? tok.name.split(".").pop().slice(0, 5).toUpperCase() : "",
  );

  function fmtSize(n) {
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
    return `${(n / 1024 / 1024).toFixed(1)} MB`;
  }

  async function download() {
    if (busy) return;
    busy = true;
    try {
      const dataUrl = await api.fetchFile(channelId, tok.blobId, tok.keys, tok.mime);
      const a = document.createElement("a");
      a.href = dataUrl;
      a.download = tok.name;
      a.click();
    } catch (err) {
      flash(err);
    } finally {
      busy = false;
    }
  }
</script>

<button class="file-card" onclick={download} title="Download {tok.name}">
  <span class="thumb">
    {#if busy}
      <span class="spin"></span>
    {:else}
      <Icon name="attach" size={18} />
    {/if}
    {#if ext && !busy}<span class="ext">{ext}</span>{/if}
  </span>
  <span class="meta">
    <span class="name">{tok.name}</span>
    <span class="muted sub">{sizeLabel} · click to download</span>
  </span>
  <span class="dl"><Icon name="download" size={16} /></span>
</button>

<style>
  .file-card {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 4px;
    max-width: 340px;
    padding: 10px 12px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    text-align: left;
    color: var(--text);
    transition:
      background 0.13s ease,
      border-color 0.13s ease,
      box-shadow 0.13s ease;
  }
  .file-card:hover {
    background: var(--bg-3);
    border-color: var(--accent);
    box-shadow: 0 2px 8px rgb(0 0 0 / 0.14);
  }
  .file-card:active {
    background: var(--bg-2);
  }
  .thumb {
    position: relative;
    width: 38px;
    height: 38px;
    flex-shrink: 0;
    display: grid;
    place-items: center;
    border-radius: var(--radius-sm);
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .ext {
    position: absolute;
    bottom: -3px;
    right: -3px;
    font-size: 8px;
    font-weight: 700;
    background: var(--accent);
    color: white;
    padding: 1px 3px;
    border-radius: 3px;
    letter-spacing: 0.02em;
  }
  .meta {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }
  .name {
    font-size: 13px;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sub {
    font-size: 11px;
  }
  .dl {
    color: var(--text-muted);
    flex-shrink: 0;
    transition:
      color 0.13s ease,
      transform 0.13s ease;
  }
  .file-card:hover .dl {
    color: var(--accent-hover);
    /* the arrow dips toward the "download" — a small directional cue */
    transform: translateY(2px);
  }
  @media (prefers-reduced-motion: reduce) {
    .file-card:hover .dl {
      transform: none;
    }
  }
  .spin {
    width: 18px;
    height: 18px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: rot 0.8s linear infinite;
  }
  @keyframes rot {
    to {
      transform: rotate(360deg);
    }
  }
</style>
