// The formatting engine, tested where it is pure.
//
// applyFormat() needs a live textarea and document.execCommand, so it is proved
// in the browser instead (the batch drove it: type, bold, Ctrl+Z twice). What
// is testable here is the PLAN — which span is replaced, with what, and where
// the selection lands — and that is where every one of these bugs lived.
import { planFormat, chordFor, WRAPS, FMT_GROUPS } from "./mdformat.js";

let failures = 0;
const fail = (msg) => {
  console.error("  FAIL: " + msg);
  failures++;
};

// apply: run a plan against a value so a test can assert on the string a user
// would see, plus the selection markers.
function apply(value, start, end, kind) {
  const p = planFormat(value, start, end, kind);
  if (!p) return null;
  const next = value.slice(0, p.from) + p.insert + value.slice(p.to);
  return { text: next, sel: [p.selStart, p.selEnd], selected: next.slice(p.selStart, p.selEnd) };
}

function eq(got, want, what) {
  if (got !== want) fail(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
}

// ---- wrapping a selection ----
eq(apply("undo me now", 0, 4, "bold").text, "**undo** me now", "bold a selection");
eq(apply("undo me now", 0, 4, "bold").selected, "undo", "bold keeps the word selected");
eq(apply("a b", 0, 3, "italic").text, "*a b*", "italic a selection");
eq(apply("x", 0, 1, "strike").text, "~~x~~", "strike a selection");
eq(apply("x", 0, 1, "spoiler").text, "||x||", "spoiler a selection");
eq(apply("x", 0, 1, "code").text, "`x`", "code a selection");

// ---- unwrapping (the second press toggles it off) ----
eq(apply("**undo** me", 2, 6, "bold").text, "undo me", "bold again unwraps from outside");
eq(apply("**undo** me", 0, 8, "bold").text, "undo me", "bold again unwraps from inside");
// The italic guard: a lone "*" check must not eat half of a surrounding "**".
eq(apply("**bold** x", 2, 6, "italic").text, "***bold*** x", "italic inside bold wraps rather than eating half the **");

// ---- the collapsed caret ----
//
// Jammed against the end of a word, the placeholder produced `word**bold** here`
// — an English word nobody typed, spliced into the middle of a sentence. The
// standard behaviour is bare markers with the caret between them, and it is the
// only one that composes with carrying on typing.
{
  const r = apply("word here", 4, 4, "bold");
  eq(r.text, "word**** here", "caret against a word inserts bare markers");
  eq(r.sel[0], 6, "caret lands between the markers");
  eq(r.sel[0], r.sel[1], "nothing is selected");
}
{
  // In whitespace there is nothing to emphasise, so the placeholder still earns
  // its place: it says what just happened and it is selected to type over.
  const r = apply("word  here", 5, 5, "bold");
  eq(r.text, "word **bold** here", "caret in whitespace keeps the placeholder");
  eq(r.selected, "bold", "the placeholder is selected");
}
{
  const r = apply("", 0, 0, "italic");
  eq(r.text, "*italic*", "empty draft gets the placeholder");
}
{
  // Unicode counts as a word: an Arabic writer must get the same behaviour.
  const r = apply("مرحبا", 5, 5, "bold");
  eq(r.text, "مرحبا****", "caret against an Arabic word inserts bare markers");
}

// ---- link ----
eq(apply("see docs here", 4, 8, "link").text, "see [docs](url) here", "link wraps the selection");
eq(apply("see docs here", 4, 8, "link").selected, "url", "link selects the url to type over");
eq(apply("", 0, 0, "link").text, "[text](url)", "link with nothing selected");
eq(apply("", 0, 0, "link").selected, "text", "…and selects the label");

// ---- quote ----
eq(apply("a\nb", 0, 3, "quote").text, "> a\n> b", "quote every line the selection touches");
eq(apply("> a\n> b", 0, 7, "quote").text, "a\nb", "quote again unquotes");

// ---- code block ----
eq(apply("x", 0, 1, "codeblock").text, "```\nx\n```", "code block fences a selection, adding only the missing newlines");
eq(apply("a x", 2, 3, "codeblock").text, "a \n```\nx\n```", "…and adds a leading newline when mid-line");
eq(apply("", 0, 0, "codeblock").text, "```\ncode\n```", "code block with nothing selected");

// ---- the chords ----
const key = (k, o = {}) => ({ key: k, code: "", ctrlKey: true, metaKey: false, altKey: false, shiftKey: false, ...o });
eq(chordFor(key("b")), "bold", "Ctrl+B");
eq(chordFor(key("i")), "italic", "Ctrl+I");
eq(chordFor(key("e")), "code", "Ctrl+E");
eq(chordFor(key("x", { shiftKey: true })), "spoiler", "Ctrl+Shift+X");
eq(chordFor(key(".", { shiftKey: true })), "quote", "Ctrl+Shift+.");
// The rebind that this batch is about: Ctrl+K belongs to the quick switcher
// everywhere, so the link moved one key over.
eq(chordFor(key("k")), null, "Ctrl+K is NOT a formatting chord any more");
eq(chordFor(key("k", { shiftKey: true })), "link", "Ctrl+Shift+K is the link");
eq(chordFor({ key: "b", ctrlKey: false, metaKey: false, altKey: false, shiftKey: false }), null, "bare b is a character");
eq(chordFor(key("b", { altKey: true })), null, "Ctrl+Alt+B is not ours");

// ---- the toolbar and the chords describe the same set ----
const kinds = new Set(FMT_GROUPS.flat().map((b) => b.kind));
for (const k of Object.keys(WRAPS)) if (!kinds.has(k)) fail(`WRAPS has "${k}" and the toolbar does not`);
for (const b of FMT_GROUPS.flat()) if (!planFormat("x", 0, 1, b.kind)) fail(`toolbar has "${b.kind}" and planFormat does not`);
// The tooltip hint and the chord must not drift apart — the link's said "K"
// while the handler answered Shift+K for exactly as long as nobody checked.
const HINTED = { bold: "b", italic: "i", code: "e", spoiler: "x", quote: ".", link: "k" };
for (const b of FMT_GROUPS.flat()) {
  if (!b.keys) continue;
  const shift = b.keys.startsWith("Shift+");
  const k = b.keys.replace("Shift+", "").toLowerCase();
  eq(k, HINTED[b.kind], `tooltip for ${b.kind} names its key`);
  eq(chordFor(key(k, { shiftKey: shift })), b.kind, `the chord the tooltip advertises for ${b.kind}`);
}

if (failures) {
  console.error(`mdformat: ${failures} failure(s)`);
  process.exit(1);
}
console.log("mdformat: all tests passed");
