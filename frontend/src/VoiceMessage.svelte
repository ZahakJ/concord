<script>
  // A voice message: an audio attachment (recorded in the composer, sent through
  // the same encrypted-blob path as any file) rendered as an inline player
  // — play/pause, a drag-to-seek waveform, and elapsed/total time. The blob is
  // fetched + decrypted on first play, so it costs nothing until you listen.
  import Icon from "./Icon.svelte";
  import { api } from "./lib/api.js";
  import { flash } from "./lib/state.svelte.js";
  import {
    parseVoiceMeta,
    envelopeFromBuffer,
    cachedEnvelope,
    cacheEnvelope,
  } from "./lib/voicemsg.js";

  let { channelId, tok } = $props();

  let audio; // HTMLAudioElement, created on first play
  let audioP = null; // in-flight ensureAudio(), so a scrub + a tap can't build two
  let waveEl = $state(null);
  let loading = $state(false);
  let playing = $state(false);
  let cur = $state(0);

  // What the sender measured, carried in the filename (see lib/voicemsg.js).
  // A message recorded before that existed has neither, and both fall back.
  const meta = parseVoiceMeta(tok.name);
  // The duration, known WITHOUT fetching anything. `dur` used to stay 0 until
  // the blob had been fetched, decrypted and had `loadedmetadata` fire — which
  // only happens on first play — so an unplayed voice message advertised itself
  // as zero seconds long. "Is this a four-second yes or a six-minute ramble" is
  // the only question anyone asks before pressing play.
  let dur = $state(meta.secs || 0);

  // Bar heights are in PIXELS off a 0..1 basis rather than a percentage of the
  // strip, so the strip can grow to a finger-sized target without the waveform
  // inflating with it.
  const WAVE_PX = 30;

  // The real envelope, in three tiers.
  //
  //   1. measured at record time and carried in the token — free, exact, and
  //      what every message sent from this build has;
  //   2. decoded from the audio on first play and remembered for the session —
  //      for messages sent before that existed;
  //   3. a flat pill.
  //
  // What it must never be again is what it was: forty-two bars hashed from the
  // blob id. That is a stable random barcode dressed as information — it cannot
  // show you the pauses, and scrubbing to a shape in it lands nowhere near what
  // the shape appeared to promise.
  let bars = $state(meta.env || cachedEnvelope(tok.blobId));

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
        audio.addEventListener("loadedmetadata", () => {
          // Only when it is a real number: a webm from MediaRecorder reports
          // Infinity until it has been seeked to the end, and overwriting a
          // duration we already know with Infinity would undo the whole point.
          if (isFinite(audio.duration) && audio.duration > 0) dur = audio.duration;
        });
        audio.addEventListener("ended", () => {
          playing = false;
          cur = 0;
        });
        audio.addEventListener("pause", () => (playing = false));
        audio.addEventListener("play", () => (playing = true));
        // The bytes are here anyway; if the sender's build didn't measure the
        // shape, measure it now — once — rather than drawing a lie.
        if (!bars) drawFromAudio(dataUrl);
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

  // Decode the clip and take its envelope. Best-effort by design: a codec the
  // WebAudio decoder won't take (some Safari builds refuse webm/opus) leaves
  // the flat pill, which is honest — the alternative was a barcode.
  async function drawFromAudio(dataUrl) {
    try {
      const AC = window.AudioContext || window.webkitAudioContext;
      if (!AC) return;
      const buf = await new AC().decodeAudioData(await (await fetch(dataUrl)).arrayBuffer());
      const env = envelopeFromBuffer(buf);
      if (!env) return;
      cacheEnvelope(tok.blobId, env);
      bars = env;
      if (!dur && isFinite(buf.duration)) dur = buf.duration;
    } catch {
      /* the player still works; the bars stay flat */
    }
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
    if (!audio || !waveEl) return;
    // The recorded length is the one to trust. A webm from MediaRecorder
    // reports `duration: Infinity` until it has been seeked to its end, so a
    // player that only believed the element could not seek one at all.
    const total = isFinite(audio.duration) && audio.duration > 0 ? audio.duration : dur;
    if (!total) return;
    const rect = waveEl.getBoundingClientRect();
    const f = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
    audio.currentTime = f * total;
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
    {#if bars}
      {#each bars as h, i (i)}
        <span
          class="vm-bar"
          class:on={i / bars.length <= frac}
          style="height:{Math.round(h * WAVE_PX)}px"
        ></span>
      {/each}
    {:else}
      <!-- Nothing measured, nothing decoded: a plain progress pill. It says
           "audio, this far through" and nothing it doesn't know. -->
      <span class="vm-pill"><span class="vm-pill-on" style="width:{Math.round(frac * 100)}%"></span></span>
    {/if}
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
  .vm-pill {
    flex: 1;
    height: 6px;
    border-radius: 3px;
    overflow: hidden;
    background: color-mix(in srgb, var(--text-faint) 45%, transparent);
  }
  .vm-pill-on {
    display: block;
    height: 100%;
    border-radius: 3px;
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
