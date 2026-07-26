<script>
  // Pick the microphone, speaker and camera calls use — and hear/see the
  // choice before trusting it to a real call. Device-local, like the theme.
  //
  // A change applies to the call in progress (the mesh swaps tracks live), and
  // is remembered for the next one.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { onMount, onDestroy } from "svelte";
  import { S, setPref, setVideoStream, flash } from "../lib/state.svelte.js";
  import {
    PREF,
    PROCESSING,
    canPickOutput,
    listDevices,
    unlockLabels,
    micStream,
    cameraStream,
    onDeviceChange,
    testTone,
  } from "../lib/devices.js";

  let { onClose } = $props();

  // The mic knobs the test meter has to honor, so what you see while dragging
  // is what the call is doing.
  const processing = () => ({
    echoCancel: S.prefs.echoCancel !== false,
    noiseSuppress: S.prefs.noiseSuppress !== false,
    autoGain: S.prefs.autoGain !== false,
  });
  const pct = (v) => `${Math.round(v * 100)}%`;

  // Opus targets. 32k is roughly what a browser negotiates on its own; the
  // ceiling is where extra bits stop being audible on a voice.
  const BITRATES = [
    { bps: 24000, label: "Low — 24 kbit/s (weak connection)" },
    { bps: 32000, label: "Standard — 32 kbit/s (browser default)" },
    { bps: 64000, label: "High — 64 kbit/s (recommended)" },
    { bps: 96000, label: "Very high — 96 kbit/s" },
    { bps: 128000, label: "Studio — 128 kbit/s" },
  ];

  // Sliders write through on every input event — you're listening while you drag.
  function slide(pref, v, apply) {
    setPref(pref, v);
    Promise.resolve(apply?.(v)).catch((err) => flash(err));
  }

  async function toggleProcessing(name) {
    const on = S.prefs[name] === false; // flipping a tri-state default of "on"
    setPref(name, on);
    try {
      await S.voice?.mesh.setProcessing(name, on);
    } catch (err) {
      flash(err);
    }
    if (micTesting) startMicTest(); // the test mic reopens with the new filter
  }

  let devices = $state({ mic: [], speaker: [], camera: [], labelled: true });
  let loading = $state(true);

  const chosen = (which) => S.prefs[PREF[which]] || "";

  async function refresh() {
    devices = await listDevices();
    loading = false;
  }

  // Names stay hidden until some media permission has been granted, so an
  // un-permissioned user would see a list of opaque ids. Ask once, explicitly.
  let asking = $state(false);
  async function reveal() {
    asking = true;
    const ok = await unlockLabels({ video: devices.camera.length > 0 });
    asking = false;
    if (!ok) flash("Permission denied — device names stay hidden", "error");
    refresh();
  }

  let stopWatching = () => {};
  onMount(() => {
    refresh();
    // Plugging in a headset mid-dialog should show it without a reopen.
    stopWatching = onDeviceChange(refresh);
  });
  onDestroy(() => {
    stopWatching();
    stopMicTest();
    stopCamTest();
  });

  // ---- applying a choice ----

  async function pick(which, id) {
    setPref(PREF[which], id);
    const mesh = S.voice?.mesh;
    try {
      if (which === "mic") {
        if (micTesting) await startMicTest(); // re-open the meter on the new mic
        await mesh?.setInputDevice(id);
      } else if (which === "speaker") {
        await mesh?.setOutputDevice(id);
        testTone(id); // confirm it out loud, like auditioning a ringtone
      } else {
        if (camTesting) await startCamTest();
        const stream = await mesh?.setCameraDevice(id);
        // The camera restarts on a switch, so the self-preview tile needs the
        // new stream (null when the camera is off — nothing to preview).
        if (mesh) setVideoStream("self:camera", stream || null, { self: true, kind: "camera" });
      }
    } catch (err) {
      flash(err?.name === "NotAllowedError" ? "Permission denied" : err);
    }
  }

  // ---- mic test: a live level meter off the selected input ----

  let micTesting = $state(false);
  let level = $state(0); // 0..1 RMS, smoothed
  let testStream = null;
  let testCtx = null;
  let testTimer = null;

  async function startMicTest() {
    stopMicTest();
    try {
      testStream = await micStream(chosen("mic"), processing());
    } catch {
      flash("Couldn't open that microphone", "error");
      return;
    }
    micTesting = true;
    try {
      testCtx = new (window.AudioContext || window.webkitAudioContext)();
      const analyser = testCtx.createAnalyser();
      analyser.fftSize = 512;
      testCtx.createMediaStreamSource(testStream).connect(analyser);
      const data = new Uint8Array(analyser.frequencyBinCount);
      testTimer = setInterval(() => {
        analyser.getByteTimeDomainData(data);
        let sum = 0;
        for (let i = 0; i < data.length; i++) {
          const v = (data[i] - 128) / 128;
          sum += v * v;
        }
        const rms = Math.sqrt(sum / data.length);
        // Scaled by the boost, so the bar shows what peers would hear and the
        // gate marker below sits at a level that means something.
        // Fast attack, slow release — reads like a real meter instead of strobing.
        const next = Math.min(1, rms * (S.prefs.micGain ?? 1) * 4);
        level = next > level ? next : level * 0.8 + next * 0.2;
      }, 60);
    } catch {
      /* metering is best-effort; the mic still opened */
    }
  }

  function stopMicTest() {
    micTesting = false;
    level = 0;
    if (testTimer) clearInterval(testTimer);
    testTimer = null;
    testCtx?.close().catch(() => {});
    testCtx = null;
    testStream?.getTracks().forEach((t) => t.stop());
    testStream = null;
  }

  // ---- camera test: a small local preview ----

  let camTesting = $state(false);
  let camStream = $state(null);
  let camEl = $state(null);

  async function startCamTest() {
    stopCamTest();
    try {
      camStream = await cameraStream(chosen("camera"));
    } catch {
      flash("Couldn't open that camera", "error");
      return;
    }
    camTesting = true;
  }

  function stopCamTest() {
    camTesting = false;
    camStream?.getTracks().forEach((t) => t.stop());
    camStream = null;
  }

  // Bind the preview stream once both the element and the stream exist.
  $effect(() => {
    if (camEl && camStream) camEl.srcObject = camStream;
  });

  // A saved device that isn't present right now (headset unplugged, pref from
  // another machine): still show it, marked, so the select doesn't quietly
  // read as some other device that we're not actually using.
  function options(which) {
    const list = devices[which];
    const id = chosen(which);
    const opts = list.map((d, i) => ({
      id: d.id,
      label: d.label || `${LABELS[which]} ${i + 1}`,
    }));
    if (id && !list.some((d) => d.id === id)) {
      opts.push({ id, label: "Saved device (not connected)" });
    }
    return opts;
  }
  const LABELS = { mic: "Microphone", speaker: "Speaker", camera: "Camera" };
