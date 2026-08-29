// sounds.js — tiny synthesized UI chimes (no audio files). One shared
// AudioContext, created lazily on first use and primed by the session's first
// gesture; withAudio() below waits for it to actually start rather than
// scheduling into a stopped clock. Muteable, persisted in localStorage. The
// module also reports whether this machine can play a sound at all — see
// audioTrouble(), which exists because on the Linux desktop it sometimes
// cannot, and says so nowhere.

import { decodeRecipe, SFX_WAVES } from "./sfxrecipe.js";

let ctx = null;
let enabled = load();
let volume = loadVolume();

function load() {
  try {
    return localStorage.getItem("concord.sounds") !== "off";
  } catch {
    return true;
  }
}

// ---- how loud ----
//
// The settings section is headed "HOW LOUD" and, until this existed, contained
// two on/off toggles and a ringtone picker — no volume control at all, under a
// header that promises one. One master gain sits between every sound in this
// file and the speakers, so the answer is one node rather than a peak
// multiplier threaded through nine synthesizers. 1 is what the chimes have
// always been tuned at, so an existing install sounds identical.
function loadVolume() {
  try {
    const v = Number(localStorage.getItem("concord.soundVolume"));
    return Number.isFinite(v) && v >= 0 && v <= 1 ? v : 1;
  } catch {
    return 1;
  }
}

export function soundVolume() {
  return volume;
}

export function setSoundVolume(v) {
  volume = Math.max(0, Math.min(1, Number(v) || 0));
  if (masterNode && masterCtx === ctx) masterNode.gain.value = volume;
  try {
    localStorage.setItem("concord.soundVolume", String(volume));
  } catch {
    /* ignore */
  }
}

// The master gain, rebuilt with the context. audio() replaces the context on an
// audio-route change, and a node from a dead context is not a node.
let masterNode = null;
let masterCtx = null;
function master(ac) {
  if (masterCtx !== ac || !masterNode) {
    masterNode = ac.createGain();
    masterNode.gain.value = volume;
    masterNode.connect(ac.destination);
    masterCtx = ac;
  }
  return masterNode;
}

export function soundsEnabled() {
  return enabled;
}

export function setSoundsEnabled(on) {
  enabled = on;
  try {
    localStorage.setItem("concord.sounds", on ? "on" : "off");
  } catch {
    /* ignore */
  }
}

// Android flips the system audio route around a WebRTC call (media rate in and
// out of the communication rate — often 48kHz ⇄ 8/16kHz). A WebAudio context
// created on one side of that flip is unreliable on the other: it can report
// "running" while rendering silence, or render against the old hardware rate
// and come out pitched/garbled. So the shared context is REBUILT, not resumed,
// whenever it may straddle a route change:
//   • voice.js calls noteAudioRouteChange() as a call's audio starts and stops
//     (the two moments Android moves the route under us);
//   • a context found "closed" (Android reclaims them under memory/focus
//     pressure) is replaced rather than returned;
//   • after each use a watchdog notices a context that claims to be running
//     but whose clock isn't advancing, and flags it so the NEXT sound gets a
//     fresh context — self-healing for route changes nobody announced (e.g. a
//     native phone call).
// A fresh context is created against the CURRENT route, so its sample rate
// matches the hardware and the impulse cache (keyed by rate) re-bakes for it.
// At the normal 48kHz the rendered chimes are identical to what this file has
// always produced — nothing about the synthesis changes.
let routeDirty = false;

export function noteAudioRouteChange() {
  routeDirty = true;
}

function audio() {
  const AC = window.AudioContext || window.webkitAudioContext;
  if (!AC) {
    health = "unsupported";
    return null;
  }
  if (ctx && (routeDirty || ctx.state === "closed")) {
    try {
      ctx.close().catch(() => {});
    } catch {
      /* already closed */
    }
    ctx = null;
  }
  routeDirty = false;
  if (!ctx) ctx = new AC();
  if (ctx.state === "suspended") ctx.resume().catch(() => {});
  watchdog(ctx);
  return ctx;
}

// withAudio hands a running context to fn — now if there is one, and otherwise
// as soon as the browser lets the context start.
//
// Every synthesizer in this file schedules against ac.currentTime, and on a
// SUSPENDED context that clock is frozen at whatever it stopped at. The old
// code fired resume() and then scheduled immediately anyway, so the first
// sound of a session — the one played by the very click that unblocked audio —
// was written into the past and came out as nothing. That is invisible on a
// desktop browser that never suspends and reliable on one that does, which is
// exactly the shape of "sometimes there is no sound and nobody can say why".
//
// Late is fine. These are all under a second long and none of them is timed
// against anything; a chime that arrives 30ms after the resume promise settles
// is a chime, and a chime scheduled into a stopped clock is silence.
function withAudio(fn) {
  const ac = audio();
  if (!ac) return false;
  if (ac.state === "running") {
    fn(ac);
    return true;
  }
  ac.resume().then(
    () => {
      // The context may have been replaced while the promise was in flight
      // (a route change, a close); a node built on a dead context is not a node.
      if (ctx === ac && ac.state === "running") fn(ac);
    },
    () => {},
  );
  return true;
}

