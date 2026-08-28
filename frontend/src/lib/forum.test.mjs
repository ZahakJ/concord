// forum.test.mjs — the board's logic, without a browser.
//
// Run: node src/lib/forum.test.mjs
import assert from "node:assert/strict";
import {
  SORTS,
  LAYOUTS,
  TAG_LIMITS,
  SCRIM_FLOOR,
  TINT_ALPHA,
  runeLen,
  normalizeHex,
  validateTag,
  sortPosts,
  filterPosts,
  arrangePosts,
  searchMatches,
  resolveTags,
  boardStats,
  isPending,
  postPreview,
  firstImage,
  relTime,
  absTime,
  hash32,
  washFor,
  tileFor,
  contrastRatio,
  composite,
  normalizeBoardPrefs,
  boardPrefKey,
} from "./forum.js";

let n = 0;
function t(name, fn) {
  fn();
  n++;
  void name;
}

// A real token, byte-for-byte the shape internal/app/attach.go emits: 64 hex
// blob ID, 75 url-safe key chars, subtype, WxH.
const BLOB = "a".repeat(64);
const BLOB2 = "b".repeat(64);
const KEYS = "K".repeat(75);
const imgTok = (blob = BLOB) => `![image](concord://attach/v1/${blob}/${KEYS}/png/800x600)`;
const fileTok = `[file](concord://file/v1/${BLOB}/${KEYS}/1234/dGV4dC9wbGFpbg/bm90ZXMudHh0)`;

const post = (o = {}) => ({
  id: "p",
  title: "",
  tags: [],
  pinned: false,
  solved: false,
  authorFingerprint: "FPR",
  authorName: "Ada",
  excerpt: "",
  replies: 0,
  created: 1e15,
  lastActivity: 1e15,
  unanswered: false,
  ...o,
});

// ---- sorting -------------------------------------------------------------

t("recent sort is newest activity first", () => {
  const a = post({ id: "a", lastActivity: 300 });
  const b = post({ id: "b", lastActivity: 100 });
  const c = post({ id: "c", lastActivity: 200 });
  assert.deepEqual(
    sortPosts([a, b, c], "recent").map((p) => p.id),
    ["a", "c", "b"],
  );
});

t("pinned posts stay first under every sort", () => {
  const pin = post({ id: "pin", pinned: true, lastActivity: 1, created: 1, replies: 0 });
  const busy = post({ id: "busy", lastActivity: 900, created: 900, replies: 40 });
  const old = post({ id: "old", lastActivity: 500, created: 2, replies: 3 });
  for (const s of SORTS.map((x) => x.id)) {
    assert.equal(sortPosts([busy, old, pin], s)[0].id, "pin", `pin lost under ${s}`);
  }
});

t("sort is stable: equal keys keep the backend's order", () => {
  const list = ["a", "b", "c", "d"].map((id) => post({ id, replies: 7, lastActivity: 500 }));
  assert.deepEqual(
    sortPosts(list, "replies").map((p) => p.id),
    ["a", "b", "c", "d"],
  );
});

t("oldest sort puts a pending post (created 0) last, not first", () => {
  const pending = post({ id: "pending", created: 0, lastActivity: 0 });
  const first = post({ id: "first", created: 100, lastActivity: 400 });
  const second = post({ id: "second", created: 200, lastActivity: 300 });
  assert.deepEqual(
    sortPosts([pending, second, first], "oldest").map((p) => p.id),
    ["first", "second", "pending"],
  );
});

t("most replies breaks ties on recency", () => {
  const a = post({ id: "a", replies: 5, lastActivity: 100 });
  const b = post({ id: "b", replies: 5, lastActivity: 900 });
  const c = post({ id: "c", replies: 9, lastActivity: 1 });
  assert.deepEqual(
    sortPosts([a, b, c], "replies").map((p) => p.id),
    ["c", "b", "a"],
  );
});

t("sortPosts does not mutate its input", () => {
  const list = [post({ id: "a", lastActivity: 1 }), post({ id: "b", lastActivity: 2 })];
  sortPosts(list, "recent");
  assert.deepEqual(
    list.map((p) => p.id),
    ["a", "b"],
  );
});

