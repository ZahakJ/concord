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

function audio() {
  if (!ctx) {
    const AC = window.AudioContext || window.webkitAudioContext;
    if (!AC) return null;
    ctx = new AC();
  }
  if (ctx.state === "suspended") ctx.resume().catch(() => {});
  return ctx;
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

// Rising two-note: someone joined the voice room.
export function playVoiceJoin() {
  play([
    [523.25, 0, 0.18],
    [783.99, 0.09, 0.22],
  ]);
}

// Falling two-note: someone left.
export function playVoiceLeave() {
  play([
    [659.25, 0, 0.18],
    [440.0, 0.09, 0.22],
  ]);
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