// ---- can this machine actually make a sound? --------------------------------
//
// On the Linux desktop the answer is not always yes, and the failure is silent
// in the worst way: WebKitGTK renders WebAudio through GStreamer, so a box
// without the audio sink plugins hands back an AudioContext that constructs,
// reports "running", accepts every node — and never produces a sample. Nothing
// throws. Nothing is logged. The app looks like it has been muted by someone
// else, which is precisely the report that arrived: no chime on joining a call,
// no easter egg on the logo, no send tick, and a Sounds switch that was on.
//
// The one observable the platform cannot fake is the clock. A running context
// advances currentTime in real time because it is being pulled by the audio
// device; a context with no device behind it sits at the same number forever.
// That is what watchdog() below has always measured — it was used only to
// rebuild a context orphaned by an Android route change. Here the same reading
// is promoted to a fact worth telling somebody, because after two rebuilds
// there is nothing left to blame on a route change.
//
//   "unknown"     nothing has been played yet
//   "ok"          a context ran and its clock moved
//   "silent"      a context claimed to run and its clock never moved
//   "unsupported" there is no AudioContext at all
let health = "unknown";
let stalls = 0;

export function audioHealth() {
  return health;
}

// A sentence somebody can act on, or null when there is nothing to say. Same
// voice as devices.js's noCallReason(), and for the same reason: naming the two
// packages is the entire difference between a bug report and a fix.
export function audioTrouble() {
  if (health === "unsupported") return "This build has no Web Audio support, so Concord can't make any sounds.";
  if (health !== "silent") return null;
  return LINUX_DESKTOP
    ? "This machine can't play Concord's sounds: the desktop app renders them through GStreamer, and the audio plugins are missing. Installing gst-plugins-good and gst-plugins-bad fixes it — everything else in Concord works without them."
    : "Concord's sounds aren't reaching your speakers: the audio device accepted them and never played them. Check that the app isn't muted in your system mixer.";
}

// The same UA test devices.js uses. Duplicated rather than imported because
// this module is loaded by the login screen, and devices.js pulls in the whole
// media-device stack behind it.
const LINUX_DESKTOP =
  typeof navigator !== "undefined" &&
  /Linux/.test(navigator.userAgent || "") &&
  /AppleWebKit/.test(navigator.userAgent || "") &&
  !/Chrome|Chromium/.test(navigator.userAgent || "");

// probeAudioOutput plays nothing and asks the only question that matters: does
// this context's clock move? Resolves with the health string. Cheap enough to
// run whenever the sound settings are opened, and it is the only way that panel
// can tell the truth rather than showing a switch that does nothing.
export function probeAudioOutput() {
  const ac = audio();
  if (!ac) return Promise.resolve(health);
  const settle = () =>
    new Promise((res) => {
      const t0 = ac.currentTime;
      setTimeout(() => {
        if (ctx !== ac) return res(health); // replaced under us; no verdict
        if (ac.state !== "running") return res(health); // still blocked, not broken
        if (ac.currentTime > t0) {
          health = "ok";
          stalls = 0;
        } else {
          health = "silent";
        }
        res(health);
      }, 320);
    });
  if (ac.state === "running") return settle();
  return ac.resume().then(settle, () => health);
}

// watchdog flags a context that reports "running" while its clock is stuck.
// One reading is an audio-route change (Android moves the route under us and
// orphans the context): flag it, and the NEXT sound gets a fresh one. Two
// readings in a row means rebuilding is not the answer — the device itself is
// not there — so stop churning contexts and record the verdict instead.
function watchdog(ac) {
  const t0 = ac.currentTime;
  setTimeout(() => {
    if (ctx !== ac || ac.state !== "running") return;
    if (ac.currentTime > t0) {
      health = "ok";
      stalls = 0;
      return;
    }
    stalls++;
    if (stalls >= 2) health = "silent";
    else routeDirty = true;
  }, 250);
}

// Browsers will not start an audio context until the page has been interacted
// with, and the FIRST sound is usually played by that very interaction — an
// ordering the code above now survives, but the cheaper fix is to have the
// context already running by then. One listener, on the first gesture of the
// session, in the capture phase so a handler that stops propagation cannot
// swallow it. Costs one suspended context on a page nobody clicks.
if (typeof window !== "undefined" && typeof window.addEventListener === "function") {
  const EVENTS = ["pointerdown", "keydown", "touchstart"];
  const prime = () => {
    for (const e of EVENTS) window.removeEventListener(e, prime, true);
    try {
      audio();
    } catch {
      /* a context this early is a bonus, never a requirement */
    }
  };
  for (const e of EVENTS) window.addEventListener(e, prime, { capture: true, passive: true });
}

// tone plays a short shaped oscillator at freq (Hz) starting at offset seconds.
function tone(ac, freq, start, dur, peak = 0.14, wave = "sine") {
  const osc = ac.createOscillator();
  const gain = ac.createGain();
  osc.type = wave;
  osc.frequency.value = freq;
  const t0 = ac.currentTime + start;
  gain.gain.setValueAtTime(0, t0);
  gain.gain.linearRampToValueAtTime(peak, t0 + 0.015);
  gain.gain.exponentialRampToValueAtTime(0.0001, t0 + dur);
  osc.connect(gain).connect(master(ac));
  osc.start(t0);
  osc.stop(t0 + dur + 0.02);
}

// `force` bypasses the mute check (for previewing a ringtone in settings).
function play(notes, wave = "sine", force = false) {
  if (!enabled && !force) return;
  withAudio((ac) => {
    for (const [freq, start, dur, peak] of notes) tone(ac, freq, start, dur, peak, wave);
  });
}