</script>

<Modal title="Voice &amp; Video" {onClose} wide>
  <p class="intro">
    Which hardware calls use on this device. Applies to a call already in
    progress, and is remembered for the next one.
  </p>

  {#if !loading && !devices.labelled}
    <button class="reveal" onclick={reveal} disabled={asking}>
      <Icon name="lock" size={15} />
      {asking ? "Waiting for permission…" : "Show device names"}
      <span class="reveal-sub">Your browser hides them until you've allowed the mic once</span>
    </button>
  {/if}

  <!-- MICROPHONE -->
  <section class="dev">
    <div class="dev-head">
      <span class="chip"><Icon name="mic" size={16} /></span>
      <span class="dev-title">Microphone</span>
      <button class="test" class:on={micTesting} onclick={() => (micTesting ? stopMicTest() : startMicTest())}>
        {micTesting ? "Stop test" : "Test"}
      </button>
    </div>
    <select
      value={chosen("mic")}
      disabled={loading}
      onchange={(e) => pick("mic", e.target.value)}
      aria-label="Microphone"
    >
      <option value="">System default</option>
      {#each options("mic") as o (o.id)}
        <option value={o.id}>{o.label}</option>
      {/each}
    </select>
    {#if micTesting}
      <div class="meter" role="presentation">
        <div class="fill" style="width:{Math.round(level * 100)}%"></div>
        {#if S.prefs.micGate > 0}
          <!-- Where the gate opens: anything left of this line isn't sent. -->
          <div class="gate-mark" style="left:{Math.min(100, S.prefs.micGate * 400)}%"></div>
        {/if}
      </div>
      <span class="hint">
        Say something — the bar should move.
        {S.prefs.micGate > 0 ? "Your voice needs to clear the line to be sent." : ""}
      </span>
    {/if}

    <div class="knob">
      <span class="knob-label">Boost</span>
      <input
        type="range"
        min="0.25"
        max="4"
        step="0.05"
        value={S.prefs.micGain ?? 1}
        oninput={(e) => slide("micGain", +e.target.value, (v) => S.voice?.mesh.setMicGain(v))}
        aria-label="Microphone boost"
      />
      <span class="knob-val">{pct(S.prefs.micGain ?? 1)}</span>
    </div>
    <span class="hint">
      Turn a quiet mic up (or a hot one down) before it's sent. Above ~200% you
      amplify the room along with your voice — the gate below is what keeps that
      out.
    </span>

    <div class="knob">
      <span class="knob-label">Gate</span>
      <input
        type="range"
        min="0"
        max="0.25"
        step="0.005"
        value={S.prefs.micGate ?? 0}
        oninput={(e) => slide("micGate", +e.target.value, (v) => S.voice?.mesh.setGate(v))}
        aria-label="Noise gate threshold"
      />
      <span class="knob-val">{S.prefs.micGate > 0 ? pct(S.prefs.micGate * 4) : "Off"}</span>
    </div>
    <span class="hint">
      Holds the mic closed until you actually speak, so a fan or a keyboard
      isn't in everyone's ears between sentences. Too high and it clips the
      start of quiet words — set it just above where the bar rests in silence.
    </span>
  </section>

  <!-- SPEAKER -->
  <section class="dev">
    <div class="dev-head">
      <span class="chip"><Icon name="speaker" size={16} /></span>
      <span class="dev-title">Speaker</span>
      {#if canPickOutput}
        <button class="test" onclick={() => testTone(chosen("speaker"))}>Test</button>
      {/if}
    </div>
    {#if canPickOutput}
      <select
        value={chosen("speaker")}
        disabled={loading}
        onchange={(e) => pick("speaker", e.target.value)}
        aria-label="Speaker"
      >
        <option value="">System default</option>
        {#each options("speaker") as o (o.id)}
          <option value={o.id}>{o.label}</option>
        {/each}
      </select>
    {:else}
      <span class="hint">
        This app's window can't route audio itself, so calls play through
        whatever your system has set as the output. Change it in your OS sound
        settings.
      </span>
    {/if}

    <div class="knob">
      <span class="knob-label">Volume</span>
      <input
        type="range"
        min="0"
        max="1"
        step="0.02"
        value={S.prefs.outputVolume ?? 1}
        oninput={(e) => slide("outputVolume", +e.target.value, (v) => S.voice?.mesh.setOutputVolume(v))}
        aria-label="Call volume"
      />
      <span class="knob-val">{pct(S.prefs.outputVolume ?? 1)}</span>
    </div>
    <span class="hint">
      Master level for everyone in a call. To turn one person up or down,
      {S.isMobile ? "long-press" : "right-click"} them in the call and use their
      own volume slider.
    </span>
  </section>

  <!-- QUALITY -->
  <section class="dev">
    <div class="dev-head">
      <span class="chip"><Icon name="poll" size={16} /></span>
      <span class="dev-title">Audio quality</span>
    </div>
    <select
      value={S.prefs.voiceBitrate ?? 64000}
      onchange={(e) => slide("voiceBitrate", +e.target.value, (v) => S.voice?.mesh.setBitrate(v))}
      aria-label="Audio quality"
    >
      {#each BITRATES as b (b.bps)}
        <option value={b.bps}>{b.label}</option>
      {/each}
    </select>
    <span class="hint">
      Browsers negotiate voice at about 32 kbit/s by default — fine for a phone
      call, thin for a conversation. Higher is clearer and costs upload: on a
      call you send one copy per person, so 64 kbit/s with four friends is
      ~256 kbit/s up. A screen share's own sound always goes stereo at a higher
      rate than this.
    </span>
  </section>

  <!-- PROCESSING: the browser's capture-time filters -->
  <section class="dev">
    <div class="dev-head">
      <span class="chip"><Icon name="spark" size={16} /></span>
      <span class="dev-title">Voice processing</span>
    </div>
    {#each Object.entries(PROCESSING) as [name, p] (name)}
      <button
        class="toggle"
        role="switch"
        aria-checked={S.prefs[name] !== false}
        onclick={() => toggleProcessing(name)}
      >
        <span class="toggle-text">
          <span class="toggle-title">{p.title}</span>
          <span class="hint">{p.sub}</span>
        </span>
        <span class="switch" class:on={S.prefs[name] !== false}><span class="sw-knob"></span></span>
      </button>
    {/each}
  </section>

  <!-- CAMERA -->
  <section class="dev">
    <div class="dev-head">
      <span class="chip"><Icon name="camera" size={16} /></span>
      <span class="dev-title">Camera</span>
      <button class="test" class:on={camTesting} onclick={() => (camTesting ? stopCamTest() : startCamTest())}>
        {camTesting ? "Stop test" : "Test"}
      </button>
    </div>
    <select
      value={chosen("camera")}
      disabled={loading}
      onchange={(e) => pick("camera", e.target.value)}
      aria-label="Camera"
    >
      <option value="">System default</option>
      {#each options("camera") as o (o.id)}
        <option value={o.id}>{o.label}</option>
      {/each}
    </select>
    {#if camTesting && camStream}
      <!-- svelte-ignore a11y_media_has_caption -->
      <video class="preview" bind:this={camEl} autoplay playsinline muted></video>
    {/if}
  </section>
</Modal>

<style>
  .intro {
    font-size: 13px;
    line-height: 1.5;
    margin: 0;
    color: var(--text-muted);
  }
  .dev {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .dev-head {
    display: flex;
    align-items: center;
    gap: 9px;
  }
  .chip {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    flex: none;
    border-radius: 8px;
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .dev-title {
    font-size: 14px;
    font-weight: 600;
    margin-right: auto;
  }
  .test {
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: 999px;
    color: var(--text-muted);
    font-size: 12px;
    padding: 4px 12px;
    transition:
      color 0.12s ease,
      border-color 0.12s ease;
  }
  .test:hover {
    color: var(--text);
    border-color: var(--accent);
  }
  .test.on {
    color: var(--accent);
    border-color: var(--accent);
  }
  select {
    width: 100%;
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text);
    font-size: 13px;
    padding: 8px 10px;
  }
  select:disabled {
    opacity: 0.6;
  }
  .hint {
    font-size: 12px;
    line-height: 1.5;
    color: var(--text-muted);
  }
  /* Level meter: accent until it's loud enough that you'd clip, then a warm tip. */
  .meter {
    position: relative;
    height: 8px;
    border-radius: 999px;
    background: var(--bg-3);
    overflow: hidden;
  }
  /* The gate threshold, drawn on the meter so it can be set by eye. */
  .gate-mark {
    position: absolute;
    top: -2px;
    bottom: -2px;
    width: 2px;
    background: var(--text);
    opacity: 0.65;
  }
  .fill {
    height: 100%;
    border-radius: 999px;
    background: linear-gradient(90deg, var(--accent) 0%, var(--accent) 70%, #e5a34a 100%);
    transition: width 0.06s linear;
  }
  .preview {
    width: 100%;
    max-height: 200px;
    object-fit: cover;
    border-radius: var(--radius-md);
    background: #000;
    /* Mirror it: a self-view that moves the other way is disorienting. */
    transform: scaleX(-1);
  }
  /* Sliders: label, track, live value — the value column is fixed-width so the
     track doesn't twitch as the number changes while you drag. */
  .knob {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .knob-label {
    font-size: 12.5px;
    color: var(--text-muted);
    min-width: 48px;
  }
  .knob input[type="range"] {
    flex: 1;
    accent-color: var(--accent);
    min-width: 0;
  }
  .knob-val {
    font-size: 12px;
    font-variant-numeric: tabular-nums;
    color: var(--text);
    min-width: 42px;
    text-align: right;
  }
  .toggle {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px 0;
    background: transparent;
    color: var(--text);
    text-align: left;
  }
  .toggle + .toggle {
    border-top: 1px solid var(--border);
  }
  .toggle-text {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .toggle-title {
    font-size: 13.5px;
    font-weight: 600;
  }
  .switch {
    flex-shrink: 0;
    margin-left: auto;
    width: 40px;
    height: 24px;
    border-radius: 12px;
    background: var(--bg-3);
    border: 1px solid var(--border);
    position: relative;
    transition:
      background 0.18s ease,
      border-color 0.18s ease;
  }
  .switch.on {
    background: var(--accent);
    border-color: var(--accent);
    box-shadow: 0 0 10px color-mix(in srgb, var(--accent) 40%, transparent);
  }
  .sw-knob {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: white;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.35);
    transition: transform 0.18s cubic-bezier(0.2, 0.9, 0.3, 1);
  }
  .switch.on .sw-knob {
    transform: translateX(16px);
  }
  @media (prefers-reduced-motion: reduce) {
    .switch,
    .sw-knob,
    .fill {
      transition: none;
    }
  }
  .reveal {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
    background: var(--bg-1);
    border: 1px dashed var(--border);
    border-radius: var(--radius-md);
    color: var(--text);
    font-size: 13px;
    text-align: left;
  }
  .reveal:hover {
    border-color: var(--accent);
  }
  .reveal-sub {
    flex-basis: 100%;
    font-size: 12px;
    color: var(--text-muted);
  }
</style>
