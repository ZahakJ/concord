// Tests for the sound-recipe format. The thing being defended is unusual for
// this codebase: the output of a successful decode here is a set of numbers
// that reach a live speaker. So the interesting cases are all rejections, and
// the rule they prove is REJECT, DO NOT CLAMP — a recipe outside any bound
// plays nothing at all, rather than playing a politely resized version of
// whatever a stranger asked for.
import {
  encodeRecipe,
  decodeRecipe,
  encodeSound,
  parseSound,
  stripSound,
  soundPayload,
  validRecipe,
  recipeTotalMs,
  recipeGlyph,
  STARTER_SHELF,
  SFX_FIELDS,
  SFX_GLYPHS,
  SFX_WAVES,
  MAX_NAME_BYTES,
  MAX_TOKEN_CHARS,
  MAX_TOTAL_MS,
  FLAG_EXP,
  FLAG_SWELL,
} from "./sfxrecipe.js";

let failures = 0;
// Key order is not part of the format — a decoded recipe is built field by
// field in the decoder's own order, and comparing it to a literal written in a
// different one would fail for no reason.
function stable(v) {
  return JSON.stringify(v, (_, x) =>
    x && typeof x === "object" && !Array.isArray(x)
      ? Object.fromEntries(Object.keys(x).sort().map((k) => [k, x[k]]))
      : x,
  );
}
function check(name, got, want) {
  const g = stable(got);
  const w = stable(want);
  if (g !== w) {
    failures++;
    console.error(`FAIL ${name}\n  got:  ${g}\n  want: ${w}`);
  }
}

const base = { ...STARTER_SHELF[0] };
const with_ = (over) => ({ ...base, ...over });

// A recipe forged byte by byte, so a field can be pushed past its cap. The
// encoder refuses to build one of these, which is the point: the only way an
// out-of-range recipe reaches a client is from something that is not this
// encoder.
function forge(r) {
  const name = Buffer.from(r.name ?? "", "utf8");
  const b = Buffer.alloc(25 + name.length);
  b[0] = r.version ?? 1;
  b[1] = r.wave ?? 0;
  b.writeUInt16BE(r.f0 ?? 440, 2);
  b.writeUInt16BE(r.f1 ?? 440, 4);
  b.writeUInt16BE(r.attack ?? 8, 6);
  b.writeUInt16BE(r.dur ?? 200, 8);
  b.writeUInt16BE(r.gain ?? 120, 10);
  b[12] = r.noise ?? 0;
  b.writeUInt16BE(r.noiseHz ?? 1000, 13);
  b[15] = r.noiseQ ?? 4;
  b[16] = r.reps ?? 1;
  b.writeUInt16BE(r.gap ?? 0, 17);
  b[19] = r.detune ?? 0;
  b[20] = r.room ?? 20;
  b[21] = r.flags ?? 0;
  b[22] = (r.step ?? 0) & 0xff;
  b[23] = r.glyph ?? 0;
  b[24] = r.nameLen == null ? name.length : r.nameLen;
  name.copy(b, 25);
  return b.toString("base64url");
}

// ---- round trip -------------------------------------------------------------

{
  const r = with_({ name: "Honk", glyph: 1, wave: 3, f0: 415, f1: 466, detune: 8, dur: 750, gain: 110, flags: FLAG_EXP | FLAG_SWELL });
  check("round trip is exact", decodeRecipe(encodeRecipe(r)), r);
}
check("a negative step survives", decodeRecipe(encodeRecipe(with_({ step: -12 })))?.step, -12);
check("the most positive step survives", decodeRecipe(encodeRecipe(with_({ step: 24 })))?.step, 24);
check("an empty name is legal", decodeRecipe(encodeRecipe(with_({ name: "" })))?.name, "");
// Names are UTF-8 and counted in BYTES, not characters — otherwise a name in
// Arabic or with an emoji in it would overrun the length byte's promise.
check("a non-ASCII name survives", decodeRecipe(encodeRecipe(with_({ name: "طَنين 🔔" })))?.name, "طَنين 🔔");

