// postdraft.js — the pure half of the two modal writing surfaces (the advanced
// composer and the forum-post composer).
//
// Two kinds of thing live here, and both are here for the same reason: they are
// the parts that are easy to get subtly wrong and impossible to eyeball in a
// screenshot.
//
//   1. Caret-exact markdown transforms. Every one takes (text, start, end) and
//      returns the new text WITH the new selection, so the Svelte side is left
//      with nothing but "assign, then restore the selection". Selection
//      arithmetic is where a formatting toolbar actually breaks (wrap a
//      selection and the caret lands three characters off; toggle bold twice and
//      you eat a neighbouring asterisk), and none of that is visible until a
//      user complains.
//
//   2. The budgets the WIRE enforces, in the units the wire uses. A post title
//      is capped at 64 BYTES by internal/app/guild.go (maxNameBytes), which
//      slices raw bytes — so a title of emoji hits the cap at 16 characters and
//      a naive maxlength="64" hands the backend a cut that can land mid-rune.
//
// Deliberately DOM-free so node can test it.

// ---- units ----------------------------------------------------------------

// runeLen counts code points, the way a person counts characters (and the way
// the backend counts tag names). Mirrors lib/forum.js — kept local rather than
// imported so this module stays usable on its own.
export const runeLen = (s) => [...String(s ?? "")].length;

const encoder = typeof TextEncoder !== "undefined" ? new TextEncoder() : null;

// utf8Bytes is what internal/app/guild.go measures a channel name in.
export function utf8Bytes(s) {
  const str = String(s ?? "");
  if (encoder) return encoder.encode(str).length;
  // No TextEncoder (ancient webview): count by code point rather than lie.
  let n = 0;
  for (const ch of str) {
    const c = ch.codePointAt(0);
    n += c < 0x80 ? 1 : c < 0x800 ? 2 : c < 0x10000 ? 3 : 4;
  }
  return n;
}

// app.maxNameBytes. A post's title IS its channel name.
export const TITLE_MAX_BYTES = 64;

// Message bodies aren't byte-capped by the backend, but an unbounded one is a
// UI problem long before it's a protocol problem: the excerpt on a forum card is
// cut at 240 runes and a wall of text reads as broken. This is the point where
// the counter goes from informative to a warning, not a hard stop.
export const BODY_SOFT_MAX = 4000;

// clampToBytes trims to a byte budget on a CODE POINT boundary, never inside
// one. This is the whole reason the title field doesn't just set maxlength: the
// backend's `title[:64]` would happily leave half a rune behind.
export function clampToBytes(s, max = TITLE_MAX_BYTES) {
  const str = String(s ?? "");
  if (utf8Bytes(str) <= max) return str;
  let out = "";
  let used = 0;
  for (const ch of str) {
    const w = utf8Bytes(ch);
    if (used + w > max) break;
    out += ch;
    used += w;
  }
  return out;
}

// titleFit is what the title field's budget readout prints. `tone` is the
// design decision, made once here so both composers agree on when a counter
// stops being neutral: quiet until the last quarter, warn in it, done at zero.
export function titleFit(title, max = TITLE_MAX_BYTES) {
  const bytes = utf8Bytes(title);
  const left = max - bytes;
  return {
    bytes,
    max,
    left,
    full: left <= 0,
    tone: left <= 0 ? "full" : left <= Math.max(6, Math.round(max / 4)) ? "warn" : "ok",
  };
}

// bodyStats: the numbers a long-form editor owes its author.
//
// Reading time uses 220 wpm and is 0 below READ_TIME_FLOOR words. The advanced
// composer is a chat composer wearing a long-form editor's clothes, and it was
// reporting "1 min read" for a four-word message — a floor of one minute on
// four words is noise at best and comic at worst. Under the floor there is
// nothing worth estimating, so it estimates nothing.
export const READ_TIME_FLOOR = 200;
export function bodyStats(body) {
  const text = String(body ?? "");
  const words = text.trim() ? text.trim().split(/\s+/).length : 0;
  return {
    chars: runeLen(text),
    words,
    lines: text ? text.split("\n").length : 0,
    minutes: words >= READ_TIME_FLOOR ? Math.max(1, Math.round(words / 220)) : 0,
    over: runeLen(text) > BODY_SOFT_MAX,
  };
}

// ---- selection transforms -------------------------------------------------
//
// The shape every one returns. `start`/`end` are the selection to restore.
const at = (text, start, end = start) => ({ text, start, end });

