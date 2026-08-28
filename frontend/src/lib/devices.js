// devices.js: which microphone, speaker and camera this app uses.
//
// Pure media plumbing — no app state. The chosen ids live in prefs (device-
// local, like the theme) and are handed to whoever opens a stream. An empty id
// always means "whatever the OS picked", which is also the out-of-the-box
// state, so an id that no longer resolves (headset unplugged, a pref carried
// over from another machine) degrades to the default instead of failing to
// open the mic at all.

import { loadDenoiser, makeDenoiseNode, nrValue } from "./denoise.js";

// Pref keys for the three choices, so nothing has to spell them twice.
export const PREF = { mic: "micId", speaker: "speakerId", camera: "cameraId" };

// canPickOutput: can this webview route audio to a chosen speaker at all?
// setSinkId is Chromium-only (WebView2 on Windows, Android's WebView, any
// Chromium browser). The Linux/macOS desktop shell runs on WebKit and iOS on
// WKWebView, where the OS default output is the only one we can reach — there
// the UI says so instead of offering a picker that would silently do nothing.
export const canPickOutput =
  typeof HTMLMediaElement !== "undefined" &&
  typeof HTMLMediaElement.prototype.setSinkId === "function";

const hasMedia = () => typeof navigator !== "undefined" && !!navigator.mediaDevices;

// canShareScreen: is there any display capture on this platform at all? Android
// never exposes MediaProjection to the web layer and WKWebView has no display
// capture, so getDisplayMedia is simply absent there — calling it throws a
// TypeError that looks exactly like a cancelled picker. Callers gate the
// share-screen button on this instead of rendering a control that can only ever
// do nothing.
export const canShareScreen =
  typeof navigator !== "undefined" &&
  typeof navigator.mediaDevices?.getDisplayMedia === "function";

const coarse = () => typeof matchMedia === "function" && matchMedia("(pointer: coarse)").matches;

const BUCKET = { audioinput: "mic", audiooutput: "speaker", videoinput: "camera" };

// listDevices returns the current hardware grouped as { mic, speaker, camera },
// each [{ id, label }]. `labelled` is false while the browser is still hiding
// device *names* — it only reveals them once the matching permission has been
// granted at least once (see unlockLabels).
export async function listDevices() {
  const out = { mic: [], speaker: [], camera: [], labelled: true };
  if (!hasMedia() || !navigator.mediaDevices.enumerateDevices) return out;
  let all = [];
  try {
    all = await navigator.mediaDevices.enumerateDevices();
  } catch {
    return out; // permissions policy / no hardware: leave the lists empty
  }
  for (const d of all) {
    const b = BUCKET[d.kind];
    if (!b) continue;
    if (b === "speaker" && !canPickOutput) continue; // can't route there anyway
    out[b].push({ id: d.deviceId, label: d.label });
    if (!d.label) out.labelled = false;
  }
  return out;
}

// unlockLabels asks for permission purely so the browser will reveal device
// names: before any grant, enumerateDevices returns anonymous ids nobody could
// choose between. The stream is stopped the instant we have it.
export async function unlockLabels({ video = false } = {}) {
  if (!hasMedia()) return false;
  try {
    const s = await navigator.mediaDevices.getUserMedia({ audio: true, video });
    s.getTracks().forEach((t) => t.stop());
    return true;
  } catch {
    return false;
  }
}

// The browser's own voice processing. All three default on — that IS the
// browser default, so leaving them alone changes nothing — but each is worth a
// switch: echo cancellation and AGC both quietly degrade music and good
// headset mics, and noise suppression can chew the top off a quiet voice.
export const PROCESSING = {
  echoCancel: {
    key: "echoCancellation",
    title: "Echo cancellation",
    sub: "Stops the other person hearing themselves back through your speakers. Turn off only on headphones — it takes some brightness out of your voice.",
  },
  noiseSuppress: {
    key: "noiseSuppression",
    title: "Noise suppression",
    sub: "Filters fans, keyboards and hiss. Off is cleaner for music or a good mic in a quiet room.",
  },
  autoGain: {
    key: "autoGainControl",
    title: "Automatic gain",
    sub: "Levels you out as you move around. Off gives steadier, more natural dynamics — pair it with the boost above.",
  },
};

// audioConstraints turns our switch names into getUserMedia's. Anything not
// passed stays at the browser default (on).
export function audioConstraints(opts = {}) {
  const c = {};
  for (const [name, p] of Object.entries(PROCESSING)) {
    if (opts[name] !== undefined) c[p.key] = !!opts[name];
  }
  return c;
}