// ---- filtering -----------------------------------------------------------

const TAGS = [
  { id: "t1", name: "Bug", color: "#e0555b", emoji: "🐛" },
  { id: "t2", name: "Idea", color: "#14a394", emoji: "" },
  { id: "t3", name: "Docs", color: "#d9a13c", emoji: "📚" },
];

t("tag filter is OR across several tags", () => {
  const bug = post({ id: "bug", tags: ["t1"] });
  const idea = post({ id: "idea", tags: ["t2"] });
  const both = post({ id: "both", tags: ["t1", "t2"] });
  const none = post({ id: "none", tags: [] });
  const docs = post({ id: "docs", tags: ["t3"] });
  const got = filterPosts([bug, idea, both, none, docs], { tagIds: ["t1", "t2"] }).map((p) => p.id);
  assert.deepEqual(got, ["bug", "idea", "both"]);
});

t("no tag chips selected shows everything", () => {
  const list = [post({ id: "a", tags: [] }), post({ id: "b", tags: ["t1"] })];
  assert.equal(filterPosts(list, { tagIds: [] }).length, 2);
});

t("unanswered filter uses the backend's flag", () => {
  const open = post({ id: "open", unanswered: true });
  const answered = post({ id: "answered", unanswered: false, replies: 2 });
  const solved = post({ id: "solved", unanswered: false, solved: true });
  assert.deepEqual(
    filterPosts([open, answered, solved], { unansweredOnly: true }).map((p) => p.id),
    ["open"],
  );
});

t("unanswered falls back to !solved && !replies when the flag is absent", () => {
  const p = { id: "x", tags: [], replies: 0, solved: false, title: "", excerpt: "" };
  assert.equal(filterPosts([p], { unansweredOnly: true }).length, 1);
  assert.equal(filterPosts([{ ...p, replies: 1 }], { unansweredOnly: true }).length, 0);
  assert.equal(filterPosts([{ ...p, solved: true }], { unansweredOnly: true }).length, 0);
});

// ---- search --------------------------------------------------------------

t("search matches titles case-insensitively", () => {
  const p = post({ title: "Voice channel keeps dropping" });
  assert.ok(searchMatches(p, "VOICE"));
  assert.ok(searchMatches(p, "dropping"));
  assert.ok(!searchMatches(p, "video"));
});

t("search terms are ANDed and order-independent", () => {
  const p = post({ title: "API bug in the invite code" });
  assert.ok(searchMatches(p, "bug api"));
  assert.ok(!searchMatches(p, "bug rendezvous"));
});

t("search folds diacritics both ways", () => {
  assert.ok(searchMatches(post({ title: "Café crash" }), "cafe"));
  assert.ok(searchMatches(post({ title: "Cafe crash" }), "café"));
});

t("search reaches the body preview and the author, not the raw token", () => {
  const p = post({ title: "Screenshot", excerpt: `${imgTok()} the sidebar looks wrong`, authorName: "Grace" });
  assert.ok(searchMatches(p, "sidebar"), "body should be searchable");
  assert.ok(searchMatches(p, "grace"), "author should be searchable");
  assert.ok(!searchMatches(p, "concord://"), "a token must not be searchable text");
  assert.ok(!searchMatches(p, BLOB.slice(0, 12)), "a blob id must not be searchable text");
});

t("an empty query matches everything", () => {
  assert.ok(searchMatches(post({ title: "x" }), ""));
  assert.ok(searchMatches(post({ title: "x" }), "   "));
});

t("arrangePosts filters then sorts, pinned still first", () => {
  const list = [
    post({ id: "a", title: "bug one", tags: ["t1"], lastActivity: 100 }),
    post({ id: "b", title: "bug two", tags: ["t1"], lastActivity: 300, pinned: true }),
    post({ id: "c", title: "idea", tags: ["t2"], lastActivity: 900 }),
  ];
  assert.deepEqual(
    arrangePosts(list, { query: "bug", tagIds: ["t1"], sort: "recent" }).map((p) => p.id),
    ["b", "a"],
  );
});

// ---- tags ----------------------------------------------------------------

