<script>
  // Full-screen QR scanner: rear camera into a <video>, frames sampled onto a
  // canvas and decoded with jsQR. Fires onScan(text) once and stops. Used by
  // the Login link flow (and anywhere else a code can arrive by camera).
  import { onMount, onDestroy } from "svelte";
  import jsQR from "jsqr";
  import Icon from "./Icon.svelte";

  import { registerOverlay } from "./lib/state.svelte.js";
  import { openSystemSettings } from "./lib/notify.js";
  import { haptic } from "./lib/touch.js";
  let { onScan, onClose } = $props();
  // Hardware-back / global overlay stack: closing this scanner is what back
  // should do, not exit the app.
  $effect(() => registerOverlay(() => onClose?.()));

  let video = $state(null);
  let error = $state("");
  let denied = $state(false); // permission, not a transient camera failure
  let torchOn = $state(false);
  let hasTorch = $state(false);
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
        haptic("medium"); // the code is off-screen the instant it reads; confirm by feel
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
    torchOn = false;
    hasTorch = false;
  }

  // Retryable: a denied permission the user has just granted in Settings, or
  // another app that was holding the camera (NotReadableError), both used to
  // need the whole overlay closed and reopened.
  async function startCamera() {
    error = "";
    denied = false;
    done = false;
    stop();
    try {
      stream = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: "environment" },
        audio: false,
      });
      video.srcObject = stream;
      await video.play();
      // Linking off a dim laptop screen in the evening is the common case, and
      // the rear camera needs the light. Torch is a non-standard constraint, so
      // only offer the button where the track admits to having one.
      const track = stream.getVideoTracks()[0];
      hasTorch = !!track?.getCapabilities?.().torch;
      raf = requestAnimationFrame(tick);
    } catch (err) {
      denied = err?.name === "NotAllowedError";
      error = denied
        ? "Camera access was denied."
        : err?.name === "NotReadableError"
          ? "Another app is using the camera."
          : "Couldn't open the camera.";
    }
  }

  async function toggleTorch() {
    const track = stream?.getVideoTracks()[0];
    if (!track) return;
    try {
      await track.applyConstraints({ advanced: [{ torch: !torchOn }] });
      torchOn = !torchOn;
    } catch {
      hasTorch = false; // it claimed the capability and then refused it
    }
  }

  onMount(startCamera);
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
    {#if hasTorch}
      <button
        type="button"
        class="torch"
        class:on={torchOn}
        aria-pressed={torchOn}
        onclick={toggleTorch}
      >
        <Icon name="bolt" size={18} />
        {torchOn ? "Light on" : "Light"}
      </button>
    {/if}
  {:else}
    <!-- Recovery, not just an apology: the old copy said "allow it in system
         settings" and gave no way to get there, and no way to try again short
         of closing the scanner. -->
    <div class="fail">
      <p class="hint err">{error}</p>
      <div class="fail-actions">
        <button type="button" class="fail-btn" onclick={startCamera}>Try again</button>
        {#if denied}
          <button type="button" class="fail-btn" onclick={openSystemSettings}>Open settings</button>
        {/if}
      </div>
      <p class="hint sub">Or close this and paste the code instead.</p>
    </div>
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
    /* --sa-* is the Android inset bridge (MainActivity); env() is iOS. */
    bottom: calc(48px + max(env(safe-area-inset-bottom), var(--sa-bottom, 0px)));
    left: 24px;
    right: 24px;
    text-align: center;
    color: #fff;
    font-size: var(--fs-body);
    text-shadow: 0 1px 4px rgb(0 0 0 / 0.7);
    margin: 0;
  }
  .hint.err {
    color: #ffb4b4;
  }
  /* Torch: sits above the hint line, wide enough to hit while the other hand
     holds the phone at the code. */
  .torch {
    position: absolute;
    bottom: calc(100px + max(env(safe-area-inset-bottom), var(--sa-bottom, 0px)));
    left: 50%;
    transform: translateX(-50%);
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-height: max(44px, var(--tap-min));
    padding: 0 var(--sp-4);
    border-radius: 999px;
    background: rgb(0 0 0 / 0.55);
    color: #fff;
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .torch.on {
    background: var(--accent);
    color: var(--accent-fg);
  }
  .fail {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-3);
    padding: var(--sp-5);
    text-align: center;
  }
  .fail .hint {
    position: static;
    left: auto;
    right: auto;
  }
  .fail .hint.sub {
    font-size: var(--fs-compact);
    color: #cfd3da;
  }
  .fail-actions {
    display: flex;
    gap: var(--sp-3);
    flex-wrap: wrap;
    justify-content: center;
  }
  .fail-btn {
    min-height: max(44px, var(--tap-min));
    padding: 0 var(--sp-4);
    border-radius: var(--radius-md);
    background: rgb(255 255 255 / 0.14);
    color: #fff;
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .close {
    position: absolute;
    top: calc(14px + max(env(safe-area-inset-top), var(--sa-top, 0px)));
    right: 14px;
    width: max(44px, var(--tap-min));
    height: max(44px, var(--tap-min));
    padding: 0;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: rgb(0 0 0 / 0.5);
    color: #fff;
    backdrop-filter: blur(6px);
  }
</style>