// Every starter preset encodes, decodes back to itself and fits its budget.
{
  let worst = 0;
  for (const p of STARTER_SHELF) {
    const payload = encodeRecipe(p);
    if (!payload) {
      failures++;
      console.error(`FAIL starter preset "${p.name}" does not encode`);
      continue;
    }
    worst = Math.max(worst, payload.length);
    check(`starter "${p.name}" round-trips`, decodeRecipe(payload), p);
    check(`starter "${p.name}" fits the time budget`, recipeTotalMs(p) <= MAX_TOTAL_MS, true);
  }
  const bytes = STARTER_SHELF.map((p) => Buffer.from(encodeRecipe(p), "base64url").length);
  console.log(
    `sfxrecipe.js: ${STARTER_SHELF.length} starter sounds — ${Math.min(...bytes)}-${Math.max(...bytes)}B each ` +
      `(${(bytes.reduce((a, b) => a + b, 0) / bytes.length).toFixed(1)}B mean), longest token ${worst} chars`,
  );
  check("a sound is tens of bytes, not thousands", Math.max(...bytes) < 64, true);
  check("names are distinct", new Set(STARTER_SHELF.map((p) => p.name)).size, STARTER_SHELF.length);
}

// ---- the token wrapper ------------------------------------------------------

{
  const tok = encodeSound(with_({ name: "Boop" }));
  check("token shape", /^\[sound\]\(concord:\/\/sfx\/v1\/[A-Za-z0-9_-]+\)$/.test(tok), true);
  check("parses out of a body with words", parseSound(`listen ${tok}`)?.name, "Boop");
  check("the payload can be recovered for the shelf", decodeRecipe(soundPayload(`listen ${tok}`))?.name, "Boop");
  check("strip leaves the words", stripSound(`listen ${tok}`), "listen");
  // As with doodles: the strip is unconditional in the render path, so a
  // REFUSED sound must also leave nothing behind.
  check("strip removes a token this client refused", stripSound("hi [sound](concord://sfx/v1/AAAA)"), "hi");
  check("an unencodable recipe is not a token", encodeSound(with_({ gain: 9000 })), "");
}

// ---- REJECT, DO NOT CLAMP ---------------------------------------------------

// The headline case. An unclamped gain is a weapon; a clamped one is a weapon
// that was politely resized, and still a noise a stranger caused in your room.
check("a gain far past the ceiling plays nothing", decodeRecipe(forge({ gain: 60000 })), null);
check("a gain one step past the ceiling plays nothing", decodeRecipe(forge({ gain: SFX_FIELDS.gain.max + 1 })), null);
check("the ceiling itself plays", decodeRecipe(forge({ gain: SFX_FIELDS.gain.max }))?.gain, SFX_FIELDS.gain.max);

// One long note.
check("a thirty-second sustain plays nothing", decodeRecipe(forge({ dur: 30000 })), null);
check("one millisecond past the length cap plays nothing", decodeRecipe(forge({ dur: SFX_FIELDS.dur.max + 1 })), null);

// And the trap the per-field caps cannot see: twenty-four legal hits, legally
// spaced, adding up to fourteen seconds.
{
  const long = forge({ dur: 400, reps: 24, gap: 600 });
  check("the forged repeat really is over the total", recipeTotalMs({ dur: 400, reps: 24, gap: 600 }) > MAX_TOTAL_MS, true);
  check("legal fields adding up to an illegal length play nothing", decodeRecipe(long), null);
  check("the same hits packed inside the budget play", decodeRecipe(forge({ dur: 60, reps: 24, gap: 100 })) !== null, true);
}

// Frequencies. Sub-audible and ultrasonic are both refused rather than pinned.
check("a frequency below the floor plays nothing", decodeRecipe(forge({ f0: 1 })), null);
check("a frequency above the ceiling plays nothing", decodeRecipe(forge({ f1: 60000 })), null);
check("noise pitch out of range plays nothing", decodeRecipe(forge({ noiseHz: 30 })), null);
check("noise focus of zero plays nothing", decodeRecipe(forge({ noiseQ: 0 })), null);
check("a noise mix over 100% plays nothing", decodeRecipe(forge({ noise: 200 })), null);
check("an attack longer than allowed plays nothing", decodeRecipe(forge({ attack: 5000 })), null);
check("more hits than allowed play nothing", decodeRecipe(forge({ reps: 200 })), null);
check("zero hits play nothing", decodeRecipe(forge({ reps: 0 })), null);
check("a gap past its cap plays nothing", decodeRecipe(forge({ gap: 5000 })), null);
check("detune past its cap plays nothing", decodeRecipe(forge({ detune: 250 })), null);
check("room past 100% plays nothing", decodeRecipe(forge({ room: 200 })), null);
check("a step past two octaves plays nothing", decodeRecipe(forge({ step: 100 })), null);
check("a step below minus two octaves plays nothing", decodeRecipe(forge({ step: -100 })), null);

