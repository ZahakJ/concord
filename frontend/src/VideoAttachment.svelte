<script>
  // An inline video attachment (mp4/webm/…). It renders as a real embedded
  // player — a proper video frame with the native centered play control — but
  // only fetches + decrypts the (encrypted) blob once it scrolls into view, so
  // a channel full of clips doesn't download them all at once. Rides the same
  // encrypted-file path as any attachment; nothing new server-side.
  import { onMount } from "svelte";
  import { api } from "./lib/api.js";
  import { S, memberByFpr, nameFor } from "./lib/state.svelte.js";
  import { fmtBytes, unavailableNote, worthRetrying } from "./lib/attachments.js";

  // `sender` is the fingerprint of whoever posted the clip — see
  // Attachment.svelte for why a failed fetch has to be able to name them.
  let { channelId, tok, sender = "" } = $props();

  let wrap = $state(null);
  let src = $state(""); // decrypted data URL, once loaded
  let loading = $state(false);
  let failed = $state(false);
  let errMsg = $state("");

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
    if (loading || src) return;
    loading = true;
    failed = false;
    try {
      src = await api.fetchFile(channelId, tok.blobId, tok.keys, tok.mime);
    } catch (err) {
      errMsg = String(err?.message || err);
      failed = true;
      // Deliberately no toast: the frame itself now carries the reason, and a
      // channel of clips whose only holder went offline would otherwise stack
      // up one error toast per clip for something nobody asked to do.
    } finally {
      loading = false;
    }
  }

  // Built here rather than in the markup: Svelte trims the whitespace around a
  // block boundary, so an inline `{#if}` around the separator glued the size to
  // the name ("clip.mp4· 11 KB").
  const label = $derived(tok.size ? `${tok.name} · ${fmtBytes(tok.size)}` : tok.name);

  const senderRow = $derived(sender ? memberByFpr(sender) : undefined);
  const note = $derived(
    unavailableNote(errMsg, {
      name: senderRow ? nameFor(sender) : "",
      self: !!sender && sender === S.identity?.fingerprint,
    }),
  );

  // Same roster-driven retry as an image: the clip was never broken, only
  // unreachable, so somebody becoming reachable is the event that fixes it.
  let seenOnline = null;
  let lastTry = 0;
  $effect(() => {
    const now = new Set(
      (S.members || []).filter((m) => m.online).map((m) => m.fingerprint),
    );
    const prev = seenOnline;
    seenOnline = now;
    if (!failed || loading) return;
    if (!worthRetrying(prev, now, Date.now() - lastTry)) return;
    lastTry = Date.now();
    load();
  });

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
        <!-- Name the clip and its size: the token carries both, and a reader
             staring at a black rectangle should at least know what it is. -->
        <div class="vid-gone">
          <span class="vid-name">{label}</span>
          <span class="vid-note">{note}</span>
          <button class="vid-retry" onclick={load}>Retry</button>
        </div>
      {:else}
        <span class="vid-idle"></span>
      {/if}
    </div>
  {/if}
</div>

<style>
  .vid-embed {
    position: relative;
    margin-top: var(--sp-1);
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
  .vid-gone {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-1);
    padding: var(--sp-3);
    text-align: center;
  }
  .vid-name {
    font-size: var(--fs-compact);
    font-weight: 600;
    color: rgba(255, 255, 255, 0.9);
    max-width: 34ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .vid-note {
    font-size: var(--fs-compact);
    line-height: 1.4;
    color: rgba(255, 255, 255, 0.6);
    max-width: 34ch;
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
