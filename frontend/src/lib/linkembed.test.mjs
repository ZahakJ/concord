// Run: node --test src/lib/linkembed.test.mjs
import test from "node:test";
import assert from "node:assert/strict";
import { stripLinkEmbed } from "./linkembed.js";
import { cardFor } from "./linkcard.js";

const b64 = (s) => Buffer.from(s, "utf8").toString("base64url");
const tok = (arr) => `[lp](concord://lp/v1/${b64(JSON.stringify(arr))})`;
const good = { u: "https://example.com/a", t: "Title", d: "Desc", s: "Example" };

test("strip removes the token from the body", () => {
  assert.equal(stripLinkEmbed(`hi https://example.com/a\n${tok([good])}`), "hi https://example.com/a");
  assert.equal(stripLinkEmbed(`${tok([good])}`), "");
});

test("a rider from a development build draws no card", () => {
  // Its site name could be anything ("YouTube" on evil.example); the card form
  // is the only one drawn, and it prints the URL's own domain.
  const m = { content: `https://evil.example/a\n${tok([{ ...good, u: "https://evil.example/a", s: "YouTube" }])}`, kind: "" };
  assert.equal(cardFor(m), null);
});