// Baseline constraints per kind, before a device or processing is pinned. The
// camera is asked for far less on a phone: voice.js is a full mesh, so the
// handset encodes and uploads ONE COPY PER PARTICIPANT, and it is doing that to
// fill a ~170px tile on the other end. 720p x3 on a cellular uplink starves the
// voice it was meant to accompany, and cooks the battery doing it.
const baseFor = (kind) =>
  kind === "audio"
    ? {}
    : coarse()
      ? { width: { ideal: 640 }, height: { ideal: 480 }, frameRate: { max: 24 } }
      : { width: 1280, height: 720 };

// micStream / cameraStream open the chosen device, falling back to the system
// default when that exact one is gone. Any other failure (denied, in use, no
// hardware) is real and propagates to the caller.
export const micStream = (deviceId = "", processing = {}) =>
  openStream("audio", deviceId, audioConstraints(processing));
// facingMode ("user" | "environment") is the phone's front/back switch. It is
// deliberately `ideal`, not `exact`: a laptop has one camera and no facing at
// all, and an exact match there fails the whole request.
export const cameraStream = (deviceId = "", facingMode = "") =>
  openStream("video", deviceId, facingMode ? { facingMode: { ideal: facingMode } } : {});

async function openStream(kind, deviceId, extra = {}) {
  const base = { ...baseFor(kind), ...extra };
  const want = { ...base };
  if (deviceId) want.deviceId = { exact: deviceId };
  try {
    return await navigator.mediaDevices.getUserMedia({ [kind]: want });
  } catch (err) {
    // OverconstrainedError = that exact device isn't here anymore.
    if (!deviceId || err?.name !== "OverconstrainedError") throw err;
    return navigator.mediaDevices.getUserMedia({ [kind]: base });
  }
}

// Is this the Linux desktop shell? WebKitGTK with no Chrome token in the UA —
// the one build whose media problems are GStreamer's rather than a permission
// dialog's, and therefore the one build whose advice has to be different.
const LINUX_DESKTOP =
  typeof navigator !== "undefined" &&
  /Linux/.test(navigator.platform || navigator.userAgent || "") &&
  /AppleWebKit/.test(navigator.userAgent || "") &&
  !/Chrome|Chromium/.test(navigator.userAgent || "");

// canCarryACall answers a question that used to have no answer at all: is
// there a peer connection to be had on this machine?
//
// On the Linux desktop build the webview is WebKitGTK and its media stack is
// GStreamer, so WebRTC is only exposed when the system has the elements to
// build a pipeline out of. A box without gst-plugins-bad has no `webrtcbin`,
// `dtls` or `srtp`, and WebKit answers by not defining RTCPeerConnection at
// all — with `enable-webrtc` set and reading back true. Without this check the
// join looked like it worked (presence goes out, the roster shows you) and
// then nobody could hear anybody, forever, with nothing anywhere saying why.
export const canCarryACall = () => typeof RTCPeerConnection !== "undefined";

// Why not, in a sentence somebody can act on. Only ever shown when
// canCarryACall() is false, which on every browser and on Android is never.
export function noCallReason() {
  return LINUX_DESKTOP
    ? "This machine can't make calls: the desktop app carries audio through GStreamer, and the WebRTC plugins are missing. Installing gst-plugins-good and gst-plugins-bad fixes it — everything else in Concord works without them."
    : "This build can't make calls: its web engine has no WebRTC support.";
}

// micReason turns a getUserMedia error NAME into the sentence a person can act
// on. It exists because every failure used to arrive as "Microphone access
// denied", which is true in one case out of four and actively misleading in the
// other three — a Linux desktop with no capture pipeline, a headset another app
// is holding, and a machine with no microphone at all each need a different
// next move, and none of them is "check your permissions".
//
// The Linux desktop line is specific on purpose. WebKitGTK builds its capture
// through GStreamer, so a box missing gst-plugins-good has the hardware, the
// permission and no way to open it; that presents as NotFoundError or
// NotReadableError and is otherwise unguessable.
export function micReason(name) {
  switch (name) {
    case "NotAllowedError":
    case "SecurityError":
      return "Microphone blocked — you're in listen-only mode. Allow the microphone and try again.";
    case "NotFoundError":
    case "OverconstrainedError":
      return LINUX_DESKTOP
        ? "No microphone found — you're in listen-only mode. On Linux the desktop app captures through GStreamer: install gst-plugins-good if your mic works elsewhere."
        : "No microphone found — you're in listen-only mode.";
    case "NotReadableError":
    case "AbortError":
      return LINUX_DESKTOP
        ? "Your microphone couldn't be opened — you're in listen-only mode. Another app may be holding it, or the desktop app's GStreamer plugins may be missing."
        : "Your microphone couldn't be opened — you're in listen-only mode. Another app may be using it.";
    case "TypeError":
      return "This build can't reach a microphone — you're in listen-only mode.";
    default:
      return name
        ? `Microphone unavailable (${name}) — you're in listen-only mode.`
        : "Microphone unavailable — you're in listen-only mode.";
  }
}

