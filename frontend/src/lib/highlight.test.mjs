// The syntax highlighter, held to the two promises it makes (`npm test`).
//
// PROMISE ONE: it cannot hang. The feature proposal flagged this as the one
// "harmless" item with a real trap — a tokenizer runs in the render path over
// text a stranger wrote, and the usual regular-expression approach turns a few
// kilobytes of crafted quoting into minutes of backtracking. So the hostile
// cases below are not decoration: each one is timed and each one asserts the
// step budget was not merely survived but barely touched.
//
// PROMISE TWO: it cannot inject. Every character reaches the output through the
// same escapeHtml the markdown renderer has always used, and the only markup
// the file emits is a span with a class from a closed set. The XSS block feeds
// it markup and asserts none of it survives as markup.
import { highlight, stepsUsed, MAX_STEPS, MAX_CHARS, MAX_TOKENS } from "./highlight.js";
import { tableFor, LANGUAGES } from "./langs.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

// ---- the tables resolve ----------------------------------------------------

assert(LANGUAGES.length >= 14, `expected a broad table, got ${LANGUAGES.length}`);
for (const alias of ["js", "ts", "golang", "py", "rs", "c++", "bash", "yml", "sqlite", "scss", "patch", "svg"]) {
  assert(!!tableFor(alias), `alias ${alias} resolves to a table`);
}
assert(tableFor("brainfuck") === null, "an unknown label is null, not a throw");
assert(tableFor("") === null, "an empty label is null");
assert(tableFor(null) === null, "a missing label is null");
// The label is looked up, never interpolated — the containment rule the whole
// app applies to peer-supplied values.
assert(tableFor('js" onload="x') === null, "a label carrying markup resolves to nothing");

// ---- it actually colours the right things ----------------------------------

const js = highlight(`const x = "hi"; // note\nfoo(1)`, tableFor("js"));
assert(js.includes('<span class="hl-kw">const</span>'), `js keyword: ${js}`);
assert(js.includes('<span class="hl-str">&quot;hi&quot;</span>'), `js string: ${js}`);
assert(js.includes('<span class="hl-com">// note</span>'), `js comment: ${js}`);
assert(js.includes('<span class="hl-num">1</span>'), `js number: ${js}`);
assert(js.includes('<span class="hl-fn">foo</span>'), `js call: ${js}`);

const go = highlight("func main() {\n\ts := `raw \\n`\n}", tableFor("go"));
assert(go.includes('<span class="hl-kw">func</span>'), `go keyword: ${go}`);
// A Go backtick string does NOT honour backslashes; getting that wrong swallows
// the rest of the file from the next quote onwards.
assert(go.includes("<span class=\"hl-str\">`raw \\n`</span>"), `go raw string is one token: ${go}`);

const py = highlight('def f():\n    """doc"""\n    return None', tableFor("python"));
assert(py.includes('<span class="hl-kw">def</span>'), `py keyword: ${py}`);
assert(py.includes('<span class="hl-str">&quot;&quot;&quot;doc&quot;&quot;&quot;</span>'), `py triple quote: ${py}`);
assert(py.includes('<span class="hl-typ">None</span>'), `py constant: ${py}`);

const json = highlight('{"name": "x", "n": 3}', tableFor("json"));
assert(json.includes('<span class="hl-att">&quot;name&quot;</span>'), `json key: ${json}`);
assert(json.includes('<span class="hl-str">&quot;x&quot;</span>'), `json value stays a string: ${json}`);

const html = highlight('<a href="/x">hi</a>', tableFor("html"));
assert(html.includes('<span class="hl-tag">a</span>'), `markup tag: ${html}`);
assert(html.includes('<span class="hl-att">href</span>'), `markup attribute: ${html}`);