// ---- the voice room's join/leave sounds ----
//
// These two get a synthesizer of their own rather than using tone() above,
// because a bare oscillator pair reads as a beep from a microwave. Three things
// separate "chime" from something with weight, and all three are cheap:
//
//   • a room. Reverb is most of what "ambient" means — the tail is the sound
//     continuing to exist after the note stops. Built here, not loaded: a
//     ConvolverNode fed a procedurally generated impulse response.
//   • a body. One sine at one frequency is a test tone. Two detuned a few cents
//     apart beat against each other (warmth), and an octave below adds the
//     weight you feel rather than hear.
//   • a landing. A short pitch-dropping thump under the first note, so the
//     sound arrives instead of merely beginning.

// Impulse responses are expensive to generate (a few hundred thousand randoms)
// and identical every time, so they're built once per shape and reused.
const irCache = new Map();
function impulse(ac, seconds, decay) {
  const key = `${seconds}:${decay}:${ac.sampleRate}`;
  const hit = irCache.get(key);
  if (hit) return hit;
  const len = Math.ceil(ac.sampleRate * seconds);
  const buf = ac.createBuffer(2, len, ac.sampleRate);
  for (let ch = 0; ch < 2; ch++) {
    const d = buf.getChannelData(ch);
    // Each channel gets its own noise. Decorrelating them is what makes the
    // tail sound wide rather than like a mono echo pinned between your ears.
    for (let i = 0; i < len; i++) d[i] = (Math.random() * 2 - 1) * Math.pow(1 - i / len, decay);
  }
  irCache.set(key, buf);
  return buf;
}

// A dry/wet bus into a limiter. The limiter isn't for loudness — layering a
// sub, two detuned oscillators and a transient can sum past 1.0 on the loudest
// note, and clipping in WebAudio is an ugly digital crunch rather than warmth.
function roomBus(ac, { seconds, decay, wet, damp, level = 1 }) {
  const inp = ac.createGain();
  const comp = ac.createDynamicsCompressor();
  comp.threshold.value = -16;
  comp.knee.value = 14;
  comp.ratio.value = 6;
  comp.attack.value = 0.004;
  comp.release.value = 0.2;
  // One master trim after the limiter, so the mix of the parts below can be
  // tuned by ear-shape (how much thump vs note) without also changing how loud
  // the whole thing is. These sit at roughly twice the old chime's level: they
  // should feel more present, but a voice channel you join often must not
  // startle you, and much of the added weight is sub-bass a laptop speaker
  // won't reproduce anyway.
  const out = ac.createGain();
  out.gain.value = level;
  comp.connect(out).connect(master(ac));
  inp.connect(comp); // dry
  const send = ac.createGain();
  send.gain.value = wet;
  const conv = ac.createConvolver();
  conv.buffer = impulse(ac, seconds, decay);
  // Roll the top off the tail. An undamped reverb on a short chime reads as a
  // cheap spring plate; darkening it reads as a room.
  const lp = ac.createBiquadFilter();
  lp.type = "lowpass";
  lp.frequency.value = damp;
  inp.connect(send).connect(conv).connect(lp).connect(comp);
  return inp;
}

// One note, but thick — see "a body" above.
function note(ac, sink, freq, start, dur, peak) {
  const t0 = ac.currentTime + start;
  const g = ac.createGain();
  g.gain.setValueAtTime(0, t0);
  // 25ms rather than 15: enough to read as a swell instead of a click, still
  // far too fast to feel slow.
  g.gain.linearRampToValueAtTime(peak, t0 + 0.025);
  g.gain.exponentialRampToValueAtTime(0.0001, t0 + dur);
  g.connect(sink);
  // [frequency multiplier, detune cents, level, waveform]
  for (const [mult, cents, level, wave] of [
    [1, -7, 0.55, "sine"],
    [1, 7, 0.55, "sine"],
    [0.5, 0, 0.45, "sine"], // sub-octave: the weight
    [2, 0, 0.09, "triangle"], // one upper harmonic, so it still cuts through
  ]) {
    const osc = ac.createOscillator();
    osc.type = wave;
    osc.frequency.value = freq * mult;
    osc.detune.value = cents;
    const lg = ac.createGain();
    lg.gain.value = level;
    osc.connect(lg).connect(g);
    osc.start(t0);
    osc.stop(t0 + dur + 0.05);
  }
}

// The landing: a fast downward pitch sweep, lowpassed so it thumps rather than
// clicks. This is the single biggest contributor to "heavy".
function thump(ac, sink, start, from, to, peak) {
  const t0 = ac.currentTime + start;
  const osc = ac.createOscillator();
  osc.frequency.setValueAtTime(from, t0);
  osc.frequency.exponentialRampToValueAtTime(to, t0 + 0.18);
  const lp = ac.createBiquadFilter();
  lp.type = "lowpass";
  lp.frequency.value = 320;
  const g = ac.createGain();
  g.gain.setValueAtTime(0, t0);
  g.gain.linearRampToValueAtTime(peak, t0 + 0.01);
  g.gain.exponentialRampToValueAtTime(0.0001, t0 + 0.34);
  osc.connect(lp).connect(g).connect(sink);
  osc.start(t0);
  osc.stop(t0 + 0.36);
}

