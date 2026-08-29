// Tests for the half of the soundboard that decides what reaches a speaker.
//
// sounds.js is otherwise WebAudio plumbing that only means anything in a
// browser, so this stubs the one API it touches and asserts on the GRAPH the
// recipe renderer builds: that a legal recipe builds one, that a refused
// recipe builds nothing at all, and — the containment claim — that no
// scheduled gain anywhere in it can exceed the ceiling the format promises.
//
// The precedent is postdraft.test.mjs, which stubs localStorage because that
// is the only browser API its module touches. This needs two.

import { encodeRecipe, STARTER_SHELF, SFX_FIELDS } from "./sfxrecipe.js";

let failures = 0;
function check(name, got, want) {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    failures++;
    console.error(`FAIL ${name}\n  got:  ${JSON.stringify(got)}\n  want: ${JSON.stringify(want)}`);
  }
}

// ---- a recording AudioContext ----------------------------------------------

const store = new Map();
globalThis.localStorage = {
  getItem: (k) => (store.has(k) ? store.get(k) : null),
  setItem: (k, v) => store.set(k, String(v)),
  removeItem: (k) => store.delete(k),
};

let log;
function reset() {
  log = { osc: [], noise: 0, gains: [], freqs: [], connectedToDestination: 0 };
}
reset();

// Every scheduled value on every gain is recorded, because the ceiling has to
// hold for the ENVELOPE, not merely for the number in the recipe: a peak the
// renderer multiplies up somewhere would be exactly the bug this is here for.
function fakeParam(sink) {
  const note = (v) => sink.push(v);
  return {
    value: 0,
    setValueAtTime: (v) => note(v),
    linearRampToValueAtTime: (v) => note(v),
    exponentialRampToValueAtTime: (v) => note(v),
  };
}

class FakeContext {
  constructor() {
    this.state = "running";
    this.currentTime = 0;
    this.sampleRate = 8000; // small: the impulse response is generated for real
    this.destination = { __destination: true, connect: () => {} };
  }
  _node(extra = {}) {
    const n = { connect: (t) => { if (t && t.__destination) log.connectedToDestination++; return t; }, disconnect: () => {}, ...extra };
    return n;
  }
  createGain() {
    const vals = [];
    log.gains.push(vals);
    return this._node({ gain: fakeParam(vals) });
  }
  createOscillator() {
    const fs = [];
    log.osc.push(fs);
    return this._node({
      type: "sine",
      detune: { value: 0 },
      frequency: fakeParam(fs),
      start: () => {},
      stop: () => {},
    });
  }
  createBufferSource() {
    log.noise++;
    return this._node({ buffer: null, start: () => {}, stop: () => {} });
  }
  createBiquadFilter() {
    return this._node({ type: "lowpass", frequency: { value: 0 }, Q: { value: 0 } });
  }
  createConvolver() {
    return this._node({ buffer: null });
  }
  createDynamicsCompressor() {
    return this._node({
      threshold: { value: 0 }, knee: { value: 0 }, ratio: { value: 0 },
      attack: { value: 0 }, release: { value: 0 },
    });
  }
  createStereoPanner() {
    return this._node({ pan: fakeParam([]) });
  }
  createBuffer(ch, len, rate) {
    const data = Array.from({ length: ch }, () => new Float32Array(len));
    return { numberOfChannels: ch, length: len, sampleRate: rate, getChannelData: (i) => data[i] };
  }
  close() {
    return Promise.resolve();
  }
  resume() {
    return Promise.resolve();
  }
}

globalThis.window = { AudioContext: FakeContext };

const { playRecipe, playSfxTrigger, soundboardEnabled, setSoundboardEnabled, SOUNDBOARD } = await import("./sounds.js");

const peakGain = () => Math.max(0, ...log.gains.flat());
// playRecipe refuses a second sound inside 220ms so a room full of people
// pressing at once stays a sound rather than a wall. Tests that want a fresh
// press have to wait it out, which is also a check that the window is real.
const settle = () => new Promise((r) => setTimeout(r, 260));
// The one number the format promises: SFX_FIELDS.gain is in permille, so its
// ceiling of 250 is 0.25 — well under 1.0, and above the loudest built-in
// (0.19) only because a sound somebody built on purpose should be able to be
// the loudest thing on the board.
const CEILING = SFX_FIELDS.gain.max / 1000;

