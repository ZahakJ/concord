// highlight.js — colouring a fenced code block, without handing a stranger a
// way to stall the render.
//
// THE TRAP THIS FILE EXISTS TO AVOID. The obvious way to highlight code is a
// pile of regular expressions run over the block in passes. Every one of those
// patterns is an alternation with a quantifier inside it, and a crafted 4 KB
// block of nested quotes makes one of them backtrack for minutes — in the
// render path, on the main thread, while the message list is trying to paint.
// A message body is attacker-controlled text. So there are no regular
// expressions here at all: what follows is a single-pass character scanner that
// never looks backwards, plus a hard step budget it bails out of. Bailing means
// the block renders as the plain escaped text it renders as today, which is the
// same outcome as an unsupported language, which is a perfectly good outcome.
//
// THE SECOND RULE: every character of the source reaches the output through
// escapeHtml, the same function the markdown renderer has always used. The only
// other thing this file emits is `<span class="hl-xx">` with a class name taken
// from a fixed internal set — never from the language label, never from the
// code. That is what keeps highlighting outside the XSS surface entirely.
//
// The language tables live in ./langs.js and are loaded on demand: a chat
// window with no code in it should not carry a keyword list for twenty
// languages. Until the table arrives, blocks render plain and upgrade on the
// next frame — the same shape as the animated-emoji swap.

import { escapeHtml } from "./markdown.js";

// ---- budgets ---------------------------------------------------------------
//
// Three of them, each answering a different way the same abuse arrives.
//
// MAX_CHARS: a block bigger than this is not highlighted at all. Nobody pastes
// 40 KB of source into a chat message meaning to be read; the ones that arrive
// at that size are logs, minified bundles and deliberate junk.
//
// MAX_STEPS: the scanner's outer loop always consumes at least one character,
// so this can only be reached by a block near MAX_CHARS. It is the backstop
// that makes the bound true by construction rather than by argument.
//
// MAX_TOKENS: the output side. A block of nothing but punctuation produces one
// span per character; ten thousand spans in a message body is a layout cost
// whether or not the scan was cheap.
export const MAX_CHARS = 40000;
export const MAX_STEPS = 120000;
export const MAX_TOKENS = 6000;

// The token kinds, and the class each one wears. Fixed set, closed on purpose —
// a language table names a kind, it never names a class.
const CLASS = {
  com: "hl-com", // comments
  str: "hl-str", // strings, characters, here-docs
  kw: "hl-kw", // keywords
  typ: "hl-typ", // built-in types and constants
  num: "hl-num", // numbers
  fn: "hl-fn", // a name being called
  att: "hl-att", // attribute / key names
  tag: "hl-tag", // markup tag names
  pun: "hl-pun", // punctuation
  add: "hl-add", // a diff's added line
  del: "hl-del", // a diff's removed line
};

// ---- the emitter -----------------------------------------------------------
//
// Runs of plain text are held and flushed as one string, so an identifier does
// not become three spans and a line of prose does not become forty.
class Out {
  constructor(src) {
    this.src = src;
    this.parts = [];
    this.plain = 0; // start of the pending plain run
    this.tokens = 0;
    this.over = false;
  }
  flush(to) {
    if (to > this.plain) this.parts.push(escapeHtml(this.src.slice(this.plain, to)));
    this.plain = to;
  }
  // push marks [from,to) as `kind`. Everything before `from` is plain.
  push(kind, from, to) {
    if (to <= from) return;
    this.flush(from);
    if (++this.tokens > MAX_TOKENS) {
      this.over = true;
      return;
    }
    this.parts.push(`<span class="${CLASS[kind]}">${escapeHtml(this.src.slice(from, to))}</span>`);
    this.plain = to;
  }
  done() {
    this.flush(this.src.length);
    return this.parts.join("");
  }
}

// ---- character classes -----------------------------------------------------
//
// Hand-written so they cost a comparison rather than a regex engine. Anything
// above ASCII counts as an identifier character: an Arabic or CJK identifier
// should read as one word, not as a run of punctuation.
const isDigit = (c) => c >= 48 && c <= 57;
const isAlpha = (c) =>
  (c >= 97 && c <= 122) || (c >= 65 && c <= 90) || c === 95 || c === 36 || c > 127;