// Someone joined: a rising fifth, C4 up to G4. The old pair sat an octave
// higher, which is exactly why it sounded thin — up there a sine has nothing
// underneath it.
function buildJoin(ac) {
  const bus = roomBus(ac, { seconds: 1.7, decay: 2.4, wet: 0.32, damp: 2800, level: 0.62 });
  // The thump sits UNDER the notes, not over them. At the level this started
  // out (3x the notes) it read as a boom with a chime somewhere behind it —
  // measurably, two thirds of the sound's energy was below 200 Hz.
  thump(ac, bus, 0, 150, 52, 0.19);
  note(ac, bus, 261.63, 0.0, 0.75, 0.15);
  note(ac, bus, 392.0, 0.085, 1.0, 0.17);
}

export function playVoiceJoin() {
  if (!enabled) return;
  if (playChimeBlob("join")) return;
  withAudio(buildJoin);
}

// Someone left: the same interval falling, darker and with a longer, quieter
// room — a door closing down the hall rather than one slammed next to you.
function buildLeave(ac) {
  const bus = roomBus(ac, { seconds: 2.0, decay: 2.9, wet: 0.34, damp: 1900, level: 0.6 });
  thump(ac, bus, 0, 120, 44, 0.13);
  note(ac, bus, 392.0, 0.0, 0.7, 0.15);
  note(ac, bus, 261.63, 0.09, 1.15, 0.16);
}

export function playVoiceLeave() {
  if (!enabled) return;
  if (playChimeBlob("leave")) return;
  withAudio(buildLeave);
}

// ---- pre-rendered chimes for the phone --------------------------------------
// Web Audio through Android's call-route flips has now failed twice: rebuilding
// the context around every transition still left chimes "clunky, timed off, or
// not heard sometimes" on the actual phone. So on Capacitor the join/leave
// chimes stop being live synthesis at all: each is rendered ONCE offline (same
// builders, same 48kHz, byte-identical sound) into a WAV blob, and played
// through an <audio> element — Android's ordinary media pipeline, which survives
// route changes as a matter of course because every media app depends on it.
// Desktop keeps the live path; it has never misbehaved there.
const chimeBlobs = { join: null, leave: null };
let chimePriming = null;

function wavFromBuffer(buf) {
  const ch = buf.numberOfChannels, len = buf.length, rate = buf.sampleRate;
  const bytes = 44 + len * ch * 2;
  const out = new DataView(new ArrayBuffer(bytes));
  const str = (o, s2) => { for (let i = 0; i < s2.length; i++) out.setUint8(o + i, s2.charCodeAt(i)); };
  str(0, "RIFF"); out.setUint32(4, bytes - 8, true); str(8, "WAVE");
  str(12, "fmt "); out.setUint32(16, 16, true); out.setUint16(20, 1, true);
  out.setUint16(22, ch, true); out.setUint32(24, rate, true);
  out.setUint32(28, rate * ch * 2, true); out.setUint16(32, ch * 2, true);
  out.setUint16(34, 16, true); str(36, "data"); out.setUint32(40, len * ch * 2, true);
  let o = 44;
  const chans = []; for (let c = 0; c < ch; c++) chans.push(buf.getChannelData(c));
  for (let i = 0; i < len; i++)
    for (let c = 0; c < ch; c++) {
      const v = Math.max(-1, Math.min(1, chans[c][i]));
      out.setInt16(o, v < 0 ? v * 0x8000 : v * 0x7fff, true);
      o += 2;
    }
  return new Blob([out.buffer], { type: "audio/wav" });
}

async function renderChime(builder, seconds) {
  const oc = new OfflineAudioContext(2, Math.ceil(seconds * 48000), 48000);
  builder(oc);
  const rendered = await oc.startRendering();
  return URL.createObjectURL(wavFromBuffer(rendered));
}

// primeChimes renders both blobs; kicked at module init on Capacitor so they
// are ready long before the first call.
function primeChimes() {
  if (chimePriming) return chimePriming;
  chimePriming = Promise.all([
    renderChime(buildJoin, 4.2).then((u) => (chimeBlobs.join = u)),
    renderChime(buildLeave, 4.8).then((u) => (chimeBlobs.leave = u)),
  ]).catch(() => (chimePriming = null));
  return chimePriming;
}
if (typeof window !== "undefined" && window.Capacitor) {
  // Offline rendering needs no user gesture; do it while the app boots.
  (window.requestIdleCallback || setTimeout)(() => primeChimes());
}

// playChimeBlob plays the pre-rendered chime on Capacitor. Returns true when it
// owned the playback; false hands the caller back to the live path (desktop,
// or a phone whose render has not landed yet — better a live chime than none).
function playChimeBlob(which) {
  if (typeof window === "undefined" || !window.Capacitor) return false;
  const url = chimeBlobs[which];
  if (!url) {
    primeChimes();
    return false;
  }
  try {
    const el = new Audio(url);
    el.volume = 1;
    el.play().catch(() => {});
    return true;
  } catch {
    return false;
  }
}

// Soft single ping for an @mention / notification.
export function playMention() {
  play([[880, 0, 0.16, 0.1]]);
}

// Warm two-note bloop for an incoming direct message — distinct from the
// single @mention ping so a DM is recognizable without looking.
export function playDM() {
  play([
    [587.33, 0, 0.14, 0.11],
    [880, 0.11, 0.2, 0.11],
  ]);
}

