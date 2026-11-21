// releasenotes.test.mjs — the splash reads the release notes, and picks the
// right ones for the running build.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  parseNotes,
  chooseNotes,
  parseVersion,
  versionOfPath,
  heroFor,
  fineKind,
  inlineRuns,
  plainText,
  firstSentence,
  TAGLINE_WORDS,
} from "./releasenotes.js";

// ---- parsing ---------------------------------------------------------------
const MD = `# Concord 9.9.0

Concord 9.9.0 does three things
in one breath.

A second lead paragraph.

## Calls that ring
<!-- hero: call -->
<!-- headline: Your calls ring through -->
<!-- tagline: A call rings on every device, and **ends** for both. -->
- A direct call **rings**, then
  ends the same way on both sides.
- Second bullet with \`code\`.

## Attaching pictures

- Recent row.

## Security

- It is harder for a hostile peer to confuse which account a device belongs to.

## Known limits

- A moderator's device does not yet know you unlinked it.
- Another limit.

## Before you downgrade

0.99 changes nothing; going back loses nothing.

## Also

- Holding a message puts **Edit** first.
`;

const n = parseNotes(MD, "9.9.0");
assert.equal(n.version, "9.9.0");
assert.equal(n.summary, "Concord 9.9.0 does three things in one breath.", "the lead paragraph, lines joined, the # title skipped");
assert.deepEqual(n.lead, ["Concord 9.9.0 does three things in one breath.", "A second lead paragraph."]);
assert.deepEqual(
  n.features.map((s) => s.title),
  ["Calls that ring", "Attaching pictures"],
  "features are the sections that are not fine print, in order",
);
assert.deepEqual(n.features[0].items, [
  { kind: "li", text: "A direct call **rings**, then ends the same way on both sides." },
  { kind: "li", text: "Second bullet with `code`." },
], "a wrapped bullet is one bullet; the hint is not text");
assert.equal(n.features[0].hero, "call", "the hint names the drawing");
assert.equal(n.features[1].hero, "attach", "no hint: the heading's keywords");
assert.deepEqual(
  n.fine.map((s) => [s.title, s.fine]),
  [["Security", "security"], ["Known limits", "limits"], ["Before you downgrade", "downgrade"], ["Also", "also"]],
  "fine print is recognised and kept, in order",
);
assert.equal(n.fine.every((s) => s.hero === ""), true, "fine print never wears a hero");
assert.deepEqual(
  n.sections.map((s) => s.title),
  ["Calls that ring", "Attaching pictures", "Security", "Known limits", "Before you downgrade", "Also"],
  "the full notes keep every section in the file's order",
);
assert.deepEqual(
  n.fine[1].items.map((i) => i.text),
  ["A moderator's device does not yet know you unlinked it.", "Another limit."],
  "Known limits are kept word for word",
);
assert.deepEqual(n.fine[2].items, [{ kind: "p", text: "0.99 changes nothing; going back loses nothing." }], "a paragraph in a section is kept as one");

// A hint for a drawing the deck does not have falls back to keywords.
assert.equal(parseNotes("## Search that finds\n<!-- hero: rocket -->\n- x", "1.0.0").features[0].hero, "search");
// Nothing to go on: the generic drawing.
assert.equal(heroFor("Quieter", "", "nothing in particular"), "spark");
assert.equal(heroFor("Quieter", "", "badges mean a mention"), "bell", "the text is read when the heading says nothing");
assert.equal(fineKind("Security"), "security");
assert.equal(fineKind("Security fixes"), "security");
assert.equal(fineKind("Known limit"), "limits");
assert.equal(fineKind("Before you update"), "downgrade");
assert.equal(fineKind("Securely sharing"), "", "a feature that starts with a similar word is not fine print");
assert.equal(fineKind("Profiles"), "");
// CRLF and an empty file.
assert.equal(parseNotes("Lead.\r\n\r\n## A\r\n- b\r\n", "1.0.0").features[0].items[0].text, "b");
assert.deepEqual(parseNotes("", "1.0.0").sections, []);
assert.equal(parseNotes(undefined).summary, "");

// ---- the real notes parse the way the deck expects --------------------------
const HERE = path.dirname(fileURLToPath(import.meta.url));
const REL = path.join(HERE, "..", "releases");
const files = fs.readdirSync(REL).filter((f) => f.endsWith(".md"));
assert.ok(files.length > 0, "src/releases holds the notes");
for (const f of files) {
  const v = versionOfPath(f);
  assert.ok(parseVersion(v), `${f} is named vX.Y.Z.md`);
  const notes = parseNotes(fs.readFileSync(path.join(REL, f), "utf8"), v);
  assert.ok(notes.summary.length > 0, `${f} has a lead paragraph`);
  assert.ok(notes.features.length > 0, `${f} has at least one feature section`);
  for (const s of notes.sections) assert.ok(s.items.length > 0, `${f}: "${s.title}" is not empty`);
}
const v062 = parseNotes(fs.readFileSync(path.join(REL, "v0.62.0.md"), "utf8"), "0.62.0");
assert.deepEqual(v062.features.map((s) => s.hero), ["rejoin", "profile", "attach", "desktop"]);
assert.deepEqual(v062.fine.map((s) => s.fine), ["security", "also", "limits", "downgrade"]);
assert.match(v062.summary, /^Concord 0\.62\.0 /);

