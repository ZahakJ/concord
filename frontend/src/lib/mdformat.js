// mdformat.js — the markdown formatting engine behind the composer's toolbar,
// its Ctrl-chords, and (since this module existed) the edit box's copies of
// both.
//
// Two jobs, kept apart on purpose:
//
//   • planFormat() is pure. Given a string, a selection and a kind, it says
//     which span to replace, what to put there, and where the selection ends
//     up. It is the part worth testing, and mdformat.test.mjs does.
//
//   • applyFormat() performs that plan on a live <textarea> through
//     document.execCommand("insertText"). That looks archaic and it is, but it
//     is still the only thing in Chromium and WebKit that edits a field's value
//     the way a keystroke does — which means the browser's own undo stack
//     records it. Assigning `el.value` (or a framework binding) from script
//     WIPES that stack, so before this every toolbar press and every Ctrl+B
//     killed Ctrl+Z for the rest of the draft, including for the plain typing
//     that came before it.
//
// execCommand also fires a real `input` event, so a Svelte `bind:value` on the
// textarea follows along without anything extra — and so does the undo, which
// is the half that matters.

export const WRAPS = {
  bold: { pre: "**", post: "**", ph: "bold" },
  italic: { pre: "*", post: "*", ph: "italic" },
  strike: { pre: "~~", post: "~~", ph: "strikethrough" },
  spoiler: { pre: "||", post: "||", ph: "spoiler" },
  code: { pre: "`", post: "`", ph: "code" },
};

// Groups render with a thin separator between them. `keys` is only the tooltip
// hint — the actual bindings live in chordFor() below.
export const FMT_GROUPS = [
  [
    { kind: "bold", label: "Bold", keys: "B" },
    { kind: "italic", label: "Italic", keys: "I" },
    { kind: "strike", label: "Strikethrough" },
    { kind: "spoiler", label: "Spoiler", keys: "Shift+X" },
  ],
  [
    { kind: "code", label: "Inline code", keys: "E" },
    { kind: "codeblock", label: "Code block" },
  ],
  [
    { kind: "quote", label: "Quote", keys: "Shift+." },
    { kind: "link", label: "Link", keys: "Shift+K" },
  ],
];

// A caret sitting against a word wants markers it can type between; a caret in
// whitespace wants the placeholder word, because there is nothing there to
// format and "**bold**" tells you what just happened. Jammed against the end of
// a word the placeholder produced `word**bold** here`, which is nobody's
// intention — the word you were about to emphasise is the one to the left.
const WORDISH = /[\p{L}\p{N}_]/u;
function touchesWord(value, at) {
  return WORDISH.test(value[at - 1] || "") || WORDISH.test(value[at] || "");
}