// applySink routes one media element to the chosen output. Best effort: an id
// that's gone, or a webview without setSinkId, just keeps playing on the
// default device rather than going silent.
export async function applySink(el, deviceId) {
  if (!el || !canPickOutput) return false;
  try {
    await el.setSinkId(deviceId || "");
    return true;
  } catch {
    return false;
  }
}

// ---- call audio routing (phones) ----
//
// On a handset the useful question is never "which of these five output devices"
// — it is earpiece / loudspeaker / the headset that just connected, the switch
// every native calling app puts one tap away. The web platform cannot answer it:
// setSinkId is absent in Android's WebView and WKWebView (canPickOutput is false
// there), enumerateDevices reports no audiooutput at all, and the route is owned
// by Android's AudioManager and iOS's AVAudioSession. There is no polyfill, only
// a native call.
//
// So this is the web half of that native call, reached through the runtime
// global exactly like lib/touch.js reaches Haptics — the web and desktop bundles
// carry no Capacitor dependency, and every function here no-ops when the plugin
// is absent. The plugin contract is small on purpose:
//
//   CallAudio.setRoute({ route: "earpiece"|"speaker"|"bluetooth" }) -> {route}
//   CallAudio.getRoute() -> { route, available: string[] }
//
// backed by setMode(MODE_IN_COMMUNICATION) + setSpeakerphoneOn / setCommunication-
// Device on Android and overrideOutputAudioPort / setCategory(.playAndRecord,
// .allowBluetooth) on iOS. Until that plugin ships, canRouteAudio() is false
// everywhere and the UI hides the control rather than offering a switch that
// silently does nothing — which is the state this replaces.
export const AUDIO_ROUTES = [
  { id: "earpiece", label: "Phone", icon: "phone" },
  { id: "speaker", label: "Speaker", icon: "speaker" },
  { id: "bluetooth", label: "Bluetooth", icon: "devices" },
];

const routePlugin = () => (typeof window !== "undefined" ? window.Capacitor?.Plugins?.CallAudio : null);

export const canRouteAudio = () => !!routePlugin();

// currentRoute reports the live route and which ones exist right now (there is
// no Bluetooth entry when nothing is paired). Returns null when unsupported, so
// callers can tell "not available" from "on the earpiece".
export async function currentRoute() {
  const p = routePlugin();
  if (!p?.getRoute) return null;
  try {
    const r = await p.getRoute();
    return { route: r?.route || "earpiece", available: r?.available?.length ? r.available : ["earpiece", "speaker"] };
  } catch {
    return null;
  }
}

// setAudioRoute moves the call. Returns the route actually in force — the OS can
// refuse (a wired headset overrides everything), and the UI should show what
// happened rather than what was asked for.
export async function setAudioRoute(route) {
  const p = routePlugin();
  if (!p?.setRoute) return null;
  try {
    const r = await p.setRoute({ route });
    return r?.route || route;
  } catch {
    return null;
  }
}

// onDeviceChange fires when hardware is plugged in or pulled out, so an open
// picker can re-enumerate. Returns an unsubscribe.
export function onDeviceChange(fn) {
  if (!hasMedia() || !navigator.mediaDevices.addEventListener) return () => {};
  navigator.mediaDevices.addEventListener("devicechange", fn);
  return () => navigator.mediaDevices.removeEventListener("devicechange", fn);
}

// ---- test tone ----
//
// A short chime rendered to a WAV blob and played through an <audio> element,
// not the shared WebAudio context in sounds.js — setSinkId lives on media
// elements, and hearing the beep come out of the right speaker is the entire
// point of the output picker.

let toneUrl = "";
function toneWav() {
  if (toneUrl) return toneUrl;
  const rate = 44100;
  const n = Math.floor(rate * 0.4);
  const buf = new ArrayBuffer(44 + n * 2);
  const view = new DataView(buf);
  const str = (off, s) => {
    for (let i = 0; i < s.length; i++) view.setUint8(off + i, s.charCodeAt(i));
  };
  str(0, "RIFF");
  view.setUint32(4, 36 + n * 2, true);
  str(8, "WAVEfmt ");
  view.setUint32(16, 16, true); // fmt chunk size
  view.setUint16(20, 1, true); // PCM
  view.setUint16(22, 1, true); // mono
  view.setUint32(24, rate, true);
  view.setUint32(28, rate * 2, true); // byte rate
  view.setUint16(32, 2, true); // block align
  view.setUint16(34, 16, true); // bits per sample
  str(36, "data");
  view.setUint32(40, n * 2, true);
  for (let i = 0; i < n; i++) {
    const t = i / rate;
    // A soft 700 Hz bell: quick attack, exponential decay, no click at either end.
    const env = Math.min(1, t * 60) * Math.exp(-5 * t);
    const s = Math.sin(2 * Math.PI * 700 * t) + 0.3 * Math.sin(2 * Math.PI * 1400 * t);
    view.setInt16(44 + i * 2, Math.round(Math.max(-1, Math.min(1, s * 0.7)) * env * 9000), true);
  }
  toneUrl = URL.createObjectURL(new Blob([buf], { type: "audio/wav" }));
  return toneUrl;
}