// wrap toggles a symmetric marker pair around the selection.
//
// Toggling matters more than it looks: without it the only way to remove bold is
// to hunt down two asterisks by hand, so the button is one-way and the second
// press corrupts the text. Two shapes count as "already wrapped": the markers
// sit just OUTSIDE the selection (you selected the words, not the stars), or
// they're INSIDE it (you selected the whole `**word**`).
//
// The italic guard is the subtle one: `*` is a prefix of `**`, so italic on a
// bold run would strip one star from each side and leave `*word*` claiming to be
// bold. Ported from Composer.svelte so all three surfaces behave alike.
export function wrap(text, s, e, pre, post = pre, placeholder = "text") {
  const sel = text.slice(s, e);
  const outside =
    sel &&
    text.slice(s - pre.length, s) === pre &&
    text.slice(e, e + post.length) === post &&
    !(pre === "*" && text.slice(s - 2, s) === "**" && text.slice(e, e + 2) === "**");
  if (outside) {
    return at(text.slice(0, s - pre.length) + sel + text.slice(e + post.length), s - pre.length, s - pre.length + sel.length);
  }
  if (sel.length >= pre.length + post.length && sel.startsWith(pre) && sel.endsWith(post)) {
    const inner = sel.slice(pre.length, sel.length - post.length);
    return at(text.slice(0, s) + inner + text.slice(e), s, s + inner.length);
  }
  const body = sel || placeholder;
  return at(text.slice(0, s) + pre + body + post + text.slice(e), s + pre.length, s + pre.length + body.length);
}

// lineSpan returns the [start, end) of every whole line the selection touches —
// the unit every block-level transform works on.
export function lineSpan(text, s, e) {
  const start = text.lastIndexOf("\n", s - 1) + 1;
  let end = text.indexOf("\n", e);
  if (end < 0) end = text.length;
  return [start, end];
}

// linePrefix toggles a literal prefix ("> ", "- ") on every touched line. All
// lines already carrying it means the press is a removal — the same
// "second press undoes" contract as wrap().
export function linePrefix(text, s, e, prefix) {
  const [ls, le] = lineSpan(text, s, e);
  const lines = text.slice(ls, le).split("\n");
  const on = lines.every((l) => l.startsWith(prefix));
  const block = lines.map((l) => (on ? l.slice(prefix.length) : prefix + l)).join("\n");
  const next = text.slice(0, ls) + block + text.slice(le);
  if (s === e) {
    // Collapsed caret: keep it where the author left it, shifted by the marker.
    const p = Math.max(ls, s + (on ? -prefix.length : prefix.length));
    return at(next, Math.min(p, ls + block.length));
  }
  return at(next, ls, ls + block.length);
}

const OL_RE = /^(\s*)(\d+)([.)])\s(.*)$/;

// orderedList toggles "1. 2. 3." numbering across the touched lines. Turning it
// ON assigns the numbers 1..n rather than inheriting whatever was pasted —
// the renderer only cares that a line starts with a digit, but the SOURCE is
// what the author reads while writing, and "7. 9. 2." reads as broken.
// A bullet already on the line is consumed rather than nested inside the number.
export function orderedList(text, s, e) {
  const [ls, le] = lineSpan(text, s, e);
  const lines = text.slice(ls, le).split("\n");
  const on = lines.every((l) => OL_RE.test(l));
  const out = lines.map((l, i) => {
    const m = OL_RE.exec(l);
    if (on) return m[1] + m[4];
    return `${i + 1}. ${l.replace(/^(\s*)[-*]\s/, "$1")}`;
  });
  const block = out.join("\n");
  const next = text.slice(0, ls) + block + text.slice(le);
  return s === e ? at(next, caretAfter(ls, s, lines[0], out[0])) : at(next, ls, ls + block.length);
}

// caretAfter keeps a collapsed caret where the author left it when only the
// LINE's prefix changed around it — clamped into the rewritten line so a
// removal can't park it before the line even starts.
function caretAfter(lineStart, caret, oldLine, newLine) {
  const delta = newLine.length - oldLine.length;
  return Math.min(Math.max(lineStart, caret + delta), lineStart + newLine.length);
}

