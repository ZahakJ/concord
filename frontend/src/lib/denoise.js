// denoise.js — spectral noise reduction for the microphone.
//
// The browser's own `noiseSuppression` is deliberately gentle: it's tuned to
// never damage speech, which means a steady fan, an air conditioner, or a mic's
// own hiss survives it. That floor is the thing people actually hear on your
// calls, because it never stops — it's there under every word and in every gap.
//
// This is the classic fix, done properly: short-time Fourier analysis, a noise
// floor learned continuously per frequency bin, and a Wiener-style gain that
// pulls each bin down by however much of it is noise. No model, no download, no
// dependency — a few hundred lines of DSP running in an AudioWorklet.
//
// Why per-bin and not a gate: a gate is a decision about the whole signal, so
// it can only choose between "all of it" and "none of it", and it can't help at
// all while you're talking. Spectral subtraction works *inside* speech —
// hiss between your harmonics gets pulled down while the harmonics stay.
//
// Three details keep it from sounding processed:
//   - An asymmetric follower for the noise estimate — quick to fall, very slow
//     to rise — so it settles just under whatever is always there and adapts on
//     its own, with no "stay silent for five seconds" calibration step.
//   - A gain floor rather than true subtraction. Zeroing a bin outright creates
//     "musical noise", the warbling artefact that makes cheap denoisers obvious.
//     Attenuating to a floor keeps a natural-sounding bed.
//   - Time smoothing of the per-bin gains, so bins fade rather than flicker.

// The worklet source, kept as a string and loaded from a blob URL: an
// AudioWorklet module has to be fetched by URL, and this way there's no extra
// build-time asset to keep in sync with the bundle.
const WORKLET_SRC = String.raw`
// Frame 512 @ 48 kHz ≈ 10.7 ms, hop 128 = one render quantum, so analysis lines
// up exactly with the audio callback and no extra buffering latency is added
// beyond the frame itself.
const N = 512;
const HOP = 128;
const HALF = N / 2;

// Hann window, applied on both analysis and synthesis. With 75% overlap the
// squared window sums to a constant, so overlap-add reconstructs exactly.
const win = new Float32Array(N);
for (let i = 0; i < N; i++) win[i] = 0.5 - 0.5 * Math.cos((2 * Math.PI * i) / N);
// Σ w² over the overlapping frames, so analysis+synthesis windowing is unity.
let wsum = 0;
for (let i = 0; i < N; i += HOP) wsum += win[i] * win[i];
const WOLA = 1 / wsum;

// Bit-reversal permutation and twiddles for an in-place radix-2 FFT.
const rev = new Uint16Array(N);
for (let i = 0, j = 0; i < N; i++) {
  rev[i] = j;
  let bit = N >> 1;
  for (; j & bit; bit >>= 1) j ^= bit;
  j ^= bit;
}
const cosT = new Float32Array(HALF);
const sinT = new Float32Array(HALF);
for (let i = 0; i < HALF; i++) {
  cosT[i] = Math.cos((-2 * Math.PI * i) / N);
  sinT[i] = Math.sin((-2 * Math.PI * i) / N);
}

function fft(re, im, inverse) {
  for (let i = 0; i < N; i++) {
    const j = rev[i];
    if (j > i) {
      let t = re[i]; re[i] = re[j]; re[j] = t;
      t = im[i]; im[i] = im[j]; im[j] = t;
    }
  }
  for (let len = 2; len <= N; len <<= 1) {
    const step = N / len;
    for (let i = 0; i < N; i += len) {
      for (let k = 0, t = 0; k < len >> 1; k++, t += step) {
        const c = cosT[t];
        const s = inverse ? -sinT[t] : sinT[t];
        const a = i + k;
        const b = a + (len >> 1);
        const xr = re[b] * c - im[b] * s;
        const xi = re[b] * s + im[b] * c;
        re[b] = re[a] - xr;
        im[b] = im[a] - xi;
        re[a] += xr;
        im[a] += xi;
      }
    }
  }
  if (inverse) {
    for (let i = 0; i < N; i++) { re[i] /= N; im[i] /= N; }
  }
}

class Denoiser extends AudioWorkletProcessor {
  static get parameterDescriptors() {
    return [
      // 0 = bypass. Otherwise how hard to pull noise down, 0..1.
      { name: "strength", defaultValue: 0, minValue: 0, maxValue: 1, automationRate: "k-rate" },
    ];
  }

  constructor() {
    super();
    this.inBuf = new Float32Array(N);   // sliding analysis window
    this.outBuf = new Float32Array(N);  // overlap-add accumulator
    this.re = new Float32Array(N);
    this.im = new Float32Array(N);
    this.power = new Float32Array(HALF + 1);   // smoothed power per bin
    this.noise = new Float32Array(HALF + 1);   // running minimum = the floor
    this.gain = new Float32Array(HALF + 1).fill(1);
    this.primed = false;
  }

  process(inputs, outputs, params) {
    const input = inputs[0];
    const output = outputs[0];
    if (!input || !input.length || !output || !output.length) return true;
    const inCh = input[0];
    const outCh = output[0];
    if (!inCh) return true;

    const strength = params.strength.length ? params.strength[0] : 0;
    if (strength <= 0) {
      outCh.set(inCh);
      // Keep the analysis window fed so switching on mid-speech doesn't start
      // from a window full of silence.
      this.inBuf.copyWithin(0, HOP);
      this.inBuf.set(inCh, N - HOP);
      return true;
    }

    // Slide the newest hop into the analysis window.
    this.inBuf.copyWithin(0, HOP);
    this.inBuf.set(inCh, N - HOP);

    for (let i = 0; i < N; i++) {
      this.re[i] = this.inBuf[i] * win[i];
      this.im[i] = 0;
    }
    fft(this.re, this.im, false);

    // How far a bin may be pushed down, and how much of the estimate to
    // subtract. Both scale with strength so the control is a single dial.
    const floor = 0.45 * (1 - strength) + 0.05;     // 0.50 (gentle) → 0.09 (hard)
    const floor2 = floor * floor;
    const over = 1.2 + 2.3 * strength;              // over-subtraction factor

    for (let b = 0; b <= HALF; b++) {
      const p = this.re[b] * this.re[b] + this.im[b] * this.im[b];
      // Heavy smoothing first. A single bin of noise has exponentially
      // distributed power — its instantaneous value swings over orders of
      // magnitude — so anything that tracks the raw periodogram is tracking
      // mostly variance.
      this.power[b] = this.primed ? 0.9 * this.power[b] + 0.1 * p : p;

      // Noise floor as an asymmetric follower rather than a hard minimum: it
      // falls toward the current level quickly and rises very slowly, so it
      // settles just under the steady part of the signal and climbs only if the
      // room really did get louder. A true running minimum sounds like the
      // right idea and isn't — it latches onto the deepest fluctuation, which
      // for random noise sits far below the actual floor, and every bin then
      // under-subtracts by a different amount.
      const n = this.noise[b];
      if (!this.primed) this.noise[b] = this.power[b];
      else if (this.power[b] < n) this.noise[b] = n + (this.power[b] - n) * 0.05;
      else this.noise[b] = n + (this.power[b] - n) * 0.0006;

      // Power-domain spectral subtraction, floored so a bin is attenuated
      // rather than erased (erasing is what produces musical noise).
      let g2 = 1 - (over * this.noise[b]) / (this.power[b] + 1e-12);
      if (g2 < floor2) g2 = floor2;
      const g = Math.sqrt(g2);

      // Asymmetric smoothing: open quickly onto speech, close gently.
      const prev = this.gain[b];
      this.gain[b] = g > prev ? 0.4 * prev + 0.6 * g : 0.85 * prev + 0.15 * g;
    }
    this.primed = true;

    // Apply the gains, mirroring the negative-frequency half so the inverse
    // transform stays real.
    for (let b = 0; b <= HALF; b++) {
      const g = this.gain[b];
      this.re[b] *= g;
      this.im[b] *= g;
      if (b > 0 && b < HALF) {
        this.re[N - b] *= g;
        this.im[N - b] *= g;
      }
    }
    fft(this.re, this.im, true);

    // Windowed overlap-add. Hann on both analysis and synthesis sums to 1.5 at
    // 75% overlap, so undo that here — otherwise switching the denoiser on also
    // turns you up by 3.5 dB, which is not what the control claims to do.
    for (let i = 0; i < N; i++) this.outBuf[i] += this.re[i] * win[i] * WOLA;
    outCh.set(this.outBuf.subarray(0, HOP));
    this.outBuf.copyWithin(0, HOP);
    this.outBuf.fill(0, N - HOP);

    // Mono in, same signal out of every channel.
    for (let c = 1; c < output.length; c++) output[c].set(outCh);
    return true;
  }
}

registerProcessor("concord-denoise", Denoiser);
`;

