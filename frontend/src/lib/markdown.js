// Minimal, XSS-safe markdown. The invariant that keeps it safe: the input is
// HTML-escaped FIRST, then a fixed set of our own tags is layered on top — so
// no user-controlled string can open a tag or attribute. Keep it that way.
//
// Supported: ```code fences``` (with language label), `inline code`, **bold**,
// *italic*, __underline__, ~~strike~~, ||spoiler||, # headers, > and >>> quotes,
// - / 1. lists, bare + [masked](url) links, ![image](data:image/...) attachments,
// @mentions.

// Named colors for {name|text}, mapped to fixed hex values so nothing but a
// vetted color string ever reaches an inline style. Chosen to read on both the
// dark and light themes.
export const COLOR_NAMES = {
  red: "#e0555b",
  orange: "#e8873c",
  yellow: "#e0b341",
  green: "#3ba55d",
  teal: "#2dd4bf",
  blue: "#4b8bf5",
  purple: "#a06bff",
  pink: "#eb6f9e",
  gray: "#8a92a6",
};

export function escapeHtml(s) {
  return s.replace(
    /[&<>"']/g,
    (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c],
  );
}

// Matches a single rendered emoji: a regional-indicator pair (flags), or a
// pictographic base plus any modifiers/variation-selectors/ZWJ-joined
// sequence (skin tones, 👨‍👩‍👧, etc.). ASCII digits/# aren't Extended_Pictographic,
// so plain text and our own markup delimiters are untouched.
const EMOJI_RE =
  /(?:\p{RI}\p{RI}|\p{Extended_Pictographic}(?:\u{FE0F}|\u{20E3}|[\u{1F3FB}-\u{1F3FF}]|\u{200D}\p{Extended_Pictographic})*)/gu;

// emojiOnly: true when a message is nothing but emoji (unicode and/or custom
// :name: shortcodes) and whitespace, so the caller can render it "jumbo" like
// Discord. Bounded to a handful of emoji.
const CUSTOM_EMOJI_RE = /:[a-z0-9_]{2,32}:/gi;
export function emojiOnly(text) {
  const stripped = text.replace(EMOJI_RE, "").replace(CUSTOM_EMOJI_RE, "").trim();
  if (stripped) return false;
  const count =
    (text.match(EMOJI_RE) || []).length + (text.match(CUSTOM_EMOJI_RE) || []).length;
  return count > 0 && count <= 27;
}

// twemojiCode maps an emoji sequence to its Twemoji asset filename: hex
// codepoints joined by '-'. FE0F variation selectors are dropped unless the
// sequence contains a ZWJ (Twemoji's own grabTheRightIcon convention).
export function twemojiCode(seq) {
  const raw = seq.indexOf("\u200D") < 0 ? seq.replace(/\uFE0F/g, "") : seq;
  const out = [];
  for (const ch of raw) out.push(ch.codePointAt(0).toString(16));
  return out.join("-");
}

// Inline rules applied to already-escaped text (code spans are cut out first
// so *bold* inside backticks stays literal).
function renderInline(s, mentionNames, customEmoji) {
  const codeSpans = [];
  s = s.replace(/`([^`]+)`/g, (_, code) => {
    codeSpans.push(code);
    return `\x00${codeSpans.length - 1}\x00`;
  });

  // Unicode emoji render as bundled Twemoji images — uniform squares on every
  // OS, exactly like Discord — instead of native font glyphs (whose size and
  // baseline wobble per platform). Done on escaped plain text before any of
  // our tags are inserted, so the swap can never land inside an attribute or
  // tag we add later. An emoji newer than the bundled set falls back to the
  // raw glyph via onerror.
  s = s.replace(
    EMOJI_RE,
    (m) =>
      `<img class="emoji" draggable="false" src="/twemoji/${twemojiCode(m)}.svg" alt="${m}" onerror="this.replaceWith(this.alt)" />`,
  );

  // Custom guild emoji: :name: -> <img>. The image is a backend-validated
  // base64 data:image URI, but we still escape it here (defense in depth) so a
  // malformed value that somehow reaches this sink can't break out of the src
  // attribute and inject script. The name charset ([a-z0-9_]) can't break out.
  if (customEmoji) {
    s = s.replace(/:([a-z0-9_]{2,32}):/g, (whole, name) => {
      const img = customEmoji[name];
      return img
        ? `<img class="cemoji" src="${escapeHtml(img)}" alt=":${name}:" title=":${name}:" />`
        : whole;
    });
  }

  // Inline image attachments (strict data-URI whitelist, so no script URIs).
  s = s.replace(
    /!\[image\]\((data:image\/(?:png|jpeg|gif|webp);base64,[A-Za-z0-9+/=]+)\)/g,
    '<img class="attachment" loading="lazy" src="$1" alt="attachment" />',
  );
  // Spoilers ||text|| — revealed on click (handler in Message.svelte). Runs
  // before emphasis so **bold**/etc. inside a spoiler still render.
  s = s.replace(/\|\|(.+?)\|\|/g, '<span class="spoiler" role="button" tabindex="0">$1</span>');
  // Strikethrough ~~text~~ and underline __text__ (underscore is free — italic
  // uses *, so __ never collides with emphasis).
  s = s.replace(/~~(.+?)~~/g, "<s>$1</s>");
  s = s.replace(/__(.+?)__/g, "<u>$1</u>");
  s = s.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  s = s.replace(/(^|[^*])\*([^*\n]+)\*/g, "$1<em>$2</em>");
  // Colored text: {#rrggbb|text} or {name|text}. The color that reaches the
  // inline style is ONLY ever a strict #hex or a name mapped to a fixed hex —
  // no user string touches the CSS, so this can't inject (same guarantee the
  // rest of this file relies on). Runs after emphasis so **bold** inside a
  // color still works. The text is already HTML-escaped.
  s = s.replace(/\{(#[0-9a-fA-F]{6}|#[0-9a-fA-F]{3}|[a-z]{3,10})\|([^{}]+)\}/g, (whole, color, text) => {
    const hex = COLOR_NAMES[color.toLowerCase()] || (/^#[0-9a-fA-F]{3,6}$/.test(color) ? color : null);
    return hex ? `<span style="color:${hex}">${text}</span>` : whole;
  });
  // Links are stashed as placeholders and only restored at the very end — so
  // neither the autolinker nor the @mention pass ever runs inside a generated
  // href (e.g. a member named "Foo" must not turn https://x.com/@Foo into a
  // mention span, which would corrupt the attribute). Masked links first, then
  // bare URLs.
  const links = [];
  const stash = (html) => {
    links.push(html);
    return `\x01${links.length - 1}\x01`;
  };
  s = s.replace(/\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\)/g, (_, text, url) =>
    stash(`<a href="${url}" target="_blank" rel="noopener noreferrer">${text}</a>`),
  );
  s = s.replace(/(https?:\/\/[^\s<]+)/g, (url) =>
    stash(`<a href="${url}" target="_blank" rel="noopener noreferrer">${url}</a>`),
  );
  if (mentionNames?.length) {
    // Longest name first so "@Ann Lee" wins over "@Ann". Names arrive escaped
    // with the same escapeHtml, so they match the escaped text. `self` (the
    // viewer's own name) gets an extra class so their mentions stand out.
    const names = (Array.isArray(mentionNames) ? mentionNames : [mentionNames])
      .map((n) => (typeof n === "string" ? { name: n, self: false } : n))
      .filter((n) => n.name);
    const escaped = names
      .map((n) => ({ ...n, esc: escapeHtml(n.name).replace(/[.*+?^${}()|[\]\\]/g, "\\$&") }))
      .sort((a, b) => b.esc.length - a.esc.length);
    for (const n of escaped) {
      // data-mention carries the raw name so the click handler can resolve
      // the member; it's escaped, so it's inert as an attribute value.
      s = s.replace(
        new RegExp(`@(${n.esc})(?![\\w])`, "g"),
        `<span class="mention${n.self ? " mention-self" : ""}" data-mention="$1">@$1</span>`,
      );
    }
  }

  // Restore links (after mentions) and code spans.
  s = s.replace(/\x01(\d+)\x01/g, (_, i) => links[+i]);
  return s.replace(/\x00(\d+)\x00/g, (_, i) => `<code>${codeSpans[+i]}</code>`);
}

// renderMarkdown converts a message body to safe HTML. mentionNames (optional)
// highlights @mentions; customEmoji (optional, {name: dataURI}) renders :name:.
export function renderMarkdown(text, mentionNames = [], customEmoji = null) {
  const parts = text.split("```");
  let out = "";
  for (let i = 0; i < parts.length; i++) {
    if (i % 2 === 1) {
      // Inside a fence (unclosed runs to the end, like Discord). Keep the
      // leading language line as a label (data-lang), then strip it.
      const lang = (/^([a-zA-Z0-9+-]+)\n/.exec(parts[i]) || [])[1] || "";
      const body = parts[i].replace(/^[a-zA-Z0-9+-]*\n/, "");
      out += `<pre${lang ? ` data-lang="${escapeHtml(lang)}"` : ""}><code>${escapeHtml(body.replace(/\n$/, ""))}</code></pre>`;
    } else {
      out += renderBlocks(escapeHtml(parts[i]), mentionNames, customEmoji);
    }
  }
  return out;
}

