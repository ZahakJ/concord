// Run: node --test src/lib/linkembed.test.mjs
import test from "node:test";
import assert from "node:assert/strict";
import { parseLinkEmbeds, stripLinkEmbed, b64urlEncode, createPreviewPrep, withPreviews, previewLinks } from "./linkembed.js";

const BLOB = "a".repeat(64);
const KEYS = "k".repeat(75);
const tok = (arr) => `[lp](concord://lp/v1/${b64urlEncode(JSON.stringify(arr))})`;
const good = { u: "https://example.com/a", t: "Title", d: "Desc", s: "Example", i: { b: BLOB, k: KEYS, f: "jpeg", w: 400, h: 200 } };

test("valid embed is parsed, image ref kept", () => {
  const r = parseLinkEmbeds(`see https://example.com/a\n${tok([good])}`);
  assert.equal(r.length, 1);
  assert.equal(r[0].title, "Title");
  assert.deepEqual(r[0].image, { blobId: BLOB, keys: KEYS, subtype: "jpeg", w: 400, h: 200 });
});

test("url must be exactly a link in the text (host mismatch and different path rejected)", () => {
  assert.equal(parseLinkEmbeds(`see https://evil.test/a ${tok([good])}`).length, 0);
  assert.equal(parseLinkEmbeds(`see https://example.com/other ${tok([good])}`).length, 0);
  assert.equal(parseLinkEmbeds(`no link here ${tok([good])}`).length, 0);
});

test("non-http schemes and userinfo rejected", () => {
  for (const u of ["javascript:alert(1)", "ftp://example.com/a", "https://u:p@example.com/a", "data:text/html,x"]) {
    assert.equal(parseLinkEmbeds(`${u} ${tok([{ ...good, u }])}`).length, 0, u);
  }
});

test("text is plain and capped; bidi and control chars stripped", () => {
  const e = { ...good, t: "T‮evil\u0007".padEnd(600, "x"), d: "d".repeat(1000), s: "s".repeat(300), i: undefined };
  const [r] = parseLinkEmbeds(`https://example.com/a ${tok([e])}`);
  assert.ok(!/[‮\u0007]/.test(r.title));
  assert.equal(Array.from(r.title).length, 200);
  assert.equal(Array.from(r.desc).length, 400);
  assert.equal(Array.from(r.site).length, 100);
});

test("at most three embeds; bad image refs dropped but card kept", () => {
  const many = ["a", "b", "c", "d"].map((x) => ({ ...good, u: `https://example.com/${x}` }));
  const text = many.map((e) => e.u).join(" ");
  assert.equal(parseLinkEmbeds(`${text} ${tok(many)}`).length, 3);
  const bad = { ...good, i: { b: "zz", k: KEYS, f: "jpeg", w: 10, h: 10 } };
  assert.equal(parseLinkEmbeds(`https://example.com/a ${tok([bad])}`)[0].image, null);
  const big = { ...good, i: { ...good.i, w: 4000 } };
  assert.equal(parseLinkEmbeds(`https://example.com/a ${tok([big])}`)[0].image, null);
});

test("garbage, oversize and malformed tokens yield nothing", () => {
  assert.deepEqual(parseLinkEmbeds("[lp](concord://lp/v1/!!!)"), []);
  assert.deepEqual(parseLinkEmbeds(`[lp](concord://lp/v1/${"A".repeat(9000)})`), []);
  assert.deepEqual(parseLinkEmbeds(`https://example.com/a ${tok({ not: "array" })}`), []);
  assert.deepEqual(parseLinkEmbeds(""), []);
});

test("strip removes the token from the body", () => {
  assert.equal(stripLinkEmbed(`hi https://example.com/a\n${tok([good])}`), "hi https://example.com/a");
});

test("prep: take returns only what is ready for the current links, never waits", async () => {
  let calls = 0;
  const prep = createPreviewPrep(async () => { calls++; return "TOKEN"; }, { delay: 5 });
  const text = "look https://example.com/a";
  prep.update(text);
  assert.equal(prep.take(text), ""); // not ready yet: send goes without
  await new Promise((r) => setTimeout(r, 30));
  assert.equal(prep.take(text), "TOKEN");
  assert.equal(prep.take("look https://example.com/b"), ""); // link changed
  prep.update(text);
  await new Promise((r) => setTimeout(r, 30));
  assert.equal(calls, 1); // same links: no refetch
});

test("prep: disabled toggle prepares and attaches nothing", async () => {
  let calls = 0;
  const prep = createPreviewPrep(async () => { calls++; return "T"; }, { delay: 1, enabled: () => false });
  prep.update("https://example.com/a");
  await new Promise((r) => setTimeout(r, 20));
  assert.equal(calls, 0);
  assert.equal(prep.take("https://example.com/a"), "");
});

test("helpers", () => {
  assert.equal(withPreviews("x", ""), "x");
  assert.equal(withPreviews("x", "T"), "x\nT");
  assert.deepEqual(previewLinks("a https://x.io/1 b https://x.io/2 https://x.io/3 https://x.io/4"), ["https://x.io/1", "https://x.io/2", "https://x.io/3"]);
});
