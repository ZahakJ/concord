// forum.js — the forum board's decisions, separated from its pixels.
//
// A board is mostly judgement calls: which posts are showing, in what order,
// summarised how, tinted what colour, and whether the ink on top of the art can
// actually be read. Every one of those is a pure function of data, so every one
// of them lives here and is tested (forum.test.mjs) instead of being trapped
// inside a component where the only way to check it is to look at it.
//
// Times from the backend are UnixNano everywhere (ForumPost.created /
// .lastActivity and ChannelView.lastActivity all agree), so nothing in this file
// takes milliseconds. Dividing by 1e6 happens once, in relTime.
import { ATTACH_RE, FILE_RE, parseAttachTokens } from "./attachments.js";
import { parsePoll } from "./polls.js";
import { parseEmbed, EMBED_RE } from "./richembed.js";

// ---- board options -------------------------------------------------------

// Sorts, in menu order. `recent` is the backend's own order, so picking it costs
// nothing; the other two re-sort in place.
export const SORTS = [
  // `short` is for the desktop segmented control, where three full labels do not
  // fit and truncating "Most replies" to its first word says "Most" — a word
  // that means nothing on its own.
  { id: "recent", label: "Recent activity", short: "Recent", icon: "bolt" },
  { id: "replies", label: "Most replies", short: "Replies", icon: "reply" },
  { id: "oldest", label: "Oldest first", short: "Oldest", icon: "clock" },
];

// Card layouts. Three, because a forum is not one kind of place: a support queue
// wants density, a screenshot board wants pictures. Device-local (see
// normalizeBoardPrefs) — a reading preference is nobody else's business and
// syncing it would need a wire field for a choice that changes per screen size.
export const LAYOUTS = [
  { id: "list", label: "List", hint: "Dense rows, thumbnail on the left" },
  { id: "gallery", label: "Gallery", hint: "Media on top, text underneath" },
  { id: "cover", label: "Cover", hint: "Full-bleed art behind the title" },
];

// Mirrors the server-side limits (internal/app/forum.go) so the UI can refuse
// before the round trip and say why. Duplicated deliberately: a client that
// discovers a limit by being rejected shows the user an error instead of a
// disabled button.
export const TAG_LIMITS = { perForum: 20, perPost: 5, nameRunes: 24, emojiChars: 8 };

export const HEX_RE = /^#[0-9a-f]{6}$/;

// runeLen counts CODE POINTS, not UTF-16 units: "𝔘" is one character to a
// person and two to str.length, and the backend counts the way a person does.
export const runeLen = (s) => [...String(s || "")].length;

// normalizeHex lowercases what a native <input type="color"> gives us. The
// backend rejects "#ABC" and "red" outright, and so should we — this only
// launders case, it never invents a format.
export function normalizeHex(v) {
  const s = String(v || "").trim().toLowerCase();
  return HEX_RE.test(s) ? s : "";
}

// validateTag returns "" for a valid palette entry or a sentence to show the
// user. Same order of checks as the backend, so the message matches the reason.
export function validateTag(tag) {
  const name = String(tag?.name || "").trim();
  if (!name) return "A tag needs a name.";
  if (runeLen(name) > TAG_LIMITS.nameRunes)
    return `Keep the name to ${TAG_LIMITS.nameRunes} characters (that one is ${runeLen(name)}).`;
  if (runeLen(tag?.emoji || "") > TAG_LIMITS.emojiChars)
    return `An emoji can be at most ${TAG_LIMITS.emojiChars} characters.`;
  if (!normalizeHex(tag?.color)) return "Pick a colour for the tag.";
  return "";
}

// ---- ordering and filtering ----------------------------------------------

// A pending post (no opening message synced yet) has created 0. For "oldest"
// that must not read as "the beginning of time" — it is an unknown, and an
// unknown sorts last.
const oldestKey = (p) => p.created || p.lastActivity || Number.MAX_SAFE_INTEGER;

