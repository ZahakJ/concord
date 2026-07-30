// postdraft.test.mjs — the composer's selection arithmetic and budgets, without
// a browser.
//
// Run: node src/lib/postdraft.test.mjs
//
// Why these and not others: every assertion here is a bug you cannot see in a
// screenshot. A caret three characters off, a "bold" toggle that eats one star
// of a neighbouring pair, a 64-byte title cut through the middle of an emoji —
// all of them look fine until someone writes a real message.
import assert from "node:assert/strict";
import {
  runeLen,
  utf8Bytes,
  clampToBytes,
  titleFit,
  bodyStats,
  TITLE_MAX_BYTES,
  BODY_SOFT_MAX,
  wrap,
  lineSpan,
  linePrefix,
  orderedList,
  heading,
  fence,
  link,
  colorize,
  insert,
  continueList,
  draftKey,
  normalizeDraft,
  draftEmpty,
  saveDraft,
  loadDraft,
  clearDraft,
  draftAge,
  DRAFT_VERSION,
  sendsAsIs,
  dataUrlBytes,
  prettyBytes,
  IMAGE_MAX_BYTES,
} from "./postdraft.js";

let n = 0;
function t(name, fn) {
  fn();
  n++;
  void name;
}

// A tiny localStorage, because that's the only browser API this module touches.
globalThis.localStorage = {
  map: new Map(),
  getItem(k) {
    return this.map.has(k) ? this.map.get(k) : null;
  },
  setItem(k, v) {
    this.map.set(k, String(v));
  },
  removeItem(k) {
    this.map.delete(k);
  },
};

// ---- units ----------------------------------------------------------------

t("utf8Bytes counts what the backend counts", () => {
  assert.equal(utf8Bytes("abc"), 3);
  assert.equal(utf8Bytes("é"), 2);
  assert.equal(utf8Bytes("→"), 3);
  assert.equal(utf8Bytes("🎉"), 4);
  assert.equal(utf8Bytes(""), 0);
  assert.equal(utf8Bytes(null), 0);
});

t("runeLen counts code points, not UTF-16 units", () => {
  assert.equal(runeLen("🎉🎉"), 2);
  assert.equal("🎉🎉".length, 4); // the trap
});

t("clampToBytes never splits a rune — the whole reason maxlength isn't enough", () => {
  // 16 party poppers is exactly 64 bytes; the 17th must not land as a half rune.
  const seventeen = "🎉".repeat(17);
  const cut = clampToBytes(seventeen, TITLE_MAX_BYTES);
  assert.equal(runeLen(cut), 16);
  assert.equal(utf8Bytes(cut), 64);
  // The naive backend-style byte slice is what this avoids.
  assert.ok(!cut.includes("�"));
  // A budget that lands mid-rune drops the whole rune rather than half of it.
  assert.equal(clampToBytes("a🎉", 3), "a");
  assert.equal(clampToBytes("a🎉", 5), "a🎉");
  assert.equal(clampToBytes("short", 64), "short");
});

t("titleFit's tone crosses over in the last quarter and at zero", () => {
  assert.equal(titleFit("").tone, "ok");
  assert.equal(titleFit("a".repeat(40)).tone, "ok");
  assert.equal(titleFit("a".repeat(58)).tone, "warn");
  assert.equal(titleFit("a".repeat(64)).tone, "full");
  assert.equal(titleFit("a".repeat(64)).left, 0);
  assert.ok(titleFit("a".repeat(64)).full);
  // Counted in bytes, so an emoji title runs out at 16 characters.
  assert.equal(titleFit("🎉".repeat(16)).left, 0);
});

t("bodyStats never claims a 0-minute read and flags the soft cap", () => {
  assert.deepEqual(bodyStats(""), { chars: 0, words: 0, lines: 0, minutes: 0, over: false });
  assert.equal(bodyStats("one two three").words, 3);
  assert.equal(bodyStats("one").minutes, 1, "a one-word body is a 1-minute read, not 0");
  assert.equal(bodyStats("a\nb\nc").lines, 3);
  assert.ok(!bodyStats("x".repeat(BODY_SOFT_MAX)).over);
  assert.ok(bodyStats("x".repeat(BODY_SOFT_MAX + 1)).over);
});

// ---- wrap -----------------------------------------------------------------

t("wrap inserts a placeholder and selects it when nothing is selected", () => {
  const r = wrap("", 0, 0, "**");
  assert.equal(r.text, "**text**");
  assert.equal(r.text.slice(r.start, r.end), "text");
});

t("wrap keeps the selection ON the words, not on the markers", () => {
  const r = wrap("hello world", 6, 11, "**");
  assert.equal(r.text, "hello **world**");
  assert.equal(r.text.slice(r.start, r.end), "world");
});

t("wrap toggles off with the markers outside the selection", () => {
  const r = wrap("a **b** c", 4, 5, "**");
  assert.equal(r.text, "a b c");
  assert.equal(r.text.slice(r.start, r.end), "b");
});

t("wrap toggles off with the markers inside the selection", () => {
  const r = wrap("a **b** c", 2, 7, "**");
  assert.equal(r.text, "a b c");
  assert.equal(r.text.slice(r.start, r.end), "b");
});