const isWord = (c) => isAlpha(c) || isDigit(c);
const isSpace = (c) => c === 32 || c === 9 || c === 13;

// starts: does `src` carry the literal `pat` at `i`? A bounded loop, so a long
// opener costs its own length and nothing more.
function starts(src, i, pat) {
  if (i + pat.length > src.length) return false;
  for (let k = 0; k < pat.length; k++) if (src[i + k] !== pat[k]) return false;
  return true;
}

// ---- the general scanner ---------------------------------------------------
//
// Covers every curly-brace and every scripting language in the table: the
// differences between Go and Python that matter at this resolution are which
// strings exist, which comment markers exist, and which words are keywords.
// All three are data.
function scanGeneral(src, T, out, budget) {
  const n = src.length;
  let i = 0;
  while (i < n) {
    if (--budget.steps < 0) return false;
    if (out.over) return false;
    const c = src.charCodeAt(i);

    // Whitespace and anything unclaimed falls through as plain text; taking the
    // cheap case first keeps prose-heavy blocks near-free.
    if (isSpace(c) || c === 10) {
      i++;
      continue;
    }

    // Line comments.
    let matched = false;
    for (const p of T.line) {
      if (starts(src, i, p)) {
        let j = i;
        while (j < n && src.charCodeAt(j) !== 10) j++;
        out.push("com", i, j);
        i = j;
        matched = true;
        break;
      }
    }
    if (matched) continue;

    // Block comments. An unterminated one runs to the end of the block, which
    // is what an editor does and what the author almost certainly meant.
    for (const [open, close] of T.block) {
      if (starts(src, i, open)) {
        let j = i + open.length;
        while (j < n && !starts(src, j, close)) {
          j++;
          if (--budget.steps < 0) return false;
        }
        j = Math.min(n, j + close.length);
        out.push("com", i, j);
        i = j;
        matched = true;
        break;
      }
    }
    if (matched) continue;

    // Strings. `esc` says a backslash hides the next character; `multi` says a
    // newline does not end it. Both are per-delimiter because Go's backtick and
    // Go's double quote disagree about each.
    for (const s of T.str) {
      if (!starts(src, i, s.q)) continue;
      let j = i + s.q.length;
      while (j < n) {
        if (--budget.steps < 0) return false;
        const d = src.charCodeAt(j);
        if (s.esc && d === 92) {
          j += 2;
          continue;
        }
        if (d === 10 && !s.multi) break;
        if (starts(src, j, s.q)) {
          j += s.q.length;
          break;
        }
        j++;
      }
      j = Math.min(j, n);
      // A quoted key — "name": in JSON, 'src': in a Python dict — reads better
      // as a label than as a value. The lookahead is bounded to a few spaces.
      out.push(T.keyColon && colonAhead(src, j) ? "att" : "str", i, j);
      i = j;
      matched = true;
      break;
    }
    if (matched) continue;

    // Numbers. Deliberately generous: 0xFF, 1_000, 3.14e-9, 12px, 100% and
    // Go's 1i all end up one token, and none of them can be mistaken for
    // anything else at this resolution.
    if (isDigit(c) || (c === 46 && isDigit(src.charCodeAt(i + 1)))) {
      let j = i;
      while (j < n) {
        if (--budget.steps < 0) return false;
        const d = src.charCodeAt(j);
        if (isWord(d) || d === 46 || d === 37) {
          // An exponent's sign belongs to the number; a minus anywhere else is
          // an operator.
          if ((d === 101 || d === 69 || d === 112 || d === 80) && j > i) {
            const e = src.charCodeAt(j + 1);
            if (e === 43 || e === 45) j++;
          }
          j++;
          continue;
        }
        break;
      }
      out.push("num", i, j);
      i = j;
      continue;
    }

    // Identifiers, and the three things one can turn out to be.
    if (isAlpha(c) || T.identStart.includes(src[i])) {
      // The opening character is consumed unconditionally. It has to be: an
      // identStart character is by definition one the CONTINUE test rejects
      // (`#` in C, `$` in shell, `@` in CSS), so a loop that starts at `i` and
      // asks "may this continue?" never advances, pushes an empty token, and
      // spins on the same character until the step budget runs out. That is
      // exactly what happened to every `#include` in the first build of this
      // file, and it is the reason the budget is a hard stop rather than a
      // warning: the bug produced a plain code block, not a frozen tab.
      let j = i + 1;
      while (j < n && (isWord(src.charCodeAt(j)) || T.identPart.includes(src[j]))) {
        j++;
        if (--budget.steps < 0) return false;
      }
      const word = src.slice(i, j);
      // A member access (`.append`, `->size`) is never a keyword, whatever it
      // spells. Looking one character back is not backtracking — the scanner
      // does not resume there.
      const dotted = i > 0 && (src[i - 1] === "." || (src[i - 1] === ">" && src[i - 2] === "-"));
      if (!dotted && T.keywords.has(word)) out.push("kw", i, j);
      else if (!dotted && T.types.has(word)) out.push("typ", i, j);
      else if (T.keyColon && colonAhead(src, j)) out.push("att", i, j);
      else if (callAhead(src, j)) out.push("fn", i, j);
      i = j;
      continue;
    }

    // Everything else is punctuation, one character at a time. Runs are common
    // (`=>`, `!==`) but colouring them individually costs nothing visually and
    // saves a table of operators per language.
    out.push("pun", i, i + 1);
    i++;
  }
  return true;
}

