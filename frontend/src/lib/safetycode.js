// The safety-number QR: what two people show each other to verify without
// reading 32 characters aloud.
//
// The payload is a version tag and the two account fingerprints, sorted, and
// nothing else — no names, no addresses — so both sides render the SAME code
// and a stranger who photographs it learns only what the fingerprints already
// say. The version tag is what lets the format change without an old build
// reading a new code as a match.
const TAG = "concord-verify";
const VERSION = "1";

// Spacing and case are display; the identity is the 32 base32 characters.
export const normFpr = (f) => String(f || "").replace(/\s+/g, "").toUpperCase();

export function encodeSafetyCode(mine, theirs) {
  const pair = [normFpr(mine), normFpr(theirs)];
  if (!pair[0] || !pair[1] || pair[0] === pair[1]) return "";
  pair.sort();
  return `${TAG}/${VERSION}/${pair[0]}/${pair[1]}`;
}

// -> { fprs: [a, b] } for a well-formed code of a version we know, else null.
export function parseSafetyCode(text) {
  const parts = String(text || "").trim().split("/");
  if (parts.length !== 4 || parts[0] !== TAG || parts[1] !== VERSION) return null;
  const [a, b] = [parts[2], parts[3]];
  if (!/^[A-Z2-7]+$/.test(a) || !/^[A-Z2-7]+$/.test(b) || a >= b) return null;
  return { fprs: [a, b] };
}

// What a scan means for the contact on screen:
//   "match"    — the code names exactly us and them
//   "mismatch" — a well-formed code, but for different people
//   "invalid"  — not a Concord safety code at all
export function checkSafetyCode(text, mine, theirs) {
  const got = parseSafetyCode(text);
  if (!got) return "invalid";
  const want = encodeSafetyCode(mine, theirs);
  return want && want === `${TAG}/${VERSION}/${got.fprs[0]}/${got.fprs[1]}` ? "match" : "mismatch";
}