// Ringtones — one "brring" each; the ring loop (App.svelte) repeats it while a
// call is incoming. Pick one in Settings → Sounds.
const RINGTONES = {
  classic: {
    label: "Classic",
    wave: "sine",
    notes: [
      [480, 0, 0.28, 0.13],
      [620, 0, 0.28, 0.13],
      [480, 0.4, 0.28, 0.13],
      [620, 0.4, 0.28, 0.13],
    ],
  },
  digital: {
    label: "Digital",
    wave: "square",
    notes: [
      [880, 0, 0.08, 0.07],
      [880, 0.13, 0.08, 0.07],
      [1100, 0.27, 0.12, 0.07],
    ],
  },
  chime: {
    label: "Chime",
    wave: "sine",
    notes: [
      [523.25, 0, 0.3, 0.11],
      [659.25, 0.13, 0.3, 0.11],
      [783.99, 0.26, 0.46, 0.11],
    ],
  },
  marimba: {
    label: "Marimba",
    wave: "triangle",
    notes: [
      [392, 0, 0.16, 0.13],
      [523.25, 0.13, 0.16, 0.13],
      [392, 0.3, 0.22, 0.13],
    ],
  },
  pulse: {
    label: "Pulse",
    wave: "sine",
    notes: [
      [300, 0, 0.45, 0.14],
      [300, 0.62, 0.45, 0.14],
    ],
  },
};

export const RINGTONE_OPTIONS = Object.entries(RINGTONES).map(([id, r]) => ({ id, label: r.label }));

let ringtone = loadRingtone();
function loadRingtone() {
  try {
    const v = localStorage.getItem("concord.ringtone");
    return v && RINGTONES[v] ? v : "classic";
  } catch {
    return "classic";
  }
}
export function getRingtone() {
  return ringtone;
}
export function setRingtone(id) {
  if (!RINGTONES[id]) return;
  ringtone = id;
  try {
    localStorage.setItem("concord.ringtone", id);
  } catch {
    /* ignore */
  }
}

export function playRing() {
  const r = RINGTONES[ringtone] || RINGTONES.classic;
  play(r.notes, r.wave);
}

// Preview a ringtone (plays even while sounds are muted, so you can audition).
export function previewRingtone(id) {
  const r = RINGTONES[id] || RINGTONES.classic;
  play(r.notes, r.wave, true);
}

// A one-shot burst of white noise. Oscillators can't make air; anything that
// should read as wind, thrust or a crack has to start from noise.
function noiseSource(ac, dur) {
  const buf = ac.createBuffer(1, Math.ceil(ac.sampleRate * dur), ac.sampleRate);
  const d = buf.getChannelData(0);
  for (let i = 0; i < d.length; i++) d[i] = Math.random() * 2 - 1;
  const src = ac.createBufferSource();
  src.buffer = buf;
  return src;
}

// The send tick: two tiny ascending notes riding the same mute switch as
// everything else. Quiet on purpose — sending happens hundreds of times a day,
// so this has to disappear into muscle memory as confirmation, not fanfare.
export function playSend() {
  play(
    [
      [1320, 0, 0.05, 0.045],
      [1760, 0.045, 0.07, 0.04],
    ],
    "triangle",
  );
}

// ---- the three confirmations ----
//
// The synthesis in this file is genuinely good and, for a long time, it was
// used for seven events: send, mention, DM, voice in, voice out, ring,
// soundboard. Nothing at all confirmed a reaction, an error or a save — the
// "Microphone access denied" toast was silent — so the app's voice went quiet
// at exactly the moments a person is asking "did that work?".
//
// All three are built from the primitives already here, and all three are
// deliberately SMALLER than the chimes above: these fire often, and a
// confirmation that demands attention becomes something you turn off.

// A tick. Thirty milliseconds of nothing but an edge — the sound a switch
// makes, not the sound a notification makes. For a reaction landing and for
// anything else that toggles.
export function playTick() {
  play([[2100, 0, 0.03, 0.03]], "triangle");
}

// Two notes down a minor third. Descending is the whole grammar of "no" in
// every interface that has ever had one; keeping it soft is what stops it
// being a scold.
export function playNope() {
  play(
    [
      [392, 0, 0.1, 0.06],
      [311.13, 0.09, 0.16, 0.055],
    ],
    "triangle",
  );
}

// Two notes up a fourth, the second a touch quieter so it settles rather than
// announcing. The counterpart to the nope, and the reason they are a pair.
export function playDone() {
  play(
    [
      [659.25, 0, 0.08, 0.05],
      [880, 0.07, 0.16, 0.04],
    ],
  );
}

// ---- the call's own switches ----
//
// Mute is the one control in this app you press without looking at it — the
// hand goes to the key while the eyes stay on the person talking — and until
// now it made no sound at all, so the only confirmation was a badge you had
// stopped watching. That is how "you're on mute" happens.
//
// Two notes, and the interval carries the meaning: down for off, up for on, so
// the pair is an answer rather than two unrelated beeps. Deafen says the same
// thing an octave lower and a little slower, because it is the bigger door:
// muting stops them hearing you, deafening stops the room existing.
//
// All four are deliberately tiny — 120ms end to end, well under the join
// chime — and they ride the same master gain and mute switch as everything
// else in this file.

// Mic off: G5 down to C5, a perfect fourth falling.
export function playMuteOn() {
  play(
    [
      [783.99, 0, 0.055, 0.05],
      [523.25, 0.05, 0.09, 0.045],
    ],
    "triangle",
  );
}