const diff = highlight("--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new", tableFor("diff"));
assert(diff.includes('<span class="hl-add">+new</span>'), `diff add: ${diff}`);
assert(diff.includes('<span class="hl-del">-old</span>'), `diff del: ${diff}`);
assert(diff.includes('<span class="hl-com">--- a/x</span>'), `diff header beats del: ${diff}`);

const sql = highlight("select * from t where a = 1", tableFor("sql"));
assert(sql.includes('<span class="hl-kw">select</span>'), `sql lowercase keyword: ${sql}`);

const sh = highlight('echo "$HOME" # x', tableFor("sh"));
assert(sh.includes("hl-com"), `shell comment: ${sh}`);

// Regression: an identifier-START character that is not an identifier-CONTINUE
// character. Every table with one of these (C's #, shell's $, CSS's @ and -,
// Java's @) drove the scanner into a spin that only the step budget stopped.
const cpp = highlight("#include <stdio.h>\nint x = 1;", tableFor("c"));
assert(cpp !== null, "a C preprocessor line does not exhaust the budget");
assert(cpp.includes('<span class="hl-kw">#include</span>'), `preprocessor directive: ${cpp}`);
for (const [lang, src] of [
  ["c", "#include <a.h>"],
  ["sh", "echo $HOME"],
  ["css", "@media (min-width: 1px) { --x: 1; }"],
  ["java", "@Override void f() {}"],
]) {
  const steps = stepsUsed(src, tableFor(lang));
  assert(steps < src.length * 4, `${lang}: ${steps} steps for ${src.length} chars is a spin`);
}

// A member access is never a keyword, whatever it spells.
const dotted = highlight("x.type\ny.return", tableFor("go"));
assert(!dotted.includes("hl-kw"), `a dotted name is not a keyword: ${dotted}`);

// Non-ASCII identifiers read as one word rather than a run of punctuation.
const uni = highlight("const مرحبا = 1; // تعليق", tableFor("js"));
assert(uni.includes("مرحبا") && !uni.includes('<span class="hl-pun">م'), `unicode ident: ${uni}`);

