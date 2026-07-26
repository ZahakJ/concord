// devices.js: which microphone, speaker and camera this app uses.
//
// Pure media plumbing — no app state. The chosen ids live in prefs (device-
// local, like the theme) and are handed to whoever opens a stream. An empty id
// always means "whatever the OS picked", which is also the out-of-the-box
// state, so an id that no longer resolves (headset unplugged, a pref carried
// over from another machine) degrades to the default instead of failing to
// open the mic at all.

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
    sub: "Levels you out as you move around. Off gives steadier, more natural dynamics — pair it with the boost below.",
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

// Baseline constraints per kind, before a device or processing is pinned.
const BASE = { audio: {}, video: { width: 1280, height: 720 } };

// micStream / cameraStream open the chosen device, falling back to the system
// default when that exact one is gone. Any other failure (denied, in use, no
// hardware) is real and propagates to the caller.
export const micStream = (deviceId = "", processing = {}) =>
  openStream("audio", deviceId, audioConstraints(processing));
export const cameraStream = (deviceId = "") => openStream("video", deviceId);

async function openStream(kind, deviceId, extra = {}) {
  const base = { ...BASE[kind], ...extra };
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
