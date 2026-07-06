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

// tone plays a short shaped sine at freq (Hz) starting at offset seconds.
function tone(ac, freq, start, dur, peak = 0.14) {
  const osc = ac.createOscillator();
  const gain = ac.createGain();
  osc.type = "sine";
  osc.frequency.value = freq;
  const t0 = ac.currentTime + start;
  gain.gain.setValueAtTime(0, t0);
  gain.gain.linearRampToValueAtTime(peak, t0 + 0.015);
  gain.gain.exponentialRampToValueAtTime(0.0001, t0 + dur);
  osc.connect(gain).connect(ac.destination);
  osc.start(t0);
  osc.stop(t0 + dur + 0.02);
}

function play(notes) {
  if (!enabled) return;
  const ac = audio();
  if (!ac) return;
  for (const [freq, start, dur, peak] of notes) tone(ac, freq, start, dur, peak);
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
