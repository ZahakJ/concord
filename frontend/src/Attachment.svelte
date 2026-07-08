<script>
  // One encrypted image attachment: reserves layout space from the token's
  // dimensions, fetches + decrypts through the backend, and renders with
  // spinner / error+retry states.
  import Icon from "./Icon.svelte";
  import { loadAttachment } from "./lib/attachments.js";

  let { channelId, tok } = $props();

  let state = $state("loading"); // loading | done | error
  let src = $state("");
  let errMsg = $state("");

  // Reserve space: scale token dims into the same box the CSS allows.
  const MAXW = 380;
  const MAXH = 280;
  const dims = $derived.by(() => {
    if (!tok.w || !tok.h) return { w: 220, h: 140 };
    const scale = Math.min(MAXW / tok.w, MAXH / tok.h, 1);
    return { w: Math.round(tok.w * scale), h: Math.round(tok.h * scale) };
  });

  async function fetchNow() {
    state = "loading";
    try {
      src = await loadAttachment(channelId, tok);
      state = "done";
    } catch (err) {
      errMsg = String(err?.message || err);
      state = "error";
    }
  }

  $effect(() => {
    tok.blobId;
    fetchNow();
  });

  let lightbox = $state(false);
  function onKeydown(e) {
    if (lightbox && e.key === "Escape") lightbox = false;
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if state === "done"}
  <button class="frame done" onclick={() => (lightbox = true)} title="Click to enlarge">
    <img {src} alt="attachment" style="max-width:{MAXW}px;max-height:{MAXH}px" />
  </button>
  {#if lightbox}
    <button class="lightbox" onclick={() => (lightbox = false)} aria-label="Close image">
      <img {src} alt="attachment full size" />
    </button>
  {/if}
{:else}
  <div class="frame placeholder" style="width:{dims.w}px;height:{dims.h}px">
    {#if state === "loading"}
      <span class="spin"></span>
      <span class="muted small">fetching image…</span>
    {:else}
      <Icon name="close" size={18} />
      <span class="muted small">{errMsg.includes("not found") ? "no one online has this image yet" : "couldn't load image"}</span>
      <button class="ghost retry" onclick={fetchNow}>Retry</button>
    {/if}
  </div>
{/if}

<style>
  .frame {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    margin-top: 4px;
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .frame.done {
    background: transparent;
    padding: 0;
    display: block;
    width: fit-content;
    cursor: zoom-in;
  }
  .frame.done:hover {
    background: transparent;
  }
  .frame.done img {
    display: block;
    border-radius: var(--radius-sm);
  }
  .placeholder {
    background: var(--bg-1);
    border: 1px solid var(--border);
    color: var(--text-faint);
    max-width: 380px;
    max-height: 280px;
  }
  .spin {
    width: 20px;
    height: 20px;
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
  .small {
    font-size: 12px;
  }
  .retry {
    padding: 3px 12px;
    font-size: 12px;
  }
  .lightbox {
    position: fixed;
    inset: 0;
    z-index: 300;
    background: rgba(0, 0, 0, 0.85);
    display: grid;
    place-items: center;
    padding: 4vh 4vw;
    cursor: zoom-out;
    border-radius: 0;
  }
  .lightbox:hover {
    background: rgba(0, 0, 0, 0.85);
  }
  .lightbox img {
    max-width: 100%;
    max-height: 100%;
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-pop);
  }
</style>