const SORT_CMP = {
  recent: (a, b) => (b.lastActivity || 0) - (a.lastActivity || 0),
  oldest: (a, b) => oldestKey(a) - oldestKey(b),
  // Ties on reply count fall back to recency, so "most replies" on a fresh
  // board (everything at 0) still reads as a sensible list rather than a
  // shuffle.
  replies: (a, b) => (b.replies || 0) - (a.replies || 0) || (b.lastActivity || 0) - (a.lastActivity || 0),
};

// sortPosts returns a NEW array, pinned posts first whatever the sort is.
//
// Pinning is a board-level decision by someone with Manage Messages; a sort is
// one reader's preference. If "Oldest first" could bury the pinned rules post,
// pinning would mean nothing. Array#sort is stable (ES2019), so equal elements
// keep the order the backend sent — which is already pinned-then-recent.
export function sortPosts(posts, sortId = "recent") {
  const cmp = SORT_CMP[sortId] || SORT_CMP.recent;
  return [...(posts || [])].sort((a, b) => Number(!!b.pinned) - Number(!!a.pinned) || cmp(a, b));
}

// fold makes text comparable: diacritics off, case off. Searching "cafe" has to
// find "Café" — anything else is a bug report from a user with an accent in
// their vocabulary.
export const fold = (s) =>
  String(s || "")
    .normalize("NFD")
    .replace(/\p{Diacritic}/gu, "")
    .toLowerCase();

// searchMatches: every whitespace-separated term must appear somewhere in the
// post's haystack (title, preview text, author name). AND over terms so a
// second word narrows — "api bug" finding "API bug" and not every post with an
// API in it — and OR over fields so people don't have to know where a word was.
export function searchMatches(post, query) {
  const terms = fold(query).split(/\s+/).filter(Boolean);
  if (!terms.length) return true;
  const hay = fold(`${post?.title || ""} ${postPreview(post?.excerpt).text} ${post?.authorName || ""}`);
  return terms.every((t) => hay.includes(t));
}

// filterPosts applies the toolbar. Tag chips are OR, not AND: posts carry one
// or two tags, so an AND of two chips almost always yields an empty board —
// and a row of chips reads as "show me these", not "show me posts that are all
// of these".
export function filterPosts(posts, { query = "", tagIds = [], unansweredOnly = false } = {}) {
  const want = new Set(tagIds || []);
  return (posts || []).filter((p) => {
    // `unanswered` is computed by the backend so every client agrees on the
    // word; the fallback is only for a post object we built ourselves.
    if (unansweredOnly && !(p.unanswered ?? (!p.solved && !p.replies))) return false;
    if (want.size && !(p.tags || []).some((id) => want.has(id))) return false;
    return searchMatches(p, query);
  });
}

// arrangePosts is what the board actually calls: filter, then sort.
export const arrangePosts = (posts, opts = {}) => sortPosts(filterPosts(posts, opts), opts.sort);

// resolveTags turns a post's tag IDs into palette entries, dropping IDs the
// palette no longer defines — deleting a tag deliberately leaves its ID on
// every post that carried it, and a chip we cannot name is worse than no chip.
// Palette order, not post order: chips then line up across every card on the
// board instead of shuffling per post.
export function resolveTags(ids, palette) {
  if (!ids?.length || !palette?.length) return [];
  const want = new Set(ids);
  return palette.filter((t) => want.has(t.id));
}

// boardStats: the numbers the header prints.
export function boardStats(posts) {
  const list = posts || [];
  let unanswered = 0;
  let pinned = 0;
  let closed = 0;
  for (const p of list) {
    if (p.unanswered ?? (!p.solved && !p.replies)) unanswered++;
    if (p.pinned) pinned++;
    // A board that could count pinned posts but not closed ones was the reason
    // "Close post" looked like an action with no effect.
    if (p.locked) closed++;
  }
  return { total: list.length, unanswered, pinned, closed };
}

// isPending: the post's channel record arrived but its opening message hasn't.
// created and the author key are written by the same query from the same row
// (store.postStatsInto), so created === 0 is exactly "no opening message yet" —
// not "a post by nobody at the epoch".
export const isPending = (p) => !p?.created;

