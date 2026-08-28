// sfxrecipe.js — the format for a sound somebody made up.
//
// The soundboard has always synthesized its six effects from oscillators
// because there are no audio files in this app and there must not be. This
// takes the same vow one step further: a sound you build yourself is not a file
// either, it is a RECIPE — a couple of dozen bytes naming a waveform, a pitch
// sweep, an envelope, a noise mix and a repeat pattern — and every client
// renders it locally from those numbers. A guild whose members have made their
// own airhorn has an in-joke that cannot exist anywhere a custom sound is an
// upload.
//
// It rides two lanes, and both already exist:
//
//   • in a text channel, as `[sound](concord://sfx/v1/<b64url>)` — the ordinary
//     in-band token, rendered as a chip you press;
//   • in a voice room, in the `target` field of the "sfx" trigger the
//     soundboard already publishes on the room's voice topic. That field is an
//     opaque string end to end, so a recipe needs no backend change at all —
//     and a build that predates this looks the payload up in its table of six
//     built-in ids, misses, and plays nothing. Fail-closed by construction.
//
// CONTAINMENT — and this is the feature where it matters most, because the
// output of a decode here is an attacker's numbers reaching a live speaker:
//
//   REJECT, DO NOT CLAMP. Every field has a legal range and a representation
//   wide enough to express values outside it, deliberately, so that "out of
//   range" is a thing a token can say and a thing this module can refuse. A
//   recipe with a gain of 60.0 or a thirty-second sustain decodes to null and
//   plays NOTHING. Clamping it would play something — quieter, shorter, but
//   still a sound a stranger chose to make happen in your room, and the whole
//   argument for refusing is that an unclamped gain is a weapon and a clamped
//   one is a weapon that was politely resized.
//
// The peak gain ceiling is 0.25, against built-ins that sit between 0.02 and
// 0.19, and everything renders through the same compressor/limiter bus they do.
// The duration ceiling is 2.5 seconds INCLUDING repeats — the trap is not one
// long note, it is twenty-four short ones spaced six hundred milliseconds
// apart, so the cap is checked on the total, not on the field.

import { bytesToB64url, b64urlToBytes } from "./b64url.js";

export const SFX_RE = /\[sound\]\(concord:\/\/sfx\/v1\/([A-Za-z0-9_-]+)\)/;

const VERSION = 1;
const FIXED = 25; // bytes before the name
export const MAX_NAME_BYTES = 40;
// 25 fixed bytes plus at most 40 of name is 65 bytes, which is 88 base64
// characters. The cap is checked before decoding so a long payload never
// reaches atob; it can only ever catch something that was not a recipe.
export const MAX_TOKEN_CHARS = 120;

export const SFX_WAVES = ["sine", "triangle", "square", "sawtooth"];

// A small closed table of faces. An index with no entry is refused, which is
// the same "safety is the lookup failing, not the list being exhaustive" rule
// the card effects and frames are resolved by.
export const SFX_GLYPHS = [
  "🔊", "📯", "🥁", "🎺", "👏", "🦗", "✨", "💥",
  "🫧", "🎉", "🔔", "⚡", "🐸", "🚨", "🎹", "🛎️",
  "🥴", "🤖", "🌊", "🪄", "🧨", "🎷", "🛸", "😾",
];

// Every parameter, its unit, and the range outside which a recipe is refused.
// The studio's sliders are generated from this table, so the editor cannot
// build a sound the decoder would reject.
export const SFX_FIELDS = {
  wave: { min: 0, max: SFX_WAVES.length - 1, label: "Waveform" },
  f0: { min: 20, max: 14000, label: "Pitch from", unit: "Hz", step: 5 },
  f1: { min: 20, max: 14000, label: "Pitch to", unit: "Hz", step: 5 },
  attack: { min: 0, max: 400, label: "Attack", unit: "ms", step: 2 },
  dur: { min: 30, max: 2500, label: "Length", unit: "ms", step: 10 },
  gain: { min: 0, max: 250, label: "Level", unit: "‰", step: 5 },
  noise: { min: 0, max: 100, label: "Noise", unit: "%", step: 1 },
  noiseHz: { min: 100, max: 12000, label: "Noise pitch", unit: "Hz", step: 25 },
  noiseQ: { min: 1, max: 120, label: "Noise focus", step: 1 },
  reps: { min: 1, max: 24, label: "Hits", step: 1 },
  gap: { min: 0, max: 600, label: "Spacing", unit: "ms", step: 5 },
  detune: { min: 0, max: 99, label: "Detune", unit: "cents", step: 1 },
  room: { min: 0, max: 100, label: "Room", unit: "%", step: 1 },
  step: { min: -24, max: 24, label: "Step per hit", unit: "semitones", step: 1 },
  glyph: { min: 0, max: SFX_GLYPHS.length - 1, label: "Face" },
};