// callAhead: is the very next non-space character an opening paren? Bounded to
// a couple of spaces so this is a constant, not a scan.
function callAhead(src, j) {
  let k = j;
  for (let s = 0; s < 2 && isSpace(src.charCodeAt(k)); s++) k++;
  return src[k] === "(";
}

// colonAhead: the same shape, for a key.
function colonAhead(src, j) {
  let k = j;
  for (let s = 0; s < 2 && isSpace(src.charCodeAt(k)); s++) k++;
  return src[k] === ":" && src[k + 1] !== ":";
}

// ---- the markup scanner ----------------------------------------------------
//
// HTML and XML are not a keyword language: the interesting thing is where a tag
// starts and stops. Two states, no nesting, so a malformed document cannot
// confuse it into a loop.
function scanMarkup(src, T, out, budget) {
  const n = src.length;
  let i = 0;
  while (i < n) {
    if (--budget.steps < 0) return false;
    if (out.over) return false;
    if (src[i] !== "<") {
      i++;
      continue;
    }
    if (starts(src, i, "<!--")) {
      let j = i + 4;
      while (j < n && !starts(src, j, "-->")) {
        j++;
        if (--budget.steps < 0) return false;
      }
      j = Math.min(n, j + 3);
      out.push("com", i, j);
      i = j;
      continue;
    }
    // Inside a tag until the matching '>', with quoted values skipped whole so
    // a '>' in an attribute value cannot close it early.
    let j = i + 1;
    if (src[j] === "/" || src[j] === "!" || src[j] === "?") j++;
    const nameStart = j;
    while (j < n && (isWord(src.charCodeAt(j)) || src[j] === "-" || src[j] === ":")) j++;
    out.push("pun", i, nameStart);
    out.push("tag", nameStart, j);
    while (j < n && src[j] !== ">") {
      if (--budget.steps < 0) return false;
      const q = src[j];
      if (q === '"' || q === "'") {
        let k = j + 1;
        while (k < n && src[k] !== q) {
          k++;
          if (--budget.steps < 0) return false;
        }
        k = Math.min(n, k + 1);
        out.push("str", j, k);
        j = k;
        continue;
      }
      if (isAlpha(src.charCodeAt(j))) {
        let k = j;
        while (k < n && (isWord(src.charCodeAt(k)) || src[k] === "-" || src[k] === ":")) k++;
        out.push("att", j, k);
        j = k;
        continue;
      }
      j++;
    }
    j = Math.min(n, j + 1);
    out.push("pun", j - 1, j);
    i = j;
  }
  return true;
}

// ---- the line scanner ------------------------------------------------------
//
// For diffs and logs, where the first characters of a line decide the whole
// line and nothing inside it matters.
function scanLines(src, T, out, budget) {
  const n = src.length;
  let i = 0;
  while (i < n) {
    if (--budget.steps < 0) return false;
    if (out.over) return false;
    let j = i;
    while (j < n && src.charCodeAt(j) !== 10) j++;
    for (const [prefix, kind] of T.lines) {
      if (starts(src, i, prefix)) {
        out.push(kind, i, j);
        break;
      }
    }
    i = j + 1;
  }
  return true;
}

