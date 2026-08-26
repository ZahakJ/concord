// langs.js — what each language is, as data.
//
// Loaded on demand by lib/highlight.js, which is why this file is nothing but
// tables: adding a language should be an entry here, never a branch in the
// scanner. The three scanners the tables can name are "general" (every
// brace-and-keyword language), "markup" (angle brackets) and "lines" (a diff,
// where the first characters of a line decide the whole line).
//
// A table is deliberately coarse. This is a chat window, not an editor: the
// useful distinctions are comment / string / keyword / type / number / call,
// and everything past that costs scanner passes to draw a difference nobody
// reads at 13px in a message.
//
// Table fields:
//   scanner    "general" (default), "markup", "lines"
//   line       line-comment openers
//   block      [open, close] pairs
//   str        string delimiters: {q, esc (backslash escapes), multi (spans newlines)}
//   keywords   control flow and declarations
//   types      built-in types, constants, and the words that behave like them
//   identStart extra characters that may begin an identifier
//   identPart  extra characters that may continue one
//   keyColon   an identifier or string followed by ':' is a key, not a value
//   lines      for the "lines" scanner: [prefix, kind] in priority order

const set = (s) => new Set(s.split(" "));

// The four string shapes that cover everything in the table below.
const DQ = { q: '"', esc: true };
const SQ = { q: "'", esc: true };
const BACKTICK_RAW = { q: "`", esc: false, multi: true };
const BACKTICK_ESC = { q: "`", esc: true, multi: true };

const C_LINE = ["//"];
const C_BLOCK = [["/*", "*/"]];

// Common denominators, spread into the tables that want them.
const braces = {
  line: C_LINE,
  block: C_BLOCK,
  str: [DQ, SQ],
  identStart: "",
  identPart: "",
};

const JS_KW = set(
  "async await break case catch class const continue debugger default delete do else export extends finally for from function get if import in instanceof let new of return set static super switch this throw try typeof var void while with yield",
);
const JS_TY = set(
  "true false null undefined NaN Infinity Array Boolean Date Error JSON Map Math Number Object Promise RegExp Set String Symbol WeakMap WeakSet console document window globalThis",
);

const TS_KW = set(
  "abstract any as asserts declare enum implements infer interface is keyof namespace never override private protected public readonly satisfies type unknown",
);

const GO_KW = set(
  "break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var",
);
const GO_TY = set(
  "bool byte complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr any true false nil iota append cap close copy delete len make new panic print println recover",
);

const PY_KW = set(
  "and as assert async await break class continue def del elif else except finally for from global if import in is lambda nonlocal not or pass raise return try while with yield match case",
);
const PY_TY = set(
  "True False None self cls bool bytes dict float int list object set str tuple type len range print open enumerate zip map filter sorted super isinstance Exception ValueError TypeError KeyError",
);

const RS_KW = set(
  "as async await break const continue crate dyn else enum extern fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait type unsafe use where while",
);
const RS_TY = set(
  "bool char f32 f64 i8 i16 i32 i64 i128 isize str u8 u16 u32 u64 u128 usize String Vec Option Result Box Some None Ok Err true false",
);

const C_KW = set(
  "alignas alignof auto break case catch class const consteval constexpr continue co_await co_return co_yield default delete do else enum explicit export extern for friend goto if inline mutable namespace new noexcept operator private protected public register return signed sizeof static static_assert struct switch template this throw try typedef typeid typename union unsigned using virtual volatile while " +
    // Preprocessor directives read as one identifier because '#' is an
    // identStart for this table, so they can be listed like any other word.
    "#include #define #undef #if #ifdef #ifndef #elif #else #endif #pragma #error #line",
);
const C_TY = set(
  "bool char double float int long short size_t ssize_t int8_t int16_t int32_t int64_t uint8_t uint16_t uint32_t uint64_t void wchar_t nullptr NULL true false std string vector map",
);

const JAVA_KW = set(
  "abstract assert break case catch class const continue default do else enum extends final finally for goto if implements import instanceof interface native new package private protected public record return sealed static strictfp super switch synchronized this throw throws transient try var volatile while yield",
);
const JAVA_TY = set(
  "boolean byte char double float int long short void Boolean Byte Character Double Float Integer Long Object String List Map Set Exception true false null",
);

const SH_KW = set(
  "if then elif else fi for while until do done case esac function select in return break continue local export readonly declare typeset unset shift trap set source eval exec",
);
const SH_TY = set(
  "echo printf cd ls cp mv rm mkdir rmdir touch cat grep sed awk find sort uniq head tail wc chmod chown kill ps env exit test true false read git go npm make curl sudo",
);

const SQL_KW = set(
  "ADD ALL ALTER AND AS ASC BEGIN BETWEEN BY CASE COMMIT CONSTRAINT CREATE CROSS DEFAULT DELETE DESC DISTINCT DROP ELSE END EXISTS FOREIGN FROM FULL GROUP HAVING IF IN INDEX INNER INSERT INTO IS JOIN KEY LEFT LIKE LIMIT NOT NULL OFFSET ON OR ORDER OUTER PRIMARY REFERENCES REPLACE RETURNING RIGHT ROLLBACK SELECT SET TABLE THEN TRANSACTION UNION UNIQUE UPDATE USING VALUES VIEW WHEN WHERE WITH",
);
const SQL_TY = set(
  "BLOB BOOLEAN CHAR DATE DATETIME DECIMAL DOUBLE FLOAT INT INTEGER JSON NUMERIC REAL SERIAL SMALLINT TEXT TIME TIMESTAMP UUID VARCHAR AUTOINCREMENT COUNT SUM AVG MIN MAX COALESCE",
);

