<script>
  // Off-grid QR link, sending side: the frames that were queued for the QR
  // link, shown as a looping sequence of QR codes for another device's camera.
  // Each code is one sealed frame; the loop keeps going so a code the camera
  // missed comes round again, and the receiver says which ones it still needs.
  import { onDestroy } from "svelte";
  import { pushLayer } from "../lib/navstack.svelte.js";
  import Icon from "../Icon.svelte";
  import { encodeCodes } from "../lib/qrseq.js";

  let { frames, onClose } = $props();
  $effect(() => pushLayer("qrseq", () => onClose?.()));

  const SPEEDS = [600, 350, 220]; // ms per code: careful, normal, fast
  let speed = $state(1);
  let images = $state([]);
  let at = $state(0);
  let paused = $state(false);
  let error = $state("");
  let timer = 0;

  // The encoder is a lazy chunk, as in the invite and link-device dialogs.
  import("qrcode")
    .then(async ({ default: QRCode }) => {
      const codes = encodeCodes(frames);
      const out = [];
      for (const c of codes) {
        out.push(await QRCode.toDataURL(c, { margin: 2, width: 480, errorCorrectionLevel: "M" }));
      }
      images = out;
      schedule();
    })
    .catch((e) => (error = String(e?.message || e)));

  function schedule() {
    clearTimeout(timer);
    if (paused || images.length < 2) return;
    timer = setTimeout(() => {
      at = (at + 1) % images.length;
      schedule();
    }, SPEEDS[speed]);
  }

  function step(d) {
    paused = true;
    clearTimeout(timer);
    at = (at + d + images.length) % images.length;
  }

  function toggle() {
    paused = !paused;
    schedule();
  }

  function faster() {
    speed = (speed + 1) % SPEEDS.length;
    schedule();
  }

  // The screen must not dim while someone holds a camera to it.
  const keepAwake = (on) =>
    window.Capacitor?.Plugins?.ConcordCore?.setKeepAwake?.({ on }).catch(() => {});
  keepAwake(true);
  onDestroy(() => {
    clearTimeout(timer);
    keepAwake(false);
  });
</script>

<div class="seq" role="dialog" aria-label="QR code sequence">
  <button type="button" class="close" aria-label="Close" onclick={() => onClose?.()}>
    <Icon name="close" size={20} />
  </button>
  {#if error}
    <p class="msg">{error}</p>
  {:else if !images.length}
    <p class="msg">Preparing {frames.length} code{frames.length === 1 ? "" : "s"}…</p>
  {:else}
    <img class="qr" src={images[at]} alt="QR code {at + 1} of {images.length}" />
    <p class="count">{at + 1} / {images.length}</p>
    <p class="msg">Scan with Concord on the other device: Settings → Off-grid links → Scan QR codes. The sequence loops until you close it.</p>
    {#if images.length > 1}
      <div class="ctl">
        <button type="button" aria-label="Previous code" onclick={() => step(-1)}><Icon name="back" size={18} /></button>
        <button type="button" aria-label={paused ? "Play" : "Pause"} onclick={toggle}>
          <Icon name={paused ? "play" : "pause"} size={18} />
        </button>
        <button type="button" aria-label="Next code" onclick={() => step(1)}><span class="fwd"><Icon name="back" size={18} /></span></button>
        <button type="button" class="speed" onclick={faster}>{["Slow", "Normal", "Fast"][speed]}</button>
      </div>
    {/if}
  {/if}
</div>

<style>
  .seq {
    position: fixed;
    inset: 0;
    z-index: 460;
    background: #fff;
    color: #111;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--sp-3);
    padding: var(--sp-5);
  }
  /* Pixelated: a smoothed QR module edge is what makes a camera hesitate. */
  .qr {
    width: min(86vw, 70vh, 520px);
    aspect-ratio: 1;
    image-rendering: pixelated;
  }
  .count {
    margin: 0;
    font-size: var(--fs-title, 20px);
    font-weight: 700;
    font-variant-numeric: tabular-nums;
  }
  .msg {
    margin: 0;
    max-width: 440px;
    text-align: center;
    font-size: var(--fs-compact);
    color: #444;
  }
  .ctl {
    display: flex;
    gap: var(--sp-2);
  }
  .ctl button {
    min-width: max(44px, var(--tap-min));
    min-height: max(44px, var(--tap-min));
    padding: 0 var(--sp-3);
    border-radius: var(--radius-md);
    background: #eceef1;
    color: #111;
    display: grid;
    place-items: center;
    font-weight: 600;
  }
  .fwd {
    display: grid;
    transform: scaleX(-1);
  }
  .close {
    position: absolute;
    top: calc(14px + max(var(--safe-top), var(--sa-top, 0px)));
    right: 14px;
    width: max(44px, var(--tap-min));
    height: max(44px, var(--tap-min));
    padding: 0;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: #eceef1;
    color: #111;
  }
</style>