// ---- versions ----------------------------------------------------------------
assert.deepEqual(parseVersion("v0.62.0"), [0, 62, 0]);
assert.deepEqual(parseVersion("0.62.10"), [0, 62, 10]);
assert.deepEqual(parseVersion("0.62.0-rc1"), [0, 62, 0]);
assert.equal(parseVersion("dev"), null);
assert.equal(parseVersion(""), null);
assert.equal(versionOfPath("./v0.62.0.md"), "0.62.0");
assert.equal(versionOfPath("../releases/v1.2.3.md"), "1.2.3");
assert.equal(versionOfPath("./README.md"), "");

const AV = ["0.60.0", "0.62.0", "0.61.0", "0.62.1", "bogus"];
assert.equal(chooseNotes(AV, "v0.62.1"), "0.62.1", "the running release's own notes");
assert.equal(chooseNotes(AV, "v0.62.0"), "0.62.0", "a v prefix is the release tag");
assert.equal(chooseNotes(AV, "0.62.5"), "0.62.1", "a patch with no notes: the newest not newer than it");
assert.equal(chooseNotes(AV, "0.61.3"), "0.61.0", "never notes newer than the app");
assert.equal(chooseNotes(AV, "dev"), "0.62.1", "a dev build: the newest there are");
assert.equal(chooseNotes(AV, ""), "0.62.1");
assert.equal(chooseNotes(AV, "0.59.0"), "", "older than every note: nothing");
assert.equal(chooseNotes([], "0.62.0"), "");
assert.equal(chooseNotes(AV, "0.62.5", "0.62.1"), "", "already offered under an earlier build: not replayed");
assert.equal(chooseNotes(AV, "0.62.5", "0.62.0"), "0.62.1", "notes newer than the last version seen are shown");
assert.equal(chooseNotes(AV, "0.62.1", "0.0.1"), "0.62.1");
assert.equal(chooseNotes(AV, "dev", "dev"), "0.62.1", "an unparseable seen version holds nothing back");

// ---- inline text ---------------------------------------------------------------
assert.deepEqual(inlineRuns("The **+** menu, `code`, *soft* and plain."), [
  { t: "text", v: "The " },
  { t: "strong", v: "+" },
  { t: "text", v: " menu, " },
  { t: "code", v: "code" },
  { t: "text", v: ", " },
  { t: "em", v: "soft" },
  { t: "text", v: " and plain." },
]);
assert.deepEqual(inlineRuns("See [the docs](https://example.com) now"), [{ t: "text", v: "See the docs now" }], "links keep their words only");
assert.deepEqual(inlineRuns("2 * 3 * 4"), [{ t: "text", v: "2 * 3 * 4" }], "a lone asterisk is text");
assert.deepEqual(inlineRuns("<img src=x onerror=alert(1)>"), [{ t: "text", v: "<img src=x onerror=alert(1)>" }], "markup is text, never HTML");
assert.equal(plainText("Puts **Edit** first."), "Puts Edit first.");
assert.deepEqual(inlineRuns(""), []);

// ---- the slide's words -------------------------------------------------------------
assert.equal(n.features[0].headline, "Your calls ring through", "a headline field wins over the heading");
assert.equal(n.features[0].tagline, "A call rings on every device, and **ends** for both.");
assert.equal(n.features[0].items.length, 2, "the fields are not bullets");
assert.equal(n.features[1].headline, "Attaching pictures", "no field: the heading");
assert.equal(n.features[1].tagline, "Recent row.", "no field: the first sentence");
assert.equal(firstSentence("One. Two."), "One.");
assert.equal(n.tagline, "Concord 9.9.0 does three things in one breath.", "no release tagline: a short lead stands in");
assert.equal(parseNotes(`${"word ".repeat(31)}end.\n\n## A\n- b.\n`).tagline, "", "a long lead is never cut to fit");
assert.equal(parseNotes("Lead.\n<!-- tagline: The release in a line. -->\n\n## A\n- b.\n").tagline, "The release in a line.");
assert.equal(parseNotes("Short lead.\n\n## A\n- b.\n").tagline, "Short lead.");
assert.equal(firstSentence("No full stop at all"), "", "a fragment is never a tagline");
const long = Array.from({ length: TAGLINE_WORDS + 1 }, () => "word").join(" ") + ".";
assert.equal(firstSentence(long), "", "a sentence too long is dropped, never cut");
// The shipped notes: every feature has its own words, inside the limits.
for (const f of fs.readdirSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "../releases"))) {
  if (!/^v\d+\.\d+\.\d+\.md$/.test(f)) continue;
  const md = fs.readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "../releases", f), "utf8");
  for (const s of parseNotes(md).features) {
    assert.ok(s.headline.split(/\s+/).length <= 8, `${f}: "${s.headline}" is longer than a headline`);
    assert.ok(s.tagline && s.tagline.split(/\s+/).length <= TAGLINE_WORDS, `${f}: ${s.title} needs a tagline of at most ${TAGLINE_WORDS} words`);
  }
}

console.log("releasenotes: ok");