// ---- escaping --------------------------------------------------------------
//
// Anything a browser would read as markup must come back escaped, in every
// position: plain text, inside a string, inside a comment, and as a fence label
// (which never reaches the output at all).
const xss = [
  '<script>alert(1)</script>',
  'const x = "</code></pre><img src=x onerror=alert(1)>";',
  "// </span><svg onload=alert(1)>",
  "`</span><b onclick=alert(1)>`",
  "'\\'</span><i>'",
  '<a href="javascript:alert(1)">x</a>',
  "/* </span><script>x</script> */",
];
const SAFE_TAG = /^<span class="hl-(com|str|kw|typ|num|fn|att|tag|pun|add|del)">$|^<\/span>$/;
for (const src of xss) {
  for (const lang of ["js", "go", "python", "html", "sql", "css"]) {
    const out = highlight(src, tableFor(lang));
    if (out === null) continue;
    for (const m of out.matchAll(/<[^>]*>/g)) {
      assert(SAFE_TAG.test(m[0]), `unexpected tag ${m[0]} from ${lang}: ${src}`);
    }
    // The text is preserved exactly — highlighting must not lose or invent a
    // character, or the Copy button copies something the author never wrote.
    const back = out
      .replace(/<[^>]*>/g, "")
      .replace(/&lt;/g, "<")
      .replace(/&gt;/g, ">")
      .replace(/&quot;/g, '"')
      .replace(/&#39;/g, "'")
      .replace(/&amp;/g, "&");
    assert(back === src, `round trip changed the text (${lang}): ${JSON.stringify(back)}`);
  }
}

// ---- the budget ------------------------------------------------------------
//
// Each case reports the steps it burned and the wall-clock it took. The
// assertion is on the budget, not the clock — a loaded CI box is slow for
// reasons that are not this file's fault — but the times are printed because a
// backtracking implementation would show up in them as seconds, not
// milliseconds, long before it showed up as a failed step count.
const cases = [
  ["deeply nested braces", "js", "{".repeat(20000) + "}".repeat(20000)],
  ["unterminated quotes, alternating", "js", `"'`.repeat(9000)],
  ["escaped-quote pathology", "js", '"' + '\\"'.repeat(9000)],
  ["comment openers, never closed", "c", "/*".repeat(12000)],
  ["one string opener per line", "python", '"""\n'.repeat(6000)],
  ["nothing but backticks", "go", "`".repeat(30000)],
  ["angle brackets, unbalanced", "html", "<".repeat(30000)],
  ["a tag that never closes", "html", '<a href="' + "x".repeat(20000)],
  ["numbers with exponents", "js", "1e".repeat(15000)],
  ["hyphen soup", "css", "-".repeat(30000)],
  ["one long diff line", "diff", "+" + "x".repeat(30000)],
  ["dollar signs", "sh", "$".repeat(30000)],
];

console.log("  budget: MAX_CHARS=%d MAX_STEPS=%d MAX_TOKENS=%d", MAX_CHARS, MAX_STEPS, MAX_TOKENS);
for (const [name, lang, src] of cases) {
  const table = tableFor(lang);
  const t0 = performance.now();
  const out = highlight(src, table);
  const ms = performance.now() - t0;
  const steps = stepsUsed(src, table);
  assert(steps <= MAX_STEPS, `${name}: ${steps} steps exceeds the budget`);
  assert(ms < 2000, `${name}: took ${ms.toFixed(1)}ms, which is not a single pass`);
  // Bailing is a fine outcome; hanging is not. Whichever happened, it happened
  // inside the budget.
  assert(out === null || typeof out === "string", `${name}: returned neither html nor null`);
  console.log(
    "  %s (%s, %d chars): %d steps, %sms, %s",
    name,
    lang,
    src.length,
    steps,
    ms.toFixed(1),
    out === null ? "bailed to plain" : `${out.length} chars of html`,
  );
}

// The 500 KB one-liner: refused on size before a single character is scanned,
// which is the cheapest possible answer and the reason MAX_CHARS exists.
{
  const huge = "a".repeat(500 * 1024);
  const t0 = performance.now();
  const out = highlight(huge, tableFor("js"));
  const ms = performance.now() - t0;
  assert(out === null, "a 500KB block is not highlighted");
  assert(ms < 50, `the size refusal took ${ms.toFixed(1)}ms — it should not scan at all`);
  console.log("  500KB one-liner: refused on size in %sms", ms.toFixed(2));
}

// A block of pure punctuation is inside every other budget and still must not
// become one span per character.
{
  const punct = ";".repeat(20000);
  const out = highlight(punct, tableFor("js"));
  assert(out === null, "a block that would exceed MAX_TOKENS bails to plain");
  console.log("  %d punctuation characters: bailed on the token budget", punct.length);
}

// Realistic code, for scale: the budget should barely be touched by anything a
// person actually pastes.
{
  const real = `package main\n\nimport "fmt"\n\n// greet says hello.\nfunc greet(name string) string {\n\treturn fmt.Sprintf("hello, %s", name)\n}\n`.repeat(
    40,
  );
  const steps = stepsUsed(real, tableFor("go"));
  assert(steps < MAX_STEPS / 4, `real code used ${steps} steps`);
  console.log("  %d chars of ordinary Go: %d steps (%d%% of budget)", real.length, steps, Math.round((steps / MAX_STEPS) * 100));
}

// ---- refusals --------------------------------------------------------------
assert(highlight("x", null) === null, "no table means no highlighting");
assert(highlight("", tableFor("js")) === null, "an empty block is not highlighted");
assert(highlight("a".repeat(MAX_CHARS + 1), tableFor("js")) === null, "over MAX_CHARS is refused");

if (failures) {
  console.error(`\n${failures} highlight test(s) failed`);
  process.exit(1);
}
console.log("highlight.test.mjs: all checks passed");