// The whole sound, repeats included, must fit here. This is the bound the
// per-field caps cannot express: 24 hits 600ms apart is fourteen seconds of
// somebody else's idea of funny.
export const MAX_TOTAL_MS = 2500;

export const FLAG_EXP = 1; // sweep the pitch exponentially rather than linearly
export const FLAG_SWELL = 2; // let the repeats grow louder rather than staying level

export function recipeTotalMs(r) {
  return (r.dur | 0) + Math.max(0, (r.reps | 0) - 1) * (r.gap | 0);
}

function inRange(v, f) {
  return Number.isInteger(v) && v >= f.min && v <= f.max;
}

// validRecipe is the single gate. It runs before signing a recipe into a token
// AND on everything that arrives, so the send and receive sides cannot drift —
// the lesson the GIF and story records already paid for.
export function validRecipe(r) {
  if (!r || typeof r !== "object") return false;
  for (const [k, f] of Object.entries(SFX_FIELDS)) {
    if (!inRange(r[k], f)) return false;
  }
  if (!Number.isInteger(r.flags) || r.flags < 0 || r.flags > 3) return false;
  if (typeof r.name !== "string") return false;
  if (nameBytes(r.name) > MAX_NAME_BYTES) return false;
  // A name is a label in somebody else's message list. Control characters and
  // line breaks in one are never a name, so they are a reason to refuse the
  // whole recipe rather than something to quietly strip.
  if (/[\u0000-\u001f\u007f]/.test(r.name)) return false;
  if (recipeTotalMs(r) > MAX_TOTAL_MS) return false;
  return true;
}

function nameBytes(s) {
  return new TextEncoder().encode(s).length;
}

// ---- encode -----------------------------------------------------------------

// encodeRecipe(r) -> payload, or "" when the recipe is out of range. The
// encoder refuses too, on purpose: the studio drives its sliders off the same
// table, so a sound this app builds is always a sound this app will play back.
export function encodeRecipe(r) {
  if (!validRecipe(r)) return "";
  const name = new TextEncoder().encode(r.name);
  const b = new Uint8Array(FIXED + name.length);
  const put16 = (at, v) => {
    b[at] = (v >> 8) & 0xff;
    b[at + 1] = v & 0xff;
  };
  b[0] = VERSION;
  b[1] = r.wave;
  put16(2, r.f0);
  put16(4, r.f1);
  put16(6, r.attack);
  put16(8, r.dur);
  put16(10, r.gain);
  b[12] = r.noise;
  put16(13, r.noiseHz);
  b[15] = r.noiseQ;
  b[16] = r.reps;
  put16(17, r.gap);
  b[19] = r.detune;
  b[20] = r.room;
  b[21] = r.flags;
  b[22] = r.step & 0xff; // two's complement; read back signed
  b[23] = r.glyph;
  b[24] = name.length;
  b.set(name, FIXED);
  return bytesToB64url(b);
}

export function encodeSound(payloadOrRecipe) {
  const p = typeof payloadOrRecipe === "string" ? payloadOrRecipe : encodeRecipe(payloadOrRecipe);
  return p ? `[sound](concord://sfx/v1/${p})` : "";
}

// ---- decode -----------------------------------------------------------------

// decodeRecipe(payload) -> recipe or null. null is the only failure, and it
// means nothing is drawn and nothing is played.
export function decodeRecipe(payload) {
  if (typeof payload !== "string" || !payload || payload.length > MAX_TOKEN_CHARS) return null;
  const b = b64urlToBytes(payload);
  if (!b || b.length < FIXED) return null;
  if (b[0] !== VERSION) return null;
  const nameLen = b[24];
  if (b.length !== FIXED + nameLen) return null;
  let name = "";
  if (nameLen) {
    try {
      // fatal: a name that is not valid UTF-8 is not a name. Letting the
      // decoder substitute replacement characters would turn malformed bytes
      // into a row of question marks in everybody's message list.
      name = new TextDecoder("utf-8", { fatal: true }).decode(b.subarray(FIXED));
    } catch {
      return null;
    }
  }
  const u16 = (at) => (b[at] << 8) | b[at + 1];
  const r = {
    wave: b[1],
    f0: u16(2),
    f1: u16(4),
    attack: u16(6),
    dur: u16(8),
    gain: u16(10),
    noise: b[12],
    noiseHz: u16(13),
    noiseQ: b[15],
    reps: b[16],
    gap: u16(17),
    detune: b[19],
    room: b[20],
    flags: b[21],
    step: (b[22] << 24) >> 24, // sign-extend
    glyph: b[23],
    name,
  };
  // Same gate as the send side. Everything above merely read the bytes; this
  // is the line that decides whether they are allowed to reach a speaker.
  return validRecipe(r) ? r : null;
}

