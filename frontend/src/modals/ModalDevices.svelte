<script>
  // Pick the microphone, speaker and camera calls use — and hear/see the
  // choice before trusting it to a real call. Device-local, like the theme.
  //
  // Layering: the three pickers and the call volume are what anyone might touch,
  // so they're what you see. Boost, the noise gate, the capture filters and the
  // bitrate are for when something's actually wrong — they live behind
  // "Advanced", one click away, not stacked in front of everyone.
  //
  // The same rule applies to the words. Why a knob exists and when to reach for
  // it goes behind the ⓘ on its label; what stays printed is the one line you
  // need to use it, plus anything you'd regret not having read (a key that also
  // types, a share that will arrive silent). A column of paragraphs under every
  // slider is how this panel became the thing people scroll past.
  //
  // A change applies to the call in progress (the mesh swaps tracks live), and
  // is remembered for the next one.
  import { slide } from "svelte/transition";
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import InfoDot from "./InfoDot.svelte";
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
    recordSelfTest,
  } from "../lib/devices.js";
  import { NR_LEVELS, canDenoise } from "../lib/denoise.js";
  import { bindLabel, makeRecorder, typesCharacter } from "../lib/keybind.js";

  let { onClose } = $props();

  let devices = $state({ mic: [], speaker: [], camera: [], labelled: true });
  let loading = $state(true);
  let advanced = $state(false);

  const chosen = (which) => S.prefs[PREF[which]] || "";
  const pct = (v) => `${Math.round(v * 100)}%`;

  // Each capture filter's description is two sentences: what it does, then when
  // you'd want it off. Print the first and keep the whole thing behind the dot —
  // three switches each trailing a paragraph of advice is the wall. Split here
  // rather than in devices.js so the copy stays one readable sentence pair for
  // anyone editing it.
  function lead(sub) {
    const i = sub.indexOf(". ");
    return i < 0 ? { line: sub, info: "" } : { line: sub.slice(0, i + 1), info: sub };
  }

  // The mic knobs the test meter has to honor, so what you see while dragging
  // is what the call is doing.
  const processing = () => ({
    echoCancel: S.prefs.echoCancel !== false,
    noiseSuppress: S.prefs.noiseSuppress !== false,
    autoGain: S.prefs.autoGain !== false,
  });

  // Opus targets. 32k is roughly what a browser negotiates on its own; the
  // ceiling is where extra bits stop being audible on a voice.
  const BITRATES = [
    { bps: 24000, label: "Low — 24 kbit/s" },
    { bps: 32000, label: "Standard — 32 kbit/s" },
    { bps: 64000, label: "High — 64 kbit/s" },
    { bps: 96000, label: "Very high — 96 kbit/s" },
    { bps: 128000, label: "Studio — 128 kbit/s" },
  ];

  // The gate's threshold, as a share of the meter's full width. Kept out of the
  // raw 0–0.25 RMS units the mesh wants, which mean nothing to anyone.
  const GATE_DEFAULT = 0.04;

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
    selfCtl?.cancel();
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

  // Sliders write through on every input event — you're listening while you drag.
  function knob(pref, v, apply) {
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

  // ---- noise gate ----
  //
  // On/off is a switch and the threshold is a slider, because they're two
  // different questions. (They used to be one slider where zero meant "off",
  // which reads as a bar that's somehow also a checkbox.) Turning it on starts
  // the meter, since a threshold you can't see against your own room noise is
  // a number you can only guess at.

  const gateOn = $derived((S.prefs.micGate ?? 0) > 0);
  async function toggleGate() {
    const next = gateOn ? 0 : GATE_DEFAULT;
    setPref("micGate", next);
    try {
      await S.voice?.mesh.setGate(next);
    } catch (err) {
      flash(err);
    }
    if (next && !micTesting) startMicTest(true);
    else if (!next && autoTest) stopMicTest();
  }

  // ---- push-to-talk ----
  //
  // The other answer to "when is my mic open". Picking it retires the noise
  // gate rather than stacking with it (the mesh parks the gate open), so the
  // gate block below hides itself here instead of offering a knob that does
  // nothing.

  const ptt = $derived(!!S.prefs.pushToTalk);
  const pttLabel = $derived(bindLabel(S.prefs.pttBind));

  function setActivation(on) {
    setPref("pushToTalk", on);
    S.voice?.mesh.setPushToTalk(on);
    S.talking = false; // the mesh just dropped the hold; keep the button honest
    if (on && !S.prefs.pttBind) recording = true; // no key yet: ask straight away
    else recording = false;
  }

  // Recording listens in the CAPTURE phase and stops the event dead, so the
  // key being bound doesn't also fire the global keymap on its way past (or,
  // worse, trigger push-to-talk itself). Escape cancels — the one key that
  // stays yours, which is also why keybind.js refuses to bind it.
  //
  // The decision of what a keypress means lives in keybind.js (makeRecorder) —
  // a modifier arms, a real key or a release settles it — so that logic is unit
  // tested rather than trapped in a component. Here we only route events to it.
  let recording = $state(false);
  let held = $state(null); // mirrors recorder.armed(), for the live preview
  let recorder = null;
  $effect(() => {
    if (!recording) {
      held = null;
      recorder = null;
      return;
    }
    recorder = makeRecorder();
    window.addEventListener("keydown", recordKey, true);
    window.addEventListener("keyup", recordRelease, true);
    // Losing focus mid-record (alt-tab is itself a modifier hold) would
    // otherwise leave the recorder armed against a key that's no longer down.
    window.addEventListener("blur", cancelRecording);
    return () => {
      window.removeEventListener("keydown", recordKey, true);
      window.removeEventListener("keyup", recordRelease, true);
      window.removeEventListener("blur", cancelRecording);
    };
  });

  const cancelRecording = () => (recording = false);

  function commit(bind) {
    setPref("pttBind", bind);
    recording = false;
  }

  function recordKey(e) {
    e.preventDefault();
    e.stopImmediatePropagation();
    if (e.code === "Escape") {
      recording = false;
      return;
    }
    const bind = recorder?.down(e);
    held = recorder?.armed() ?? null;
    if (bind) commit(bind);
  }

  function recordRelease(e) {
    if (!recorder?.armed()) return;
    const bind = recorder.up(e);
    if (!bind) return;
    e.preventDefault();
    e.stopImmediatePropagation();
    held = null;
    commit(bind); // let go with nothing else pressed: bind the modifier itself
  }

  // What the button says while armed: the modifiers currently down, with a
  // trailing "…" standing in for the key that would complete them.
  const recLabel = $derived.by(() => {
    if (!held) return "Press any key…";
    const mods = [
      held.ctrl && "Ctrl",
      held.shift && "Shift",
      held.alt && "Alt",
      held.meta && "Meta",
    ].filter(Boolean);
    return `${mods.join(" + ")} + …`;
  });

  // ---- mic test: a live level meter off the selected input ----

  let micTesting = $state(false);
  let autoTest = false; // started by the gate rather than by the Test button
  let level = $state(0); // 0..1 RMS, smoothed
  let testStream = null;
  let testCtx = null;
  let testTimer = null;

  async function startMicTest(auto = false) {
    stopMicTest();
    try {
      testStream = await micStream(chosen("mic"), processing());
    } catch {
      flash("Couldn't open that microphone", "error");
      return;
    }
    micTesting = true;
    autoTest = auto;
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
        // gate marker sits at a level that means something.
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
    autoTest = false;
    level = 0;
    if (testTimer) clearInterval(testTimer);
    testTimer = null;
    testCtx?.close().catch(() => {});
    testCtx = null;
    testStream?.getTracks().forEach((t) => t.stop());
    testStream = null;
  }

  // ---- hear yourself ----
  //
  // The question every one of these knobs is really answering. Records a few
  // seconds through the same boost/gate a call would apply, then plays it back
  // out of the chosen speaker.
  let selfTest = $state(""); // "" | "recording" | "playing"
  let selfCtl = null;

  async function hearMyself() {
    if (selfTest) {
      // Mid-record: stop early and go straight to playback. Mid-playback: quit.
      if (selfTest === "recording") selfCtl?.stop();
      else selfCtl?.cancel();
      return;
    }
    stopMicTest(); // one mic at a time
    selfTest = "recording";
    try {
      selfCtl = recordSelfTest({
        deviceId: chosen("mic"),
        processing: processing(),
        gain: S.prefs.micGain ?? 1,
        gate: S.prefs.micGate ?? 0,
        nr: S.prefs.micNr || "",
        sinkId: chosen("speaker"),
      });
      selfCtl.onLevel((v) => (level = v));
      const playing = selfCtl.done;
      // Flip the label the moment recording ends; `done` covers both phases.
      setTimeout(() => selfTest === "recording" && (selfTest = "playing"), 5000);
      await playing;
    } catch {
      flash("Couldn't record from that microphone", "error");
    }
    selfTest = "";
    selfCtl = null;
    level = 0;
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
  const LABELS = { mic: "Microphone", speaker: "Speaker", camera: "Camera" };
  function options(which) {
    const list = devices[which];
    const id = chosen(which);
    const opts = list.map((d, i) => ({ id: d.id, label: d.label || `${LABELS[which]} ${i + 1}` }));
    if (id && !list.some((d) => d.id === id)) {
      opts.push({ id, label: "Saved device (not connected)" });
    }
    return opts;
  }
</script>

<Modal title="Voice &amp; Video" {onClose} wide>
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
      <button
        class="test"
        class:on={!!selfTest}
        disabled={micTesting}
        onclick={hearMyself}
        title="Record a few seconds and play it straight back"
      >
        {selfTest === "recording" ? "Recording…" : selfTest === "playing" ? "Playing…" : "Hear myself"}
      </button>
      <button
        class="test"
        class:on={micTesting}
        disabled={!!selfTest}
        onclick={() => (micTesting ? stopMicTest() : startMicTest())}
      >
        {micTesting ? "Stop" : "Test"}
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
    {#if micTesting || selfTest}
      <div transition:slide={{ duration: 180 }}>
        <div class="meter" role="presentation">
          <div class="fill" style="width:{Math.round(level * 100)}%"></div>
          {#if gateOn && !ptt}
            <div class="gate-mark" style="left:{Math.min(100, S.prefs.micGate * 400)}%"></div>
          {/if}
        </div>
        <span class="hint">
          {#if selfTest === "recording"}
            Say something — you'll hear it back in a moment, exactly as the call
            would send it. Click again to play it now.
          {:else if selfTest === "playing"}
            That's how you sound to everyone else.
          {:else}
            Say something — the bar should move.
          {/if}
        </span>
      </div>
    {/if}

    <div class="act">
      <span class="gate-title">Mic activation</span>
      <div class="seg two" role="radiogroup" aria-label="Mic activation">
        <button class:sel={!ptt} role="radio" aria-checked={!ptt} onclick={() => setActivation(false)}>
          Voice activity
        </button>
        <button class:sel={ptt} role="radio" aria-checked={ptt} onclick={() => setActivation(true)}>
          Push to talk
        </button>
      </div>
      {#if ptt}
        <div class="ptt" transition:slide={{ duration: 180 }}>
          <div class="knob">
            <span class="knob-label">
              Hold
              <InfoDot
                text="Concord doesn't reach outside its own process, so it can't claim a key from the whole desktop. Alt-tab away and the key goes back to whatever you switched to."
                label="Why? Push-to-talk key"
              />
            </span>
            <button class="rec" class:rec-on={recording} onclick={() => (recording = !recording)}>
              {recording ? recLabel : pttLabel || "Set a key"}
            </button>
          </div>
          <span class="hint">
            Your mic stays shut until you hold this, while Concord's window has focus.
          </span>
          {#if S.prefs.pttBind && typesCharacter(S.prefs.pttBind)}
            <span class="hint warn">
              {pttLabel} also types. It won't open your mic while you're writing
              a message — hold it with Ctrl or Alt, or pick a function key, to
              have it work everywhere.
            </span>
          {/if}
        </div>
      {/if}
    </div>
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
        whatever your system has set as the output.
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
        oninput={(e) => knob("outputVolume", +e.target.value, (v) => S.voice?.mesh.setOutputVolume(v))}
        aria-label="Call volume"
      />
      <span class="knob-val">{pct(S.prefs.outputVolume ?? 1)}</span>
    </div>
  </section>

  <!-- CAMERA -->
  <section class="dev">
    <div class="dev-head">
      <span class="chip"><Icon name="camera" size={16} /></span>
      <span class="dev-title">Camera</span>
      <button class="test" class:on={camTesting} onclick={() => (camTesting ? stopCamTest() : startCamTest())}>
        {camTesting ? "Stop" : "Test"}
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
      <video class="preview" bind:this={camEl} autoplay playsinline muted transition:slide={{ duration: 180 }}
      ></video>
    {/if}
  </section>

  <!-- ADVANCED: everything you only reach for when something's wrong. -->
  <button class="disclose" onclick={() => (advanced = !advanced)} aria-expanded={advanced}>
    <span class="disclose-chev" class:open={advanced}>›</span>
    Advanced audio
    <span class="disclose-sub">Boost, noise gate, filters &amp; quality</span>
  </button>

  {#if advanced}
    <div class="adv" transition:slide={{ duration: 220 }}>
      <section class="dev">
        <div class="dev-head">
          <span class="chip"><Icon name="mic" size={16} /></span>
          <span class="dev-title">Input</span>
        </div>
        <div class="knob">
          <span class="knob-label">
            Boost
            <InfoDot
              text="Turns a quiet mic up, or a hot one down, before it's sent. Watch the meter with Test on: if the bar reaches the warm tip when you talk normally, it's too high."
              label="Why? Boost"
            />
          </span>
          <input
            type="range"
            min="0.25"
            max="4"
            step="0.05"
            value={S.prefs.micGain ?? 1}
            oninput={(e) => knob("micGain", +e.target.value, (v) => S.voice?.mesh.setMicGain(v))}
            aria-label="Microphone boost"
          />
          <span class="knob-val">{pct(S.prefs.micGain ?? 1)}</span>
        </div>

        {#if canDenoise()}
          <div class="nr-block">
            <span class="gate-title">
              Noise reduction
              <InfoDot
                text="Learns the steady noise in your room — a fan, a hum, mic hiss — and pulls it out from under your voice, not just between sentences. Stronger settings can thin your voice, so use the least that works."
                label="Why? Noise reduction"
              />
            </span>
            <div class="seg" role="radiogroup" aria-label="Noise reduction">
              {#each NR_LEVELS as l (l.id)}
                <button
                  class:sel={(S.prefs.micNr || "") === l.id}
                  role="radio"
                  aria-checked={(S.prefs.micNr || "") === l.id}
                  title={l.hint}
                  onclick={() => knob("micNr", l.id, (v) => S.voice?.mesh.setNoiseReduction(v))}
                >
                  {l.label}
                </button>
              {/each}
            </div>
          </div>
        {/if}

        <!-- Push-to-talk already answers "when is my mic open", so the gate is
             absent rather than present-and-ignored. -->
        <div class="gate-row">
          <span class="gate-text">
            <span class="gate-title">Noise gate</span>
            <span class="hint">
              {ptt ? "Not used — push to talk decides this instead" : "Stay silent between sentences, not just quiet"}
            </span>
          </span>
          {#if !ptt}
            <button class="switch" class:on={gateOn} role="switch" aria-checked={gateOn} aria-label="Noise gate" onclick={toggleGate}>
              <span class="sw-knob"></span>
            </button>
          {/if}
        </div>
        {#if gateOn && !ptt}
          <div class="gate-body" transition:slide={{ duration: 180 }}>
            <div class="meter tall" role="presentation">
              <div class="fill" style="width:{Math.round(level * 100)}%"></div>
              <div class="gate-mark" style="left:{Math.min(100, S.prefs.micGate * 400)}%"></div>
            </div>
            <div class="knob">
              <span class="knob-label">
                Opens at
                <InfoDot
                  text="Set the line just above where the bar rests when you're not talking. Too high and it clips the start of quiet words."
                  label="Why? Gate threshold"
                />
              </span>
              <input
                type="range"
                min="0.005"
                max="0.25"
                step="0.005"
                value={S.prefs.micGate}
                oninput={(e) => knob("micGate", +e.target.value, (v) => S.voice?.mesh.setGate(v))}
                aria-label="Noise gate threshold"
              />
              <span class="knob-val">{pct(S.prefs.micGate * 4)}</span>
            </div>
          </div>
        {/if}
      </section>

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
              <span class="toggle-title">
                {p.title}
                {#if lead(p.sub).info}<InfoDot text={lead(p.sub).info} label="Why? {p.title}" />{/if}
              </span>
              <span class="hint">{lead(p.sub).line}</span>
            </span>
            <span class="switch" class:on={S.prefs[name] !== false}><span class="sw-knob"></span></span>
          </button>
        {/each}
      </section>

      <section class="dev">
        <div class="dev-head">
          <span class="chip"><Icon name="screen" size={16} /></span>
          <span class="dev-title">
            Screen-share sound
            <InfoDot
              text="Chromium can share a tab's or window's sound if you tick the box in its picker. When your system won't — the Linux desktop app, or sharing a whole screen — pick an input here and Concord sends that alongside the picture. On Linux the one you want is called “Monitor of” your speakers: that device is literally what's coming out right now."
              label="About screen-share sound"
            />
          </span>
        </div>
        <select
          value={S.prefs.shareAudioId || ""}
          disabled={loading}
          onchange={(e) => knob("shareAudioId", e.target.value, (v) => S.voice?.mesh.setShareAudioDevice(v))}
          aria-label="Screen-share sound source"
        >
          <option value="">Whatever the system shares (nothing extra)</option>
          {#each options("mic") as o (o.id)}
            <option value={o.id}>{o.label}</option>
          {/each}
        </select>
        <!-- Kept in print: someone who shares a screen and is told nothing will
             believe the sound is broken rather than not asked for. -->
        <span class="hint">Only used when the share itself arrives silent.</span>
      </section>

      <section class="dev">
        <div class="dev-head">
          <span class="chip"><Icon name="poll" size={16} /></span>
          <span class="dev-title">
            Quality
            <InfoDot
              text="Browsers negotiate about 32 kbit/s on their own. Higher is clearer and costs upload — on a call you send one copy per person, so a five-person room sends it five times. A screen share's own sound always goes stereo at a higher rate."
              label="About audio quality"
            />
          </span>
        </div>
        <select
          value={S.prefs.voiceBitrate ?? 64000}
          onchange={(e) => knob("voiceBitrate", +e.target.value, (v) => S.voice?.mesh.setBitrate(v))}
          aria-label="Audio quality"
        >
          {#each BITRATES as b (b.bps)}
            <option value={b.bps}>{b.label}</option>
          {/each}
        </select>
      </section>
    </div>
  {/if}
</Modal>

<style>
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
    padding: 4px 14px;
    transition:
      color 0.12s ease,
      border-color 0.12s ease;
  }
  .test:hover:not(:disabled) {
    color: var(--text);
    border-color: var(--accent);
  }
  .test:disabled {
    opacity: 0.45;
  }
  .test.on {
    color: var(--accent-hover);
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
    display: block;
    font-size: 12px;
    line-height: 1.5;
    color: var(--text-muted);
  }
  /* Level meter: accent until it's loud enough that you'd clip, then a warm tip. */
  .meter {
    position: relative;
    height: 8px;
    margin-bottom: 6px;
    border-radius: 999px;
    background: var(--bg-3);
    overflow: hidden;
  }
  .meter.tall {
    height: 12px;
    margin-bottom: 2px;
  }
  .fill {
    height: 100%;
    border-radius: 999px;
    background: linear-gradient(90deg, var(--accent) 0%, var(--accent) 70%, var(--warn) 100%);
    transition: width 0.06s linear;
  }
  /* The gate threshold, drawn on the meter so it can be set by eye. */
  .gate-mark {
    position: absolute;
    top: -2px;
    bottom: -2px;
    width: 2px;
    background: var(--text);
    opacity: 0.7;
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
  /* The label is a fixed column so the sliders line up; the ⓘ has to sit on the
     same line as the word rather than pushing the track around. */
  .knob-label {
    font-size: 12.5px;
    color: var(--text-muted);
    min-width: 76px;
    flex: none;
    white-space: nowrap;
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
  .nr-block {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding-top: 8px;
    border-top: 1px solid var(--border);
  }
  .seg {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 6px;
    margin-top: 2px;
  }
  .seg > button {
    padding: 6px 4px;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    font-size: 12.5px;
  }
  .seg > button:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .seg > button.sel {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--text);
  }
  /* Two choices, not four — the NR strip's grid would leave half of it empty. */
  .seg.two {
    grid-template-columns: repeat(2, 1fr);
  }
  /* Mic activation: the mode picker plus, in push-to-talk, its key. */
  .act {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding-top: 8px;
    border-top: 1px solid var(--border);
  }
  .ptt {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .rec {
    flex: 1;
    padding: 6px 10px;
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-size: 12.5px;
    font-weight: 600;
  }
  .rec:hover {
    border-color: var(--accent);
  }
  .rec-on {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .hint.warn {
    color: var(--warn-text);
  }
  /* The gate: a switch for whether, a slider for where. */
  .gate-row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding-top: 4px;
    border-top: 1px solid var(--border);
  }
  .gate-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin-right: auto;
  }
  .gate-title {
    font-size: 13.5px;
    font-weight: 600;
  }
  .gate-body {
    display: flex;
    flex-direction: column;
    gap: 8px;
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
    padding: 0;
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
  /* Advanced disclosure: quiet until you want it. */
  .disclose {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 10px 12px;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text);
    font-size: 13.5px;
    font-weight: 600;
    text-align: left;
    transition:
      border-color 0.14s ease,
      background 0.14s ease;
  }
  .disclose:hover {
    background: var(--bg-1);
    border-color: var(--accent);
  }
  .disclose-chev {
    color: var(--text-faint);
    font-size: 17px;
    line-height: 1;
    transition: transform 0.22s cubic-bezier(0.34, 1.4, 0.64, 1);
  }
  .disclose-chev.open {
    transform: rotate(90deg);
  }
  .disclose-sub {
    margin-left: auto;
    font-size: 12px;
    font-weight: 400;
    color: var(--text-muted);
  }
  .adv {
    display: flex;
    flex-direction: column;
    gap: 10px;
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
  @media (prefers-reduced-motion: reduce) {
    .switch,
    .sw-knob,
    .fill,
    .disclose-chev {
      transition: none;
    }
  }
</style>