t("italic on a bold run does not eat one star from each side", () => {
  // The guard: "*" is a prefix of "**", so a naive outside-check sees "already
  // italic" here and would produce "*b*" claiming to be bold.
  const r = wrap("a **b** c", 4, 5, "*");
  assert.equal(r.text, "a ***b*** c");
  assert.equal(r.text.slice(r.start, r.end), "b");
});

// ---- block transforms -----------------------------------------------------

t("lineSpan covers whole lines the selection merely touches", () => {
  const text = "one\ntwo\nthree";
  assert.deepEqual(lineSpan(text, 5, 6), [4, 7]); // inside "two"
  assert.deepEqual(lineSpan(text, 1, 9), [0, 13]); // "one".."three"
});

t("linePrefix quotes every touched line and un-quotes when all already are", () => {
  const on = linePrefix("one\ntwo", 1, 5, "> ");
  assert.equal(on.text, "> one\n> two");
  const off = linePrefix(on.text, 1, 8, "> ");
  assert.equal(off.text, "one\ntwo");
});

t("linePrefix keeps a collapsed caret next to the character it was next to", () => {
  const r = linePrefix("hello", 3, 3, "- ");
  assert.equal(r.text, "- hello");
  assert.equal(r.start, 5, "caret still sits between 'hel' and 'lo'");
  assert.equal(r.end, 5);
});

t("orderedList numbers from one, and a second press removes the numbering", () => {
  const r = orderedList("a\nb\nc", 0, 5);
  assert.equal(r.text, "1. a\n2. b\n3. c", "numbers are assigned, never inherited from whatever was pasted");
  const off = orderedList(r.text, 0, r.text.length);
  assert.equal(off.text, "a\nb\nc", "already numbered ⇒ the press is a removal, like every other toggle");
  // …which is also how an author fixes "7. 9. 2.": off, then on.
  const fixed = orderedList(orderedList("7. a\n9. b\n2. c", 0, 14).text, 0, 5);
  assert.equal(fixed.text, "1. a\n2. b\n3. c");
});

t("orderedList converts a bullet list rather than nesting a marker in a marker", () => {
  const r = orderedList("- a\n- b", 0, 7);
  assert.equal(r.text, "1. a\n2. b");
});

t("heading levels are exclusive, and the same level toggles off", () => {
  const h2 = heading("Title", 0, 0, 2);
  assert.equal(h2.text, "## Title");
  const h3 = heading(h2.text, 0, 0, 3);
  assert.equal(h3.text, "### Title", "H3 replaces H2 instead of stacking hashes");
  const off = heading(h3.text, 0, 0, 3);
  assert.equal(off.text, "Title");
});

t("fence adds only the newlines that are missing and selects the code", () => {
  const r = fence("ab", 1, 1, "go");
  assert.equal(r.text, "a\n```go\ncode\n```\nb");
  assert.equal(r.text.slice(r.start, r.end), "code", "the caret lands on the code, not the language");
  const already = fence("x\n\ny", 2, 2, "");
  assert.equal(already.text, "x\n```\ncode\n```\ny");
});

t("link selects the label when empty and the url when text was selected", () => {
  const empty = link("", 0, 0);
  assert.equal(empty.text, "[text](url)");
  assert.equal(empty.text.slice(empty.start, empty.end), "text");
  const sel = link("see docs", 4, 8);
  assert.equal(sel.text, "see [docs](url)");
  assert.equal(sel.text.slice(sel.start, sel.end), "url");
});

t("colorize wraps, recolours, and — crucially — removes", () => {
  const on = colorize("warning", 0, 7, "red");
  assert.equal(on.text, "{red|warning}");
  assert.equal(on.text.slice(on.start, on.end), "warning");
  const re = colorize(on.text, 0, on.text.length, "#00ff00");
  assert.equal(re.text, "{#00ff00|warning}", "recolour must not nest one span in another");
  const off = colorize(re.text, 0, re.text.length, "");
  assert.equal(off.text, "warning", "there has to be a way back out of a colour");
  // Nothing to remove is a no-op, not a mangling.
  assert.equal(colorize("plain", 0, 5, "").text, "plain");
});

t("insert replaces the selection and parks the caret after it", () => {
  const r = insert("ab", 1, 1, "🎉");
  assert.equal(r.text, "a🎉b");
  assert.equal(r.start, r.end);
  assert.equal(r.text.slice(0, r.start), "a🎉");
});

// ---- Enter in a list ------------------------------------------------------

t("continueList carries the marker down and increments numbers", () => {
  const b = continueList("- milk", 6);
  assert.equal(b.text, "- milk\n- ");
  assert.equal(b.caret, b.text.length);
  const o = continueList("3. third", 8);
  assert.equal(o.text, "3. third\n4. ");
  const p = continueList("1) a", 4);
  assert.equal(p.text, "1) a\n2) ");
  const q = continueList("> quoted", 8);
  assert.equal(q.text, "> quoted\n> ");
});

