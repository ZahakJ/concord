<script>
  // The gallery's viewer: one photo or video from a conversation's Media tab,
  // fitted inside the window (lib/viewerfit.js, the feed viewer's own fit),
  // with "Go to message" to leave for the message it came in, Save, Copy for
  // a photo, and Previous / Next through what the gallery has loaded. Esc,
  // Back and a backdrop click close it.
  import Icon from "./Icon.svelte";
  import { api } from "./lib/api.js";
  import { loadAttachment, copyImageToClipboard, saveImageSrc, unavailableNote } from "./lib/attachments.js";
  import { KIND_VIDEO, noteDuration, unsignedItem } from "./lib/gallery.js";
  import { UNSIGNED_LABEL, UNSIGNED_TIP } from "./lib/unsigned.js";
  import { tooltip } from "./lib/tooltip.js";
  import { fitSize, MARGIN_DESKTOP, MARGIN_PHONE } from "./lib/viewerfit.js";
  import { pushLayer } from "./lib/navstack.svelte.js";
  import { portal } from "./lib/portal.js";
  import { focusOnMount } from "./lib/focus.js";
  import { S, flash, openContextMenu } from "./lib/state.svelte.js";
  import { saveBlob } from "./lib/savefile.js";

  let { item, index = 0, count = 1, onClose, onGoto, onStep, onLoaded = () => {} } = $props();

  const video = $derived(item.kind === KIND_VIDEO);
  // The feed's "unsigned copy" mark for the message this came in.
  const unsigned = $derived(unsignedItem(item, S.identity.fingerprint));
  let src = $state("");
  let err = $state("");
  let natural = $state(null);
  let vp = $state({ w: window.innerWidth, h: window.innerHeight });
  const onResize = () => (vp = { w: window.innerWidth, h: window.innerHeight });

  // A different item (Previous / Next) loads its own bytes; a stale answer
  // for the one before is dropped.
  let want = "";
  $effect(() => {
    const key = `${item.messageId}:${item.ord}`;
    if (key === want) return;
    want = key;
    src = "";
    err = "";
    natural = item.w && item.h ? { w: item.w, h: item.h } : null;
    const p = video
      ? api.fetchFile(item.channelId, item.blobId, item.keys, item.mime)
      : loadAttachment(item.channelId, { blobId: item.blobId, keys: item.keys, subtype: item.subtype });
    p.then(
      (s) => {
        if (want !== key) return;
        src = s;
        onLoaded(item);
      },
      (e) => {
        if (want === key) err = String(e?.message || e);
      },
    );
  });

  const fit = $derived(natural && vp.w ? fitSize(natural, vp, vp.w < 768 ? MARGIN_PHONE : MARGIN_DESKTOP) : null);

  function onImgLoad(e) {
    const el = e.currentTarget;
    if (el.naturalWidth) natural = { w: el.naturalWidth, h: el.naturalHeight };
  }
  function onMeta(e) {
    const v = e.currentTarget;
    if (v.videoWidth) natural = { w: v.videoWidth, h: v.videoHeight };
    noteDuration(item.blobId, v.duration);
  }

  $effect(() => pushLayer("lightbox", () => onClose()));

  function onKey(e) {
    if (e.key === "Escape") {
      e.stopPropagation();
      onClose();
    } else if (e.key === "ArrowRight" && index < count - 1) onStep(1);
    else if (e.key === "ArrowLeft" && index > 0) onStep(-1);
  }

  const fileName = () => item.name || `concord-${item.blobId.slice(0, 8)}${video ? "" : `.${item.subtype || "png"}`}`;
  async function save() {
    if (!src) return;
    if (video) {
      const how = await saveBlob(fileName(), await (await fetch(src)).blob());
      if (!how) flash("Couldn't save that video");
      return;
    }
    const how = await saveImageSrc(src, fileName());
    if (how === "gallery") flash("Saved to your gallery", "success");
    else if (!how) flash("Couldn't save that image");
  }
  async function copy() {
    try {
      await copyImageToClipboard(src);
      flash("Image copied", "success");
    } catch (e) {
      flash(`Couldn't copy image: ${e?.message || e}`);
    }
  }
  function menu(e) {
    openContextMenu(e, [
      { label: "Go to message", icon: "bubble", onClick: onGoto },
      src && { label: "Save", icon: "download", onClick: save },
      src && !video && { label: "Copy image", icon: "copy", onClick: copy },
    ]);
  }

  let overlayEl = $state(null);
  function onBackdrop(e) {
    if (e.target === overlayEl) onClose();
  }
</script>

<svelte:window onkeydown={onKey} onresize={onResize} />

<!-- svelte-ignore a11y_no_noninteractive_element_interactions, a11y_click_events_have_key_events -->
<div
  class="gv"
  use:portal
  role="dialog"
  aria-modal="true"
  aria-label={video ? "Video viewer" : "Image viewer"}
  bind:this={overlayEl}
  onclick={onBackdrop}