// ---- a legal recipe builds a graph ------------------------------------------

for (const preset of STARTER_SHELF) {
  reset();
  const played = playRecipe(preset, { force: true });
  const nodes = log.osc.length + log.noise;
  if (!played || nodes === 0) {
    failures++;
    console.error(`FAIL "${preset.name}" built no audio graph (played=${played}, nodes=${nodes})`);
  }
  if (peakGain() > CEILING + 1e-9) {
    failures++;
    console.error(`FAIL "${preset.name}" scheduled ${peakGain()} — over the ${CEILING} ceiling`);
  }
}
console.log(`sounds.js: ${STARTER_SHELF.length} starter sounds render, peak scheduled gain never above ${CEILING}`);

// A recipe with more hits builds proportionally more sources — the repeats are
// real, not a single node with a longer envelope.
{
  reset();
  playRecipe({ ...STARTER_SHELF[0], reps: 1 }, { force: true });
  const one = log.osc.length;
  reset();
  playRecipe({ ...STARTER_SHELF[0], reps: 6, gap: 50 }, { force: true });
  check("six hits build six times the sources", log.osc.length, one * 6);
}

// Detune spreads the voice into three, which is what makes a sawtooth honk.
{
  reset();
  playRecipe({ ...STARTER_SHELF[0], detune: 0 }, { force: true });
  const plain = log.osc.length;
  reset();
  playRecipe({ ...STARTER_SHELF[0], detune: 9 }, { force: true });
  check("detune triples the voices", log.osc.length, plain * 3);
  // …and splits the level between them rather than adding three at full peak.
  check("the three voices stay under the ceiling", peakGain() <= CEILING + 1e-9, true);
}

// A pure-noise recipe makes no oscillators at all, and a pure-tone one makes
// no noise sources — otherwise the mix parameter is decoration.
{
  reset();
  playRecipe({ ...STARTER_SHELF[0], noise: 100 }, { force: true });
  check("all noise means no oscillators", log.osc.length, 0);
  check("all noise means a buffer source", log.noise > 0, true);
  reset();
  playRecipe({ ...STARTER_SHELF[0], noise: 0 }, { force: true });
  check("no noise means no buffer sources", log.noise, 0);
}

// ---- nothing legal, nothing played ------------------------------------------

reset();
check("a refused recipe plays nothing", playRecipe(null), false);
check("…and builds no graph", log.osc.length + log.noise, 0);

// playSfxTrigger is the receive-side resolver: a built-in id, a recipe payload,
// or silence. Both halves are LOOKUPS, and a lookup that fails is the whole
// safety story — which is also exactly what a client one build behind does
// with a recipe payload it has never seen.
reset();
playSfxTrigger("airhorn");
check("a built-in id renders", log.osc.length > 0, true);
check("the built-in table is the one the UI shows", SOUNDBOARD.map((s) => s.id).includes("airhorn"), true);

await settle();
reset();
playSfxTrigger(encodeRecipe(STARTER_SHELF[3]));
check("a recipe payload renders", log.osc.length + log.noise > 0, true);

reset();
playSfxTrigger("an-effect-from-the-future");
check("an id with no entry renders nothing", log.osc.length + log.noise, 0);

reset();
// A forged payload over the gain ceiling: it decodes to null, so nothing is
// built. This is the end of the chain the recipe tests start — refusing at the
// format is only worth anything if the renderer never sees the numbers.
{
  const b = Buffer.alloc(25);
  b[0] = 1;
  b.writeUInt16BE(440, 2);
  b.writeUInt16BE(440, 4);
  b.writeUInt16BE(8, 6);
  b.writeUInt16BE(200, 8);
  b.writeUInt16BE(60000, 10); // gain 60.0
  b.writeUInt16BE(1000, 13);
  b[15] = 4;
  b[16] = 1;
  b[20] = 20;
  playSfxTrigger(b.toString("base64url"));
  check("a forged 60x gain builds no graph at all", log.osc.length + log.noise, 0);
}

