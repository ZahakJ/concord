// alertwords.js — words that ping you like your own name.
//
// A list of terms that make a message reach you the way a mention does. It is
// device-local in the strongest sense available: the list lives in this
// browser's storage, it is handed to the local query that needs it and never
// written down by the core, and it does not exist on the wire in any form.
//
// That is not a nice-to-have, it is the point of the feature. Anywhere with a
// server, alert words have to be evaluated where the messages are — which means
// the operator learns the list of things you are quietly watching for: your own
// name, a competitor, a diagnosis, a person you are avoiding. Here the messages
// are already on your machine, so the matching is too.
//
// Everything below is pure: state in, new state out. The matcher is duplicated
// on the Go side (internal/app/inbox.go) because the scan over history happens
// there, and the two are held to the same rule by the same cases — a word
// matches only on whole-word edges, so "cat" never fires on "concatenate". That
// single failure is why people turn this feature off everywhere else.

export const MAX_WORDS = 50;
export const MAX_LEN = 64;
export const MIN_LEN = 2;

// normalize accepts whatever storage held — including nothing, or a shape from
// a build that predates this file — and returns something safe to match with.
// The bounds are the same ones the Go side applies, because a list that is legal
// here and dropped there would be a setting that silently does nothing.
export function normalize(raw) {
  const out = [];
  const seen = new Set();
  for (const v of Array.isArray(raw) ? raw : []) {
    // Anything that is not text is not a word. Without this an object in
    // storage stringifies to "[object Object]", which is a legal length and
    // would sit in the list matching nothing forever.
    if (typeof v !== "string" && typeof v !== "number") continue;
    const w = String(v)
      .trim()
      .toLowerCase();
    if (w.length < MIN_LEN || w.length > MAX_LEN || seen.has(w)) continue;
    seen.add(w);
    out.push(w);
    if (out.length >= MAX_WORDS) break;
  }
  return out;
}

// addWord returns the list with a word added, or the same list unchanged when
// the word is empty, a duplicate, out of bounds, or the list is full. Returning
// the SAME array on a no-op lets the caller tell "added" from "rejected"
// without a second validation that could disagree with this one.
export function addWord(list, word) {
  const w = String(word ?? "")
    .trim()
    .toLowerCase();
  if (w.length < MIN_LEN || w.length > MAX_LEN) return list;
  if (list.includes(w) || list.length >= MAX_WORDS) return list;
  return [...list, w];
}

export function removeWord(list, word) {
  return list.filter((w) => w !== word);
}

// rejectReason explains a refusal in the words the field will show. Separate
// from addWord so the input can warn before anything is attempted.
export function rejectReason(list, word) {
  const w = String(word ?? "")
    .trim()
    .toLowerCase();
  if (!w) return "";
  if (w.length < MIN_LEN) return "An alert word needs at least two characters";
  if (w.length > MAX_LEN) return `An alert word can be at most ${MAX_LEN} characters`;
  if (list.includes(w)) return "That word is already on the list";
  if (list.length >= MAX_WORDS) return `The list holds at most ${MAX_WORDS} words`;
  return "";
}

// ---- matching --------------------------------------------------------------

const isWordChar = (ch) => !!ch && /[\p{L}\p{N}_]/u.test(ch);

// matchedWord returns the first alert word the text contains as a whole word, or
// "". Case-insensitive, and the edges are Unicode-aware: an Arabic or Cyrillic
// term has boundaries too, and a byte-wise test would find one inside a longer
// word and call it a hit.
//
// A single forward pass per word with no backtracking — this runs on the render
// path for every message on screen, so it has the same discipline the syntax
// highlighter has and for the same reason.
export function matchedWord(text, words) {
  if (!text || !words?.length) return "";
  const hay = text.toLowerCase();
  for (const w of words) {
    if (!w) continue;
    let from = 0;
    for (;;) {
      const i = hay.indexOf(w, from);
      if (i < 0) break;
      const before = i > 0 ? hay[i - 1] : "";
      const after = i + w.length < hay.length ? hay[i + w.length] : "";
      if (!isWordChar(before) && !isWordChar(after)) return w;
      from = i + 1;
    }
  }
  return "";
}

export const hasAlertWord = (text, words) => matchedWord(text, words) !== "";

// markInHtml wraps every whole-word occurrence of `word` in <mark>, over the
// HTML the markdown renderer has already produced.
//
// It has to run on the rendered form rather than on the source, because the
// source is markdown: an alert word can be split across emphasis markers, and
// re-rendering after an insertion would let the insertion itself be parsed.
// So this walks the string and only ever touches the runs BETWEEN tags — never
// inside `<a href=…>`, never inside an `&entity;`, and never inside the alt
// text of a tag that has already been written. `<mark>` is the same element
// search results use, so the two highlights read as one idea.
export function markInHtml(html, word) {
  if (!html || !word) return html;
  const w = String(word).toLowerCase();
  let out = "";
  let i = 0;
  while (i < html.length) {
    if (html[i] === "<") {
      const end = html.indexOf(">", i);
      if (end < 0) {
        out += html.slice(i);
        break;
      }
      out += html.slice(i, end + 1);
      i = end + 1;
      continue;
    }
    const next = html.indexOf("<", i);
    const seg = html.slice(i, next < 0 ? html.length : next);
    out += markSegment(seg, w);
    i = next < 0 ? html.length : next;
  }
  return out;
}

function markSegment(seg, w) {
  const hay = seg.toLowerCase();
  let out = "";
  let cursor = 0; // everything before this has been emitted
  let scan = 0; // where the next search starts
  for (;;) {
    const at = hay.indexOf(w, scan);
    if (at < 0) break;
    const before = at > 0 ? hay[at - 1] : "";
    const after = at + w.length < hay.length ? hay[at + w.length] : "";
    // Inside an HTML entity (`&amp;`, `&#1575;`) the letters spell a character,
    // not a word — an alert on "amp" must not cut one in half.
    const amp = hay.lastIndexOf("&", at);
    const inEntity =
      amp >= 0 && /^&#?[a-z0-9]*$/.test(hay.slice(amp, at)) && hay.indexOf(";", amp) >= at;
    if (!isWordChar(before) && !isWordChar(after) && !inEntity) {
      out += seg.slice(cursor, at) + "<mark>" + seg.slice(at, at + w.length) + "</mark>";
      cursor = at + w.length;
      scan = cursor;
    } else {
      scan = at + 1;
    }
  }
  return out + seg.slice(cursor);
}
