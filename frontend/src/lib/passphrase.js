// passphrase.js — how strong is this passphrase, honestly.
//
// A Concord passphrase is the only thing between someone and their account, and
// there is no reset link behind it. That makes a strength readout worth having
// — and it makes an unearned one actively harmful, because a green bar is a
// promise. So this scores what actually matters and says what it thinks in
// plain words.
//
// It sets NO minimum and blocks nothing. The field it decorates already accepts
// whatever is typed, deliberately: a floor imposed here would strand every
// account whose passphrase was chosen before the floor existed, and the app has
// no way to ask those people to change it. This is a mirror, not a gate.
//
// The model is a rough entropy estimate, not a dictionary check:
//   • length dominates, because for a passphrase it genuinely does — four
//     common words beat eight characters of punctuation soup, and pretending
//     otherwise is what trains people to write P@ssw0rd!;
//   • character variety adds a little, and only a little;
//   • repetition and a tiny alphabet take it away, so "aaaaaaaaaaaaaaaa" does
//     not score as sixteen characters of anything.
//
// Bits are a fiction we are honest about: the alphabet size is inferred from
// what was actually typed, which is what an attacker who knows the shape would
// use, and no attempt is made to detect that "correcthorse" is two words.

const LOWER = /[a-z]/;
const UPPER = /[A-Z]/;
const DIGIT = /[0-9]/;
const SYMBOL = /[^a-zA-Z0-9]/;

// alphabetSize: the pool an attacker would search given the classes present.
// Deliberately conservative — the symbol pool is counted as the ~32 ASCII
// punctuation characters, not as all of Unicode, because a passphrase with one
// emoji in it is not astronomically strong and should not claim to be.
export function alphabetSize(s) {
  let n = 0;
  if (LOWER.test(s)) n += 26;
  if (UPPER.test(s)) n += 26;
  if (DIGIT.test(s)) n += 10;
  if (SYMBOL.test(s)) n += 32;
  return n || 1;
}

// bitsOf: log2(alphabet) per character, discounted for how much of the string
// is actually distinct. A sixteen-character string built from three different
// letters is not sixteen characters of search space, and the ratio of distinct
// characters to length is a cheap, undramatic way of saying so.
export function bitsOf(input) {
  const s = String(input || "");
  if (!s) return 0;
  const distinct = new Set(s).size;
  // Ranges from 0.45 (one character repeated) to 1 (every character different).
  const variety = 0.45 + 0.55 * Math.min(1, distinct / Math.max(8, s.length / 2));
  return effectiveLength(s, distinct) * Math.log2(alphabetSize(s)) * variety;
}

// effectiveLength: how many characters of this string a guesser actually has
// to find. The variety discount alone multiplied the raw length, so length
// always won: 600 × "x" scored Excellent. Repetition is the guesser's first
// try, not extra work, so a string that is one short unit over and over counts
// as that unit plus a little for the count, and no string counts as more than
// three characters per distinct character in it.
export function effectiveLength(s, distinct = new Set(s).size) {
  const chars = [...s];
  const n = chars.length;
  let len = n;
  for (let p = 1; p <= n / 2; p++) {
    let periodic = true;
    for (let i = p; i < n; i++) {
      if (chars[i] !== chars[i - p]) {
        periodic = false;
        break;
      }
    }
    if (periodic) {
      len = p + Math.log2(n / p);
      break;
    }
  }
  return Math.min(len, distinct * 3);
}

// normalizePassphrase: the form the core seals a passphrase under (NFKC, see
// internal/identity NormalizePassphrase). Two fields showing the same text can
// hold different code points — a composed "é" in one, "e" plus an accent in
// the other — and comparing the raw strings told people their matching
// passphrases did not match.
export function normalizePassphrase(p) {
  return String(p ?? "").normalize("NFKC");
}

export function samePassphrase(a, b) {
  return normalizePassphrase(a) === normalizePassphrase(b);
}

// The bands. Named for what they mean to the person reading them, and pitched
// so that the thing we actually want — a few unrelated words — lands in the top
// two without needing a symbol in it.
const BANDS = [
  { min: 0, level: 0, label: "Too short to protect anything" },
  { min: 40, level: 1, label: "Weak — a determined guess would get there" },
  { min: 60, level: 2, label: "Fair — fine unless someone is really trying" },
  { min: 85, level: 3, label: "Strong" },
  { min: 110, level: 4, label: "Excellent" },
];

// strength returns { level: 0-4, label, bits, percent } for a passphrase.
// An empty string is level 0 with an empty label — there is nothing to say yet,
// and a red "too short" under a field nobody has typed in is a scolding.
export function strength(input) {
  const s = String(input || "");
  const bits = bitsOf(s);
  if (!s) return { level: 0, label: "", bits: 0, percent: 0 };
  let band = BANDS[0];
  for (const b of BANDS) if (bits >= b.min) band = b;
  return {
    level: band.level,
    label: band.label,
    bits: Math.round(bits),
    // The bar fills against the top band, so "Excellent" is a full bar rather
    // than an asymptote nobody reaches.
    percent: Math.max(3, Math.min(100, Math.round((bits / 110) * 100))),
  };
}