// SQL keywords are shouted by convention but typed either way; matching both
// spellings costs one extra set rather than a case-insensitive lookup on every
// identifier in every other language.
const bothCases = (s) => {
  const out = new Set();
  for (const w of s) {
    out.add(w);
    out.add(w.toLowerCase());
  }
  return out;
};

const TABLES = {
  javascript: { ...braces, str: [DQ, SQ, BACKTICK_ESC], keywords: JS_KW, types: JS_TY },
  typescript: {
    ...braces,
    str: [DQ, SQ, BACKTICK_ESC],
    keywords: new Set([...JS_KW, ...TS_KW]),
    types: JS_TY,
  },
  go: { ...braces, str: [DQ, BACKTICK_RAW, SQ], keywords: GO_KW, types: GO_TY },
  python: {
    line: ["#"],
    block: [],
    // Triple quotes first: the scanner takes delimiters in table order, so a
    // """docstring""" must be offered before the single " that starts it.
    str: [
      { q: '"""', esc: true, multi: true },
      { q: "'''", esc: true, multi: true },
      DQ,
      SQ,
    ],
    keywords: PY_KW,
    types: PY_TY,
    identStart: "",
    identPart: "",
  },
  rust: { ...braces, keywords: RS_KW, types: RS_TY, identPart: "!" },
  c: { ...braces, keywords: C_KW, types: C_TY, identStart: "#", identPart: "" },
  cpp: { ...braces, keywords: C_KW, types: C_TY, identStart: "#", identPart: "" },
  java: { ...braces, keywords: JAVA_KW, types: JAVA_TY, identStart: "@" },
  shell: {
    line: ["#"],
    block: [],
    str: [DQ, SQ],
    keywords: SH_KW,
    types: SH_TY,
    identStart: "$",
    identPart: "-",
  },
  json: {
    line: [],
    block: [],
    str: [DQ],
    keywords: set("true false null"),
    types: new Set(),
    identStart: "",
    identPart: "",
    keyColon: true,
  },
  yaml: {
    line: ["#"],
    block: [],
    str: [DQ, SQ],
    keywords: set("true false null yes no on off"),
    types: new Set(),
    identStart: "",
    identPart: "-",
    keyColon: true,
  },
  toml: {
    line: ["#"],
    block: [],
    str: [DQ, SQ],
    keywords: set("true false"),
    types: new Set(),
    identStart: "",
    identPart: "-",
  },
  sql: {
    line: ["--"],
    block: C_BLOCK,
    str: [SQ, DQ],
    keywords: bothCases(SQL_KW),
    types: bothCases(SQL_TY),
    identStart: "",
    identPart: "",
  },
  css: {
    line: [],
    block: C_BLOCK,
    str: [DQ, SQ],
    keywords: set(
      "@media @supports @keyframes @import @font-face @charset @layer @container !important from to",
    ),
    types: new Set(),
    // A CSS identifier is a hyphenated word, and a custom property or an
    // at-rule begins with a character no other language treats as a word.
    identStart: "@-",
    identPart: "-",
    keyColon: true,
  },
  html: { scanner: "markup" },
  diff: {
    scanner: "lines",
    // Order matters: the file headers start with the same characters as the
    // added and removed lines, so they have to be offered first.
    lines: [
      ["+++", "com"],
      ["---", "com"],
      ["@@", "typ"],
      ["diff ", "com"],
      ["index ", "com"],
      ["+", "add"],
      ["-", "del"],
    ],
  },
};

// What people actually type after the opening fence. An unknown label is not an
// error — it renders plain, exactly as every fence did before this existed.
const ALIASES = {
  js: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  jsx: "javascript",
  node: "javascript",
  ts: "typescript",
  tsx: "typescript",
  golang: "go",
  py: "python",
  python3: "python",
  rs: "rust",
  "c++": "cpp",
  cc: "cpp",
  h: "c",
  hpp: "cpp",
  cs: "java", // near enough at this resolution, and far better than plain
  kotlin: "java",
  kt: "java",
  swift: "java",
  sh: "shell",
  bash: "shell",
  zsh: "shell",
  console: "shell",
  shellsession: "shell",
  fish: "shell",
  jsonc: "json",
  json5: "json",
  yml: "yaml",
  postgres: "sql",
  postgresql: "sql",
  sqlite: "sql",
  mysql: "sql",
  scss: "css",
  sass: "css",
  less: "css",
  xml: "html",
  svg: "html",
  svelte: "html",
  vue: "html",
  htm: "html",
  patch: "diff",
};

// tableFor resolves a fence label to a table, or null. The label is lowercased
// and looked up — it never reaches the output, and a lookup that fails yields
// the pre-feature default (plain text), which is the house rule for every
// peer-supplied value in the app.
export function tableFor(label) {
  if (!label) return null;
  const key = String(label).toLowerCase();
  return TABLES[key] || TABLES[ALIASES[key]] || null;
}

// The languages the app claims to know, for anything that wants to list them.
export const LANGUAGES = Object.keys(TABLES);