t("resolveTags drops unknown ids silently and keeps palette order", () => {
  assert.deepEqual(
    resolveTags(["t3", "deleted-id", "t1"], TAGS).map((x) => x.name),
    ["Bug", "Docs"],
  );
  assert.deepEqual(resolveTags(["gone"], TAGS), []);
  assert.deepEqual(resolveTags([], TAGS), []);
  assert.deepEqual(resolveTags(["t1"], []), []);
});

t("runeLen counts characters, not UTF-16 units", () => {
  assert.equal(runeLen("abc"), 3);
  assert.equal("𝔘".length, 2);
  assert.equal(runeLen("𝔘"), 1);
  assert.equal(runeLen("𝔘".repeat(TAG_LIMITS.nameRunes)), TAG_LIMITS.nameRunes);
});

t("normalizeHex accepts only a full lowercase #rrggbb", () => {
  assert.equal(normalizeHex("#AABBCC"), "#aabbcc");
  assert.equal(normalizeHex("#abc"), "");
  assert.equal(normalizeHex("red"), "");
  assert.equal(normalizeHex(""), "");
});

t("validateTag mirrors the backend's limits", () => {
  assert.equal(validateTag({ name: "Bug", color: "#e0555b", emoji: "🐛" }), "");
  assert.notEqual(validateTag({ name: "", color: "#e0555b" }), "");
  assert.notEqual(validateTag({ name: "x".repeat(TAG_LIMITS.nameRunes + 1), color: "#e0555b" }), "");
  assert.equal(validateTag({ name: "𝔘".repeat(TAG_LIMITS.nameRunes), color: "#e0555b" }), "");
  assert.notEqual(validateTag({ name: "𝔘".repeat(TAG_LIMITS.nameRunes + 1), color: "#e0555b" }), "");
  assert.notEqual(validateTag({ name: "Bug", color: "#e0555b", emoji: "x".repeat(9) }), "");
  assert.notEqual(validateTag({ name: "Bug", color: "#abc" }), "");
});

// ---- preview -------------------------------------------------------------

t("preview never leaks an attachment token", () => {
  const p = postPreview(`${imgTok()} look at this`);
  assert.equal(p.text, "look at this");
  assert.equal(p.kind, "image");
  assert.equal(p.images, 1);
  assert.ok(!p.text.includes("concord://"));
});

t("preview survives a token cut in half by the 240-rune excerpt limit", () => {
  const cut = `here it is ![image](concord://attach/v1/${BLOB}/KKKK…`;
  const p = postPreview(cut);
  assert.equal(p.text, "here it is");
  assert.ok(!p.text.includes("concord://"));
});

t("an image-only post previews as empty text, flagged as media", () => {
  const p = postPreview(imgTok());
  assert.equal(p.text, "");
  assert.equal(p.kind, "image");
});

t("file tokens are stripped and flagged", () => {
  const p = postPreview(`${fileTok} the log`);
  assert.equal(p.text, "the log");
  assert.equal(p.kind, "file");
});

t("preview strips markdown to its words", () => {
  assert.equal(postPreview("**bold** and *italic* and ~~gone~~").text, "bold and italic and gone");
  assert.equal(postPreview("# Heading then text").text, "Heading then text");
  assert.equal(postPreview("`code()` runs").text, "code() runs");
  assert.equal(postPreview("see [the docs](https://example.com/x) now").text, "see the docs now");
  assert.equal(postPreview("||secret|| told").text, "secret told");
  assert.equal(postPreview("> quoted line").text, "quoted line");
  assert.equal(postPreview("- one - two").text, "one two");
});

t("preview leaves snake_case and arithmetic alone", () => {
  assert.equal(postPreview("call some_helper_name once").text, "call some_helper_name once");
  assert.equal(postPreview("3 * 4 is 12").text, "3 * 4 is 12");
});

t("preview sweeps a marker left dangling by truncation", () => {
  assert.equal(postPreview("**unclosed bold…").text, "unclosed bold…");
});

t("a poll body previews as its question", () => {
  // Same token shape polls.js emits.
  const payload = Buffer.from(JSON.stringify({ q: "Ship it?", opts: ["yes", "no"] }))
    .toString("base64")
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
  const p = postPreview(`[poll](concord://poll/v1/${payload})`);
  assert.equal(p.text, "Ship it?");
  assert.equal(p.kind, "poll");
});

