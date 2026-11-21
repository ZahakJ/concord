<script>
  // The one full-screen media viewer. The feed (Attachment.svelte) and the
  // Media tab (GalleryViewer.svelte) both render it; each hands over the
  // CURRENT item and how to reach its neighbours, and keeps its own sources.
  //
  // It is a stage (DESIGN-063.md): it opens out of the thumbnail it was
  // started from (a FLIP: the picture is laid out at its fitted size and a
  // transform carries it from the thumbnail's rect to rest), and closes back
  // into it, or fades when that thumbnail is gone. Only two things move: the
  // picture (transform, and opacity when it cannot fly) and the backdrop
  // (opacity). Chrome fades with opacity on its own, at rest.
  //
  // Zoom and pan are lib/viewerfit.js, unchanged: the picture is drawn at its
  // fitted size and translate(tx,ty) scale(zoom) sits on top of it, around
  // the window's centre, so zoom 1 is "fitted". Mouse: a click steps in, a
  // double-click toggles fit and 100%, the wheel zooms toward the cursor, a
  // drag pans. Touch: pinch, a double tap zooms in or back to fit, a single
  // tap shows or hides the chrome, a sideways drag goes to the neighbour, a
  // drag down lets go of the picture with the backdrop thinning as it goes.
  // Keys: Esc, ← → Home End, + − 0, and i for the details.
  import { untrack } from "svelte";
  import Icon from "./Icon.svelte";
  import { pushLayer } from "./lib/navstack.svelte.js";
  import { portal } from "./lib/portal.js";
  import { focusOnMount } from "./lib/focus.js";
  import { longpress, haptic } from "./lib/touch.js";
  import { pointOf, rectOf } from "./lib/place.js";
  import { fmtBytes } from "./lib/attachments.js";
  import { tooltip } from "./lib/tooltip.js";
  import {
    clampPan as clampView,
    clampZoom,
    fitSize,
    MARGIN_DESKTOP,
    MARGIN_PHONE,
    refit,
    toggleZoom,
    wheelSteps,
    zoomAt,
    ZOOM_STEP_CLICK,
    ZOOM_STEP_WHEEL,
  } from "./lib/viewerfit.js";

  // item: { key, kind: "image"|"video", src, err, note, w, h, name, desc,
  //         size, sender, when, spoiler, unsigned }
  //   `src` empty and no `err` means still loading; `note` replaces the media
  //   with a sentence (a video this engine must not play); `spoiler` is true
  //   while the picture is still covered.
  let {
    item,
    index = 0,
    count = 1,
    origin = () => null,
    onStep = null,
    onClose,
    onGoto = null,
    onSave = null,
    onCopy = null,
    onMenu = null,
    onReveal = null,
    unsignedLabel = "",
    unsignedTip = "",
  } = $props();

  const video = $derived(item.kind === "video");

  // ---- geometry ----
  let overlayEl = $state(null);
  let backdropEl = $state(null);
  let mediaEl = $state(null);
  let ambientEl = $state(null);
  let vp = $state({ w: 0, h: 0 });
  let natural = $state(null);
  let zoom = $state(1);
  let tx = $state(0);
  let ty = $state(0);
  let dragging = $state(false);

  function measure() {
    if (!overlayEl) return;
    const r = rectOf(overlayEl);
    vp = { w: r.w, h: r.h };
  }
  function onResize() {
    measure();
    if (!fit) return;
    const v = refit({ zoom, tx, ty }, fit, vp, fit.scale);
    zoom = v.zoom;
    tx = v.tx;
    ty = v.ty;
  }
  $effect(() => {
    if (!overlayEl) return;
    measure();
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  });

  // A new item starts at fit with the token's size until its own arrives.
  let seenKey = "";
  $effect(() => {
    const k = item.key;
    if (k === seenKey) return;
    seenKey = k;
    untrack(() => {
      natural = item.w && item.h ? { w: item.w, h: item.h } : null;
      zoom = 1;
      tx = 0;
      ty = 0;
    });
  });
  const fit = $derived.by(() => {
    if (!natural || !vp.w) return null;
    return fitSize(natural, vp, vp.w < 768 ? MARGIN_PHONE : MARGIN_DESKTOP);
  });
  function onImgLoad(e) {
    const el = e.currentTarget;
    if (el.naturalWidth) natural = { w: el.naturalWidth, h: el.naturalHeight };
    paintAmbient(el);
  }
  function onMeta(e) {
    const v = e.currentTarget;
    if (v.videoWidth) natural = { w: v.videoWidth, h: v.videoHeight };
  }

  const transformNow = () => `translate(${tx}px, ${ty}px) scale(${zoom})`;

  // ---- motion ----
  const reduced = () => document.documentElement.dataset.motion === "reduced";
  const cssVar = (name) => getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  function msOf(name, fallback) {
    const v = cssVar(name);
    const n = parseFloat(v);
    if (!Number.isFinite(n)) return fallback;
    return v.endsWith("ms") ? n : v.endsWith("s") ? n * 1000 : n;
  }
  const ease = (name) => cssVar(name) || "ease-out";

  // Where the thumbnail's PICTURE is, in layout pixels — for a contained
  // image the drawn picture, not its box — or null when it is gone or off
  // screen, in which case the viewer fades instead of flying.
  function originRect() {
    const el = origin?.();
    if (!el?.isConnected || !natural) return null;
    const img = el.tagName === "IMG" ? el : el.querySelector("img");
    const box = rectOf(img || el);
    if (box.w < 4 || box.h < 4) return null;
    if (box.y + box.h < 0 || box.y > vp.h || box.x + box.w < 0 || box.x > vp.w) return null;
    if (img && getComputedStyle(img).objectFit === "contain") {
      const s = Math.min(box.w / natural.w, box.h / natural.h);
      const w = natural.w * s;
      const h = natural.h * s;
      return { el: img, x: box.x + (box.w - w) / 2, y: box.y + (box.h - h) / 2, w, h };
    }
    return { el: img || el, ...box };
  }
  function flipFrom(r) {
    const ov = rectOf(overlayEl);
    const s = Math.min(r.w / fit.w, r.h / fit.h);
    return `translate(${r.x + r.w / 2 - (ov.x + ov.w / 2)}px, ${r.y + r.h / 2 - (ov.y + ov.h / 2)}px) scale(${s})`;
  }
  // The thumbnail is hidden while the picture is in flight from or to it, so
  // the picture reads as lifting out of the feed rather than as a copy.
  let hiddenOrigin = null;
  function hideOrigin(el) {
    restoreOrigin();
    hiddenOrigin = el;
    el.style.visibility = "hidden";
  }
  function restoreOrigin() {
    if (hiddenOrigin) hiddenOrigin.style.visibility = "";
    hiddenOrigin = null;
  }
  $effect(() => () => restoreOrigin());

  let opened = false; // the first item has made its entrance
  let animatedKey = "";
  let enterDir = 0; // which side the next item comes in from
  let phase = $state("open"); // open | closing

  // The entrance of whichever item just became drawable: the first flies out
  // of its thumbnail, a neighbour slides in from the side it was asked from.
  $effect(() => {
    const el = mediaEl;
    const k = item.key;
    if (!el || !fit || animatedKey === k) return;
    animatedKey = k;
    untrack(() => enter(el));
  });
  function enter(el) {
    const first = !opened;
    opened = true;
    if (reduced()) return;
    const to = transformNow();
    if (first) {
      const r = originRect();
      const dur = msOf("--dur-stage", 420);
      if (r) {
        hideOrigin(r.el);
        const a = el.animate([{ transform: flipFrom(r) }, { transform: to }], { duration: dur, easing: ease("--ease-spring") });
        a.onfinish = a.oncancel = () => phase === "open" && restoreOrigin();
      } else {
        el.animate(
          [{ transform: `${to} scale(0.96)`, opacity: 0 }, { transform: to, opacity: 1 }],
          { duration: dur, easing: ease("--ease-spring") },
        );
      }
      return;
    }
    const off = Math.min(vp.w * 0.18, 160) * (enterDir || 1);
    el.animate(
      [{ transform: `translate(${off}px, 0px) scale(1)`, opacity: 0 }, { transform: to, opacity: 1 }],
      { duration: msOf("--dur-stage", 420) * 0.75, easing: ease("--ease-spring") },
    );
  }

  function requestClose(how = "") {
    if (phase === "closing") return;
    phase = "closing";
    clearTimeout(idleTimer);
    clearTimeout(tapTimer);
    clearTimeout(clickTimer);
    pointers.clear();
    const el = mediaEl;
    if (how === "instant" || reduced() || !el || !fit) return finish();
    const r = originRect();
    const dur = msOf("--dur-stage", 420) * 0.6;
    const from = transformNow();
    backdropEl?.animate([{ opacity: dim }, { opacity: 0 }], { duration: dur, easing: ease("--ease-in"), fill: "forwards" });
    let a;
    if (r) {
      hideOrigin(r.el);
      a = el.animate([{ transform: from }, { transform: flipFrom(r) }], { duration: dur, easing: ease("--ease-spring"), fill: "forwards" });
    } else {
      a = el.animate([{ transform: from, opacity: 1 }, { transform: `${from} scale(0.94)`, opacity: 0 }], { duration: dur, easing: ease("--ease-in"), fill: "forwards" });
    }
    let done = false;
    const end = () => {
      if (done) return;
      done = true;
      finish();
    };
    a.onfinish = end;
    setTimeout(end, dur + 120); // a cancelled animation must not strand the viewer
  }
  function finish() {
    restoreOrigin();
    onClose();
  }

  $effect(() => pushLayer("lightbox", () => requestClose()));

  // ---- neighbours ----
  const canPrev = $derived(!!onStep && index > 0);
  const canNext = $derived(!!onStep && index < count - 1);
  function go(d) {
    if (!onStep || phase !== "open") return;
    const target = Math.max(0, Math.min(count - 1, index + d));
    if (target === index) return;
    enterDir = Math.sign(target - index);
    onStep(target - index);
  }
  // A committed swipe carries the current picture off the side it was thrown
  // toward, then the neighbour comes in from the other.
  function swipeTo(d) {
    const el = mediaEl;
    if (!el || reduced()) return go(d);
    const a = el.animate(
      [{ transform: transformNow(), opacity: 1 }, { transform: `translate(${-d * vp.w * 0.6}px, 0px) scale(1)`, opacity: 0 }],
      { duration: msOf("--dur-quick", 120), easing: ease("--ease-in"), fill: "forwards" },
    );
    a.onfinish = () => {
      tx = 0;
      ty = 0;
      go(d);
    };
  }

  // ---- chrome ----
  // Shown at rest. A mouse that stops moving hides it after a while; a touch
  // toggles it with a tap. It never hides while a key was the last input,
  // while focus is in it, while the details are open or the pointer is on it:
  // a keyboard or screen-reader user is never left with nothing to land on.
  let chromeOn = $state(true);
  let lastInput = "pointer";
  let idleTimer = 0;
  const IDLE_MS = 2600;
  function poke(kind) {
    chromeOn = true;
    clearTimeout(idleTimer);
    lastInput = kind;
    if (kind === "mouse") idleTimer = setTimeout(maybeHide, IDLE_MS);
  }
  function maybeHide() {
    if (phase !== "open" || infoOpen || lastInput !== "mouse") return;
    if (overlayEl?.querySelector(".mv-chrome:hover")) return poke("mouse");
    const a = document.activeElement;
    if (a && overlayEl?.contains(a) && a.matches?.(":focus-visible") && a.closest(".mv-chrome")) return;
    chromeOn = false;
  }
  $effect(() => () => {
    clearTimeout(idleTimer);
    clearTimeout(tapTimer);
    clearTimeout(clickTimer);
  });

  // The details layer: who, when, the caption, and the file itself. Its
  // state outlives the viewer, so someone who reads them always has them.
  const INFO_KEY = "concord.viewerInfo";
  let infoOpen = $state(readInfo());
  function readInfo() {
    try {
      return localStorage.getItem(INFO_KEY) === "1";
    } catch {
      return false;
    }
  }
  function toggleInfo() {
    infoOpen = !infoOpen;
    chromeOn = true;
    try {
      localStorage.setItem(INFO_KEY, infoOpen ? "1" : "0");
    } catch {
      /* a convenience only */
    }
  }
  const dims = $derived(natural ? `${natural.w} × ${natural.h}` : "");

  // ---- keys ----
  function onKeydown(e) {
    if (phase !== "open") return;
    if (e.key !== "Tab" || !chromeOn) poke("key");
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    if (e.key === "Tab") trapTab(e);
    else if (e.key === "Escape") {
      e.stopPropagation();
      requestClose();
    } else if (e.key === "ArrowRight" && zoom === 1) go(1);
    else if (e.key === "ArrowLeft" && zoom === 1) go(-1);
    else if (e.key === "Home" && zoom === 1) go(-index);
    else if (e.key === "End" && zoom === 1) go(count - 1 - index);
    else if (e.key.startsWith("Arrow") && zoom > 1) {
      // Zoomed, the arrows look around the picture instead.
      e.preventDefault();
      const d = 80;
      tx += e.key === "ArrowLeft" ? d : e.key === "ArrowRight" ? -d : 0;
      ty += e.key === "ArrowUp" ? d : e.key === "ArrowDown" ? -d : 0;
      clampPan();
    } else if (e.key === "+" || e.key === "=") zoomAround(center(), zoom * 1.5);
    else if (e.key === "-") zoomAround(center(), zoom / 1.5);
    else if (e.key === "0") zoomAround(center(), 1);
    else if ((e.key === "i" || e.key === "I") && !e.target?.closest?.("input, textarea")) toggleInfo();
  }
  function trapTab(e) {
    const items = [...(overlayEl?.querySelectorAll("button:not(:disabled)") || [])];
    if (!items.length) return;
    const at = items.indexOf(document.activeElement);
    e.preventDefault();
    const next = e.shiftKey ? (at <= 0 ? items.length - 1 : at - 1) : at < 0 || at === items.length - 1 ? 0 : at + 1;
    items[next].focus();
  }

  // ---- zoom (lib/viewerfit.js; everything in layout pixels) ----
  function center() {
    const r = rectOf(overlayEl);
    return { x: r.x + r.w / 2, y: r.y + r.h / 2 };
  }
  function zoomAround(p, next) {
    if (!fit || video || item.spoiler) return;
    const c = center();
    const v = zoomAt({ zoom, tx, ty }, { x: p.x - c.x, y: p.y - c.y }, next, fit, vp, fit.scale);
    zoom = v.zoom;
    tx = v.tx;
    ty = v.ty;
  }
  function clampPan() {
    if (!fit) return;
    const v = clampView({ zoom, tx, ty }, fit, vp);
    tx = v.tx;
    ty = v.ty;
  }
  let wheelAcc = null;
  function onWheel(e) {
    e.preventDefault(); // never scroll the chat behind the overlay
    if (!e.deltaY || phase !== "open") return;
    const r = wheelSteps(wheelAcc, e.deltaY, e.deltaMode, e.timeStamp);
    wheelAcc = r.acc;
    if (r.steps) zoomAround(pointOf(e), zoom * ZOOM_STEP_WHEEL ** -r.steps);
  }
  let clickTimer = 0;
  const CLICK_WAIT_MS = 220;
  function clickStep(p) {
    clearTimeout(clickTimer);
    clickTimer = setTimeout(() => {
      if (!fit) return;
      const next = clampZoom(zoom * ZOOM_STEP_CLICK, fit.scale);
      zoomAround(p, next === zoom ? 1 : next);
    }, CLICK_WAIT_MS);
  }
  let lastPointer = "mouse";
  let downOnMedia = false;
  function onDblClick(e) {
    if (!fit || !downOnMedia || lastPointer !== "mouse") return;
    clearTimeout(clickTimer);
    zoomAround(pointOf(e), toggleZoom(zoom, fit.scale));
  }

  // ---- pointers ----
  const pointers = new Map();
  let moved = 0;
  let pinchDist = 0;
  let downOnBackdrop = false;
  let pressedMenu = false;
  let gesture = $state(""); // "" | pan | dismiss | swipe
  let start = { x: 0, y: 0 };
  let track = []; // recent {x, y, t} for the release velocity
  const FLING_PX = 110;
  // The backdrop thins as the picture is dragged away, to nothing at about
  // two thirds of the screen.
  const dim = $derived(gesture === "dismiss" && vp.h ? Math.max(0.08, 1 - Math.abs(ty) / (vp.h * 0.66)) : 1);

  function onPointerDown(e) {
    downOnMedia = false;
    if (phase !== "open") return;
    if (e.pointerType === "mouse") poke("mouse");
    if (e.button !== 0 && e.pointerType === "mouse") return;
    if (e.target.closest?.(".mv-chrome, .mv-spoil")) return; // buttons stay buttons
    if (video && e.target === mediaEl) return; // the player's own controls
    pressedMenu = false;
    lastPointer = e.pointerType;
    downOnBackdrop = e.target === overlayEl || e.target === backdropEl || e.target === ambientEl;
    downOnMedia = e.target === mediaEl;
    overlayEl.setPointerCapture?.(e.pointerId);
    const p = pointOf(e);
    pointers.set(e.pointerId, p);
    if (pointers.size === 1) {
      moved = 0;
      gesture = "";
      start = p;
      track = [{ x: p.x, y: p.y, t: e.timeStamp }];
    }
    if (pointers.size === 2) pinchDist = pinchDistance();
  }
  function pinchDistance() {
    const [a, b] = [...pointers.values()];
    return Math.hypot(a.x - b.x, a.y - b.y);
  }
  function onPointerMove(e) {
    if (e.pointerType === "mouse" && phase === "open") poke("mouse");
    const prev = pointers.get(e.pointerId);
    if (!prev) return;
    const cur = pointOf(e);
    pointers.set(e.pointerId, cur);
    if (pointers.size === 2) {
      const [a, b] = [...pointers.values()];
      const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
      const d = pinchDistance();
      if (pinchDist > 0) zoomAround(mid, zoom * (d / pinchDist));
      pinchDist = d;
      moved = 99;
      gesture = "pan";
      return;
    }
    const dx = cur.x - prev.x;
    const dy = cur.y - prev.y;
    moved += Math.abs(dx) + Math.abs(dy);
    track.push({ x: cur.x, y: cur.y, t: e.timeStamp });
    if (track.length > 6) track.shift();
    if (moved > 4 && zoom > 1) {
      gesture = "pan";
      dragging = true;
      tx += dx;
      ty += dy;
      clampPan();
    } else if (moved > 6 && e.pointerType !== "mouse" && zoom === 1) {
      // Touch at fit: which way the finger first went decides the gesture.
      if (!gesture) {
        const ax = Math.abs(cur.x - start.x);
        const ay = Math.abs(cur.y - start.y);
        gesture = ax > ay && count > 1 ? "swipe" : "dismiss";
      }
      dragging = true;
      if (gesture === "swipe") {
        // Past the first or the last item it gives, but only a little.
        const edge = (dx > 0 && !canPrev) || (dx < 0 && !canNext);
        tx += edge ? dx * 0.3 : dx;
      } else if (gesture === "dismiss") {
        ty += dy;
        tx += dx * 0.3;
      }
    }
  }
  function velocity() {
    if (track.length < 2) return { x: 0, y: 0 };
    const a = track[0];
    const b = track[track.length - 1];
    const dt = Math.max(1, b.t - a.t);
    return { x: (b.x - a.x) / dt, y: (b.y - a.y) / dt };
  }
  let tapTimer = 0;
  let lastTap = null;
  const TAP_MS = 260;
  function onPointerUp(e) {
    const wasPinching = pointers.size === 2;
    if (!pointers.delete(e.pointerId)) return;
    if (pointers.size > 0 || wasPinching) {
      dragging = false;
      return;
    }
    const g = gesture;
    gesture = g === "dismiss" ? "dismiss" : "";
    dragging = false;
    if (g === "dismiss") {
      const v = velocity();
      if (Math.abs(ty) > FLING_PX || Math.abs(v.y) > 0.7) {
        haptic("light");
        requestClose();
      } else {
        gesture = "";
        tx = 0; // short of it: back to rest (the picture keeps its transition)
        ty = 0;
      }
      return;
    }
    if (g === "swipe") {
      const v = velocity();
      const d = tx < 0 ? 1 : -1;
      const far = Math.abs(tx) > Math.min(120, vp.w * 0.22) || Math.abs(v.x) > 0.5;
      if (far && (d > 0 ? canNext : canPrev)) swipeTo(d);
      else {
        tx = 0;
        ty = 0;
      }
      return;
    }
    if (g === "pan" || moved > 4 || pressedMenu) return;
    if (downOnBackdrop) return requestClose();
    if (!downOnMedia || item.spoiler) return;
    const p = pointOf(e);
    if (e.pointerType === "mouse") {
      if (!video) clickStep(p);
      return;
    }
    // Touch on the picture: one tap is the chrome, two are the zoom.
    if (lastTap && e.timeStamp - lastTap.t < TAP_MS && Math.hypot(p.x - lastTap.x, p.y - lastTap.y) < 40) {
      clearTimeout(tapTimer);
      lastTap = null;
      if (zoom === 1) zoomAround(p, 2.5);
      else zoomAround(center(), 1);
      return;
    }
    lastTap = { x: p.x, y: p.y, t: e.timeStamp };
    clearTimeout(tapTimer);
    tapTimer = setTimeout(() => {
      lastTap = null;
      lastInput = "touch";
      chromeOn = !chromeOn;
    }, TAP_MS);
  }
  function onPointerCancel(e) {
    pointers.delete(e.pointerId);
    if (pointers.size) return;
    dragging = false;
    gesture = "";
    if (zoom === 1) {
      tx = 0;
      ty = 0;
    }
  }
  const mediaLongPress = (e) => {
    if (item.spoiler || !onMenu) return;
    pressedMenu = true;
    onMenu(e);
  };

  // ---- the ambient light ----
  // The picture, shrunk to a few pixels and drawn back up, behind everything:
  // the room takes on the picture's colour. Painted once per picture on a
  // small canvas; the blur is the scaling, so nothing filters per frame.
  function paintAmbient(img) {
    const c = ambientEl;
    if (!c) return;
    try {
      const small = document.createElement("canvas");
      small.width = 10;
      small.height = 10;
      small.getContext("2d").drawImage(img, 0, 0, 10, 10);
      c.width = 40;
      c.height = 40;
      const x = c.getContext("2d");
      x.imageSmoothingEnabled = true;
      x.imageSmoothingQuality = "high";
      x.clearRect(0, 0, 40, 40);
      x.drawImage(small, 0, 0, 40, 40);
      ambientOn = true;
    } catch {
      ambientOn = false;
    }
  }
  let ambientOn = $state(false);
  $effect(() => {
    item.key;
    untrack(() => {
      if (video || item.spoiler) ambientOn = false;
    });
  });

  const counter = $derived(count > 1 ? `${index + 1} of ${count}` : "");
  const label = $derived(video ? "Video viewer" : "Image viewer");
  const showMedia = $derived(!!item.src && !item.note);
