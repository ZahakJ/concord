// Tests for the passphrase strength readout. The bar is a promise, so the
// properties that matter are ORDERING ones: things that are genuinely harder to
// guess must never score below things that are easier.
import { strength, bitsOf, alphabetSize } from "./passphrase.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

// ---- nothing typed says nothing ----
const empty = strength("");
assert(empty.level === 0 && empty.label === "" && empty.bits === 0, "empty is silent");
assert(strength(null).bits === 0, "null is empty");

// ---- alphabet inference ----
assert(alphabetSize("abc") === 26, `lower only: ${alphabetSize("abc")}`);
assert(alphabetSize("abC") === 52, `mixed case: ${alphabetSize("abC")}`);
assert(alphabetSize("abC1") === 62, `plus digits: ${alphabetSize("abC1")}`);
assert(alphabetSize("abC1!") === 94, `plus symbols: ${alphabetSize("abC1!")}`);

// ---- length beats punctuation soup ----
// This is the whole reason the meter exists: it must not teach people that
// "P@ssw0rd!" is the goal.
const soup = strength("P@ssw0rd!");
const words = strength("correct horse battery staple");
assert(
  words.level > soup.level,
  `four words (${words.level}/${words.bits}b) must beat punctuation soup (${soup.level}/${soup.bits}b)`,
);
assert(words.level >= 3, `four words should reach at least Strong, got ${words.level}`);

// ---- repetition is not length ----
const repeated = strength("aaaaaaaaaaaaaaaaaaaaaaaa");
const varied = strength("thequickbrownfoxjumpsover");
assert(
  repeated.bits < varied.bits,
  `one letter repeated (${repeated.bits}b) must score under real text (${varied.bits}b)`,
);
assert(repeated.level <= 2, `a repeated character must not read as strong: ${repeated.level}`);

// ---- monotonic in length, for the same character mix ----
let prev = -1;
for (const n of [4, 8, 12, 16, 24, 32]) {
  const b = bitsOf("abcdefghijklmnopqrstuvwxyz".slice(0, n) + "");
  assert(b > prev, `bits must rise with length at ${n}: ${b} vs ${prev}`);
  prev = b;
}

// ---- the bar is bounded and never zero-width once something is typed ----
for (const s of ["a", "hunter2", "correct horse battery staple", "x".repeat(400)]) {
  const r = strength(s);
  assert(r.percent >= 3 && r.percent <= 100, `percent in range for ${JSON.stringify(s.slice(0, 12))}: ${r.percent}`);
  assert(r.level >= 0 && r.level <= 4, `level in range: ${r.level}`);
  assert(typeof r.label === "string" && r.label.length > 0, `a typed passphrase gets a label: ${r.label}`);
}

// ---- no gate: every band is reachable, and nothing is ever rejected ----
const seen = new Set(
  ["a", "hunter2", "hunter2hunter2", "correct horse battery", "correct horse battery staple xylophone"].map(
    (s) => strength(s).level,
  ),
);
assert(seen.size >= 3, `the bands should actually spread out, saw ${[...seen].sort().join(",")}`);

if (failures) {
  console.error(`${failures} failure(s)`);
  process.exit(1);
}
console.log("passphrase: all tests pass");