// ---- card preview --------------------------------------------------------

// The backend's excerpt is the opening message's RAW text with whitespace
// collapsed and a cut at 240 runes. Raw means it still carries markdown and
// attachment tokens, and the cut can land inside one — so a naive card prints
// "![image](concord://attach/v1/9f3c…" as its body. This is where that stops.

// Local copies: the exported regexes are global, and .test()/.exec() advance
// lastIndex on the shared object. Sharing that mutable cursor across modules is
// exactly the bug attachments.js documents against itself.
const reImg = () => new RegExp(ATTACH_RE.source, "g");
const reFile = () => new RegExp(FILE_RE.source, "g");
// A token cut in half by the 240-rune excerpt limit: an unclosed image/file
// link running to the end of the string (the trailing "…" included).
const TRUNCATED_TOKEN = /!?\[(?:image|file)\]\([^)]*$/;

// A list marker at the very start of the excerpt. Only when one is there do we
// treat the whole line as a list and strip the markers between items — the
// excerpt has already had its newlines collapsed, so "- a - b" and the sentence
// "3 * 4 is 12" are otherwise indistinguishable, and eating the asterisk out of
// the sentence is the worse mistake.
const LEAD_LIST = /^(?:[-*+]|\d{1,3}\.)\s+/;

// stripMarkdown reduces formatting to the words inside it. Paired markers only
// (so snake_case and 3 * 4 survive), then a sweep for the multi-character
// markers a truncated excerpt can leave dangling.
//
// Exported because a search result is the same problem as a post excerpt: a
// line of prose printed somewhere that will not render it, where `**bold**`
// and `> quoted` are noise standing between the reader and the words they
// searched for.
export function stripMarkdown(input) {
  let s = input
    .replace(/```+/g, " ")
    .replace(/`([^`]*)`/g, "$1")
    .replace(/!\[[^\]]*\]\([^)]*\)/g, " ") // images: the alt text is not the post
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1") // links: keep the label, drop the URL
    .replace(/\*\*\*(.+?)\*\*\*/g, "$1")
    .replace(/\*\*(.+?)\*\*/g, "$1")
    .replace(/\*(.+?)\*/g, "$1")
    .replace(/___(.+?)___/g, "$1")
    .replace(/__(.+?)__/g, "$1")
    .replace(/(^|\W)_(.+?)_(\W|$)/g, "$1$2$3")
    .replace(/~~(.+?)~~/g, "$1")
    .replace(/\|\|(.+?)\|\|/g, "$1")
    .replace(/(\*\*|~~|\|\|)/g, "") // unpaired remnants of a truncated excerpt
    .replace(/^#{1,6}\s+/, "")
    .replace(/(^|\s)>\s?/g, "$1")
    .replace(/\s{2,}/g, " ")
    .trim();
  if (LEAD_LIST.test(s)) s = s.replace(LEAD_LIST, "").replace(/\s(?:[-*+]|\d{1,3}\.)\s+/g, " ");
  return s.trim();
}

// postPreview: { text, kind, images } where kind is "" | "image" | "file" |
// "poll" | "embed". kind is the CARD's business — it decides whether to show a
// paperclip, a chart glyph, or nothing at all — and text is never a token.
export function postPreview(excerpt) {
  const raw = String(excerpt || "");
  if (!raw) return { text: "", kind: "", images: 0 };

  const images = parseAttachTokens(raw);
  const hasFile = reFile().test(raw);

  // A poll or a rich embed IS the body; its own text beats a stripped token.
  // Both can be cut mid-payload by the excerpt limit, in which case the parse
  // fails and we fall through to the generic label rather than showing base64.
  const poll = parsePoll(raw);
  if (poll) return { text: poll.q || "Poll", kind: "poll", images: 0 };
  const embed = parseEmbed(raw);
  if (embed) return { text: embed.title || embed.desc || "Rich embed", kind: "embed", images: 0 };

  let text = raw.replace(reImg(), " ").replace(reFile(), " ").replace(TRUNCATED_TOKEN, " ");
  // An un-decodable poll/embed token still has to go — it is 200 characters of
  // base64 and never a preview.
  text = text.replace(EMBED_RE, " ").replace(/\[poll\]\(concord:\/\/[^)]*\)?/g, " ");
  text = stripMarkdown(text);
  // A lone ellipsis is all that's left when the cut fell right after a token.
  if (text === "…" || text === "...") text = "";

  const kind = images.length ? "image" : hasFile ? "file" : "";
  return { text, kind, images: images.length };
}