>
  {#if src && video}
    <!-- svelte-ignore a11y_media_has_caption -->
    <video
      class="gv-media"
      {src}
      controls
      autoplay
      playsinline
      onloadedmetadata={onMeta}
      oncontextmenu={menu}
      style={fit ? `width:${fit.w}px;height:${fit.h}px` : ""}
    ></video>
  {:else if src}
    <img
      class="gv-media"
      class:unsized={!fit}
      {src}
      alt={item.desc || item.name || "Photo"}
      draggable="false"
      onload={onImgLoad}
      oncontextmenu={menu}
      style={fit ? `width:${fit.w}px;height:${fit.h}px` : ""}
    />
  {:else if err}
    <div class="gv-msg">
      <Icon name="close" size={20} />
      <span>{unavailableNote(err)}</span>
    </div>
  {:else}
    <span class="gv-spin" aria-label="Loading"></span>
  {/if}

  <div class="gv-bar">
    {#if count > 1}
      <button class="gv-btn" onclick={() => onStep(-1)} disabled={index <= 0} aria-label="Previous">
        <Icon name="back" size={18} />
      </button>
      <span class="gv-pill">{index + 1} of {count}</span>
      <button class="gv-btn" onclick={() => onStep(1)} disabled={index >= count - 1} aria-label="Next">
        <Icon name="chevron" size={18} />
      </button>
    {/if}
    {#if unsigned}
      <span class="gv-pill gv-uns" data-testid="unsigned-mark" use:tooltip={UNSIGNED_TIP}
        ><Icon name="info" size={12} /> {UNSIGNED_LABEL}</span
      >
    {/if}
    <button class="gv-goto" onclick={onGoto}>Go to message</button>
    {#if src && !video}
      <button class="gv-btn" onclick={copy} aria-label="Copy image"><Icon name="copy" size={18} /></button>
    {/if}
    <button class="gv-btn" onclick={save} disabled={!src} aria-label={video ? "Save video" : "Save image"}>
      <Icon name="download" size={18} />
    </button>
    <button class="gv-btn" onclick={onClose} aria-label="Close (Esc)" use:focusOnMount>
      <Icon name="close" size={18} />
    </button>
  </div>
</div>

<style>
  .gv {
    position: fixed;
    inset: 0;
    z-index: 300;
    background: rgba(0, 0, 0, 0.88);
    display: grid;
    place-items: center;
    overflow: hidden;
    animation: gv-in var(--dur-standard) ease;
  }
  @keyframes gv-in {
    from {
      opacity: 0;
    }
  }
  .gv-media {
    max-width: none;
    max-height: none;
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-pop);
    user-select: none;
    -webkit-user-drag: none;
    background: #000;
  }
  img.unsized {
    visibility: hidden;
  }
  .gv-msg {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-2);
    max-width: 32ch;
    text-align: center;
    color: rgba(255, 255, 255, 0.85);
    font-size: var(--fs-compact);
  }
  .gv-spin {
    width: 26px;
    height: 26px;
    border: 2px solid rgba(255, 255, 255, 0.25);
    border-top-color: #fff;
    border-radius: 50%;
    animation: gv-rot 0.8s linear infinite;
  }
  @keyframes gv-rot {
    to {
      transform: rotate(360deg);
    }
  }
  .gv-bar {
    position: absolute;
    top: calc(14px + var(--safe-top));
    right: calc(16px + var(--safe-right));
    left: calc(16px + var(--safe-left));
    display: flex;
    justify-content: flex-end;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .gv-pill {
    font-size: var(--fs-compact);
    font-variant-numeric: tabular-nums;
    color: rgba(255, 255, 255, 0.85);
    background: rgba(0, 0, 0, 0.45);
    border-radius: 999px;
    padding: 3px 10px;
  }
  .gv-uns {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
  }
  .gv-btn,
  .gv-goto {
    display: grid;
    place-items: center;
    height: 34px;
    border-radius: 999px;
    background: rgba(0, 0, 0, 0.5);
    color: rgba(255, 255, 255, 0.92);
    border: none;
    cursor: pointer;
  }
  .gv-btn {
    width: 34px;
  }
  .gv-goto {
    padding: 0 14px;
    font-size: var(--fs-compact);
    font-weight: 600;
  }
  .gv-btn:hover,
  .gv-goto:hover {
    background: rgba(255, 255, 255, 0.18);
  }
  .gv-btn:disabled {
    opacity: 0.4;
    cursor: default;
  }
  @media (max-width: 768px) {
    .gv-btn {
      width: var(--tap-min);
      height: var(--tap-min);
    }
    .gv-goto {
      height: var(--tap-min);
    }
  }
</style>