// Mic on: the same fourth climbing back. Two presses of the toggle are two
// halves of one gesture, and they sound like it.
export function playMuteOff() {
  play(
    [
      [523.25, 0, 0.055, 0.045],
      [783.99, 0.05, 0.09, 0.05],
    ],
    "triangle",
  );
}

// Deafen: an octave below the mute pair, on sines rather than triangles, so it
// reads as the heavier of the two without being any louder.
export function playDeafenOn() {
  play([
    [392, 0, 0.07, 0.055],
    [261.63, 0.065, 0.13, 0.05],
  ]);
}

export function playDeafenOff() {
  play([
    [261.63, 0, 0.07, 0.05],
    [392, 0.065, 0.13, 0.055],
  ]);
}

// You were disconnected — the call is still on your screen and you are no
// longer in it. Distinct from the leave chime on purpose: leaving is something
// you did, and this is something that happened to you. Three notes down a minor
// triad through the leave chime's darker room, so it lands as a fall rather
// than as a farewell.
export function playCallDropped() {
  if (!enabled) return;
  withAudio((ac) => {
    const bus = roomBus(ac, { seconds: 2.2, decay: 3.1, wet: 0.36, damp: 1600, level: 0.6 });
    thump(ac, bus, 0, 110, 40, 0.14);
    note(ac, bus, 440.0, 0.0, 0.5, 0.14);
    note(ac, bus, 349.23, 0.11, 0.55, 0.14);
    note(ac, bus, 261.63, 0.22, 1.3, 0.15);
  });
}

// Someone put their screen up. A soft two-note rise with a little room on it —
// enough to make you look, nowhere near enough to interrupt whoever is talking
// over it. Quieter than the join chime, because a share is an addition to a
// call you are already in.
export function playShareStart() {
  if (!enabled) return;
  withAudio((ac) => {
    const bus = roomBus(ac, { seconds: 1.1, decay: 2.6, wet: 0.24, damp: 3400, level: 0.42 });
    note(ac, bus, 587.33, 0.0, 0.34, 0.1);
    note(ac, bus, 880.0, 0.075, 0.52, 0.1);
  });
}

// A message arriving in the channel you are LOOKING at. Off by default and it
// has to be: a chime for something already on screen is the definition of a
// notification nobody asked for. It exists because in a quiet room some people
// want to hear the room. Quieter than the mention ping, and lower, so the two
// can never be confused.
export function playArrive() {
  play([[523.25, 0, 0.09, 0.035]], "triangle");
}

// ---- the voice-room soundboard ----
// Six effects built from oscillators and noise, honoring the file's vow: there
// are no audio files in this app and there must not be. On the wire a
// soundboard press is a ~30-byte trigger on the room's voice topic; every
// client synthesizes the sound LOCALLY from the same recipe — nothing to
// download, nothing to cache, and an airhorn arrives as fast as a heartbeat.
export const SOUNDBOARD = [
  { id: "airhorn", name: "Airhorn", emoji: "📯" },
  { id: "drumroll", name: "Drumroll", emoji: "🥁" },
  { id: "rimshot", name: "Ba dum tss", emoji: "🪘" },
  { id: "trombone", name: "Sad trombone", emoji: "🎺" },
  { id: "applause", name: "Applause", emoji: "👏" },
  { id: "crickets", name: "Crickets", emoji: "🦗" },
];

// A noise burst through a bandpass — the grain applause, drums and cymbals
// are all made of. peak is pre-compressor; keep these modest.
function noiseHit(ac, sink, start, dur, freq, q, peak) {
  const src = noiseSource(ac, dur);
  const bp = ac.createBiquadFilter();
  bp.type = "bandpass";
  bp.frequency.value = freq;
  bp.Q.value = q;
  const g = ac.createGain();
  const t0 = ac.currentTime + start;
  g.gain.setValueAtTime(0, t0);
  g.gain.linearRampToValueAtTime(peak, t0 + 0.008);
  g.gain.exponentialRampToValueAtTime(0.0001, t0 + dur);
  src.connect(bp).connect(g).connect(sink);
  src.start(t0);
  src.stop(t0 + dur + 0.02);
}

// A pitched slide — the trombone's wah and the airhorn's opening scoop.
function slide(ac, sink, wave, from, to, start, dur, peak) {
  const osc = ac.createOscillator();
  osc.type = wave;
  const t0 = ac.currentTime + start;
  osc.frequency.setValueAtTime(from, t0);
  osc.frequency.linearRampToValueAtTime(to, t0 + dur);
  const g = ac.createGain();
  g.gain.setValueAtTime(0, t0);
  g.gain.linearRampToValueAtTime(peak, t0 + 0.03);
  g.gain.setValueAtTime(peak, t0 + dur - 0.08);
  g.gain.exponentialRampToValueAtTime(0.0001, t0 + dur);
  osc.connect(g).connect(sink);
  osc.start(t0);
  osc.stop(t0 + dur + 0.02);
}