t("Enter on an empty item ENDS the list instead of adding another bullet", () => {
  const r = continueList("- a\n- ", 6);
  assert.equal(r.text, "- a\n");
  assert.equal(r.caret, 4);
});

t("continueList keeps indentation and stays out of the way elsewhere", () => {
  const r = continueList("  - a", 5);
  assert.equal(r.text, "  - a\n  - ");
  assert.equal(continueList("plain text", 10), null);
  assert.equal(continueList("", 0), null);
});

// ---- drafts ---------------------------------------------------------------

t("draftKey is namespaced away from the inline composer's own drafts", () => {
  assert.ok(draftKey("post:f1").startsWith("concord.compose."));
  assert.notEqual(draftKey("post:f1"), "concord.draft.f1");
  assert.notEqual(draftKey("post:f1"), draftKey("post:f2"));
});

t("normalizeDraft refuses junk from storage", () => {
  assert.deepEqual(normalizeDraft(null), { v: DRAFT_VERSION, title: "", body: "", tags: [], embed: null, at: 0 });
  assert.deepEqual(normalizeDraft({ title: 5, body: [], tags: "no", at: "soon" }).tags, []);
  assert.deepEqual(normalizeDraft({ tags: ["a", 7, "b"] }).tags, ["a", "b"]);
});

t("draftEmpty ignores an untouched embed skeleton", () => {
  assert.ok(draftEmpty({}));
  assert.ok(draftEmpty({ body: "   " }));
  assert.ok(draftEmpty({ embed: { color: "#14a394", title: "", desc: "", fields: [] } }));
  assert.ok(!draftEmpty({ embed: { color: "#14a394", title: "Hi", desc: "", fields: [] } }));
  assert.ok(!draftEmpty({ embed: { fields: [{ name: "", value: "v" }] } }));
  assert.ok(!draftEmpty({ body: "hi" }));
  assert.ok(!draftEmpty({ tags: ["t1"] }));
});

t("a saved draft round-trips; an emptied one is removed, not stored blank", () => {
  clearDraft("post:f1");
  saveDraft("post:f1", { title: "T", body: "B", tags: ["x"] });
  const got = loadDraft("post:f1");
  assert.equal(got.title, "T");
  assert.equal(got.body, "B");
  assert.deepEqual(got.tags, ["x"]);
  assert.ok(got.at > 0, "an age is needed for the restored banner");
  saveDraft("post:f1", { title: "", body: "", tags: [] });
  assert.equal(loadDraft("post:f1"), null);
  assert.equal(localStorage.getItem(draftKey("post:f1")), null);
});

t("a draft from another shape version is dropped, never half-restored", () => {
  localStorage.setItem(draftKey("post:f9"), JSON.stringify({ v: 99, body: "from the future" }));
  assert.equal(loadDraft("post:f9"), null);
  localStorage.setItem(draftKey("post:f8"), "{not json");
  assert.equal(loadDraft("post:f8"), null);
});

t("attachments are never written to storage", () => {
  clearDraft("post:f2");
  saveDraft("post:f2", { body: "b", pending: [{ dataUrl: "data:image/png;base64," + "A".repeat(4096) }] });
  const raw = localStorage.getItem(draftKey("post:f2"));
  assert.ok(!raw.includes("data:image"), "a base64 image in localStorage is how you break every draft");
  assert.ok(raw.length < 200);
});

t("draftAge reads like a person, and says nothing without a timestamp", () => {
  const now = 1_000_000_000_000;
  assert.equal(draftAge(0, now), "");
  assert.equal(draftAge(now - 5_000, now), "moments ago");
  assert.equal(draftAge(now - 5 * 60_000, now), "5 minutes ago");
  assert.equal(draftAge(now - 60 * 60_000, now), "1 hour ago");
  assert.equal(draftAge(now - 50 * 60 * 60_000, now), "2 days ago");
});

// ---- staging --------------------------------------------------------------

t("sendsAsIs keeps a GIF animated and re-encodes everything else", () => {
  assert.ok(sendsAsIs("image/gif", 1000));
  assert.ok(sendsAsIs("image/png", 1000));
  assert.ok(!sendsAsIs("image/gif", IMAGE_MAX_BYTES + 1), "over the cap it must go through the canvas");
  assert.ok(!sendsAsIs("image/heic", 1000));
  assert.ok(!sendsAsIs("image/svg+xml", 10));
});

t("dataUrlBytes measures the decoded size without decoding", () => {
  assert.equal(dataUrlBytes("data:image/png;base64,AAAA"), 3);
  assert.equal(dataUrlBytes("data:image/png;base64,AAA="), 2);
  assert.equal(dataUrlBytes("data:image/png;base64,AA=="), 1);
  assert.equal(dataUrlBytes("nonsense"), 0);
});

t("prettyBytes stays short in the tray", () => {
  assert.equal(prettyBytes(0), "");
  assert.equal(prettyBytes(512), "512 B");
  assert.equal(prettyBytes(2048), "2 KB");
  assert.equal(prettyBytes(1024 * 1024 * 3.5), "3.5 MB");
  assert.equal(prettyBytes(1024 * 1024 * 24), "24 MB");
});

console.log(`postdraft.js: all ${n} tests passed`);