// firstImage returns the token for a post's picture, or null. That token is all
// a card needs — the bytes are fetched lazily and cached by attachments.js,
// keyed on the blob ID, so the board shares one decrypt with the thread you open
// afterwards.
//
// It reads post.media, which the BACKEND derives from the post's own messages.
// This used to scan the excerpt instead, and that failed in the one case that
// matters: the composer sends staged attachments as their own messages, so the
// token was never in the opening body, and every card for a real post was a
// letter tile. Even inline it was a race with the 240-character excerpt cut —
// prose length silently deciding whether your picture appears.
//
// The excerpt is still scanned as a fallback, for a post whose stats came from a
// peer that predates the media field.
export function firstImage(post) {
  const media = typeof post === "string" ? "" : String(post?.media || "");
  const source = media || (typeof post === "string" ? post : post?.excerpt || "");
  const toks = parseAttachTokens(String(source));
  // A spoilered image is hidden on purpose. Putting it on a card as the headline
  // picture would un-hide it for the whole board — which is why this stays on the
  // client, where the token's flags are already parsed.
  return toks.find((t) => !t.spoiler) || null;
}

// ---- time ----------------------------------------------------------------

// relTime: a card's "3h ago". "" for an unknown time, so the caller shows the
// pending state instead of pretending. A timestamp in the future is clock skew
// on the sending peer, not a prophecy — clamp it.
export function relTime(ns, now = Date.now()) {
  if (!ns) return "";
  const d = now - ns / 1e6;
  if (d < 45e3) return "just now";
  // Never "0m ago": between 45s and a minute the floor is 0, and a card that
  // says a post is zero minutes old is saying nothing.
  if (d < 3600e3) return `${Math.max(1, Math.floor(d / 60e3))}m ago`;
  if (d < 86400e3) return `${Math.floor(d / 3600e3)}h ago`;
  if (d < 7 * 86400e3) return `${Math.floor(d / 86400e3)}d ago`;
  if (d < 365 * 86400e3) return `${Math.floor(d / (7 * 86400e3))}w ago`;
  return `${Math.floor(d / (365 * 86400e3))}y ago`;
}

// absTime: the full date for a title attribute, so the relative label never has
// to be the only answer.
export function absTime(ns) {
  if (!ns) return "";
  return new Date(ns / 1e6).toLocaleString();
}

// ---- generated art -------------------------------------------------------

// FNV-1a over code units. Small, fast, and — the only property that matters
// here — stable: the same forum or post gets the same colours on every device,
// so a board looks like itself for everyone without storing or syncing a thing.
export function hash32(s) {
  let h = 0x811c9dc5;
  const str = String(s || "");
  for (let i = 0; i < str.length; i++) {
    h ^= str.charCodeAt(i);
    h = (h * 0x01000193) >>> 0;
  }
  return h >>> 0;
}