// ---- the mute --------------------------------------------------------------

check("the soundboard starts on", soundboardEnabled(), true);
setSoundboardEnabled(false);
reset();
check("muted, a press plays nothing", playRecipe(STARTER_SHELF[0]), false);
check("…and builds no graph", log.osc.length + log.noise, 0);
reset();
playSfxTrigger("airhorn");
check("muted, a built-in plays nothing either", log.osc.length, 0);
reset();
check("the studio's own preview still plays while muted", playRecipe(STARTER_SHELF[0], { force: true }), true);
setSoundboardEnabled(true);
check("the setting persists", store.get("concord.soundboard"), "on");

// Two presses in the same instant are one sound, not two — a shared room can
// have several people pressing at once and the output has to stay a sound.
await settle();
reset();
check("the first press plays", playRecipe(STARTER_SHELF[0]), true);
check("a press in the same instant is dropped", playRecipe(STARTER_SHELF[1]), false);
await settle();
check("…and plays again once the window has passed", playRecipe(STARTER_SHELF[1]), true);

// ---- can this machine make a sound at all? ----------------------------------
//
// Two failures that used to be indistinguishable from "the app is muted".
// A fresh module instance per scenario: the health verdict and the shared
// context are module state, and ?q= is how ESM is asked for a second copy.

// (1) A context that is SUSPENDED when the sound is played — the browser's
// autoplay gate, and the reason the first chime of a session went missing.
// Nothing may be scheduled until resume() settles, and then everything must be.
{
  let resumed;
  class Suspended extends FakeContext {
    constructor() {
      super();
      this.state = "suspended";
    }
    resume() {
      return new Promise((res) => {
        resumed = () => {
          this.state = "running";
          this.currentTime = 0.4;
          res();
        };
      });
    }
  }
  globalThis.window = { AudioContext: Suspended };
  const m = await import("./sounds.js?suspended");
  reset();
  check("a suspended context accepts the sound", m.playRecipe(STARTER_SHELF[0], { force: true }), true);
  check("…and schedules nothing into the stopped clock", log.osc.length + log.noise, 0);
  resumed();
  await new Promise((r) => setTimeout(r, 0));
  check("…then builds the whole sound once it is running", log.osc.length + log.noise > 0, true);
}

// (2) A context that claims to be RUNNING and whose clock never moves: the
// signature of WebKitGTK with no GStreamer audio sink. It must be named, not
// silently endured.
{
  class Deaf extends FakeContext {
    // currentTime stays 0 forever — inherited, and never advanced.
  }
  globalThis.window = { AudioContext: Deaf };
  const m = await import("./sounds.js?deaf");
  check("nothing is claimed before anything is played", m.audioHealth(), "unknown");
  check("…and there is nothing to report", m.audioTrouble(), null);
  const verdict = await m.probeAudioOutput();
  check("a clock that never moves is reported silent", verdict, "silent");
  check("…and audioHealth agrees", m.audioHealth(), "silent");
  const why = m.audioTrouble();
  check("…with a sentence to show somebody", typeof why === "string" && why.length > 20, true);
}

// (3) A context whose clock DOES move is healthy, and says nothing.
{
  class Live extends FakeContext {
    constructor() {
      super();
      const t0 = Date.now();
      Object.defineProperty(this, "currentTime", { get: () => (Date.now() - t0) / 1000 });
    }
  }
  globalThis.window = { AudioContext: Live };
  const m = await import("./sounds.js?live");
  check("a clock that moves is healthy", await m.probeAudioOutput(), "ok");
  check("…and a healthy machine is told nothing", m.audioTrouble(), null);
}

// (4) No Web Audio at all — an old webview, or one built without it.
{
  globalThis.window = {};
  const m = await import("./sounds.js?none");
  check("no AudioContext is not silence, it is unsupported", await m.probeAudioOutput(), "unsupported");
  check("…and it says so", /Web Audio/.test(m.audioTrouble() || ""), true);
}

if (failures) {
  console.error(`\n${failures} sounds test(s) failed`);
  process.exit(1);
}
console.log("sounds.js: all tests passed");
