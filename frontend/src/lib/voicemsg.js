// voicemsg.js — what a voice message knows about itself.
//
// A voice message is an ordinary encrypted file attachment tagged audio/*, and
// until now that was ALL it was: the chip in the feed said "0:00" until you
// played it (the duration only arrives with `loadedmetadata`, which only fires
// on first play) and its forty-two waveform bars were a hash of the blob id — a
// stable random barcode with no relationship to the sound. The recorder knew
// both facts and threw them away.
//
// So they ride along, in the one field a file token already carries and nobody
// renders for audio: the NAME. The token stores the name base64url-encoded
// (see internal/app/attach.go), so the bytes are free-form, and a build that
// predates this reads the whole thing as a filename and behaves exactly as it
// does today. Nothing on the wire changes shape.
//
//   Voice message.webm                       ← before, and still valid
//   Voice message [12s Kx9…42 chars].webm    ← with a duration and an envelope
//
// Deliberately legible rather than packed: if this string ever does surface in
// front of a person — a download, a log, a client that renders it as a file —
// it should read as a voice message of twelve seconds, not as line noise.

// 42 buckets, one character each, six bits apiece. Forty-two because that is
// how many bars the player draws; storing more would be throwing detail away
// at render time instead of at record time.
export const ENV_BUCKETS = 42;
const ALPHA = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";

const NAME_RE = /^(.*?) \[(\d{1,4})s ([A-Za-z0-9_-]{42})\]\.([A-Za-z0-9]+)$/;

// encodeEnvelope takes levels in 0..1 and returns 42 characters.
export function encodeEnvelope(levels) {
  const out = [];
  for (let i = 0; i < ENV_BUCKETS; i++) {
    const v = levels[i];
    const n = Math.max(0, Math.min(63, Math.round((Number.isFinite(v) ? v : 0) * 63)));
    out.push(ALPHA[n]);
  }
  return out.join("");
}

export function decodeEnvelope(s) {
  if (typeof s !== "string" || s.length !== ENV_BUCKETS) return null;
  const out = [];
  for (const ch of s) {
    const i = ALPHA.indexOf(ch);
    if (i < 0) return null; // refuse rather than guess: a bad character is a bad token
    out.push(i / 63);
  }
  return out;
}

// voiceFileName builds the name a recording travels under. A recording with no
// measured envelope (an analyser that refused to open) keeps the plain name
// rather than shipping forty-two zeroes, which would draw a flat line and
// claim it was the sound.
export function voiceFileName(ext, secs, levels) {
  const base = "Voice message";
  const s = Math.max(0, Math.min(9999, Math.round(secs || 0)));
  if (!s || !levels?.length) return `${base}.${ext}`;
  return `${base} [${s}s ${encodeEnvelope(levels)}].${ext}`;
}

// parseVoiceMeta reads it back. Returns { secs, env } — both null-ish for a
// message recorded before this existed, which is the case the player's
// decode-on-first-play path is for.
export function parseVoiceMeta(name) {
  const m = NAME_RE.exec(name || "");
  if (!m) return { secs: 0, env: null };
  return { secs: Number(m[2]) || 0, env: decodeEnvelope(m[3]) };
}

// resample folds however many level samples were taken during a recording into
// exactly ENV_BUCKETS, by taking the PEAK of each bucket rather than the mean.
// A mean turns speech into a flat sausage — the loud parts are brief and the
// gaps between words are most of the samples — and the whole point of the bars
// is that you can see where the pauses are.
export function resampleEnvelope(samples) {
  if (!samples?.length) return null;
  const out = new Array(ENV_BUCKETS).fill(0);
  for (let i = 0; i < ENV_BUCKETS; i++) {
    const a = Math.floor((i * samples.length) / ENV_BUCKETS);
    const b = Math.max(a + 1, Math.floor(((i + 1) * samples.length) / ENV_BUCKETS));
    let peak = 0;
    for (let j = a; j < b && j < samples.length; j++) peak = Math.max(peak, samples[j]);
    out[i] = peak;
  }
  // Normalise to the loudest moment. Absolute levels say more about the
  // microphone's gain than about the recording, and a quiet clip drawn as a
  // flat line is the same lie the fake bars were.
  const top = Math.max(...out);
  if (top <= 0) return null;
  return out.map((v) => Math.max(0.06, v / top));
}

// Envelopes decoded from the audio itself, for messages sent before the
// recorder measured anything. Decoding is only ever done once per blob per
// session — the bars are a picture of bytes that cannot change.
const envCache = new Map();
const ENV_CACHE_MAX = 200; // ~40 numbers each: kilobytes, not megabytes

export function cachedEnvelope(blobId) {
  return envCache.get(blobId) || null;
}

export function cacheEnvelope(blobId, env) {
  if (!blobId || !env) return;
  envCache.set(blobId, env);
  if (envCache.size > ENV_CACHE_MAX) envCache.delete(envCache.keys().next().value);
}

// envelopeFromBuffer computes the same shape from decoded audio, for messages
// that were sent before the recorder measured anything. Peak per bucket, same
// normalisation, so an old message and a new one are drawn on one scale.
export function envelopeFromBuffer(buf) {
  const ch = buf.getChannelData(0);
  const out = new Array(ENV_BUCKETS).fill(0);
  const per = Math.max(1, Math.floor(ch.length / ENV_BUCKETS));
  for (let i = 0; i < ENV_BUCKETS; i++) {
    let peak = 0;
    const a = i * per;
    const b = Math.min(ch.length, a + per);
    // One in every few hundred samples is plenty for a 42-bar picture and
    // keeps a five-minute clip from costing a visible pause.
    const step = Math.max(1, Math.floor((b - a) / 512));
    for (let j = a; j < b; j += step) peak = Math.max(peak, Math.abs(ch[j]));
    out[i] = peak;
  }
  const top = Math.max(...out);
  if (top <= 0) return null;
  return out.map((v) => Math.max(0.06, v / top));
}