// ---- hear yourself ----
//
// Record a few seconds of your own mic and play it straight back, through the
// same boost and gate the call would apply and out of the speaker you picked.
// Recorded-then-played rather than monitored live on purpose: live monitoring
// through speakers is a feedback loop, and even on headphones the delay makes
// people talk strangely. What you want to know is "what do I sound like to
// them", and that question is answered by a playback.
//
// Returns a controller: stop() ends the recording early, and the promise
// resolves when playback finishes (or is cancelled).
export function recordSelfTest({
  deviceId = "",
  processing = {},
  gain = 1,
  gate = 0,
  nr = "",
  sinkId = "",
  seconds = 5,
} = {}) {
  let onLevel = () => {};
  let cancelled = false;
  let stopRec = () => {};

  const done = (async () => {
    const raw = await micStream(deviceId, processing);
    const ctx = new (window.AudioContext || window.webkitAudioContext)();
    if (ctx.state === "suspended") await ctx.resume().catch(() => {});
    if (nr) await loadDenoiser(ctx);
    const src = ctx.createMediaStreamSource(raw);
    // The same chain a call would use, in the same order — the playback is only
    // worth anything if it's processed exactly like the real thing.
    const hp = ctx.createBiquadFilter();
    hp.type = "highpass";
    hp.frequency.value = 85;
    hp.Q.value = 0.7;
    const denoise = nr ? makeDenoiseNode(ctx, nrValue(nr)) : null;
    const gainNode = ctx.createGain();
    gainNode.gain.value = gain;
    const gateNode = ctx.createGain();
    gateNode.gain.value = gate ? 0 : 1;
    const dest = ctx.createMediaStreamDestination();
    const analyser = ctx.createAnalyser();
    analyser.fftSize = 512;
    src.connect(analyser);
    let node = src.connect(hp);
    if (denoise) node = node.connect(denoise);
    node.connect(gainNode).connect(gateNode).connect(dest);

    // Same gate behaviour as a live call: measured pre-gate and scaled by the
    // boost, fast to open, slow to close, so the playback is honest about what
    // the gate is doing to your first syllable.
    const data = new Uint8Array(analyser.frequencyBinCount);
    let openUntil = 0;
    const meter = setInterval(() => {
      analyser.getByteTimeDomainData(data);
      let sum = 0;
      for (let i = 0; i < data.length; i++) {
        const v = (data[i] - 128) / 128;
        sum += v * v;
      }
      const rms = Math.sqrt(sum / data.length);
      onLevel(Math.min(1, rms * gain * 4));
      if (!gate) return;
      if (rms * gain >= gate) openUntil = Date.now() + 500;
      const open = Date.now() < openUntil;
      gateNode.gain.setTargetAtTime(open ? 1 : 0, ctx.currentTime, open ? 0.01 : 0.15);
    }, 60);

    const chunks = [];
    const rec = new MediaRecorder(dest.stream);
    rec.ondataavailable = (e) => e.data.size && chunks.push(e.data);
    const recorded = new Promise((r) => (rec.onstop = r));
    rec.start();
    stopRec = () => rec.state !== "inactive" && rec.stop();
    const timer = setTimeout(stopRec, seconds * 1000);

    await recorded;
    clearTimeout(timer);
    clearInterval(meter);
    onLevel(0);
    raw.getTracks().forEach((t) => t.stop());
    ctx.close().catch(() => {});
    if (cancelled || !chunks.length) return false;

    const el = new Audio(URL.createObjectURL(new Blob(chunks, { type: chunks[0].type || "audio/webm" })));
    await applySink(el, sinkId);
    const played = new Promise((r) => {
      el.onended = r;
      el.onerror = r;
    });
    try {
      await el.play();
    } catch {
      return false;
    }
    await played;
    URL.revokeObjectURL(el.src);
    return !cancelled;
  })();

  return {
    done,
    stop: () => stopRec(),
    cancel: () => {
      cancelled = true;
      stopRec();
    },
    onLevel: (fn) => (onLevel = fn || (() => {})),
  };
}

// testTone plays the chime through one specific output device.
export async function testTone(deviceId = "") {
  const el = new Audio(toneWav());
  await applySink(el, deviceId);
  try {
    await el.play();
  } catch {
    /* no gesture / output busy: nothing to recover */
  }
}