// hueOfCss: the hue of a resolved CSS colour, or null.
//
// A computed style always hands back a concrete colour — var() and color-mix
// are already resolved — but WHICH concrete form depends on the engine, so all
// three that turn up in practice are handled: hex, rgb()/rgba(), and the
// color(srgb …) Chromium returns for a color-mix result. Anything else answers
// null and the caller keeps its old behaviour rather than guessing.
export function hueOfCss(css) {
  const s = String(css || "").trim();
  let rgb = null;
  let m = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(s);
  if (m) {
    const hx = m[1].length === 3 ? [...m[1]].map((c) => c + c) : m[1].match(/../g);
    rgb = hx.map((x) => parseInt(x, 16) / 255);
  } else if ((m = /^rgba?\(([^)]+)\)$/i.exec(s))) {
    rgb = m[1].split(/[\s,/]+/).filter(Boolean).slice(0, 3).map((n) => Number(n) / 255);
  } else if ((m = /^color\(\s*srgb\s+([^)]+)\)$/i.exec(s))) {
    rgb = m[1].split(/[\s/]+/).filter(Boolean).slice(0, 3).map(Number);
  }
  if (!rgb || rgb.length < 3 || rgb.some((n) => !Number.isFinite(n))) return null;
  const [r, g, b] = rgb;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const d = max - min;
  if (!d) return null; // grey has no hue to borrow
  let hue;
  if (max === r) hue = ((g - b) / d) % 6;
  else if (max === g) hue = (b - r) / d + 2;
  else hue = (r - g) / d + 4;
  return ((hue * 60) % 360 + 360) % 360;
}

// washFor: a forum's own two-tone header wash, derived from its ID.
//
// Fixed saturation and lightness, only the hue varies. That is the restraint
// that keeps this from turning into a clown parade: every forum is recognisably
// a different place, every forum is the same weight of colour, and the white
// ink over it has a known floor (see SCRIM_FLOOR).
// `baseHue`, when given, is the hue of the accent this board is being drawn
// under. A forum's colour used to be a raw hash — an arbitrary hue with no
// relation to anything else on the screen, which is how a help desk ended up a
// saturated block that matched neither the guild header above it nor the app's
// own accent nor the five tag chips underneath it. Rotating a bounded amount
// AROUND the accent keeps every forum recognisably its own place while putting
// all of them in one family, which is the same trade the guild header makes
// with its pack. Without a base hue it behaves exactly as it did.
export function washFor(seed, baseHue = null) {
  const h = hash32(seed);
  const hue = baseHue === null ? h % 360 : Math.round(baseHue + ((h % 5) - 2) * 18 + 360) % 360;
  // Bright enough to SURVIVE the scrim: the header's contrast guarantee costs
  // the art 62% of its luminance where the words sit, so a wash mixed at the
  // lightness you want to see ends up as mud. These two are chosen so the
  // composited result is still a colour.
  return {
    color: `hsl(${hue} 62% 46%)`,
    // +34° rather than a complement: a small rotation reads as one material
    // catching light, a complement reads as two colours fighting.
    color2: `hsl(${(hue + 34) % 360} 68% 24%)`,
    angle: 110 + (h % 3) * 25,
  };
}

// tileFor: the placeholder a post with no picture gets, instead of a hole where
// a thumbnail would be.
//
// This is the case the board has MOST of, so it is designed first and it is not
// a grey box: a deterministic two-tone gradient plus the title's first letter,
// which makes an image-less board look composed and — because the colour is
// derived from the post ID — makes a post recognisable when you come back to it.
export function tileFor(seed, title = "", baseHue = null) {
  const h = hash32(seed);
  // A wider swing than the hero's: these are 88px plates sitting side by side
  // and have to be told apart at a glance, where the hero only has to belong.
  const hue = baseHue === null ? h % 360 : Math.round(baseHue + ((h % 7) - 3) * 14 + 360) % 360;
  // The initial: first letter of the first word that has one, upper-cased. Falls
  // back to a glyph rather than printing "?" at people.
  const letter = [...String(title || "")].find((c) => /\p{L}|\p{N}/u.test(c)) || "";
  return {
    color: `hsl(${hue} 46% 30%)`,
    color2: `hsl(${(hue + 42) % 360} 50% 18%)`,
    angle: 135 + (h % 4) * 15,
    letter: letter.toUpperCase(),
  };
}

// ---- contrast, measured --------------------------------------------------

// SCRIM_FLOOR is the black-scrim alpha the "cover" layout and the hero header
// guarantee wherever they print text. Proven, not assumed: forum.test.mjs
// composites it over WHITE — the brightest art any preset or uploaded image
// could put back there — and asserts white ink still clears 4.5:1. Change this
// number and that test tells you what it cost.
export const SCRIM_FLOOR = 0.62;