</script>

<svelte:window onkeydown={onKeydown} />

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div
  class="lightbox mv"
  class:chrome-off={!chromeOn}
  class:closing={phase === "closing"}
  class:zoomed={zoom > 1}
  class:info-on={infoOpen}
  use:portal
  role="dialog"
  aria-modal="true"
  aria-label={label}
  bind:this={overlayEl}
  onwheel={onWheel}
  onpointerdown={onPointerDown}
  onpointermove={onPointerMove}
  onpointerup={onPointerUp}
  onpointercancel={onPointerCancel}
  ondblclick={onDblClick}
  onfocusin={(e) => e.target.closest?.(".mv-chrome") && (chromeOn = true)}
>
  <div class="mv-backdrop" class:tracking={gesture === "dismiss"} bind:this={backdropEl} style="opacity:{dim}">
    <canvas class="mv-ambient" class:on={ambientOn} bind:this={ambientEl} aria-hidden="true"></canvas>
  </div>

  {#key item.key}
    {#if item.note}
      <div class="mv-msg" role="note" oncontextmenu={onMenu}>
        <span>{item.note}</span>
      </div>
    {:else if showMedia && video}
      <!-- svelte-ignore a11y_media_has_caption -->
      <video
        class="mv-media"
        class:unsized={!fit}
        src={item.src}
        controls
        autoplay
        playsinline
        bind:this={mediaEl}
        onloadedmetadata={onMeta}
        oncontextmenu={onMenu}
        style="{fit ? `width:${fit.w}px;height:${fit.h}px;` : ''}transform:{transformNow()}"
      ></video>
    {:else if showMedia}
      <img
        class="mv-media"
        class:unsized={!fit}
        class:dragging
        class:covered={item.spoiler}
        src={item.src}
        alt={item.desc || item.name || "Photo"}
        draggable="false"
        bind:this={mediaEl}
        onload={onImgLoad}
        oncontextmenu={(e) => !item.spoiler && onMenu?.(e)}
        use:longpress={{ handler: mediaLongPress }}
        style="{fit ? `width:${fit.w}px;height:${fit.h}px;` : ''}transform:{transformNow()}"
      />
    {:else if item.err}
      <div class="mv-msg">
        <Icon name="close" size={20} />
        <span>{item.err}</span>
      </div>
    {:else}
      <span class="mv-spin" aria-label="Loading"></span>
    {/if}
  {/key}

  {#if showMedia && item.spoiler}
    <button type="button" class="mv-spoil" onclick={() => onReveal?.()}>
      <Icon name="spoiler" size={18} /> Show spoiler
    </button>
  {/if}

  <div class="mv-chrome mv-top">
    <div class="mv-top-start">
      {#if counter}<span class="mv-pill" aria-live="polite">{counter}</span>{/if}
      <span class="mv-pill mv-zoom" class:show={zoom > 1} role="status" aria-live="polite">{fit ? Math.round(zoom * fit.scale * 100) : 100}%</span>
      {#if item.unsigned}
        <span class="mv-pill mv-uns" data-testid="unsigned-mark" use:tooltip={unsignedTip}><Icon name="info" size={12} /> {unsignedLabel}</span>
      {/if}
    </div>
    <div class="mv-top-end">
      {#if onGoto}
        <button type="button" class="mv-btn mv-goto" aria-label="Go to message" onclick={() => onGoto()}>
          <Icon name="bubble" size={16} /><span>Go to message</span>
        </button>
      {/if}
      <button
        type="button"
        class="mv-btn"
        class:on={infoOpen}
        aria-pressed={infoOpen}
        aria-label="Details"
        aria-keyshortcuts="i"
        use:tooltip={"Details (i)"}
        onclick={toggleInfo}
      >
        <Icon name="info" size={18} />
      </button>
      {#if onCopy && !video}
        <button type="button" class="mv-btn" disabled={!item.src || item.spoiler} aria-label="Copy image" use:tooltip={"Copy image"} onclick={() => onCopy()}>
          <Icon name="copy" size={18} />
        </button>
      {/if}
      {#if onSave}
        <button
          type="button"
          class="mv-btn"
          disabled={!item.src}
          aria-label={video ? "Save video" : "Save image"}
          use:tooltip={video ? "Save video" : "Save image"}
          onclick={() => onSave()}
        >
          <Icon name="download" size={18} />
        </button>
      {/if}
      <button type="button" class="mv-btn" aria-label="Close (Esc)" use:tooltip={"Close (Esc)"} onclick={() => requestClose()} use:focusOnMount>
        <Icon name="close" size={18} />
      </button>
    </div>
  </div>

  {#if count > 1}
    <button type="button" class="mv-chrome mv-nav prev" disabled={!canPrev} aria-label="Previous" onclick={() => go(-1)}>
      <Icon name="back" size={22} />
    </button>
    <button type="button" class="mv-chrome mv-nav next" disabled={!canNext} aria-label="Next" onclick={() => go(1)}>
      <Icon name="chevron" size={22} />
    </button>
  {/if}

  {#if item.sender || item.when || item.desc || infoOpen}
    <div class="mv-chrome mv-foot">
      <div class="mv-cap">
        {#if item.sender || item.when}
          <div class="mv-who">
            {#if item.sender}<bdi class="mv-sender">{item.sender}</bdi>{/if}
            {#if item.when}<span class="mv-when">{item.when}</span>{/if}
          </div>
        {/if}
        {#if item.desc && !item.spoiler}<p class="mv-desc" dir="auto">{item.desc}</p>{/if}
        {#if infoOpen}
          <dl class="mv-details">
            {#if item.name}<div><dt>File</dt><dd><bdi>{item.name}</bdi></dd></div>{/if}
            {#if dims}<div><dt>Dimensions</dt><dd>{dims}</dd></div>{/if}
            {#if item.size}<div><dt>Size</dt><dd>{fmtBytes(item.size)}</dd></div>{/if}
            <div><dt>Kind</dt><dd>{video ? "Video" : "Photo"}</dd></div>
          </dl>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .mv {
    position: fixed;
    inset: 0;
    z-index: 300;
    display: grid;
    place-items: center;
    /* No padding: the margin is in the fitted size (lib/viewerfit.js), and the
       picture's resting centre is the window's centre, which the zoom math
       measures from. */
    overflow: hidden;
    touch-action: none; /* pointer events own pinch, pan and the throws */
    color: #f2f5f4;
  }
  .mv.chrome-off {
    cursor: none;
  }
  /* The room. Its own black in both themes: it darkens a photograph, it does
     not dim the app. */
  .mv-backdrop {
    position: absolute;
    inset: 0;
    background: rgb(3, 5, 6);
    animation: mv-fade var(--dur-stage) var(--ease-out);
    transition: opacity var(--dur-move) var(--ease-out);
    overflow: hidden;
  }
  .mv-backdrop.tracking {
    transition: none; /* 1:1 with the finger */
  }
  @keyframes mv-fade {
    from {
      opacity: 0;
    }
  }
  .mv-ambient {
    position: absolute;
    inset: -12%;
    width: 124%;
    height: 124%;
    opacity: 0;
    transition: opacity var(--dur-reveal) var(--ease-out);
  }
  .mv-ambient.on {
    opacity: 0.42;
  }
  /* A soft floor under the ambient light so the darkest photographs still sit
     on something, and the chrome's ink always has black behind it. */
  .mv-backdrop::after {
    content: "";
    position: absolute;
    inset: 0;
    background: radial-gradient(ellipse at center, transparent 30%, rgba(3, 5, 6, 0.7) 100%);
  }

  .mv-media {
    position: relative;
    max-width: none;
    max-height: none;
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-stage);
    user-select: none;
    -webkit-user-drag: none;
    cursor: zoom-in;
    transition: transform var(--dur-move) var(--ease-out);
    will-change: transform;
    background: #000;
  }
  img.mv-media {
    background: transparent;
  }
  .mv.zoomed .mv-media {
    cursor: grab;
    border-radius: 0;
  }
  .mv-media.dragging {
    cursor: grabbing;
    transition: none;
  }
  video.mv-media {
    cursor: default;
  }
  .mv-media.unsized {
    visibility: hidden;
  }
  /* A spoiler stays covered in here too, until asked. A static blur on the
     picture; nothing animates it. */
  .mv-media.covered {
    filter: blur(36px) brightness(0.7);
    cursor: default;
  }
  .mv-spoil {
    position: absolute;
    display: inline-flex;
    align-items: center;
    gap: var(--sp-2);
    min-height: 44px;
    padding: 0 var(--sp-5);
    border-radius: 999px;
    border: 1px solid rgba(255, 255, 255, 0.35);
    background: rgba(0, 0, 0, 0.55);
    color: #fff;
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .mv-spoil:hover {
    background: rgba(0, 0, 0, 0.72);
  }

  .mv-msg {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-2);
    max-width: 34ch;
    text-align: center;
    color: rgba(255, 255, 255, 0.85);
    font-size: var(--fs-compact);
    line-height: 1.5;
  }
  .mv-spin {
    position: relative;
    width: 28px;
    height: 28px;
    border: 2px solid rgba(255, 255, 255, 0.2);
    border-top-color: #fff;
    border-radius: 50%;
    animation: mv-rot 0.8s linear infinite;
  }
  @keyframes mv-rot {
    to {
      transform: rotate(360deg);
    }
  }

  /* ---- chrome: opacity only, at rest ---- */
  .mv-chrome {
    transition: opacity var(--dur-move) var(--ease-out);
  }
  .mv.chrome-off .mv-chrome,
  .mv.closing .mv-chrome {
    opacity: 0;
    pointer-events: none;
  }
  .mv.chrome-off .mv-cap,
  .mv.closing .mv-cap {
    pointer-events: none;
  }
  .mv-top {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-3);
    padding: calc(var(--sp-3) + var(--safe-top)) calc(var(--sp-4) + var(--safe-right)) var(--sp-6)
      calc(var(--sp-4) + var(--safe-left));
    background: linear-gradient(to bottom, rgba(0, 0, 0, 0.62), rgba(0, 0, 0, 0.28) 55%, transparent);
    animation: mv-chrome-in var(--dur-stage) var(--ease-spring);
  }
  @keyframes mv-chrome-in {
    from {
      opacity: 0;
    }
  }
  .mv-top-start,
  .mv-top-end {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-width: 0;
  }
  .mv-top-end {
    flex: none;
  }
  .mv-pill {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    height: 28px;
    padding: 0 var(--sp-3);
    border-radius: 999px;
    background: rgba(255, 255, 255, 0.12);
    border: 1px solid rgba(255, 255, 255, 0.16);
    color: rgba(255, 255, 255, 0.92);
    font-size: var(--fs-compact);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .mv-zoom {
    opacity: 0;
    transition: opacity var(--dur-standard) ease;
  }
  .mv-zoom.show {
    opacity: 1;
  }
  .mv-btn {
    display: inline-grid;
    grid-auto-flow: column;
    place-items: center;
    gap: var(--sp-2);
    width: 40px;
    height: 40px;
    padding: 0;
    border-radius: 999px;
    border: 1px solid rgba(255, 255, 255, 0.14);
    background: rgba(12, 14, 15, 0.5);
    color: rgba(255, 255, 255, 0.94);
    cursor: pointer;
  }
  .mv-btn:hover {
    background: rgba(255, 255, 255, 0.2);
  }
  .mv-btn.on {
    background: rgba(255, 255, 255, 0.92);
    color: #0b0d0e;
  }
  .mv-btn:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .mv-btn:focus-visible,
  .mv-nav:focus-visible,
  .mv-spoil:focus-visible {
    outline: var(--focus-ring);
    outline-offset: var(--focus-ring-offset);
  }
  .mv-goto {
    width: auto;
    padding: 0 var(--sp-4) 0 var(--sp-3);
    font-size: var(--fs-compact);
    font-weight: 600;
  }

  /* Neighbours: big round glass at the edges, vertically centred. */
  .mv-nav {
    position: absolute;
    top: 50%;
    width: 52px;
    height: 52px;
    margin-top: -26px;
    display: grid;
    place-items: center;
    padding: 0;
    border-radius: 50%;
    border: 1px solid rgba(255, 255, 255, 0.16);
    background: rgba(12, 14, 15, 0.5);
    color: #fff;
    cursor: pointer;
  }
  .mv-nav.prev {
    left: calc(var(--sp-4) + var(--safe-left));
  }
  .mv-nav.next {
    right: calc(var(--sp-4) + var(--safe-right));
  }
  .mv-nav:hover {
    background: rgba(255, 255, 255, 0.2);
  }
  .mv-nav:disabled {
    opacity: 0;
    pointer-events: none;
  }

  /* Who sent it, when, and what they said about it: the caption sits low over
     its own fade, and opens into the details. */
  .mv-foot {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    display: flex;
    justify-content: center;
    padding: var(--sp-7) calc(var(--sp-4) + var(--safe-right)) calc(var(--sp-4) + var(--safe-bottom))
      calc(var(--sp-4) + var(--safe-left));
    background: linear-gradient(to top, rgba(0, 0, 0, 0.7), rgba(0, 0, 0, 0.3) 60%, transparent);
    pointer-events: none;
    animation: mv-chrome-in var(--dur-stage) var(--ease-spring);
  }
  .mv-cap {
    width: min(640px, 100%);
    pointer-events: auto;
    text-align: center;
    user-select: text;
  }
  .mv-who {
    display: flex;
    justify-content: center;
    align-items: baseline;
    gap: var(--sp-2);
    font-size: var(--fs-compact);
    color: rgba(255, 255, 255, 0.72);
  }
  .mv-sender {
    font-weight: 600;
    color: #fff;
  }
  .mv-desc {
    margin: var(--sp-1) 0 0;
    font-size: var(--fs-body);
    line-height: 1.45;
    color: #fff;
    text-wrap: pretty;
    max-height: 7.3em;
    overflow-y: auto;
  }
  .mv-details {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: var(--sp-2);
    margin: var(--sp-3) 0 0;
    animation: mv-details-in var(--dur-move) var(--ease-out);
  }
  @keyframes mv-details-in {
    from {
      opacity: 0;
      transform: translateY(6px);
    }
  }
  .mv-details div {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
    min-width: 0;
    max-width: 100%;
    padding: var(--sp-2) var(--sp-3);
    border-radius: var(--radius-md);
    background: rgba(255, 255, 255, 0.1);
    border: 1px solid rgba(255, 255, 255, 0.12);
    text-align: start;
  }
  .mv-details dt {
    font-size: var(--fs-micro);
    color: rgba(255, 255, 255, 0.7);
  }
  .mv-details dd {
    margin: 0;
    font-size: var(--fs-compact);
    font-weight: 600;
    color: #fff;
    font-variant-numeric: tabular-nums;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 36ch;
  }

  @media (max-width: 768px) {
    .mv-btn {
      width: var(--tap-min);
      height: var(--tap-min);
      min-width: 44px;
      min-height: 44px;
    }
    .mv-goto span {
      display: none;
    }
    .mv-goto {
      padding: 0;
      width: 44px;
    }
    .mv-top {
      padding-left: calc(var(--sp-3) + var(--safe-left));
      padding-right: calc(var(--sp-3) + var(--safe-right));
    }
    .mv-top-end {
      gap: var(--sp-1);
    }
    .mv-nav {
      width: 40px;
      height: 40px;
      margin-top: -20px;
    }
    .mv-nav.prev {
      left: calc(var(--sp-2) + var(--safe-left));
    }
    .mv-nav.next {
      right: calc(var(--sp-2) + var(--safe-right));
    }
    .mv-desc {
      font-size: var(--fs-ui);
    }
  }
</style>