// Block rules (quotes, lists) over escaped text, line by line.
function renderBlocks(s, mentionNames, customEmoji) {
  const lines = s.split("\n");
  let out = "";
  let list = null; // "ul" | "ol" | null
  const closeList = () => {
    if (list) out += `</${list}>`;
    list = null;
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    // Multi-line blockquote: >>> turns the rest of the message into one quote.
    const bq3 = /^&gt;&gt;&gt;\s?([\s\S]*)$/.exec(line);
    if (bq3) {
      closeList();
      const rest = [bq3[1], ...lines.slice(i + 1)].join("\n");
      out += `<blockquote>${renderBlocks(rest, mentionNames, customEmoji)}</blockquote>`;
      return out;
    }
    // Headers # / ## / ### (mapped to h3–h5 so they stay chat-sized).
    const hdr = /^(#{1,3}) (.+)$/.exec(line);
    const ul = /^\s*[-*] (.*)$/.exec(line);
    const ol = /^\s*\d+[.)] (.*)$/.exec(line);
    const quote = /^&gt; ?(.*)$/.exec(line);
    if (hdr) {
      closeList();
      const tag = ["h3", "h4", "h5"][hdr[1].length - 1];
      out += `<${tag} class="md-h">${renderInline(hdr[2], mentionNames, customEmoji)}</${tag}>`;
    } else if (ul || ol) {
      const kind = ul ? "ul" : "ol";
      if (list !== kind) {
        closeList();
        out += `<${kind}>`;
        list = kind;
      }
      out += `<li>${renderInline((ul || ol)[1], mentionNames, customEmoji)}</li>`;
    } else if (quote) {
      closeList();
      out += `<blockquote>${renderInline(quote[1], mentionNames, customEmoji)}</blockquote>`;
    } else {
      closeList();
      out += renderInline(line, mentionNames, customEmoji);
      if (i < lines.length - 1) out += "\n";
    }
  }
  closeList();
  return out;
}

// containsMention: does this message body @-mention one of the given names?
export function containsMention(text, names) {
  return names.some(
    (n) => n && new RegExp(`@${n.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}(?![\\w])`).test(text),
  );
}