// TINT_ALPHA is how strongly a tag's own colour tints its chip. A tag colour is
// arbitrary (a member picks it), so the chip identifies with colour and reads
// with the theme's ink — the label is never painted IN the tag colour. The test
// composites the worst case (a white tag over the darkest and lightest card
// surfaces) and asserts --text still clears 4.5:1.
export const TINT_ALPHA = 0.22;

const hexToRgb = (hex) => {
  const s = String(hex || "").replace("#", "");
  const n = parseInt(s.length === 3 ? s.replace(/./g, "$&$&") : s, 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
};

const chan = (c) => {
  const v = c / 255;
  return v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
};

// relLuminance / contrastRatio: WCAG 2.1, on sRGB hex.
export function relLuminance(hex) {
  const [r, g, b] = hexToRgb(hex);
  return 0.2126 * chan(r) + 0.7152 * chan(g) + 0.0722 * chan(b);
}

export function contrastRatio(a, b) {
  const la = relLuminance(a);
  const lb = relLuminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

// composite: `over` at `alpha` painted on `under`, both opaque sRGB hex. Simple
// source-over in gamma space, which is what a browser does for a CSS layer.
export function composite(under, over, alpha) {
  const u = hexToRgb(under);
  const o = hexToRgb(over);
  const mix = u.map((c, i) => Math.round(c * (1 - alpha) + o[i] * alpha));
  return `#${mix.map((c) => c.toString(16).padStart(2, "0")).join("")}`;
}

// ---- device-local board preferences --------------------------------------

export const BOARD_DEFAULTS = { layout: "list", sort: "recent", banner: "" };

// The key is per forum: a support queue and a screenshot board in the same
// guild want different layouts, and remembering one choice for both would be
// the same as not remembering it.
export const boardPrefKey = (forumId) => `concord.board.${forumId}`;

// normalizeBoardPrefs is the gate between localStorage and the board. Stored
// prefs are the one input a user can hand-edit and a future version can change
// the shape of, so an unknown layout or sort falls back rather than rendering
// nothing.
export function normalizeBoardPrefs(raw) {
  const r = raw && typeof raw === "object" ? raw : {};
  return {
    layout: LAYOUTS.some((l) => l.id === r.layout) ? r.layout : BOARD_DEFAULTS.layout,
    sort: SORTS.some((s) => s.id === r.sort) ? r.sort : BOARD_DEFAULTS.sort,
    banner: typeof r.banner === "string" ? r.banner : BOARD_DEFAULTS.banner,
  };
}

// Two components render the same board look — the board itself and its settings
// dialog, which sits on top of it — and localStorage fires no storage event in
// the tab that wrote it. Without a nudge, choosing a layout in the dialog
// changed nothing until a reload, which reads as a broken control. So the write
// announces itself, and the reader listens. (The banner is NOT in this loop any
// more: it moved onto the channel record when it became shared, so it arrives
// like any other guild change — render it from forum.banner, never from here.)
export const BOARD_PREFS_EVENT = "concord:board-prefs";

export function readBoardPrefs(forumId) {
  try {
    return normalizeBoardPrefs(JSON.parse(localStorage.getItem(boardPrefKey(forumId)) || "null"));
  } catch {
    // No storage (or junk in it) is not an error: fall back to the defaults.
    return normalizeBoardPrefs(null);
  }
}

export function writeBoardPrefs(forumId, patch, current = null) {
  const next = normalizeBoardPrefs({ ...(current || readBoardPrefs(forumId)), ...patch });
  try {
    localStorage.setItem(boardPrefKey(forumId), JSON.stringify(next));
  } catch {
    // A full or blocked localStorage must not break the board — the choice just
    // doesn't survive the session.
  }
  try {
    window.dispatchEvent(new CustomEvent(BOARD_PREFS_EVENT, { detail: { forumId, prefs: next } }));
  } catch {
    // No window (tests): the return value is still the truth.
  }
  return next;
}
