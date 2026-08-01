// sounds.js — tiny synthesized UI chimes (no audio files). One shared
// AudioContext, created lazily on first use (a user gesture will have happened
// by the time any of these fire). Muteable, persisted in localStorage.

let ctx = null;
let enabled = load();

function load() {
  try {
    return localStorage.getItem("concord.sounds") !== "off";
  } catch {
    return true;
  }
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
  if (!AC) return null;
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

// watchdog flags a context that reports "running" while its clock is stuck —
// the signature of a context orphaned by an audio-route change. The current
// sound is already lost (there is no way to know before playing into it); the
// point is that the next one isn't.
function watchdog(ac) {
  const t0 = ac.currentTime;
  setTimeout(() => {
    if (ctx === ac && ac.state === "running" && ac.currentTime === t0) routeDirty = true;
  }, 250);
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
  osc.connect(gain).connect(ac.destination);
  osc.start(t0);
  osc.stop(t0 + dur + 0.02);
}

// `force` bypasses the mute check (for previewing a ringtone in settings).
function play(notes, wave = "sine", force = false) {
  if (!enabled && !force) return;
  const ac = audio();
  if (!ac) return;
  for (const [freq, start, dur, peak] of notes) tone(ac, freq, start, dur, peak, wave);
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
  comp.connect(out).connect(ac.destination);
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
  const ac = audio();
  if (!ac) return;
  buildJoin(ac);
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
  const ac = audio();
  if (!ac) return;
  buildLeave(ac);
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

// Easter egg: the home button's logo is a Concorde, so hammering it flies one
// past you. Synthesized like everything else here — there are no audio files in
// this app and there must not be.
//
// This ignores the mute setting on purpose. Mute exists to stop the app making
// noise AT you (mentions, DMs, an incoming call); this fires only after you've
// deliberately hit the same button eight times in a row, which is a request for
// sound, and staying silent there is indistinguishable from a broken egg.
export function playFlyby() {
  const ac = audio();
  if (!ac) return;
  const t0 = ac.currentTime;
  const dur = 2.4;
  const near = t0 + 1.0; // closest approach: loudest point, and where the boom lands

  // Panning it across the stereo field is most of what sells "passing overhead";
  // createStereoPanner is ubiquitous but the fallback costs one line.
  const out = ac.createStereoPanner?.() ?? null;
  if (out) {
    out.pan.setValueAtTime(-0.9, t0);
    out.pan.linearRampToValueAtTime(0.9, t0 + dur);
    out.connect(ac.destination);
  }
  const sink = out || ac.destination;

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