const SFX = {
  airhorn(ac) {
    const bus = roomBus(ac, { seconds: 0.9, decay: 2.2, wet: 0.22, damp: 3200, level: 0.5 });
    // Three sawtooths a few cents apart: the beat between them IS the honk.
    for (const det of [1, 1.006, 0.993]) {
      slide(ac, bus, "sawtooth", 415 * det, 466 * det, 0, 0.75, 0.05);
    }
  },
  drumroll(ac) {
    const bus = roomBus(ac, { seconds: 0.8, decay: 2.0, wet: 0.15, damp: 2500, level: 0.7 });
    // Alternating-hand hits, quickening crescendo.
    for (let i = 0; i < 26; i++) {
      const t = i * 0.052;
      noiseHit(ac, bus, t, 0.05, 190 + (i % 2) * 25, 1.6, 0.02 + (i / 26) * 0.045);
    }
    noiseHit(ac, bus, 26 * 0.052, 0.5, 900, 0.7, 0.09); // the cymbal it was building to
  },
  rimshot(ac) {
    const bus = roomBus(ac, { seconds: 1.2, decay: 2.4, wet: 0.25, damp: 3000, level: 0.8 });
    thump(ac, bus, 0, 160, 70, 0.12); // ba
    thump(ac, bus, 0.17, 150, 65, 0.12); // dum
    noiseHit(ac, bus, 0.34, 0.7, 3800, 0.8, 0.07); // tss
  },
  trombone(ac) {
    const bus = roomBus(ac, { seconds: 1.4, decay: 2.6, wet: 0.3, damp: 2200, level: 0.55 });
    // Wah, wah, wah, waaah — four droops, the last one giving up entirely.
    slide(ac, bus, "sawtooth", 233, 224, 0.0, 0.32, 0.045);
    slide(ac, bus, "sawtooth", 220, 211, 0.38, 0.32, 0.045);
    slide(ac, bus, "sawtooth", 208, 199, 0.76, 0.32, 0.045);
    slide(ac, bus, "sawtooth", 196, 170, 1.14, 0.85, 0.05);
  },
  applause(ac) {
    const bus = roomBus(ac, { seconds: 1.0, decay: 2.0, wet: 0.3, damp: 4000, level: 0.7 });
    // Decorrelated grains — many small hands, no loop, dying out naturally.
    for (let i = 0; i < 46; i++) {
      const t = Math.random() * 1.5;
      noiseHit(ac, bus, t, 0.045, 1200 + Math.random() * 2200, 1.1, 0.02 * (1 - t / 2.1));
    }
  },
  crickets(ac) {
    const bus = roomBus(ac, { seconds: 0.6, decay: 1.6, wet: 0.2, damp: 6000, level: 0.5 });
    // Two crickets trading chirps: high tones pulsed into short triplets.
    for (const [base, offset] of [
      [4300, 0],
      [3900, 0.9],
    ]) {
      for (let c = 0; c < 3; c++) {
        for (let p = 0; p < 4; p++) {
          const t = offset + c * 0.55 + p * 0.055;
          tone(ac, base + Math.random() * 120, t, 0.035, 0.02, "sine");
        }
      }
    }
  },
};

// playSfx renders one soundboard effect. Rides the global sound mute like
// every other chime; rate limiting and per-sender gates live with the caller
// (lib/state.svelte.js), which knows who pressed it.
export function playSfx(id) {
  if (!enabled || !boardOn) return;
  withAudio((ac) => SFX[id]?.(ac));
}

// ---- sounds people made up --------------------------------------------------
//
// A recipe is a couple of dozen numbers (lib/sfxrecipe.js) rather than one of
// the six functions above, and this renders it out of the same primitives —
// the same roomBus, the same noise burst, the same slide. Nothing here trusts
// its input: everything below has already been through decodeRecipe, which
// REFUSES anything out of range rather than clamping it, so by the time a
// recipe arrives at this function its gain, duration and frequencies are all
// inside bounds this file chose.

// One recipe, rendered.
function buildRecipe(ac, r) {
  const level = 0.55 + (r.room / 100) * 0.2;
  const bus = roomBus(ac, {
    seconds: 0.5 + (r.room / 100) * 1.6,
    decay: 2.0 + (r.room / 100) * 0.9,
    wet: (r.room / 100) * 0.55,
    damp: 2000 + (1 - r.noise / 100) * 3000,
    level,
  });
  const peak = r.gain / 1000;
  const dur = r.dur / 1000;
  const gap = r.gap / 1000;
  const attack = Math.min(r.attack / 1000, dur * 0.6);
  const toneMix = 1 - r.noise / 100;
  const exp = (r.flags & 1) !== 0;
  const swell = (r.flags & 2) !== 0;
  for (let i = 0; i < r.reps; i++) {
    const at = i * gap;
    // Repeats can climb or fall a fixed interval — that is what turns one
    // parameter block into an arpeggio, a drum roll or a sad trombone.
    const shift = Math.pow(2, (r.step * i) / 12);
    // A swell rises across the repeats instead of every hit landing at the
    // same weight; it is the difference between a roll and a metronome.
    const level2 = swell ? peak * (0.35 + (0.65 * (i + 1)) / r.reps) : peak;
    if (toneMix > 0.01) {
      // Detune spreads two extra voices either side, which is what makes a
      // sawtooth honk rather than buzz.
      const voices = r.detune > 0 ? [0, r.detune, -r.detune] : [0];
      for (const cents of voices) {
        const osc = ac.createOscillator();
        osc.type = SFX_WAVES[r.wave] || "sine";
        osc.detune.value = cents;
        const t0 = ac.currentTime + at;
        const f0 = Math.max(20, Math.min(20000, r.f0 * shift));
        const f1 = Math.max(20, Math.min(20000, r.f1 * shift));
        osc.frequency.setValueAtTime(f0, t0);
        if (exp) osc.frequency.exponentialRampToValueAtTime(f1, t0 + dur);
        else osc.frequency.linearRampToValueAtTime(f1, t0 + dur);
        const g = ac.createGain();
        const v = (level2 * toneMix) / voices.length;
        g.gain.setValueAtTime(0, t0);
        g.gain.linearRampToValueAtTime(v, t0 + Math.max(0.002, attack));
        g.gain.exponentialRampToValueAtTime(0.0001, t0 + dur);
        osc.connect(g).connect(bus);
        osc.start(t0);
        osc.stop(t0 + dur + 0.03);
      }
    }
    if (r.noise > 0) {
      noiseHit(ac, bus, at, dur, Math.max(60, Math.min(16000, r.noiseHz * shift)), r.noiseQ / 4, level2 * (r.noise / 100));
    }
  }
}