// Table lookups. The safety is the lookup failing closed, not the list being
// exhaustive — the rule validEffect was reopened on.
check("a waveform with no entry plays nothing", decodeRecipe(forge({ wave: SFX_WAVES.length })), null);
check("a face with no entry plays nothing", decodeRecipe(forge({ glyph: SFX_GLYPHS.length })), null);
check("an unknown flag bit plays nothing", decodeRecipe(forge({ flags: 128 })), null);
check("a face at the last entry plays", decodeRecipe(forge({ glyph: SFX_GLYPHS.length - 1 }))?.glyph, SFX_GLYPHS.length - 1);

// Structure.
check("a version we did not write plays nothing", decodeRecipe(forge({ version: 7 })), null);
check("garbage plays nothing", decodeRecipe("!!!!"), null);
check("empty plays nothing", decodeRecipe(""), null);
check("a non-string plays nothing", decodeRecipe({}), null);
check("a payload over the character cap is refused unread", decodeRecipe("A".repeat(MAX_TOKEN_CHARS + 1)), null);
check("a truncated recipe plays nothing", decodeRecipe(forge({}).slice(0, 12)), null);
check("a name length that lies plays nothing", decodeRecipe(forge({ name: "abc", nameLen: 30 })), null);
check("trailing bytes play nothing", decodeRecipe(forge({ name: "abc", nameLen: 1 })), null);
check("a name over the byte cap plays nothing", decodeRecipe(forge({ name: "x".repeat(MAX_NAME_BYTES + 1) })), null);

// A name is a label in somebody else's message list.
check("a control character in a name plays nothing", decodeRecipe(forge({ name: "honk" })), null);
check("a newline in a name plays nothing", decodeRecipe(forge({ name: "a\nb" })), null);
{
  // Bytes that are not valid UTF-8 at all: a decoder that substituted
  // replacement characters would turn these into a row of question marks in
  // every reader's feed.
  const b = Buffer.alloc(25 + 3);
  b[0] = 1;
  b.writeUInt16BE(440, 2);
  b.writeUInt16BE(440, 4);
  b.writeUInt16BE(8, 6);
  b.writeUInt16BE(200, 8);
  b.writeUInt16BE(120, 10);
  b.writeUInt16BE(1000, 13);
  b[15] = 4;
  b[16] = 1;
  b[20] = 20;
  b[24] = 3;
  b[25] = 0xff;
  b[26] = 0xfe;
  b[27] = 0xfd;
  check("a name that is not UTF-8 plays nothing", decodeRecipe(b.toString("base64url")), null);
}

// ---- the encoder refuses too ------------------------------------------------
// The studio drives its sliders from SFX_FIELDS, so it cannot BUILD a recipe
// the decoder would reject; this proves the encoder agrees rather than trusting
// the UI to be careful.
check("the encoder refuses an out-of-range gain", encodeRecipe(with_({ gain: 9000 })), "");
check("the encoder refuses a too-long total", encodeRecipe(with_({ dur: 400, reps: 24, gap: 600 })), "");
check("the encoder refuses a name over the byte cap", encodeRecipe(with_({ name: "x".repeat(MAX_NAME_BYTES + 1) })), "");
check("the encoder counts name BYTES, not characters", encodeRecipe(with_({ name: "🔔".repeat(11) })), "");
check("the encoder refuses a non-integer", encodeRecipe(with_({ dur: 200.5 })), "");
check("validRecipe agrees with the encoder", validRecipe(with_({ gain: 9000 })), false);
check("validRecipe accepts every starter", STARTER_SHELF.every(validRecipe), true);
check("validRecipe refuses nothing at all", validRecipe(null), false);

// ---- small helpers ----------------------------------------------------------

check("total time counts the gaps between hits", recipeTotalMs({ dur: 100, reps: 4, gap: 50 }), 250);
check("a single hit is its own length", recipeTotalMs({ dur: 100, reps: 1, gap: 999 }), 100);
check("a glyph resolves", recipeGlyph({ glyph: 1 }), SFX_GLYPHS[1]);
check("an unknown glyph falls back rather than printing undefined", recipeGlyph({ glyph: 999 }), SFX_GLYPHS[0]);
check("a missing recipe still has a face", recipeGlyph(null), SFX_GLYPHS[0]);

if (failures) {
  console.error(`\n${failures} sfxrecipe test(s) failed`);
  process.exit(1);
}
console.log("sfxrecipe.js: all tests passed");