const HDR_RE = /^(#{1,3})\s(.*)$/;

// heading sets the touched lines to exactly `level` hashes, or strips them when
// they already carry that level — so the H2 button is a toggle and the three
// levels are exclusive rather than additive.
export function heading(text, s, e, level = 2) {
  const [ls, le] = lineSpan(text, s, e);
  const lines = text.slice(ls, le).split("\n");
  const bare = lines.map((l) => {
    const m = HDR_RE.exec(l);
    return m ? m[2] : l;
  });
  const already = lines.every((l) => (HDR_RE.exec(l)?.[1]?.length ?? 0) === level);
  const out = bare.map((l) => (already ? l : "#".repeat(level) + " " + l));
  const block = out.join("\n");
  const next = text.slice(0, ls) + block + text.slice(le);
  return s === e ? at(next, caretAfter(ls, s, lines[0], out[0])) : at(next, ls, ls + block.length);
}

// fence wraps the selection in a ``` block, adding only the newlines that are
// missing, and puts the caret on the CODE rather than the language label so the
// common case (a fence with a language chosen from the menu) is ready to type in.
export function fence(text, s, e, lang = "") {
  const body = text.slice(s, e) || "code";
  const pre = (s > 0 && text[s - 1] !== "\n" ? "\n" : "") + "```" + lang + "\n";
  const post = "\n```" + (e < text.length && text[e] !== "\n" ? "\n" : "");
  return at(text.slice(0, s) + pre + body + post + text.slice(e), s + pre.length, s + pre.length + body.length);
}

// link: a selection becomes the label with `url` selected to type over; nothing
// selected inserts the whole skeleton with the LABEL selected, because that's
// what you write first.
export function link(text, s, e, url = "url") {
  const sel = text.slice(s, e);
  const label = sel || "text";
  const next = text.slice(0, s) + `[${label}](${url})` + text.slice(e);
  if (sel) {
    const us = s + label.length + 3; // past "[label]("
    return at(next, us, us + url.length);
  }
  return at(next, s + 1, s + 1 + label.length);
}

// COLOR_RE matches one {color|text} span, the renderer's own syntax
// (lib/markdown.js): a named colour or a #hex.
const COLOR_RE = /^\{(#[0-9a-fA-F]{3,6}|[a-z]{3,10})\|([\s\S]*)\}$/;

// colorize wraps the selection in {color|…}. Passing color "" REMOVES the
// wrapper, which is the only way back out of a colour once applied — a colour
// picker without an off switch is a trap.
export function colorize(text, s, e, color) {
  const sel = text.slice(s, e);
  const inner = COLOR_RE.exec(sel);
  if (inner) {
    // Already a coloured span: recolour it, or unwrap when color is "".
    const body = inner[2];
    const next = color ? `{${color}|${body}}` : body;
    return at(text.slice(0, s) + next + text.slice(e), s, s + next.length);
  }
  if (!color) return at(text, s, e); // nothing to remove
  const body = sel || "text";
  const open = `{${color}|`;
  return at(text.slice(0, s) + open + body + "}" + text.slice(e), s + open.length, s + open.length + body.length);
}

// insert drops literal text in, replacing the selection, caret after it. Used by
// the emoji picker and by mention/shortcode completion.
export function insert(text, s, e, str) {
  return at(text.slice(0, s) + str + text.slice(e), s + str.length);
}

const LIST_LINE_RE = /^(\s*)(?:([-*])\s|(\d+)([.)])\s|(>)\s?)(.*)$/;

// continueList is the Enter key inside a list, quote, or numbered list: it
// carries the marker down, increments the number, and — the part that makes it
// feel right instead of annoying — an Enter on an EMPTY item ends the list
// rather than adding another empty bullet. Returns null when the caret isn't in
// a list, meaning "let the browser insert its own newline".
export function continueList(text, caret) {
  const ls = text.lastIndexOf("\n", caret - 1) + 1;
  const line = text.slice(ls, caret);
  const m = LIST_LINE_RE.exec(line);
  if (!m) return null;
  const [, indent, bullet, num, punct, quote, rest] = m;
  if (!rest.trim()) {
    // Empty item: drop the marker and leave a blank line.
    const next = text.slice(0, ls) + indent + text.slice(caret);
    return { text: next, caret: ls + indent.length };
  }
  const marker = bullet ? `${bullet} ` : quote ? "> " : `${Number(num) + 1}${punct} `;
  const add = "\n" + indent + marker;
  return { text: text.slice(0, caret) + add + text.slice(caret), caret: caret + add.length };
}

// ---- draft persistence ----------------------------------------------------
//
// Same idea as Composer.svelte's per-channel drafts (concord.draft.<id>), one
// namespace over so a modal draft can never be mistaken for the inline one and
// posted twice. The scope string is the caller's: "post:<forumId>" for a new
// forum post, "channel:<id>" / "edit:<messageId>" for the advanced composer.

export const DRAFT_VERSION = 1;
export const draftKey = (scope) => `concord.compose.${scope}`;

// ATTACHMENTS ARE NEVER PERSISTED. A staged image is a base64 data URL —
// megabytes of it — and localStorage is a few megabytes total, shared with the
// read state, prefs and every other draft. Writing one there is how you get a
// QuotaExceededError that silently kills draft saving for the whole app. The
// tray is session-only, on purpose.
export function normalizeDraft(raw) {
  const r = raw && typeof raw === "object" ? raw : {};
  return {
    v: DRAFT_VERSION,
    title: typeof r.title === "string" ? r.title : "",
    body: typeof r.body === "string" ? r.body : "",
    tags: Array.isArray(r.tags) ? r.tags.filter((t) => typeof t === "string").slice(0, 8) : [],
    embed: r.embed && typeof r.embed === "object" ? r.embed : null,
    at: Number.isFinite(r.at) ? r.at : 0,
  };
}

// draftEmpty: nothing worth rescuing. A draft carrying only an untouched embed
// skeleton (colour but no text) is empty too — restoring that would put a
// "you have a draft" banner in front of someone who typed nothing.
export function draftEmpty(d) {
  const n = normalizeDraft(d);
  const embedHasText =
    !!n.embed &&
    (String(n.embed.title || "").trim() ||
      String(n.embed.desc || "").trim() ||
      (n.embed.fields || []).some((f) => String(f?.name || "").trim() || String(f?.value || "").trim()));
  return !n.title.trim() && !n.body.trim() && n.tags.length === 0 && !embedHasText;
}

export function saveDraft(scope, draft) {
  if (!scope) return;
  const d = normalizeDraft({ ...draft, at: Date.now() });
  try {
    if (draftEmpty(d)) localStorage.removeItem(draftKey(scope));
    else localStorage.setItem(draftKey(scope), JSON.stringify(d));
  } catch {
    // Storage blocked or full: the draft just doesn't survive a reload. Never a
    // reason to interrupt someone mid-sentence.
  }
}

export function loadDraft(scope) {
  if (!scope) return null;
  try {
    const raw = JSON.parse(localStorage.getItem(draftKey(scope)) || "null");
    // A draft from a future/older shape is dropped rather than half-restored:
    // a title that used to be a body is worse than a blank form.
    if (!raw || raw.v !== DRAFT_VERSION) return null;
    const d = normalizeDraft(raw);
    return draftEmpty(d) ? null : d;
  } catch {
    return null;
  }
}

export function clearDraft(scope) {
  if (!scope) return;
  try {
    localStorage.removeItem(draftKey(scope));
  } catch {
    /* nothing to clear if there's no storage */
  }
}

// draftAge is the words on the "restored" banner. Vague on purpose past a day:
// the useful question is "is this the thing I was writing?", not the timestamp.
export function draftAge(at, now = Date.now()) {
  if (!at) return "";
  const s = Math.max(0, Math.round((now - at) / 1000));
  if (s < 45) return "moments ago";
  const m = Math.round(s / 60);
  if (m < 60) return `${m} minute${m === 1 ? "" : "s"} ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h} hour${h === 1 ? "" : "s"} ago`;
  const d = Math.round(h / 24);
  return `${d} day${d === 1 ? "" : "s"} ago`;
}

// ---- staging limits -------------------------------------------------------
// Mirrors Composer.svelte, which is the shipped contract with internal/app:
// images are sealed as blobs up to 5 MB, other files up to 25 MB, and the four
// NATIVE types travel untouched so a GIF keeps animating.

export const IMAGE_MAX_BYTES = 5 * 1024 * 1024;
export const FILE_MAX_BYTES = 25 * 1024 * 1024;
export const NATIVE_IMAGE_TYPES = ["image/png", "image/jpeg", "image/gif", "image/webp"];

// sendsAsIs: can this image go straight to the backend, or does it need a canvas
// round trip through JPEG? Re-encoding a GIF kills the animation, so "as is"
// isn't an optimisation here — it's correctness.
export const sendsAsIs = (type, size) => NATIVE_IMAGE_TYPES.includes(type) && size <= IMAGE_MAX_BYTES;

// dataUrlBytes: the decoded size of a base64 data URL, without decoding it.
export function dataUrlBytes(dataUrl) {
  const i = String(dataUrl || "").indexOf(",");
  if (i < 0) return 0;
  const b64 = dataUrl.slice(i + 1);
  const pad = b64.endsWith("==") ? 2 : b64.endsWith("=") ? 1 : 0;
  return Math.max(0, Math.floor((b64.length * 3) / 4) - pad);
}

// prettyBytes for the tray's file chips.
export function prettyBytes(n) {
  if (!n) return "";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`;
  return `${(n / (1024 * 1024)).toFixed(n < 10 * 1024 * 1024 ? 1 : 0)} MB`;
}