t("a truncated poll token does not leak base64", () => {
  const p = postPreview("[poll](concord://poll/v1/eyJxIjoiU2hpcCBp…");
  assert.ok(!p.text.includes("eyJx"), `leaked: ${p.text}`);
});

t("empty and missing excerpts are safe", () => {
  assert.deepEqual(postPreview(""), { text: "", kind: "", images: 0 });
  assert.deepEqual(postPreview(undefined), { text: "", kind: "", images: 0 });
  assert.deepEqual(postPreview(null), { text: "", kind: "", images: 0 });
});

// ---- card media ----------------------------------------------------------

t("firstImage returns the first inline image token", () => {
  const tok = firstImage(`hi ${imgTok()} and ${imgTok(BLOB2)}`);
  assert.equal(tok.blobId, BLOB);
  assert.equal(tok.subtype, "png");
  assert.equal(tok.w, 800);
  assert.equal(tok.h, 600);
});

t("firstImage skips a spoilered image so a board can't unhide it", () => {
  const spoiler = `![image](concord://attach/v2/${BLOB}/${KEYS}/png/800x600/1//)`;
  assert.equal(firstImage(spoiler), null);
  assert.equal(firstImage(`${spoiler} ${imgTok(BLOB2)}`).blobId, BLOB2);
});

t("firstImage returns null with no image", () => {
  assert.equal(firstImage("just words"), null);
  assert.equal(firstImage(fileTok), null);
  assert.equal(firstImage(""), null);
});

// ---- time ----------------------------------------------------------------

t("relTime speaks in the units a card has room for", () => {
  const now = 1_700_000_000_000;
  const ns = (msAgo) => (now - msAgo) * 1e6;
  assert.equal(relTime(0, now), "");
  assert.equal(relTime(ns(1000), now), "just now");
  assert.equal(relTime(ns(5 * 60e3), now), "5m ago");
  // Never "0m ago" in the 45s–60s gap between "just now" and one minute.
  assert.equal(relTime(ns(50e3), now), "1m ago");
  assert.equal(relTime(ns(3 * 3600e3), now), "3h ago");
  assert.equal(relTime(ns(2 * 86400e3), now), "2d ago");
  assert.equal(relTime(ns(3 * 7 * 86400e3), now), "3w ago");
  assert.equal(relTime(ns(400 * 86400e3), now), "1y ago");
});

t("a peer's clock running fast reads as 'just now', not a negative age", () => {
  const now = 1_700_000_000_000;
  assert.equal(relTime((now + 60e3) * 1e6, now), "just now");
});

t("absTime is empty for an unknown time", () => {
  assert.equal(absTime(0), "");
  assert.ok(absTime(1_700_000_000_000 * 1e6).length > 0);
});

// ---- generated art -------------------------------------------------------

t("hash32 is stable and spreads", () => {
  assert.equal(hash32("forum-a"), hash32("forum-a"));
  assert.notEqual(hash32("forum-a"), hash32("forum-b"));
  assert.equal(hash32(""), hash32(""));
});

t("washFor is deterministic and stays inside its lightness band", () => {
  const a = washFor("forum-1");
  assert.deepEqual(a, washFor("forum-1"));
  assert.notDeepEqual(a, washFor("forum-2"));
  for (const seed of ["a", "b", "c", "forum-xyz", ""]) {
    const w = washFor(seed);
    assert.match(w.color, /^hsl\(\d{1,3} 62% 46%\)$/);
    assert.match(w.color2, /^hsl\(\d{1,3} 68% 24%\)$/);
    assert.ok(w.angle >= 110 && w.angle <= 160);
  }
});

t("tileFor picks the title's first letter, or nothing at all", () => {
  assert.equal(tileFor("id", "Voice bug").letter, "V");
  assert.equal(tileFor("id", "  ...bug").letter, "B");
  assert.equal(tileFor("id", "42 things").letter, "4");
  assert.equal(tileFor("id", "…").letter, "");
  assert.equal(tileFor("id", "").letter, "");
  assert.deepEqual(tileFor("id", "x"), tileFor("id", "x"));
});

