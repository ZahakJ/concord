<script>
  // Full-screen QR scanner: rear camera into a <video>, frames sampled onto a
  // canvas and decoded with jsQR. Fires onScan(text) once and stops. Used by
  // the Login link flow (and anywhere else a code can arrive by camera).
  import { onMount, onDestroy } from "svelte";
  import jsQR from "jsqr";
  import Icon from "./Icon.svelte";

  import { registerOverlay } from "./lib/state.svelte.js";
  let { onScan, onClose } = $props();
  // Hardware-back / global overlay stack: closing this scanner is what back
  // should do, not exit the app.
  $effect(() => registerOverlay(() => onClose?.()));

  let video = $state(null);
  let error = $state("");
  let stream = null;
  let raf = 0;
  let done = false;

  const canvas = document.createElement("canvas");
  const ctx = canvas.getContext("2d", { willReadFrequently: true });

  function tick() {
    if (done) return;
    if (video && video.readyState >= 2 && video.videoWidth) {
      // Sample at a capped size — decoding a 4K frame every tick melts phones.
      const scale = Math.min(1, 640 / video.videoWidth);
      canvas.width = video.videoWidth * scale;
      canvas.height = video.videoHeight * scale;
      ctx.drawImage(video, 0, 0, canvas.width, canvas.height);
      const img = ctx.getImageData(0, 0, canvas.width, canvas.height);
      const hit = jsQR(img.data, img.width, img.height, { inversionAttempts: "dontInvert" });
      if (hit?.data) {
        done = true;
        stop();
        onScan?.(hit.data);
        return;
      }
    }
    raf = requestAnimationFrame(tick);
  }

  function stop() {
    cancelAnimationFrame(raf);
    stream?.getTracks().forEach((t) => t.stop());
    stream = null;
  }

  onMount(async () => {
    try {
      stream = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: "environment" },
        audio: false,
      });
      video.srcObject = stream;
      await video.play();
      raf = requestAnimationFrame(tick);
    } catch (err) {
      error =
        err?.name === "NotAllowedError"
          ? "Camera access was denied — allow it in system settings, or paste the code instead."
          : "Couldn't open the camera — paste the code instead.";
    }
  });
  onDestroy(stop);
</script>

<div class="scanner" role="dialog" aria-label="Scan QR code">
  <!-- svelte-ignore a11y_media_has_caption -->
  <video bind:this={video} playsinline muted></video>
  {#if !error}
    <div class="frame" aria-hidden="true">
      <span class="c tl"></span><span class="c tr"></span>
      <span class="c bl"></span><span class="c br"></span>
      <span class="sweep"></span>
    </div>
    <p class="hint">Point at the QR code on your other device</p>
  {:else}
    <p class="hint err">{error}</p>
  {/if}
  <button type="button" class="close" aria-label="Close scanner" onclick={() => (stop(), onClose?.())}>
    <Icon name="close" size={20} />
  </button>
</div>

<style>
  .scanner {
    position: fixed;
    inset: 0;
    z-index: 460;
    background: #000;
    display: grid;
    place-items: center;
  }
  video {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  /* Viewfinder: four accent corners + a slow scanning sweep, floating over a
     dimmed surround so the target area reads instantly. */
  .frame {
    position: relative;
    width: min(68vw, 300px);
    aspect-ratio: 1;
    border-radius: 18px;
    box-shadow: 0 0 0 100vmax rgb(0 0 0 / 0.45);
    overflow: hidden;
  }
  .c {
    position: absolute;
    width: 34px;
    height: 34px;
    border: 3.5px solid var(--accent);
  }
  .tl { top: 0; left: 0; border-right: 0; border-bottom: 0; border-top-left-radius: 18px; }
  .tr { top: 0; right: 0; border-left: 0; border-bottom: 0; border-top-right-radius: 18px; }
  .bl { bottom: 0; left: 0; border-right: 0; border-top: 0; border-bottom-left-radius: 18px; }
  .br { bottom: 0; right: 0; border-left: 0; border-top: 0; border-bottom-right-radius: 18px; }
  .sweep {
    position: absolute;
    left: 8%;
    right: 8%;
    height: 2px;
    background: linear-gradient(90deg, transparent, var(--accent), transparent);
    box-shadow: 0 0 12px var(--accent);
    animation: sweep 2.2s ease-in-out infinite;
  }
  @keyframes sweep {
    0%, 100% { top: 12%; }
    50% { top: 86%; }
  }
  @media (prefers-reduced-motion: reduce) {
    .sweep { display: none; }
  }
  .hint {
    position: absolute;
    bottom: calc(48px + env(safe-area-inset-bottom));
    left: 24px;
    right: 24px;
    text-align: center;
    color: #fff;
    font-size: 14px;
    text-shadow: 0 1px 4px rgb(0 0 0 / 0.7);
    margin: 0;
  }
  .hint.err {
    color: #ffb4b4;
  }
  .close {
    position: absolute;
    top: calc(14px + env(safe-area-inset-top));
    right: 14px;
    width: 42px;
    height: 42px;
    padding: 0;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: rgb(0 0 0 / 0.5);
    color: #fff;
    backdrop-filter: blur(6px);
  }
</style>