// planFormat: what this press does to `value`, as a replacement of
// [from, to) with `insert`, leaving the selection at [selStart, selEnd).
// Returns null when the kind is unknown.
export function planFormat(value, start, end, kind) {
  const sel = value.slice(start, end);

  if (kind === "quote") {
    // Toggle "> " on every line the selection touches.
    const lineStart = value.lastIndexOf("\n", start - 1) + 1;
    let lineEnd = value.indexOf("\n", end);
    if (lineEnd < 0) lineEnd = value.length;
    const lines = value.slice(lineStart, lineEnd).split("\n");
    const quoted = lines.every((l) => l.startsWith("> "));
    const block = lines.map((l) => (quoted ? l.slice(2) : "> " + l)).join("\n");
    if (start === end) {
      const p = Math.max(lineStart, start + (quoted ? -2 : 2));
      const caret = Math.min(p, lineStart + block.length);
      return { from: lineStart, to: lineEnd, insert: block, selStart: caret, selEnd: caret };
    }
    return {
      from: lineStart,
      to: lineEnd,
      insert: block,
      selStart: lineStart,
      selEnd: lineStart + block.length,
    };
  }

  if (kind === "link") {
    // A selection becomes the label with "url" selected to type over; with
    // nothing selected, insert a full placeholder and select the label.
    const label = sel || "text";
    const insert = `[${label}](url)`;
    if (sel) {
      const s = start + label.length + 3; // just past "[label]("
      return { from: start, to: end, insert, selStart: s, selEnd: s + 3 };
    }
    return { from: start, to: end, insert, selStart: start + 1, selEnd: start + 1 + label.length };
  }

  if (kind === "codeblock") {
    // Fences want their own lines — only add the newlines that are missing.
    const text = sel || "code";
    const pre = (start > 0 && value[start - 1] !== "\n" ? "\n" : "") + "```\n";
    const post = "\n```" + (end < value.length && value[end] !== "\n" ? "\n" : "");
    const s = start + pre.length;
    return { from: start, to: end, insert: pre + text + post, selStart: s, selEnd: s + text.length };
  }

  const wrap = WRAPS[kind];
  if (!wrap) return null;
  const { pre, post, ph } = wrap;
  // Already wrapped (markers just outside, or included in the selection)? Then
  // this press toggles the formatting back off. The italic guard keeps a lone
  // "*" check from eating half of a surrounding "**".
  const outside =
    sel &&
    value.slice(start - pre.length, start) === pre &&
    value.slice(end, end + post.length) === post &&
    !(kind === "italic" && value.slice(start - 2, start) === "**" && value.slice(end, end + 2) === "**");
  const inside = sel.length >= pre.length + post.length && sel.startsWith(pre) && sel.endsWith(post);
  if (outside) {
    return {
      from: start - pre.length,
      to: end + post.length,
      insert: sel,
      selStart: start - pre.length,
      selEnd: start - pre.length + sel.length,
    };
  }
  if (inside) {
    const inner = sel.slice(pre.length, sel.length - post.length);
    return { from: start, to: end, insert: inner, selStart: start, selEnd: start + inner.length };
  }
  if (!sel && touchesWord(value, start)) {
    // Bare markers, caret between them: the standard behaviour, and the one
    // that composes with carrying on typing.
    const caret = start + pre.length;
    return { from: start, to: end, insert: pre + post, selStart: caret, selEnd: caret };
  }
  const text = sel || ph;
  return {
    from: start,
    to: end,
    insert: pre + text + post,
    selStart: start + pre.length,
    selEnd: start + pre.length + text.length,
  };
}

// replaceRange: put `insert` where [from, to) was, through the browser's own
// editing pipeline so the undo stack keeps it. Returns true if the UA path was
// taken; false means the caller has to fall back to assigning the value (and
// losing undo, which is why the fallback is only a fallback).
export function replaceRange(el, from, to, insert, selStart, selEnd) {
  if (!el) return false;
  el.focus();
  el.setSelectionRange(from, to);
  let ok = false;
  try {
    // insertText with an empty string is a no-op in some engines, so a pure
    // deletion goes through delete instead. (Unwrapping a marker pair is a
    // replacement, never an empty insert, so this is belt and braces.)
    ok = insert === ""
      ? document.execCommand("delete")
      : document.execCommand("insertText", false, insert);
  } catch {
    ok = false;
  }
  if (!ok) return false;
  el.setSelectionRange(selStart, selEnd);
  return true;
}

// applyFormat: plan, then perform. `onFallback(nextValue, selStart, selEnd)` is
// called when the UA path is unavailable, so the caller can assign the value
// itself — the one case where undo is lost, and it is not reachable in either
// engine Concord ships on.
export function applyFormat(el, kind, onFallback) {
  if (!el) return false;
  const value = el.value;
  const start = el.selectionStart ?? value.length;
  const end = el.selectionEnd ?? start;
  const plan = planFormat(value, start, end, kind);
  if (!plan) return false;
  if (replaceRange(el, plan.from, plan.to, plan.insert, plan.selStart, plan.selEnd)) return true;
  const next = value.slice(0, plan.from) + plan.insert + value.slice(plan.to);
  onFallback?.(next, plan.selStart, plan.selEnd);
  return true;
}

// chordFor: which formatting kind a keydown asks for, or null.
//
// Ctrl+K used to be "link" here. It is the quick switcher now, everywhere and
// in every focus state, because that is the one chord every piece of software a
// person also uses agrees about — so the link moved one key over to
// Ctrl+Shift+K, which is also where the toolbar tooltip now points.
export function chordFor(e) {
  if (!(e.ctrlKey || e.metaKey) || e.altKey) return null;
  const k = e.key.toLowerCase();
  if (!e.shiftKey) {
    if (k === "b") return "bold";
    if (k === "i") return "italic";
    if (k === "e") return "code";
    return null;
  }
  if (k === "x") return "spoiler";
  if (k === "k") return "link";
  if (e.code === "Period" || k === "." || k === ">") return "quote";
  return null;
}
