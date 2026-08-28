// snippet.test.mjs — the flattening rule, without a browser.
//
// Run: node src/lib/snippet.test.mjs
//
// These are the same cases internal/app/snippet_test.go holds the Go half to.
// The two implementations exist because the inbox cuts its previews in Go
// before they reach the page and the reply strip cuts its own in the browser;
// they must agree, because an inbox row and a reply strip quoting the SAME
// message are read one after the other.
import assert from "node:assert/strict";
import { plainSnippet, TOKEN_LABELS } from "./snippet.js";

const b64u = (s) => Buffer.from(s, "utf8").toString("base64url");
const hex64 = "a".repeat(64);
const key75 = "A".repeat(75);

const cases = [
  ["an image token never reaches the reader", `![image](concord://attach/v1/${hex64}/${key75}/png/800x600)`, "🖼 image"],
  [
    "an image with a caption shows the caption",
    `look at this ![image](concord://attach/v1/${hex64}/${key75}/png/800x600)`,
    "🖼 look at this",
  ],
  [
    "a half-cut token is still not a URI",
    "![image](concord://attach/v2/20ed697d4012474c24a40a5de4f800133abc/AAA/png/800/600/0//)",
    "🖼 image",
  ],
  ["a file token becomes a paperclip", `[file](concord://file/v1/${hex64}/${key75}/1234/dGV4dA/bmFtZQ)`, "📎 file"],
  ["inline code loses its backticks", "`rules.apply` returning null…", "rules.apply returning null…"],
  ["bold and italics lose their markers", "the **whole** trick is *the fold*", "the whole trick is the fold"],
  ["a link keeps its label", "see [the spec](https://example.org/spec) first", "see the spec first"],
  ["a quote loses its angle", "> quoted line", "quoted line"],
  [
    "a fenced block is named, not flattened",
    "here it is in eleven lines:\n```js\nexport function fold(a, b) {\n  return a + b;\n}\n```",
    "here it is in eleven lines: 📄 code block",
  ],
  ["an unterminated fence is named too", "cut mid-payload:\n```js\nexport function fo", "cut mid-payload: 📄 code block"],
  [
    "a poll is its question",
    `[poll](concord://poll/v1/${b64u(JSON.stringify({ q: "Which surface next?", opts: ["a", "b"] }))})`,
    "📊 Which surface next?",
  ],
  ["a corrupt poll is still not base64", "[poll](concord://poll/v1/zzzz)", "📊 Poll"],
  ["a game is named", "[game](concord://game/v1/AQIDBAUGBwgJ)", TOKEN_LABELS.game],
  ["a doodle is named", "[doodle](concord://doodle/v1/AQIDBAUGBwgJ)", TOKEN_LABELS.doodle],
  ["a sound is named", "[sound](concord://sfx/v1/AQIDBAUGBwgJ)", TOKEN_LABELS.sound],
  [
    "an announcement is its body",
    `[announcement](concord://announce/v1/${b64u(JSON.stringify({ body: "**Doors** at seven", from: "general" }))})`,
    "📣 Doors at seven",
  ],
  ["a send effect is not the message", "[fx](concord://fx/v1/confetti) we shipped", "we shipped"],
  ["a disappearing timer is not the message", "[eph](concord://eph/v1/1234567890) burn after reading", "burn after reading"],
  ["snake_case survives", "call read_all_rows now", "call read_all_rows now"],
  ["newlines collapse", "one\n\ntwo\nthree", "one two three"],
];

let fails = 0;
for (const [name, input, want] of cases) {
  const got = plainSnippet(input);
  if (got !== want) {
    fails++;
    console.error(`FAIL ${name}\n  in   ${JSON.stringify(input)}\n  got  ${JSON.stringify(got)}\n  want ${JSON.stringify(want)}`);
  }
}
assert.equal(fails, 0, `${fails} snippet case(s) disagree with the Go half`);

// The cap trims and marks; it never cuts in the middle of a surrogate pair,
// because slicing a JS string by code unit can.
const long = "سلام ".repeat(80);
const capped = plainSnippet(long, 60);
assert.ok(capped.length <= 61, `cap overshot: ${capped.length}`);
assert.ok(capped.endsWith("…"), "capped text should end with an ellipsis");
assert.equal(plainSnippet("short", 60), "short");
assert.equal(plainSnippet("", 60), "");
assert.equal(plainSnippet(null, 60), "");

console.log(`snippet.test.mjs: ${cases.length + 4} checks passed`);