// ---- contrast, measured not assumed --------------------------------------

t("white ink over the scrim floor clears 4.5:1 on the brightest possible art", () => {
  // Worst case: the art under the scrim is pure white. Every preset gradient and
  // every uploaded image is darker than this somewhere, so clearing it here
  // clears it everywhere.
  const worst = composite("#ffffff", "#000000", SCRIM_FLOOR);
  const ratio = contrastRatio("#ffffff", worst);
  assert.ok(ratio >= 4.5, `white on the scrim floor is only ${ratio.toFixed(2)}:1`);
});

t("the scrim floor is not wastefully dark either", () => {
  // If a much thinner scrim would do, the art is being hidden for nothing. This
  // pins the floor to roughly the alpha the guarantee actually needs.
  const thinner = composite("#ffffff", "#000000", SCRIM_FLOOR - 0.12);
  assert.ok(contrastRatio("#ffffff", thinner) < 4.5, "the scrim could be lighter");
});

t("a tag chip's label survives any tag colour on any card surface", () => {
  // Worst case for a tint is white; the two surfaces are the dark theme's card
  // (--bg-1) and the light theme's card (--bg-1 in light).
  for (const surface of ["#16181c", "#e9ebee"]) {
    const ink = surface === "#16181c" ? "#e8eaed" : "#1c1e22";
    for (const tag of ["#ffffff", "#000000", "#14a394", "#e0555b", "#d9a13c"]) {
      const tinted = composite(surface, tag, TINT_ALPHA);
      const ratio = contrastRatio(ink, tinted);
      assert.ok(ratio >= 4.5, `${ink} on ${tag}@${TINT_ALPHA} over ${surface} is ${ratio.toFixed(2)}:1`);
    }
  }
});

t("contrastRatio agrees with the known values", () => {
  assert.equal(Math.round(contrastRatio("#ffffff", "#000000")), 21);
  assert.equal(Math.round(contrastRatio("#ffffff", "#ffffff")), 1);
});

// ---- device-local prefs --------------------------------------------------

t("board prefs are keyed per forum", () => {
  assert.notEqual(boardPrefKey("f1"), boardPrefKey("f2"));
  assert.ok(boardPrefKey("f1").includes("f1"));
});

t("normalizeBoardPrefs refuses junk from localStorage", () => {
  assert.deepEqual(normalizeBoardPrefs(null), { layout: "list", sort: "recent", banner: "" });
  assert.deepEqual(normalizeBoardPrefs("nonsense"), { layout: "list", sort: "recent", banner: "" });
  assert.deepEqual(normalizeBoardPrefs({ layout: "wat", sort: "wat", banner: 5 }), {
    layout: "list",
    sort: "recent",
    banner: "",
  });
  assert.deepEqual(normalizeBoardPrefs({ layout: "cover", sort: "oldest", banner: "preset:galaxy" }), {
    layout: "cover",
    sort: "oldest",
    banner: "preset:galaxy",
  });
  for (const l of LAYOUTS) assert.equal(normalizeBoardPrefs({ layout: l.id }).layout, l.id);
});

// ---- board header numbers ------------------------------------------------

t("boardStats counts what the header prints", () => {
  const s = boardStats([
    post({ unanswered: true }),
    post({ unanswered: true }),
    post({ unanswered: false, replies: 3 }),
    post({ unanswered: false, pinned: true, solved: true }),
    post({ unanswered: false, replies: 1, locked: true }),
  ]);
  assert.deepEqual(s, { total: 5, unanswered: 2, pinned: 1, closed: 1 });
  assert.deepEqual(boardStats([]), { total: 0, unanswered: 0, pinned: 0, closed: 0 });
});

t("isPending is exactly 'no opening message yet'", () => {
  assert.ok(isPending({ created: 0, authorFingerprint: "", excerpt: "" }));
  assert.ok(!isPending({ created: 1, authorFingerprint: "", excerpt: "" }));
  assert.ok(!isPending(post()));
});

console.log(`forum.js: all ${n} tests passed`);
