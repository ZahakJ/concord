// Alert words, held to the rule that decides whether people keep them on
// (`npm test`).
//
// The matcher exists twice — here for the live render path and in
// internal/app/inbox.go for the scan over history — so the cases below are
// deliberately the same cases the Go test uses. If the two ever disagree, a
// message highlights in the feed with no inbox entry to go with it, or the other
// way round, and the feature stops being trustworthy.
import {
  normalize,
  addWord,
  removeWord,
  rejectReason,
  matchedWord,
  hasAlertWord,
  MAX_WORDS,
  MAX_LEN,
} from "./alertwords.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const eq = (got, want, what) =>
  assert(
    JSON.stringify(got) === JSON.stringify(want),
    `${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`,
  );

// ---- whole-word edges ------------------------------------------------------

const cases = [
  ["the release is broken", "release", true],
  ["Release is broken", "release", true],
  ["RELEASE", "release", true],
  ["pre-release notes", "release", true],
  ["(release)", "release", true],
  ["release, then", "release", true],
  ["prerelease notes", "release", false],
  ["releases", "release", false],
  ["unreleased", "release", false],
  ["concatenate", "cat", false],
  ["the cat sat", "cat", true],
  ["cat", "cat", true],
  ["a cat.", "cat", true],
  ["scatter", "cat", false],
  ["", "cat", false],
  ["ship it", "ship it", true],
  ["we should ship it now", "ship it", true],
  ["shipping it", "ship it", false],
];
for (const [text, word, want] of cases) {
  assert(hasAlertWord(text, [word]) === want, `"${word}" in "${text}" should be ${want}`);
}

// Unicode edges. A boundary is a character class, not a byte.
const uni = [
  ["نشر الإصدار اليوم", "الإصدار", true],
  ["الإصدارات", "الإصدار", false],
  ["выпуск готов", "выпуск", true],
  ["выпускной", "выпуск", false],
  ["日本語 テスト", "テスト", true],
  ["emoji 🎉 party", "party", true],
  // An emoji is neither a letter nor a digit, so it counts as an edge. Stated,
  // not assumed — the Go test pins the same behaviour.
  ["🎉party", "party", true],
];
for (const [text, word, want] of uni) {
  assert(hasAlertWord(text, [word]) === want, `unicode: "${word}" in "${text}" should be ${want}`);
}

// The word that matched is reported, because the row has to be able to say why.
eq(matchedWord("the release shipped", ["nope", "release"]), "release", "reports which word");
eq(matchedWord("nothing here", ["release"]), "", "no match reports nothing");
eq(matchedWord("anything", []), "", "an empty list matches nothing");
eq(matchedWord("anything", null), "", "a missing list matches nothing");
eq(matchedWord(null, ["x"]), "", "missing text matches nothing");

// A pathological body must not send the scan quadratic-looking or backtracking:
// this runs on the render path for every message on screen.
{
  const body = "a".repeat(50000);
  const t0 = performance.now();
  const got = matchedWord(body, ["aa", "ab", "ac"]);
  const ms = performance.now() - t0;
  eq(got, "", "no whole-word match inside one long run");
  assert(ms < 500, `50,000 characters against three words took ${ms.toFixed(1)}ms`);
  console.log("  50,000-character body against 3 words: %sms", ms.toFixed(2));
}

// ---- the list itself -------------------------------------------------------

eq(normalize(null), [], "nothing normalises to nothing");
eq(normalize("release"), [], "a string is not a list");
eq(normalize(["  Release ", "release", "RELEASE"]), ["release"], "trims, folds, dedupes");
eq(normalize(["x", "", "  ", "ok"]), ["ok"], "drops what cannot be matched");
eq(normalize([{}, 5, "ok"]), ["ok"], "survives junk in storage");
assert(normalize(new Array(200).fill(0).map((_, i) => `w${i}`)).length === MAX_WORDS, "caps the list");
assert(normalize(["a".repeat(MAX_LEN + 1)]).length === 0, "drops an over-long word");

eq(addWord([], "Release"), ["release"], "adds folded");
eq(addWord(["release"], "release"), ["release"], "a duplicate is a no-op");
eq(addWord(["release"], " RELEASE "), ["release"], "a duplicate after folding is a no-op");
eq(addWord([], "x"), [], "too short is a no-op");
// A no-op returns the SAME array, which is how the caller tells added from
// rejected without a second validation that could disagree with this one.
{
  const list = ["release"];
  assert(addWord(list, "release") === list, "a rejected add returns the same array");
  assert(addWord(list, "ship") !== list, "an accepted add returns a new array");
}
eq(removeWord(["a1", "b2"], "a1"), ["b2"], "removes");
eq(removeWord(["a1"], "nope"), ["a1"], "removing what is not there changes nothing");

eq(rejectReason([], ""), "", "an empty field is not an error");
assert(rejectReason([], "x").includes("two characters"), "too short says so");
assert(rejectReason(["ok"], "ok").includes("already"), "duplicate says so");
assert(rejectReason([], "a".repeat(MAX_LEN + 1)).includes(String(MAX_LEN)), "too long says so");
assert(
  rejectReason(new Array(MAX_WORDS).fill(0).map((_, i) => `w${i}`), "new").includes(String(MAX_WORDS)),
  "a full list says so",
);

if (failures) {
  console.error(`\n${failures} alertwords test(s) failed`);
  process.exit(1);
}
console.log("alertwords.test.mjs: all checks passed");