let moduleURL = "";
const loaded = new WeakSet(); // AudioContexts that already have the module

// canDenoise: AudioWorklet is required. Every current Chromium and WebKit has
// it; anything older simply keeps the browser's own suppression and no more.
export const canDenoise = () =>
  typeof AudioWorklet !== "undefined" && typeof AudioWorkletNode !== "undefined";

// loadDenoiser registers the processor on a context. Safe to call repeatedly.
export async function loadDenoiser(ctx) {
  if (!canDenoise() || !ctx?.audioWorklet) return false;
  if (loaded.has(ctx)) return true;
  if (!moduleURL) {
    moduleURL = URL.createObjectURL(new Blob([WORKLET_SRC], { type: "application/javascript" }));
  }
  try {
    await ctx.audioWorklet.addModule(moduleURL);
    loaded.add(ctx);
    return true;
  } catch (err) {
    console.warn("denoise worklet unavailable", err);
    return false;
  }
}

// Strengths the UI offers. "off" leaves the signal completely untouched — the
// processor short-circuits rather than running a transform at gain 1.
export const NR_LEVELS = [
  { id: "", label: "Off", value: 0, hint: "Only the browser's own suppression" },
  { id: "low", label: "Low", value: 0.35, hint: "Takes the edge off a quiet room" },
  { id: "medium", label: "Medium", value: 0.6, hint: "Fans, air conditioning, mic hiss" },
  { id: "high", label: "High", value: 0.85, hint: "Loud rooms — may thin your voice" },
];
export const nrValue = (id) => NR_LEVELS.find((l) => l.id === (id || ""))?.value ?? 0;

// makeDenoiseNode builds the node, or null when unavailable. Callers treat null
// as "just don't insert it" so every path degrades to the old behavior.
export function makeDenoiseNode(ctx, strength) {
  if (!loaded.has(ctx)) return null;
  try {
    const node = new AudioWorkletNode(ctx, "concord-denoise", {
      numberOfInputs: 1,
      numberOfOutputs: 1,
      outputChannelCount: [1],
    });
    node.parameters.get("strength").value = strength;
    return node;
  } catch (err) {
    console.warn("denoise node", err);
    return null;
  }
}
