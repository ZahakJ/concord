<script>
  // One encrypted image attachment: reserves layout space from the token's
  // dimensions, fetches + decrypts through the backend, and renders with
  // spinner / error+retry states. Clicking opens a Discord-style lightbox:
  // click zooms to the cursor, wheel zooms, drag pans, pinch works on touch,
  // Esc / backdrop / ✕ close.
  import Icon from "./Icon.svelte";
  import { pushLayer } from "./lib/navstack.svelte.js";
  import { loadAttachment, copyImageToClipboard, saveImageSrc } from "./lib/attachments.js";
  import { knownRecipe } from "./lib/memerecipe.js";
  import { openContextMenu, flash, S } from "./lib/state.svelte.js";
  import { longpress, haptic } from "./lib/touch.js";

  // `messageId`/`own` exist only for "Edit meme": editing a picture in place
  // means editing the message that carries it, and only its author may.
  // `tile` says this image is one cell of a multi-image grid rather than a
  // standalone attachment: it fills the cell its parent sized instead of
  // sizing itself, and the cell is what keeps four photos looking like four
  // photos rather than a ragged stack of four different shapes.
  let { channelId, tok, messageId = "", own = false, tile = false } = $props();

  // Right-click on the image (thumbnail or lightbox): copy to the system
  // clipboard as a real image (paste it anywhere — in Concord or outside),
  // or save it to disk. Like Discord.
  // Can this picture be reopened in the editor it came out of? Three things
  // have to hold, and the check is done at click time rather than in a
  // $derived because the recipe index is plain module state, not reactive:
  // it must be YOUR message (an edit is a message edit), it must be an image
  // we can address, and the recipe — which never leaves the device that made
  // the meme — must still be here. Fail any of them and only the ordinary
  // "Make a Meme" is offered, which starts a NEW meme from the flattened
  // picture and says so.
  const editable = () => own && !!messageId && knownRecipe(tok.blobId);

  function imageMenu(e) {
    openContextMenu(e, [
      {
        label: "Copy Image",
        icon: "copy",
        onClick: async () => {
          try {
            await copyImageToClipboard(src);
            flash("Image copied", "success");
          } catch (err) {
            flash(`Couldn't copy image: ${err?.message || err}`);
          }
        },
      },
      {
        label: "Save Image",
        icon: "download",
        // Honour a sender-supplied file name when there is one: it is the only
        // thing the v2 token's name field is good for, and without this the
        // rename control in the composer has no observable effect anywhere.
        onClick: async () => {
          const how = await saveImageSrc(
            src,
            tok.name || `concord-${(tok.blobId || "image").slice(0, 8)}.png`,
          );
          if (how === "gallery") flash("Saved to your gallery", "success");
          else if (!how) flash("Couldn't save that image");
        },
      },
      { sep: true },
      editable() && {
        label: "Edit meme",
        icon: "edit",
        onClick: () =>
          (S.modal = { kind: "meme", edit: { channelId, messageId, blobId: tok.blobId } }),
      },
      // `src` is the already-decrypted data URL, so the editor gets the picture
      // itself and never has to know it came from an encrypted attachment.
      // Still offered next to "Edit meme": one fixes this meme, the other
      // starts a fresh one on top of the flattened result.
      { label: "Make a Meme", icon: "spark", onClick: () => (S.modal = { kind: "meme", src }) },
    ]);
  }

  // Touch counterpart of the right-click above. Svelte delegates touchstart at
  // the root, so an attribute handler would run AFTER the message row's own
  // longpress action has armed and both sheets would open; a native listener
  // here stops the bubble first. A covered spoiler lets it through on purpose —
  // there is no picture to act on yet, so the row's menu is the useful one.
  function imageTouch(node) {
    const stop = (ev) => {
      if (!hidden) ev.stopPropagation();
    };
    node.addEventListener("touchstart", stop, { passive: true });
    return { destroy: () => node.removeEventListener("touchstart", stop) };
  }
  const imageLongPress = (e) => {
    if (!hidden) imageMenu(e);
  };

  let state = $state("loading"); // loading | done | error
  // A spoiler stays covered until clicked. Tracked here rather than derived
  // from the token so revealing one doesn't un-hide every other copy of the
  // same image, and so it re-covers if the row is rebuilt.
  let revealed = $state(false);
  const hidden = $derived(!!tok.spoiler && !revealed);
  const reveal = () => (revealed = true);
  let src = $state("");
  let errMsg = $state("");

  // Reserve space: scale token dims into the same box the CSS allows. The width
  // is only the DESKTOP ceiling — the placeholder carries the ratio as an
  // aspect-ratio so `max-width:100%` can shrink it inside a phone's ~300px
  // column without the reserved box ending up the wrong shape (and without the
  // frame visibly jumping when the real image lands).
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

  // Refetch only when this is genuinely a different blob.
  //
  // `tok` is parsed fresh out of the message content, so every re-render hands
  // us a new object with the same contents — and a new message arriving in the
  // channel re-renders the whole feed. Keying the effect on the object rather
  // than the id meant an unrelated message made every visible image drop back
  // to `state = "loading"`, which tears down the <img> and, with it, the
  // lightbox mounted beside it: open a picture, someone says hello, the picture
  // blinks or closes. The bytes were cached the whole time; only the state flag
  // moved.
  let loadedBlob = "";
  $effect(() => {
    const id = tok.blobId;
    if (id === loadedBlob) return;
    loadedBlob = id;
    fetchNow();
  });

  // ---- lightbox ----
  // The image renders fitted to the viewport; zoom/pan are a transform on top:
  // translate(tx,ty) then scale(s), both around the viewport center.
  const ZMIN = 1;
  const ZMAX = 8;
  let lightbox = $state(false);
  let zoom = $state(1);
  let tx = $state(0);
  let ty = $state(0);
  let dragging = $state(false);
  let overlayEl; // for backdrop-click detection and pointer math
  let imgEl; // for pan clamping (fitted size × zoom = rendered size)

  function openLightbox() {
    zoom = 1;
    tx = 0;
    ty = 0;
    lightbox = true;
  }
  function closeLightbox() {
    lightbox = false;
    pointers.clear();
    dragging = false;
    flinging = false;
  }

  // Android's hardware back is the reflex for "get this picture off my screen".
  // Without registering here the back handler fell straight through to closing
  // drawers behind the black overlay — or exiting the app with the photo still
  // up. Escape (below) is the desktop half of the same job.
  $effect(() => {
    if (!lightbox) return;
    return pushLayer("lightbox", closeLightbox);
  });

  function onKeydown(e) {
    if (!lightbox) return;
    if (e.key === "Escape") closeLightbox();
    else if (e.key === "+" || e.key === "=") zoomAround(center(), zoom * 1.5);
    else if (e.key === "-") zoomAround(center(), zoom / 1.5);
    else if (e.key === "0") zoomAround(center(), 1);
  }

  function center() {
    const r = overlayEl.getBoundingClientRect();
    return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
  }

  // zoomAround rescales while keeping the point under `p` (client coords)
  // visually fixed — Discord/maps-style anchored zoom. At fit scale (1) the
  // pan resets so the image is always centered when fully zoomed out.
  function zoomAround(p, next) {
    next = Math.min(ZMAX, Math.max(ZMIN, next));
    const c = center();
    const k = next / zoom;
    tx = p.x - c.x - (p.x - c.x - tx) * k;
    ty = p.y - c.y - (p.y - c.y - ty) * k;
    zoom = next;
    if (zoom === 1) {
      tx = 0;
      ty = 0;
    }
    clampPan();
  }

  // clampPan keeps the image on screen: edges never pan past the viewport
  // center-ish, so a wild drag can't strand you on an all-black overlay.
  function clampPan() {
    if (!imgEl || !overlayEl) return;
    const r = overlayEl.getBoundingClientRect();
    const maxX = Math.max(0, (imgEl.offsetWidth * zoom - r.width) / 2) + r.width * 0.25;
    const maxY = Math.max(0, (imgEl.offsetHeight * zoom - r.height) / 2) + r.height * 0.25;
    tx = Math.max(-maxX, Math.min(maxX, tx));
    ty = Math.max(-maxY, Math.min(maxY, ty));
  }

  function onWheel(e) {
    e.preventDefault(); // never scroll the chat behind the overlay
    const factor = Math.exp(-e.deltaY * 0.0015);
    zoomAround({ x: e.clientX, y: e.clientY }, zoom * factor);
  }

  // Pointers: one finger/button drags (pan) — or clicks (zoom toggle) when it
  // never really moved; two fingers pinch.
  const pointers = new Map(); // pointerId -> {x, y}
  let moved = 0; // px travelled since pointerdown (click vs drag)
  let pinchDist = 0;
  let downOnBackdrop = false; // pointer capture retargets pointerup, so record at DOWN
  // A long-press inside the viewer opens the image sheet; the lift that follows
  // is a motionless pointerup, which the click path below would otherwise read
  // as "zoom to 2.5×" — sheet and zoom at once. Cleared at each pointerdown.
  let pressedMenu = false;
  const lightboxLongPress = (e) => {
    pressedMenu = true;
    imageMenu(e);
  };
  // At fit scale there is nothing to pan, so a one-finger drag used to be a
  // dead gesture that also swallowed its own lift. It is now the dismissal:
  // the photo follows the finger, the backdrop thins, and a throw past
  // FLING_PX (or a fast flick) lets go of it — what every native viewer does.
  let flinging = $state(false);
  const FLING_PX = 110;
  // Backdrop opacity tracks the throw so the photo feels like it is leaving.
  const backdrop = $derived(
    flinging ? Math.max(0.15, 0.85 - Math.abs(ty) / 420) : 0.85,
  );

  function onPointerDown(e) {
    if (e.button !== 0 && e.pointerType === "mouse") return;
    if (e.target.closest?.(".lb-bar")) return; // let the ✕ button be a button
    pressedMenu = false;
    downOnBackdrop = e.target === overlayEl;
    overlayEl.setPointerCapture?.(e.pointerId);
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (pointers.size === 1) moved = 0;
    if (pointers.size === 2) pinchDist = pinchDistance();
  }

  function pinchDistance() {
    const [a, b] = [...pointers.values()];
    return Math.hypot(a.x - b.x, a.y - b.y);
  }

  function onPointerMove(e) {
    const prev = pointers.get(e.pointerId);
    if (!prev) return;
    const cur = { x: e.clientX, y: e.clientY };
    pointers.set(e.pointerId, cur);
    if (pointers.size === 2) {
      // Pinch: zoom around the midpoint, pan with the midpoint drift.
      const [a, b] = [...pointers.values()];
      const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
      const d = pinchDistance();
      if (pinchDist > 0) zoomAround(mid, zoom * (d / pinchDist));
      pinchDist = d;
      moved = 99;
      return;
    }
    const dx = cur.x - prev.x;
    const dy = cur.y - prev.y;
    moved += Math.abs(dx) + Math.abs(dy);
    if (moved > 4 && zoom > 1) {
      dragging = true;
      tx += dx;
      ty += dy;
      clampPan();
    } else if (moved > 6 && e.pointerType !== "mouse") {
      // Fit scale: throw-to-dismiss. Touch only — a mouse has the ✕, Escape and
      // a backdrop click, and dragging a photo out of a desktop viewer isn't a
      // gesture anyone means. Sideways gets a fraction of the travel so the
      // image still feels grabbed without pretending it can be panned.
      flinging = true;
      dragging = true;
      ty += dy;
      tx += dx * 0.3;
    }
  }

  function onPointerUp(e) {
    const wasPinching = pointers.size === 2;
    pointers.delete(e.pointerId);
    if (pointers.size > 0 || wasPinching) return;
    if (flinging) {
      const thrown = Math.abs(ty) > FLING_PX;
      flinging = false;
      dragging = false;
      if (thrown) {
        haptic("light");
        closeLightbox();
      } else {
        tx = 0; // short of the threshold: spring back (the img keeps its transition)
        ty = 0;
      }
      return;
    }
    const wasDrag = dragging || moved > 4;
    dragging = false;
    if (wasDrag || pressedMenu) return;
    // A clean click: on the backdrop it closes; on the image it toggles zoom
    // at the click point (in), or back to fit (out).
    if (downOnBackdrop) {
      closeLightbox();
    } else if (zoom === 1) {
      zoomAround({ x: e.clientX, y: e.clientY }, 2.5);
    } else {
      zoomAround(center(), 1);
    }
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if state === "done"}
  <button
    class="frame done"
    class:tile
    class:hidden={hidden}
    onclick={hidden ? reveal : openLightbox}
    oncontextmenu={hidden ? undefined : imageMenu}
    title={hidden ? "Spoiler — click to reveal" : "Click to enlarge"}
    use:imageTouch
    use:longpress={{ handler: imageLongPress }}
  >
    <!-- min(): 380px is the desktop ceiling, but a phone's message column is
         ~300px and the feed clips its overflow — an unclamped landscape photo
         simply lost its right-hand third with no way to reach it. -->
    <img
      {src}
      alt={tok.desc || "attachment"}
      class:blur={hidden}
      style={tile
        ? "width:100%;height:100%;object-fit:contain"
        : `max-width:min(${MAXW}px, 100%);max-height:${MAXH}px`}
    />
    {#if hidden}<span class="spoiler-tag">SPOILER</span>{/if}
  </button>
  {#if tok.desc && !hidden}
    <!-- The sender's description, shown as a caption as well as being the alt
         text: it's useful to everyone, not only to a screen reader. -->
    <span class="att-desc">{tok.desc}</span>
  {/if}
  {#if lightbox}
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
    <div
      class="lightbox"
      class:flinging
      style="--lb-dim:{backdrop}"
      role="dialog"
      aria-modal="true"
      aria-label="Image viewer"
      bind:this={overlayEl}
      onwheel={onWheel}
      onpointerdown={onPointerDown}
      onpointermove={onPointerMove}
      onpointerup={onPointerUp}
      onpointercancel={onPointerUp}
    >
      <img
        {src}
        alt="attachment full size"
        bind:this={imgEl}
        class:zoomed={zoom > 1}
        class:dragging
        style="transform: translate({tx}px, {ty}px) scale({zoom})"
        draggable="false"
        oncontextmenu={imageMenu}
        use:longpress={{ handler: lightboxLongPress }}
      />
      <div class="lb-bar">
        <span class="lb-zoom" class:show={zoom > 1}>{Math.round(zoom * 100)}%</span>
        <button class="lb-close" onclick={closeLightbox} aria-label="Close image (Esc)">
          <Icon name="close" size={18} />
        </button>
      </div>
    </div>
  {/if}
{:else}
  <div
    class="frame placeholder"
    class:tile
    style={tile ? "" : `width:${dims.w}px;aspect-ratio:${dims.w} / ${dims.h}`}
  >
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
    position: relative; /* anchors the SPOILER label over the blurred image */
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    margin-top: 4px;
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .frame.done img.blur {
    filter: blur(22px);
  }
  .frame.hidden {
    cursor: pointer;
  }
  .spoiler-tag {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    font-size: var(--fs-small);
    font-weight: 800;
    letter-spacing: 1.2px;
    color: #fff;
    text-shadow: 0 1px 4px rgba(0, 0, 0, 0.8);
    pointer-events: none;
  }
  .att-desc {
    display: block;
    margin-top: 3px;
    font-size: var(--fs-small);
    line-height: 1.4;
    color: var(--text-muted);
    max-width: min(420px, 100%);
  }
  .frame.done {
    background: transparent;
    padding: 0;
    display: block;
    width: fit-content;
    /* The shrink-to-fit box must itself be capped, or the image's own
       max-width:100% has a containing block wider than the column. */
    max-width: 100%;
    cursor: zoom-in;
  }
  .frame.done:hover {
    background: transparent;
  }
  /* One cell of a grid: the cell owns the box, the image fills it. `contain`
     rather than `cover` on purpose — a cropped thumbnail of a screenshot is
     a picture of the middle of a screenshot, and the whole reason to send four
     at once is usually that each one shows something different. */
  .frame.tile {
    width: 100%;
    height: 100%;
    margin-top: 0;
    background: var(--bg-0);
    border: 1px solid var(--border);
  }
  .frame.done.tile img {
    border-radius: 0;
  }
  .frame.done img {
    display: block;
    border-radius: var(--radius-sm);
    transition: filter 0.15s ease;
  }
  /* Dim a touch on hover so the thumbnail reads as "click to enlarge". Gated on
     a real pointer: Chromium latches :hover onto the last-tapped element, so on
     a phone this left every photo you had opened permanently darkened. */
  @media (pointer: fine) {
    .frame.done:hover img {
      filter: brightness(0.9);
    }
  }
  .placeholder {
    background: var(--bg-1);
    border: 1px solid var(--border);
    color: var(--text-faint);
    max-width: 100%;
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
    font-size: var(--fs-compact);
  }
  .retry {
    padding: 3px 12px;
    font-size: var(--fs-compact);
  }
  .lightbox {
    position: fixed;
    inset: 0;
    z-index: 300;
    background: rgba(0, 0, 0, var(--lb-dim, 0.85));
    display: grid;
    place-items: center;
    padding: 4vh 4vw;
    overflow: hidden;
    touch-action: none; /* pointer events own pinch/drag */
    animation: lb-in 0.15s ease;
    transition: background 0.18s ease;
  }
  .lightbox.flinging {
    transition: none; /* the backdrop tracks the finger 1:1 during a throw */
  }
  @keyframes lb-in {
    from {
      opacity: 0;
    }
  }
  .lightbox img {
    max-width: 100%;
    max-height: 100%;
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-pop);
    cursor: zoom-in;
    user-select: none;
    -webkit-user-drag: none;
    transition: transform 0.18s ease;
    will-change: transform;
  }
  .lightbox img.zoomed {
    cursor: grab;
  }
  .lightbox img.dragging {
    cursor: grabbing;
    transition: none; /* 1:1 with the pointer while panning */
  }
  /* The overlay is edge-to-edge, and the app draws under the status bar (see
     MobileShell's own inset padding) — without the safe-area offset the ✕ sits
     beneath the clock and the camera cutout. */
  .lb-bar {
    position: absolute;
    top: calc(14px + var(--safe-top));
    right: calc(16px + var(--safe-right));
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .lb-zoom {
    font-size: var(--fs-compact);
    font-variant-numeric: tabular-nums;
    color: rgba(255, 255, 255, 0.85);
    background: rgba(0, 0, 0, 0.45);
    border-radius: 999px;
    padding: 3px 10px;
    opacity: 0;
    transition: opacity 0.15s ease;
    pointer-events: none;
  }
  .lb-zoom.show {
    opacity: 1;
  }
  .lb-close {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    border-radius: 50%;
    background: rgba(0, 0, 0, 0.45);
    color: rgba(255, 255, 255, 0.9);
    border: none;
    cursor: pointer;
  }
  .lb-close:hover {
    background: rgba(255, 255, 255, 0.18);
  }
  @media (pointer: coarse), (max-width: 768px) {
    /* Far top-right is the worst place on a phone for a thumb, so the button
       gets the full tap minimum — and throw-to-dismiss plus hardware back are
       the gestures actually meant to be used. */
    .lb-close {
      width: var(--tap-min);
      height: var(--tap-min);
    }
  }
</style>
