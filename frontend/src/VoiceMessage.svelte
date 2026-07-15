<script>
  // A voice message: an audio attachment (recorded in the composer, sent through
  // the same encrypted-blob path as any file) rendered as a Discord-style player
  // — play/pause, a click-to-seek waveform, and elapsed/total time. The blob is
  // fetched + decrypted on first play, so it costs nothing until you listen.
  import Icon from "./Icon.svelte";
  import { api } from "./lib/api.js";
  import { flash } from "./lib/state.svelte.js";

  let { channelId, tok } = $props();

  let audio; // HTMLAudioElement, created on first play
  let loading = $state(false);
  let playing = $state(false);
  let cur = $state(0);
  let dur = $state(0);

  // A stable pseudo-waveform from the blob id — looks like a waveform without
  // the cost of decoding the audio, and is identical on every device.
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

  async function ensureAudio() {
    if (audio) return audio;
    loading = true;
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
      return null;
    } finally {
      loading = false;
    }
  }

  async function toggle() {
    const a = await ensureAudio();
    if (!a) return;
    if (a.paused) a.play().catch(() => {});
    else a.pause();
  }

  async function seek(e) {
    const a = await ensureAudio();
    if (!a || !isFinite(a.duration)) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const f = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    a.currentTime = f * a.duration;
    cur = a.currentTime;
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
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="vm-wave" onclick={seek} title="Seek">
    {#each bars as h, i (i)}
      <span
        class="vm-bar"
        class:on={i / bars.length <= frac}
        style="height:{Math.round(h * 100)}%"
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
    margin-top: 4px;
    max-width: 340px;
    padding: 8px 12px;
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
    color: #fff;
    transition:
      transform 0.12s ease,
      background 0.12s ease;
  }
  .vm-play:hover {
    background: var(--accent-hover);
    transform: scale(1.06);
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
    font-size: 11px;
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
  @media (prefers-reduced-motion: reduce) {
    .vm-play:hover {
      transform: none;
    }
  }
</style>