const SCANNERS = { general: scanGeneral, markup: scanMarkup, lines: scanLines };

// ---- the public surface ----------------------------------------------------

// highlight returns the HTML for one block, or null to mean "render it plain".
// Null is returned for an unknown language, an oversized block, and an
// exhausted budget — the caller does not need to tell those apart.
export function highlight(src, table) {
  if (!table || typeof src !== "string" || src.length === 0 || src.length > MAX_CHARS) return null;
  const scan = SCANNERS[table.scanner || "general"];
  if (!scan) return null;
  const out = new Out(src);
  const budget = { steps: MAX_STEPS };
  const ok = scan(src, table, out, budget);
  if (!ok || out.over) return null;
  return out.done();
}

// stepsUsed reports the budget a block actually consumed. Only the test suite
// calls it; it is exported so the budget claim is a measurement rather than an
// assertion about code nobody re-ran.
export function stepsUsed(src, table) {
  const scan = SCANNERS[table?.scanner || "general"];
  if (!scan || !table) return -1;
  const budget = { steps: MAX_STEPS };
  scan(src, table, new Out(src), budget);
  return MAX_STEPS - budget.steps;
}

// ---- lazy tables -----------------------------------------------------------
//
// One dynamic import for the whole table set. Splitting it per language would
// buy a few hundred bytes and cost a request per language on screen; the set is
// small enough that the win is in not shipping it at boot, not in slicing it.
let tables = null;
let loading = null;
export function loadTables() {
  if (tables) return Promise.resolve(tables);
  if (!loading) {
    loading = import("./langs.js")
      .then((m) => (tables = m.tableFor))
      .catch(() => (tables = () => null));
  }
  return loading.then(() => tables);
}

// tableForLoaded is the synchronous half: it answers only if the tables are
// already in memory, so a caller can highlight during a render without ever
// making the rendered markup depend on whether a module has landed yet.
export const tableForLoaded = (lang) => (tables ? tables(lang) : null);

// ---- the DOM upgrade -------------------------------------------------------
//
// A Svelte action, attached to whatever element holds rendered markdown. The
// markdown renderer keeps emitting exactly what it emitted before — escaped
// plain text inside <pre><code> — and this reaches in afterwards and replaces
// the contents of blocks whose language it knows.
//
// Doing it here rather than inside renderMarkdown is deliberate. The rendered
// HTML string stays a pure function of the message, so it does not change the
// moment a lazily-imported module resolves; if it did, Svelte would replace the
// whole body and re-fetch every image in it. That mistake has already been paid
// for once, in lib/anemoji.js.
//
// The action takes the body text as its argument purely as a change signal —
// the value is never read. That is what lets it rescan when a message is edited
// without an observer per row: the feed windows to 160 rows, and 160 live
// MutationObservers watching subtrees is a real cost for something that has an
// exact, free trigger available.
export function highlightCode(node) {
  let dead = false;

  const upgrade = (pre) => {
    if (pre.dataset.hl) return;
    const code = pre.querySelector("code");
    if (!code) return;
    const lang = pre.dataset.lang || "";
    // textContent, not innerHTML: the browser has already undone the escaping,
    // so the scanner sees the author's original characters and every one of
    // them is escaped again on the way out.
    const src = code.textContent || "";
    if (src.length > MAX_CHARS) {
      pre.dataset.hl = "big";
      return;
    }
    const paint = () => {
      if (dead || pre.dataset.hl) return;
      const html = highlight(src, tableForLoaded(lang));
      pre.dataset.hl = html ? "on" : "off";
      if (html) code.innerHTML = html;
    };
    if (tables) paint();
    else loadTables().then(paint);
  };

  const scan = () => {
    if (dead) return;
    for (const pre of node.querySelectorAll("pre[data-lang]")) upgrade(pre);
  };

  // Immediately for the case where the markup is already in place, and once
  // more on the microtask queue for the case where it is not. Both are cheap
  // and the second is a no-op when the first found everything.
  scan();
  queueMicrotask(scan);
  return {
    update() {
      scan();
      queueMicrotask(scan);
    },
    destroy() {
      dead = true;
    },
  };
}