export function parseSound(content) {
  if (!content) return null;
  const m = content.match(SFX_RE);
  return m ? decodeRecipe(m[1]) : null;
}

export function soundPayload(content) {
  const m = content?.match(SFX_RE);
  return m ? m[1] : "";
}

export function stripSound(content) {
  return content ? content.replace(SFX_RE, "").trim() : content;
}

// A sound is short enough that seconds to one decimal reads "0.0s" for half
// the shelf. Below a second, say milliseconds.
export function soundLength(r) {
  const ms = recipeTotalMs(r);
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)}s`;
}

export function recipeGlyph(r) {
  return SFX_GLYPHS[r?.glyph] || SFX_GLYPHS[0];
}

// ---- the starter shelf ------------------------------------------------------

// Twelve sounds to begin with, so the studio has somewhere to start from rather
// than a row of sliders at zero. Each is a plain object here and becomes a
// token only when it is used, so the shelf costs a few hundred bytes of source
// and nothing on the wire until somebody presses one.
function sfx(name, glyph, over) {
  return {
    name,
    glyph,
    wave: 0,
    f0: 440,
    f1: 440,
    attack: 8,
    dur: 200,
    gain: 120,
    noise: 0,
    noiseHz: 1000,
    noiseQ: 4,
    reps: 1,
    gap: 0,
    detune: 0,
    room: 20,
    flags: 0,
    step: 0,
    ...over,
  };
}

export const STARTER_SHELF = [
  sfx("Boop", 0, { f0: 660, f1: 660, dur: 130, gain: 130, room: 12 }),
  sfx("Tada", 9, { wave: 1, f0: 523, f1: 523, dur: 300, reps: 2, gap: 150, step: 5, gain: 125, room: 38 }),
  // Not 🎺 and not "Drumroll": the room's six built-ins already own a trombone
  // and a drum, and a starter shelf that hands you a second one under the same
  // face is two anonymous twins on a board identified by pictures. (Seed data
  // only — a shelf that already exists lives in localStorage and is left alone.)
  sfx("Womp", 16, { wave: 3, f0: 233, f1: 196, dur: 320, reps: 4, gap: 380, step: -1, gain: 110, room: 30, flags: FLAG_EXP }),
  sfx("Rumble", 18, { noise: 100, noiseHz: 200, noiseQ: 6, dur: 60, reps: 24, gap: 55, gain: 95, room: 22, flags: FLAG_SWELL }),
  sfx("Chime", 10, { f0: 880, f1: 880, attack: 12, dur: 620, reps: 3, gap: 160, step: 4, gain: 105, room: 62 }),
  sfx("Pop", 8, { f0: 420, f1: 180, attack: 2, dur: 95, gain: 165, room: 8, flags: FLAG_EXP }),
  sfx("Zap", 11, { wave: 3, f0: 1800, f1: 200, dur: 260, noise: 25, noiseHz: 3000, noiseQ: 12, gain: 120, room: 18, flags: FLAG_EXP }),
  sfx("Click", 7, { noise: 100, noiseHz: 2400, noiseQ: 8, dur: 40, gain: 120, room: 5 }),
  sfx("Coin", 15, { wave: 2, f0: 988, f1: 988, dur: 95, reps: 2, gap: 95, step: 5, gain: 105, room: 16 }),
  sfx("Siren", 13, { f0: 620, f1: 1000, dur: 480, reps: 3, gap: 500, gain: 105, room: 42 }),
  sfx("Thud", 20, { f0: 140, f1: 45, attack: 3, dur: 340, noise: 15, noiseHz: 180, noiseQ: 4, gain: 200, room: 26, flags: FLAG_EXP }),
  sfx("Sparkle", 6, { wave: 1, f0: 1600, f1: 2400, dur: 90, reps: 6, gap: 72, step: 3, gain: 90, room: 48 }),
];