// The soundboard's own mute, separate from the app-wide one. Sound effects are
// the thing a person most wants to switch off without also losing the mention
// ping and the incoming-call ring, and before this the only lever was all of
// them at once.
let boardOn = loadBoard();
function loadBoard() {
  try {
    return localStorage.getItem("concord.soundboard") !== "off";
  } catch {
    return true;
  }
}
export function soundboardEnabled() {
  return boardOn;
}
export function setSoundboardEnabled(on) {
  boardOn = on;
  try {
    localStorage.setItem("concord.soundboard", on ? "on" : "off");
  } catch {
    /* ignore */
  }
}

// A sound is short and a room is small: two of them at once is noise, and a
// held button is a siren. The wire-side rate limits are per sender; this one is
// per SPEAKER, so however many people press at once the output stays a sound
// rather than a wall. `force` is the studio's preview, which is a deliberate
// request for a sound and must not be swallowed by somebody else's press.
let lastPlay = 0;
const MIN_GAP_MS = 220;

export function playRecipe(recipe, { force = false } = {}) {
  if (!recipe) return false;
  if (!force && (!enabled || !boardOn)) return false;
  const now = Date.now();
  if (!force && now - lastPlay < MIN_GAP_MS) return false;
  lastPlay = now;
  return withAudio((ac) => buildRecipe(ac, recipe));
}

// playSfxTrigger resolves what arrived in a soundboard press: one of the six
// built-in ids, or a recipe payload. Both are looked up rather than trusted,
// and both fail to nothing — an id with no entry and a payload that will not
// decode are the same silence, which is exactly what a client one version
// behind does with a recipe it has never heard of.
export function playSfxTrigger(target) {
  if (!enabled || !boardOn) return;
  if (SFX[target]) return playSfx(target);
  playRecipe(decodeRecipe(target));
}

// Easter egg: the home button's logo is a Concorde, so hammering it flies one
// past you. Synthesized like everything else here — there are no audio files in
// this app and there must not be.
//
// It obeys the mute, and used to claim not to. The claim was that eight
// deliberate clicks are a request for sound and staying silent there looks like
// a broken egg — true as far as it goes, but the sound was routed through the
// master gain anyway, so an install at zero volume got silence regardless and
// the exemption bought nothing but a comment that disagreed with the code. Now
// that "off" IS zero on one control in settings, the honest rule is the simple
// one: sound off means no sound, including this one.
export function playFlyby() {
  if (!enabled) return;
  withAudio(buildFlyby);
}

function buildFlyby(ac) {
  const t0 = ac.currentTime;
  const dur = 2.4;
  const near = t0 + 1.0; // closest approach: loudest point, and where the boom lands

  // Panning it across the stereo field is most of what sells "passing overhead";
  // createStereoPanner is ubiquitous but the fallback costs one line.
  const out = ac.createStereoPanner?.() ?? null;
  if (out) {
    out.pan.setValueAtTime(-0.9, t0);
    out.pan.linearRampToValueAtTime(0.9, t0 + dur);
    out.connect(master(ac));
  }
  const sink = out || master(ac);

  // Engine: broadband noise through a bandpass swept up on approach and back
  // down as it recedes. The sweep IS the Doppler shift — a fixed filter just
  // sounds like wind.
  const eng = noiseSource(ac, dur);
  const bp = ac.createBiquadFilter();
  bp.type = "bandpass";
  bp.Q.value = 1.4;
  bp.frequency.setValueAtTime(320, t0);
  bp.frequency.exponentialRampToValueAtTime(2800, near);
  bp.frequency.exponentialRampToValueAtTime(220, t0 + dur);
  const eg = ac.createGain();
  eg.gain.setValueAtTime(0.0001, t0);
  eg.gain.exponentialRampToValueAtTime(0.2, near);
  eg.gain.exponentialRampToValueAtTime(0.0001, t0 + dur);
  eng.connect(bp).connect(eg).connect(sink);
  eng.start(t0);
  eng.stop(t0 + dur);

  // Sonic boom: a lowpassed crack at the moment it passes, with the cutoff
  // falling so the tail rolls off into a rumble instead of a click.
  const bang = noiseSource(ac, 0.5);
  const lp = ac.createBiquadFilter();
  lp.type = "lowpass";
  lp.frequency.setValueAtTime(420, near);
  lp.frequency.exponentialRampToValueAtTime(90, near + 0.45);
  const bg = ac.createGain();
  bg.gain.setValueAtTime(0.0001, near);
  bg.gain.linearRampToValueAtTime(0.28, near + 0.012);
  bg.gain.exponentialRampToValueAtTime(0.0001, near + 0.5);
  bang.connect(lp).connect(bg).connect(sink);
  bang.start(near);
  bang.stop(near + 0.5);
}
