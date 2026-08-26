<script>
  // A voice message: an audio attachment (recorded in the composer, sent through
  // the same encrypted-blob path as any file) rendered as a Discord-style player
  // — play/pause, a drag-to-seek waveform, and elapsed/total time. The blob is
  // fetched + decrypted on first play, so it costs nothing until you listen.
  import Icon from "./Icon.svelte";
  import { api } from "./lib/api.js";
  import { flash } from "./lib/state.svelte.js";

  let { channelId, tok } = $props();

  let audio; // HTMLAudioElement, created on first play
  let audioP = null; // in-flight ensureAudio(), so a scrub + a tap can't build two
  let waveEl = $state(null);
  let loading = $state(false);
  let playing = $state(false);
  let cur = $state(0);
  let dur = $state(0);

  // A stable pseudo-waveform from the blob id — looks like a waveform without
  // the cost of decoding the audio, and is identical on every device. Bar
  // heights are in PIXELS off this basis rather than a percentage of the strip,
  // so the strip can grow to a finger-sized target without the waveform
  // inflating with it.
  const WAVE_PX = 30;
  const bars = (() => {
    const out = [];
    for (let i = 0; i < 42; i++) {
      let h = 2166136261;
      const s = tok.blobId + ":" + i;
      for (let j = 0; j < s.length; j++) h = Math.imul(h ^ s.charCodeAt(j), 16777619) >>> 0;
      out.push(0.25 + (h % 76) / 100);
    }
    return out;
  })();

  const frac = $derived(dur > 0 ? cur / dur : 0);

  function fmt(t) {
    if (!isFinite(t)) return "0:00";
    const s = Math.floor(t % 60);
    return `${Math.floor(t / 60)}:${s < 10 ? "0" : ""}${s}`;
  }

  function ensureAudio() {
    if (audio) return Promise.resolve(audio);
    // Memoised: pressing play while a scrub is already fetching used to decrypt
    // the blob twice and leave two <audio> elements playing over each other.
    if (audioP) return audioP;
    loading = true;
    audioP = (async () => {
      try {
        const dataUrl = await api.fetchFile(channelId, tok.blobId, tok.keys, tok.mime);
        audio = new Audio(dataUrl);
        audio.addEventListener("timeupdate", () => (cur = audio.currentTime));
        audio.addEventListener("loadedmetadata", () => (dur = audio.duration));
        audio.addEventListener("ended", () => {
          playing = false;
          cur = 0;
        });
        audio.addEventListener("pause", () => (playing = false));
        audio.addEventListener("play", () => (playing = true));
        return audio;
      } catch (err) {
        flash(err);
        audioP = null; // let a retry through
        return null;
      } finally {
        loading = false;
      }
    })();
    return audioP;
  }

  async function toggle() {
    const a = await ensureAudio();
    if (!a) return;
    if (a.paused) a.play().catch(() => {});
    else a.pause();
  }

  // Scrubbing is a DRAG, not a click. One bar is ~5px wide, so jabbing at the
  // strip and hoping was the whole seeking story on a phone; with pointer
  // capture the finger can slide along it and land where it means to. (The
  // strip sets touch-action:none so the chat pane's pan-y doesn't eat the
  // horizontal movement before we see it.)
  let scrubbing = false;
  function seekTo(clientX) {
    if (!audio || !isFinite(audio.duration) || !waveEl) return;
    const rect = waveEl.getBoundingClientRect();
    const f = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
    audio.currentTime = f * audio.duration;
    cur = audio.currentTime;
  }
  async function onScrubDown(e) {
    waveEl?.setPointerCapture?.(e.pointerId);
    scrubbing = true;
    const a = await ensureAudio();
    if (!a) {
      scrubbing = false;
      return;
    }
    if (scrubbing) seekTo(e.clientX); // the finger may already have lifted
  }
  function onScrubMove(e) {
    if (scrubbing) seekTo(e.clientX);
  }
  function onScrubUp(e) {
    scrubbing = false;
    waveEl?.releasePointerCapture?.(e.pointerId);
  }
</script>

<div class="vm">
  <button class="vm-play" onclick={toggle} aria-label={playing ? "Pause" : "Play voice message"}>
    {#if loading}
      <span class="vm-spin"></span>
    {:else}
      <Icon name={playing ? "pause" : "play"} size={16} />
    {/if}
  </button>
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="vm-wave"
    bind:this={waveEl}
    onpointerdown={onScrubDown}
    onpointermove={onScrubMove}
    onpointerup={onScrubUp}
    onpointercancel={onScrubUp}
    title="Drag to seek"
  >
    {#each bars as h, i (i)}
      <span
        class="vm-bar"
        class:on={i / bars.length <= frac}
        style="height:{Math.round(h * WAVE_PX)}px"
      ></span>
    {/each}
  </div>
  <span class="vm-time">{fmt(playing || cur ? cur : dur)}</span>
</div>

<style>
  .vm {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: var(--sp-1);
    max-width: 340px;
    padding: var(--sp-2) var(--sp-3);
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .vm-play {
    flex-shrink: 0;
    width: 34px;
    height: 34px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: var(--accent);
    color: var(--accent-fg);
    transition:
      transform var(--dur-quick) ease,
      background var(--dur-quick) ease;
  }
  @media (pointer: fine) {
    .vm-play:hover {
      background: var(--accent-hover);
      transform: scale(1.06);
    }
  }
  .vm-play:active {
    transform: scale(0.94);
  }
  .vm-wave {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 2px;
    height: 30px;
    cursor: pointer;
    /* The pointer handlers own this strip: without it the chat pane's
       touch-action:pan-y swallows a horizontal drag and the scrub never
       starts. */
    touch-action: none;
  }
  .vm-bar {
    flex: 1;
    min-width: 2px;
    border-radius: 2px;
    background: color-mix(in srgb, var(--text-faint) 70%, transparent);
    transition: background 0.1s linear;
  }
  .vm-bar.on {
    background: var(--accent);
  }
  .vm-time {
    flex-shrink: 0;
    font-size: var(--fs-tiny);
    font-variant-numeric: tabular-nums;
    color: var(--text-muted);
    min-width: 30px;
    text-align: right;
  }
  .vm-spin {
    width: 16px;
    height: 16px;
    border: 2px solid rgba(255, 255, 255, 0.4);
    border-top-color: #fff;
    border-radius: 50%;
    animation: vm-rot 0.8s linear infinite;
  }
  @keyframes vm-rot {
    to {
      transform: rotate(360deg);
    }
  }
  /* The primary control of the component was 34px, and the scrub strip 30px.
     The bars keep their pixel heights (see WAVE_PX), so the strip grows into a
     finger-sized target without the waveform growing with it. */
  @media (pointer: coarse), (max-width: 768px) {
    .vm-play {
      width: var(--tap-min);
      height: var(--tap-min);
    }
    .vm-wave {
      height: var(--tap-min);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .vm-play:hover {
      transform: none;
    }
  }
</style>
