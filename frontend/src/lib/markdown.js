// Minimal, XSS-safe markdown. The invariant that keeps it safe: the input is
// HTML-escaped FIRST, then a fixed set of our own tags is layered on top — so
// no user-controlled string can open a tag or attribute. Keep it that way.
//
// Supported: ```code fences```, `inline code`, **bold**, *italic*, > quotes,
// - / 1. lists, bare links, ![image](data:image/...) attachments, @mentions.

export function escapeHtml(s) {
  return s.replace(
    /[&<>"']/g,
    (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c],
  );
}

// Inline rules applied to already-escaped text (code spans are cut out first
// so *bold* inside backticks stays literal).
function renderInline(s, mentionNames) {
  const codeSpans = [];
  s = s.replace(/`([^`]+)`/g, (_, code) => {
    codeSpans.push(code);
    return `\x00${codeSpans.length - 1}\x00`;
  });

  // Inline image attachments (strict data-URI whitelist, so no script URIs).
  s = s.replace(
    /!\[image\]\((data:image\/(?:png|jpeg|gif|webp);base64,[A-Za-z0-9+/=]+)\)/g,
    '<img class="attachment" loading="lazy" src="$1" alt="attachment" />',
  );
  s = s.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  s = s.replace(/(^|[^*])\*([^*\n]+)\*/g, "$1<em>$2</em>");
  s = s.replace(
    /(https?:\/\/[^\s<]+)/g,
    '<a href="$1" target="_blank" rel="noopener noreferrer">$1</a>',
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

  return s.replace(/\x00(\d+)\x00/g, (_, i) => `<code>${codeSpans[+i]}</code>`);
}

// renderMarkdown converts a message body to safe HTML. mentionNames (optional)
// is the list of display names to highlight as @mentions.
export function renderMarkdown(text, mentionNames = []) {
  const parts = text.split("```");
  let out = "";
  for (let i = 0; i < parts.length; i++) {
    if (i % 2 === 1) {
      // Inside a fence (unclosed runs to the end, like Discord). Strip one
      // leading language line.
      const body = parts[i].replace(/^[a-zA-Z0-9+-]*\n/, "");
      out += `<pre><code>${escapeHtml(body.replace(/\n$/, ""))}</code></pre>`;
    } else {
      out += renderBlocks(escapeHtml(parts[i]), mentionNames);
    }
  }
  return out;
}

// Block rules (quotes, lists) over escaped text, line by line.
function renderBlocks(s, mentionNames) {
  const lines = s.split("\n");
  let out = "";
  let list = null; // "ul" | "ol" | null
  const closeList = () => {
    if (list) out += `</${list}>`;
    list = null;
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const ul = /^\s*[-*] (.*)$/.exec(line);
    const ol = /^\s*\d+[.)] (.*)$/.exec(line);
    const quote = /^&gt; ?(.*)$/.exec(line);
    if (ul || ol) {
      const kind = ul ? "ul" : "ol";
      if (list !== kind) {
        closeList();
        out += `<${kind}>`;
        list = kind;
      }
      out += `<li>${renderInline((ul || ol)[1], mentionNames)}</li>`;
    } else if (quote) {
      closeList();
      out += `<blockquote>${renderInline(quote[1], mentionNames)}</blockquote>`;
    } else {
      closeList();
      out += renderInline(line, mentionNames);
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
