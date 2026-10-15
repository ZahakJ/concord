// BUILD THE DOCUMENTATION SITE.
//
//   node scripts/build-docs.mjs           →  docs/manual/<slug>/index.html
//   node scripts/build-docs.mjs --check   →  exit 1 if docs/manual is stale or a link is broken
//
// The sources are the Markdown files a reader can already open on GitHub:
// the user manual in docs/manual/*.md, the developer pages in docs/dev/*.md,
// and a handful of documents that live elsewhere in the repository and are
// rendered as pages rather than copied (FAQ, BOTS, RENDEZVOUS, CONTRIBUTING,
// RELEASING, DESIGN, PUSH). A link between two sources is written as a
// relative path to the other .md file, so it works on GitHub, and is
// rewritten here to the page it became.
//
// GitHub Pages serves docs/ from main, so the output is committed next to
// its sources and published by the push that changes them. CI runs --check,
// which fails when the committed pages do not match their sources: a page
// edited without a rebuild would otherwise publish the previous text.
//
// No dependencies. The Markdown renderer below covers what these documents
// use (GitHub-flavoured blocks, tables, task lists, fenced code), and
// nothing more; docs.css and docs.js in docs/manual/ are written by hand.

import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, join, posix, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const OUT = "docs/manual"; // repo-relative
const REPO = "https://github.com/ZahakJ/concord";

/** The navigation, in reading order. `file` is repo-relative. */
export const SECTIONS = [
  {
    title: "Start here",
    pages: [
      ["getting-started", "docs/manual/getting-started.md", "Getting started"],
      ["identity", "docs/manual/identity.md", "Accounts and identity"],
      ["devices", "docs/manual/devices.md", "Multiple devices"],
      ["backups", "docs/manual/backups.md", "Backups and recovery"],
    ],
  },
  {
    title: "Talking",
    pages: [
      ["guilds", "docs/manual/guilds.md", "Guilds and channels"],
      ["roles", "docs/manual/roles.md", "Roles and permissions"],
      ["forums", "docs/manual/forums.md", "Forum boards"],
      ["direct-messages", "docs/manual/direct-messages.md", "Direct messages"],
      ["files-and-media", "docs/manual/files-and-media.md", "Files and media"],
      ["search", "docs/manual/search.md", "Search"],
      ["voice-and-video", "docs/manual/voice-and-video.md", "Voice and video"],
      ["notifications", "docs/manual/notifications.md", "Notifications"],
    ],
  },
  {
    title: "Safety",
    pages: [
      ["moderation", "docs/manual/moderation.md", "Moderation and safety"],
      ["privacy", "docs/manual/privacy.md", "Privacy and encryption"],
    ],
  },
  {
    title: "Making it yours",
    pages: [
      ["settings", "docs/manual/settings.md", "Settings and themes"],
      ["shortcuts", "docs/manual/shortcuts.md", "Keyboard shortcuts"],
      ["bots", "docs/BOTS.md", "Bots"],
      ["rendezvous", "docs/RENDEZVOUS.md", "Running a rendezvous"],
    ],
  },
  {
    title: "Help",
    pages: [
      ["troubleshooting", "docs/manual/troubleshooting.md", "Troubleshooting"],
      ["faq", "docs/FAQ.md", "FAQ"],
    ],
  },
  {
    title: "Developers",
    pages: [
      ["architecture", "docs/dev/architecture.md", "Architecture"],
      ["protocol", "docs/dev/protocol.md", "Protocol and data model"],
      ["building", "docs/dev/building.md", "Building from source"],
      ["testing", "docs/dev/testing.md", "Testing"],
      ["contributing", "CONTRIBUTING.md", "Contributing"],
      ["releasing", "docs/RELEASING.md", "Releasing"],
      ["design", "docs/DESIGN.md", "Design in depth"],
      ["push", "docs/PUSH.md", "Push wakes"],
    ],
  },
].map((s) => ({ ...s, pages: s.pages.map(([slug, file, title]) => ({ slug, file, title })) }));

export const PAGES = SECTIONS.flatMap((s) => s.pages.map((p) => ({ ...p, section: s.title })));
const BY_FILE = new Map(PAGES.map((p) => [p.file, p]));

// ── Markdown ────────────────────────────────────────────────────────────────

const esc = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
const NAMED = { amp: "&", lt: "<", gt: ">", quot: '"', apos: "'", nbsp: " " };
const decode = (s) =>
  s
    .replace(/&#x([0-9a-f]+);/gi, (_, h) => String.fromCodePoint(parseInt(h, 16)))
    .replace(/&#(\d+);/g, (_, d) => String.fromCodePoint(Number(d)))
    .replace(/&([a-z]+);/gi, (m, n) => NAMED[n.toLowerCase()] ?? m);
const plain = (html) => decode(html.replace(/<[^>]+>/g, "")).replace(/\s+/g, " ").trim();

/** A heading's id, the way GitHub makes it, so `page.md#anchor` lands in
 *  both places: lowercase the text, drop everything that is not a letter,
 *  mark, digit, connector, hyphen or space, then spaces become hyphens. */
export const slugify = (text) =>
  text
    .toLowerCase()
    .replace(/[^\p{L}\p{M}\p{Nd}\p{Pc}\- ]/gu, "")
    .replace(/ /g, "-");

const ESCAPABLE = /[\\`*_{}[\]()#+\-.!|~<>"'$&:;,/=?@^]/;

/** Inline Markdown → HTML. `link(href, kind)` maps a destination. */
function inline(src, ctx) {
  const slots = [];
  const hold = (html) => `\u0000${slots.push(html) - 1}\u0000`;
  let out = "";
  let i = 0;
  while (i < src.length) {
    const c = src[i];
    if (c === "\\" && i + 1 < src.length) {
      if (src[i + 1] === "\n") { out += hold("<br>\n"); i += 2; continue; }
      if (ESCAPABLE.test(src[i + 1])) { out += hold(esc(src[i + 1])); i += 2; continue; }
    }
    if (c === "`") {
      let n = 1;
      while (src[i + n] === "`") n++;
      const fence = "`".repeat(n);
      let j = i + n;
      let end = -1;
      while ((j = src.indexOf(fence, j)) !== -1) {
        if (src[j + n] !== "`" && src[j - 1] !== "`") { end = j; break; }
        while (src[j] === "`") j++;
      }
      if (end !== -1) {
        let code = src.slice(i + n, end).replace(/\n/g, " ");
        if (/^ .*[^ ].* $/.test(code)) code = code.slice(1, -1);
        out += hold(`<code>${esc(code)}</code>`);
        i = end + n;
        continue;
      }
      out += fence;
      i += n;
      continue;
    }
    if ((c === "!" && src[i + 1] === "[") || c === "[") {
      const img = c === "!";
      const open = img ? i + 1 : i;
      const close = matchBracket(src, open, "[", "]");
      if (close !== -1 && src[close + 1] === "(") {
        const end = matchBracket(src, close + 1, "(", ")");
        if (end !== -1) {
          const label = src.slice(open + 1, close);
          const dest = /^\s*<?([^\s>]*)>?(?:\s+"([^"]*)")?\s*$/.exec(src.slice(close + 2, end));
          if (dest) {
            const [, href, title] = dest;
            if (img) out += hold(ctx.image(href, plain(inline(label, ctx)), title));
            else out += hold(ctx.anchor(href, inline(label, ctx), title));
            i = end + 1;
            continue;
          }
        }
      }
    }
    if (c === "<") {
      const m = /^<(https?:\/\/[^\s>]+|mailto:[^\s>]+)>/.exec(src.slice(i));
      if (m) { out += hold(ctx.anchor(m[1], esc(m[1].replace(/^mailto:/, "")))); i += m[0].length; continue; }
    }
    if (c === "h" && /^https?:\/\//.test(src.slice(i, i + 8)) && !/[\w/]/.test(src[i - 1] ?? "")) {
      let m = /^https?:\/\/[^\s<]+/.exec(src.slice(i))[0];
      while (/[.,:;!?'")\]*_]$/.test(m) && !(m.endsWith(")") && (m.match(/\(/g) ?? []).length >= (m.match(/\)/g) ?? []).length)) m = m.slice(0, -1);
      out += hold(ctx.anchor(m, esc(m)));
      i += m.length;
      continue;
    }
    if (c === " " && src.startsWith("  \n", i)) {
      let j = i;
      while (src[j] === " ") j++;
      if (src[j] === "\n") { out += hold("<br>\n"); i = j + 1; continue; }
    }
    out += c;
    i++;
  }
  let html = esc(out);
  html = html
    .replace(/~~(?=\S)([\s\S]*?\S)~~/g, "<del>$1</del>")
    .replace(/\*\*(?=\S)([\s\S]*?\S)\*\*/g, "<strong>$1</strong>")
    .replace(/(^|[^\p{L}\p{Nd}_])__(?=\S)([\s\S]*?\S)__(?![\p{L}\p{Nd}_])/gu, "$1<strong>$2</strong>")
    .replace(/(^|[^*])\*(?=[^\s*])([^*]*?[^\s*])\*(?!\*)/g, "$1<em>$2</em>")
    .replace(/(^|[^\p{L}\p{Nd}_])_(?=[^\s_])([\s\S]*?[^\s_])_(?![\p{L}\p{Nd}_])/gu, "$1<em>$2</em>");
  // Twice: a slot can hold a link whose label holds slots of its own.
  for (let k = 0; k < 2; k++) html = html.replace(/\u0000(\d+)\u0000/g, (_, n) => slots[n]);
  return html;
}

function matchBracket(s, at, open, close) {
  let depth = 0;
  for (let j = at; j < s.length; j++) {
    if (s[j] === "\\") { j++; continue; }
    if (s[j] === "`") {
      const k = s.indexOf("`", j + 1);
      if (k !== -1 && open === "[") { j = k; continue; }
    }
    if (s[j] === open) depth++;
    else if (s[j] === close && --depth === 0) return j;
  }
  return -1;
}

const RE = {
  fence: /^( {0,3})(`{3,}|~{3,})\s*([\w+#.-]*)[^`]*$/,
  heading: /^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$/,
  hr: /^ {0,3}([-*_])(?:[ \t]*\1){2,}[ \t]*$/,
  quote: /^ {0,3}> ?/,
  item: /^( {0,12})([-*+]|\d{1,9}[.)])([ \t]+|$)(.*)$/,
  tableDelim: /^ {0,3}\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$/,
  html: /^ {0,3}<\/?(details|summary|div|p|picture|source|img|table|br|hr|kbd|sup|sub|a)\b/i,
  blank: /^\s*$/,
};

const splitRow = (line) => {
  let s = line.trim();
  if (s.startsWith("|")) s = s.slice(1);
  if (s.endsWith("|") && !s.endsWith("\\|")) s = s.slice(0, -1);
  const cells = [];
  let cur = "";
  let inCode = false;
  for (let i = 0; i < s.length; i++) {
    if (s[i] === "\\" && s[i + 1] === "|") { cur += "|"; i++; continue; }
    if (s[i] === "`") inCode = !inCode;
    if (s[i] === "|" && !inCode) { cells.push(cur.trim()); cur = ""; continue; }
    cur += s[i];
  }
  cells.push(cur.trim());
  return cells;
};

/** Does `line` start a block that interrupts a paragraph? */
const interrupts = (line) =>
  RE.fence.test(line) || RE.heading.test(line) || RE.hr.test(line) || RE.quote.test(line) ||
  /^ {0,3}([-*+]|1[.)])[ \t]+\S/.test(line) || RE.html.test(line);

function blocks(lines, ctx, tight = false) {
  let html = "";
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (RE.blank.test(line)) { i++; continue; }
    let m;
    if ((m = RE.fence.exec(line))) {
      const [, indent, fence, lang] = m;
      const body = [];
      i++;
      while (i < lines.length && !new RegExp(`^ {0,3}${fence[0] === "`" ? "`" : "~"}{${fence.length},}\\s*$`).test(lines[i])) {
        body.push(lines[i].replace(new RegExp(`^ {0,${indent.length}}`), ""));
        i++;
      }
      i++;
      html += `<pre${lang ? ` data-lang="${esc(lang)}"` : ""}><code${lang ? ` class="language-${esc(lang)}"` : ""}>${esc(body.join("\n"))}</code></pre>\n`;
      continue;
    }
    if ((m = RE.heading.exec(line))) {
      html += ctx.heading(m[1].length, inline(m[2] ?? "", ctx));
      i++;
      continue;
    }
    if (RE.hr.test(line)) { html += "<hr>\n"; i++; continue; }
    if (RE.quote.test(line)) {
      const body = [];
      while (i < lines.length && !RE.blank.test(lines[i]) && (RE.quote.test(lines[i]) || !interrupts(lines[i]))) {
        body.push(lines[i].replace(RE.quote, ""));
        i++;
      }
      html += `<blockquote>\n${blocks(body, ctx)}</blockquote>\n`;
      continue;
    }
    if (line.includes("|") && i + 1 < lines.length && RE.tableDelim.test(lines[i + 1]) && lines[i + 1].includes("-")) {
      const head = splitRow(line);
      const align = splitRow(lines[i + 1]).map((c) => (/^:-+:$/.test(c) ? "center" : /-+:$/.test(c) ? "right" : /^:-/.test(c) ? "left" : ""));
      i += 2;
      const rows = [];
      while (i < lines.length && !RE.blank.test(lines[i]) && lines[i].includes("|") && !interrupts(lines[i])) rows.push(splitRow(lines[i++]));
      const cell = (tag, c, k) => `<${tag}${align[k] ? ` style="text-align:${align[k]}"` : ""}>${inline(c ?? "", ctx)}</${tag}>`;
      html += `<div class="table"><table><thead><tr>${head.map((c, k) => cell("th", c, k)).join("")}</tr></thead><tbody>${rows
        .map((r) => `<tr>${head.map((_, k) => cell("td", r[k], k)).join("")}</tr>`)
        .join("")}</tbody></table></div>\n`;
      continue;
    }
    if ((m = RE.item.exec(line)) && m[1].length < 4) {
      const ordered = /\d/.test(m[2]);
      const start = ordered ? parseInt(m[2], 10) : 1;
      const markerType = ordered ? m[2].slice(-1) : m[2];
      const items = [];
      let loose = false;
      while (i < lines.length) {
        const im = RE.item.exec(lines[i]);
        if (!im || (/\d/.test(im[2]) ? im[2].slice(-1) : im[2]) !== markerType || im[1].length >= m[1].length + 4) break;
        const contentIndent = im[1].length + im[2].length + Math.min(Math.max(im[3].length, 1), 4);
        const body = [im[4]];
        i++;
        let sawBlank = false;
        while (i < lines.length) {
          const l = lines[i];
          if (RE.blank.test(l)) { body.push(""); sawBlank = true; i++; continue; }
          const indent = l.match(/^ */)[0].length;
          if (indent >= contentIndent) { body.push(l.slice(contentIndent)); if (sawBlank) loose = true; sawBlank = false; i++; continue; }
          if (!sawBlank && !interrupts(l) && !RE.item.test(l)) { body.push(l.trim()); i++; continue; }
          break;
        }
        while (body.length && body[body.length - 1] === "") body.pop();
        items.push(body);
        if (sawBlank && i < lines.length) {
          const next = RE.item.exec(lines[i]);
          if (next && next[1].length < m[1].length + 4) loose = true;
          else break;
        }
      }
      const tag = ordered ? "ol" : "ul";
      const lis = items.map((body) => {
        let pre = "";
        const task = /^\[([ xX])\][ \t]+/.exec(body[0]);
        if (task) {
          body[0] = body[0].slice(task[0].length);
          pre = `<input type="checkbox" disabled${task[1] === " " ? "" : " checked"}> `;
        }
        return `<li${task ? ' class="task"' : ""}>${pre}${blocks(body, ctx, !loose).trim()}</li>`;
      });
      html += `<${tag}${ordered && start !== 1 ? ` start="${start}"` : ""}>\n${lis.join("\n")}\n</${tag}>\n`;
      continue;
    }
    if (/^( {4}|\t)/.test(line)) {
      const body = [];
      while (i < lines.length && (/^( {4}|\t)/.test(lines[i]) || RE.blank.test(lines[i]))) body.push(lines[i++].replace(/^( {4}|\t)/, ""));
      while (body.length && RE.blank.test(body[body.length - 1])) body.pop();
      html += `<pre><code>${esc(body.join("\n"))}</code></pre>\n`;
      continue;
    }
    if (RE.html.test(line)) {
      // Raw HTML is not rendered: the sources are meant to be plain Markdown.
      // A block of it is shown as text so the mistake is visible.
      const body = [];
      while (i < lines.length && !RE.blank.test(lines[i])) body.push(lines[i++]);
      html += `<pre><code>${esc(body.join("\n"))}</code></pre>\n`;
      ctx.warn(`raw HTML block rendered as text: ${body[0].trim().slice(0, 60)}`);
      continue;
    }
    const para = [line.trim()];
    i++;
    while (i < lines.length && !RE.blank.test(lines[i]) && !interrupts(lines[i]) && !(lines[i].includes("|") && RE.tableDelim.test(lines[i + 1] ?? ""))) {
      if (/^ {0,3}=+\s*$/.test(lines[i])) break;
      para.push(lines[i].replace(/^\s+/, ""));
      i++;
    }
    if (i < lines.length && /^ {0,3}(=+|-+)\s*$/.test(lines[i]) && !RE.blank.test(lines[i])) {
      html += ctx.heading(lines[i].trim()[0] === "=" ? 1 : 2, inline(para.join("\n"), ctx));
      i++;
      continue;
    }
    const text = inline(para.join("\n"), ctx);
    html += tight ? `${text}\n` : `<p>${text}</p>\n`;
  }
  return html;
}

// ── Pages ───────────────────────────────────────────────────────────────────

let tracked = null;
/** Files a published link may point at: tracked or about to be, never ignored. */
function isTracked(path) {
  if (!tracked) {
    try {
      tracked = new Set(execFileSync("git", ["ls-files", "-co", "--exclude-standard"], { cwd: ROOT, encoding: "utf8" }).split("\n"));
    } catch {
      tracked = { has: (p) => existsSync(join(ROOT, p)) };
    }
  }
  return tracked.has(path) || (tracked.has && [...(tracked.keys?.() ?? [])].some((t) => t.startsWith(`${path}/`)));
}

/** Render one source. `depth` is how far the page sits below docs/manual/. */
function renderPage(page, problems) {
  if (!existsSync(join(ROOT, page.file))) {
    problems.push(`${page.file}: missing (listed in SECTIONS)`);
    return { body: "", toc: [], ids: new Map(), links: [], title: page.title, summary: "" };
  }
  const md = readFileSync(join(ROOT, page.file), "utf8").replace(/\r\n/g, "\n");
  const from = posix.dirname(page.file);
  const toc = [];
  const ids = new Map();
  const links = [];
  const warn = (msg) => problems.push(`${page.file}: ${msg}`);
  let title = page.title;
  const ctx = {
    warn,
    heading(depth, html) {
      const text = plain(html);
      let id = slugify(text);
      const n = ids.get(id) ?? 0;
      ids.set(id, n + 1);
      if (n > 0) id = `${id}-${n}`;
      if (depth === 1) { title = text; return `<h1 id="${id}">${html}</h1>\n`; }
      if (depth <= 3) toc.push({ depth, id, text });
      return `<h${depth} id="${id}"><a class="anchor" href="#${id}" aria-hidden="true">#</a>${html}</h${depth}>\n`;
    },
    anchor(href, html, ttl) {
      const out = rewrite(href, from, links, warn);
      const ext = /^https?:/.test(out) ? ' rel="noreferrer"' : "";
      return `<a href="${esc(out)}"${ttl ? ` title="${esc(ttl)}"` : ""}${ext}>${html}</a>`;
    },
    image(href, alt, ttl) {
      const out = rewrite(href, from, links, warn);
      const target = /^https?:/.test(href) ? null : posix.normalize(posix.join(from, href.split("#")[0]));
      let media = `<img src="${esc(out)}" alt="${esc(alt)}" loading="lazy">`;
      // A diagram is inlined so the page's colors reach it: its shapes carry
      // classes the stylesheet themes, over attributes that work on GitHub.
      if (target && target.endsWith(".svg") && existsSync(join(ROOT, target))) {
        media = readFileSync(join(ROOT, target), "utf8").replace(/^<\?xml[^>]*>\s*/, "").replace(/<svg\b/, `<svg role="img" aria-label="${esc(alt)}"`);
      }
      return `<figure>${media}${ttl || alt ? `<figcaption>${esc(ttl || alt)}</figcaption>` : ""}</figure>`;
    },
  };
  // A summary is the first paragraph after the title.
  const summary = plain(inline((/^#[^\n]*\n+((?:[^\n#|>`\-*!][^\n]*\n?)+)/.exec(md)?.[1] ?? "").trim(), { ...ctx, warn() {}, anchor: (_h, h) => h, image: () => "" }));
  const body = blocks(md.split("\n"), ctx);
  return { body, toc, ids, links, title, summary };
}

/** Where a link in `from/` points once rendered. Collects page links for checking. */
function rewrite(href, from, links, warn) {
  if (/^(https?:|mailto:)/.test(href)) return href;
  if (href.startsWith("#")) { links.push({ page: null, hash: href.slice(1) }); return href; }
  const [path, hash = ""] = href.split("#");
  const target = posix.normalize(posix.join(from, path)).replace(/\/$/, "");
  const anchor = hash ? `#${hash}` : "";
  const page = BY_FILE.get(target);
  if (page) { links.push({ page, hash }); return `../${page.slug}/${anchor}`; }
  if (target.startsWith("..") || !isTracked(target)) { warn(`link to a file that is not in the repository: ${href}`); return href; }
  if (target.startsWith("docs/") && /\.[a-z0-9]+$/i.test(target) && !target.endsWith(".md")) return `../../${target.slice(5)}${anchor}`;
  const isDir = !/\.[a-z0-9]+$/i.test(posix.basename(target)) && !existsSync(join(ROOT, target)) ? false : !/\.[a-z0-9]+$/i.test(posix.basename(target));
  return `${REPO}/${isDir ? "tree" : "blob"}/main/${target}${anchor}`;
}

const LOGO = `<svg viewBox="0 0 64 64" aria-hidden="true"><path fill-rule="evenodd" d="M32 6C31.1 6 30.6 6.7 30.6 8.2L30.2 17C27.3 17.8 25.2 20.5 25.2 23.8L25.2 27.6L4.6 41.6C3.4 42.4 4.1 44.3 5.5 44L24 40.7C24.7 41.9 25.8 42.8 27.1 43.3L26.2 50.6C26.1 51.6 26.9 52.2 27.7 51.7L30.3 45.9L31 55.2C31 56 31.4 56.4 32 56.4C32.6 56.4 33 56 33 55.2L33.7 45.9L36.3 51.7C37.1 52.2 37.9 51.6 37.8 50.6L36.9 43.3C38.2 42.8 39.3 41.9 40 40.7L58.5 44C59.9 44.3 60.6 42.4 59.4 41.6L38.8 27.6L38.8 23.8C38.8 20.5 36.7 17.8 33.8 17L33.4 8.2C33.4 6.7 32.9 6 32 6ZM28.4 22.3L31 22.3L31 27.2L27.4 27.2L27.4 24.3C27.4 23.3 27.7 22.3 28.4 22.3ZM33 22.3L35.6 22.3C36.3 22.3 36.6 23.3 36.6 24.3L36.6 27.2L33 27.2Z"/></svg>`;

function shell({ title, body, toc, current, prev, next, file, summary, base }) {
  const nav = SECTIONS.map(
    (s) => `<div class="nav__section"><div class="nav__title">${esc(s.title)}</div>${s.pages
      .map((p) => `<a class="nav__link${p.slug === current ? " is-current" : ""}" href="${base}${p.slug}/"${p.slug === current ? ' aria-current="page"' : ""}>${esc(p.title)}</a>`)
      .join("")}</div>`,
  ).join("");
  const tocLinks = toc.map((h) => `<a class="toc__link toc__link--${h.depth}" href="#${h.id}">${esc(h.text)}</a>`).join("");
  const pagerLink = (p, kind, label) =>
    p ? `<a class="pager__link pager__link--${kind}" href="${base}${p.slug}/"><span class="pager__kicker">${label}</span><span class="pager__title">${esc(p.title)}</span></a>` : "<span></span>";
  return `<!doctype html>
<!-- Generated by scripts/build-docs.mjs${file ? ` from ${file}` : ""}. Edit the source, then run make docs. -->
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${esc(title)} · Concord manual</title>
<meta name="description" content="${esc(summary)}">
<meta name="color-scheme" content="dark light">
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 64 64'%3E%3Crect width='64' height='64' rx='14' fill='%2316181c'/%3E%3Cpath fill='%23fff' d='M32 12l2 14 12 9-12-3-2 18-2-18-12 3 12-9z'/%3E%3C/svg%3E">
<link rel="stylesheet" href="${base}docs.css">
<script>try{var t=localStorage.getItem("concord-docs-theme");if(t)document.documentElement.dataset.theme=t}catch(e){}</script>
</head>
<body>
<a class="skip" href="#content">Skip to content</a>
<header class="top">
  <button class="top__menu" type="button" aria-label="Menu" aria-expanded="false" data-menu><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M4 12h16M4 17h16"/></svg></button>
  <a class="top__brand" href="${base}">${LOGO}<span>Concord</span><span class="top__docs">Manual</span></a>
  <button class="top__search" type="button" data-open-search aria-label="Search the manual"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="6.5"/><path d="m20 20-4.2-4.2"/></svg><span>Search the manual</span><kbd>Ctrl K</kbd></button>
  <button class="top__icon" type="button" aria-label="Switch between light and dark" title="Light / dark" data-theme><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="8.5"/><path d="M12 3.5a8.5 8.5 0 0 1 0 17z" class="fill"/></svg></button>
  <a class="top__link" href="${base}../">Home</a>
  <a class="top__link" href="${REPO}" rel="noreferrer">GitHub</a>
</header>
<div class="palette" data-palette hidden>
  <div class="palette__box" role="dialog" aria-modal="true" aria-label="Search the manual">
    <div class="palette__field"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="6.5"/><path d="m20 20-4.2-4.2"/></svg><input type="search" placeholder="Search the manual" aria-label="Search the manual" data-search autocomplete="off" spellcheck="false"><kbd>Esc</kbd></div>
    <div class="palette__results" data-results role="listbox"></div>
    <div class="palette__foot"><span><kbd>↑</kbd><kbd>↓</kbd> move</span><span><kbd>Enter</kbd> open</span><span><kbd>Esc</kbd> close</span></div>
  </div>
</div>
<div class="layout${current ? "" : " layout--home"}">
  <nav class="nav" data-nav aria-label="Manual"><a class="nav__link nav__home${current ? "" : " is-current"}" href="${base}">Overview</a>${nav}</nav>
  <div class="scrim" data-scrim hidden></div>
  <main class="main" id="content">
    ${current ? `<div class="crumb">${esc(PAGES.find((p) => p.slug === current).section)}</div>` : ""}
    ${toc.length > 1 ? `<details class="toc-inline"><summary>On this page</summary>${tocLinks}</details>` : ""}
    <article class="doc">
${body}    </article>
    ${file ? `<p class="edit"><a href="${REPO}/edit/main/${file}" rel="noreferrer">Edit this page on GitHub</a></p>` : ""}
    ${current ? `<nav class="pager" aria-label="Previous and next">${pagerLink(prev, "prev", "Previous")}${pagerLink(next, "next", "Next")}</nav>` : ""}
    <footer class="foot">Concord is free software under the GNU Affero General Public License, version 3 or later. These pages are built from the Markdown in the repository.</footer>
  </main>
  ${toc.length > 1 ? `<aside class="toc" aria-label="On this page"><div class="toc__title">On this page</div>${tocLinks}</aside>` : "<span></span>"}
</div>
<script>window.__docs={base:${JSON.stringify(base)}}</script>
<script src="${base}docs.js" defer></script>
</body>
</html>
`;
}

/** The home page: a hero whose quick answers are drawn as a conversation,
 *  then a card per section. `href` is relative to docs/manual/. */
const ASKS = [
  ["M", "mira", "How do I get my friends into a guild?", "guilds/", "Invites, channels and roles"],
  ["T", "tomas", "Is anyone in the middle reading this?", "privacy/", "What peers and the rendezvous see"],
  ["A", "ade", "I got a new phone. Is my history gone?", "devices/", "Link it, and history follows"],
  ["K", "kai", "Nobody can connect to me.", "troubleshooting/", "Reachability, step by step"],
];
const ICONS = {
  "Start here": '<path d="M5 12h14M13 6l6 6-6 6"/>',
  Talking: '<path d="M4 6.5A2.5 2.5 0 0 1 6.5 4h11A2.5 2.5 0 0 1 20 6.5v7a2.5 2.5 0 0 1-2.5 2.5H10l-4 4v-4h0A2 2 0 0 1 4 14z"/>',
  Safety: '<path d="M12 3l7 3v5c0 4.5-3 8.3-7 10-4-1.7-7-5.5-7-10V6z"/><path d="m9 12 2 2 4-4"/>',
  "Making it yours": '<path d="M12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M5.6 18.4l2.1-2.1M16.3 7.7l2.1-2.1"/><circle cx="12" cy="12" r="3"/>',
  Help: '<circle cx="12" cy="12" r="9"/><path d="M9.5 9.5a2.5 2.5 0 1 1 3.5 2.3c-.6.3-1 .9-1 1.6V14M12 17.5v.01"/>',
  Developers: '<path d="m8 8-4 4 4 4M16 8l4 4-4 4M13.5 5l-3 14"/>',
};

function homeBody(summaries) {
  const asks = ASKS.map(
    ([initial, name, q, href, a], k) => `<a class="ask" href="${href}" style="--i:${k}"><span class="ask__avatar ask__avatar--${k}" aria-hidden="true">${initial}</span><span class="ask__body"><span class="ask__name">${name}</span><span class="ask__text">${esc(q)}</span><span class="ask__reply">${esc(a)} <span aria-hidden="true">→</span></span></span></a>`,
  ).join("");
  const cards = SECTIONS.map(
    (s) => `<section class="home__section"><div class="home__head"><span class="home__icon"><svg viewBox="0 0 24 24" aria-hidden="true">${ICONS[s.title] ?? ""}</svg></span><h2>${esc(s.title)}</h2></div><div class="cards">${s.pages
      .map((p) => `<a class="card" href="${p.slug}/"><span class="card__title"><span class="card__hash" aria-hidden="true">#</span>${esc(p.title)}</span><span class="card__sub">${esc(summaries[p.slug] ?? "")}</span></a>`)
      .join("")}</div></section>`,
  ).join("\n");
  return `<div class="hero">
<div class="hero__copy">
<p class="hero__kicker">Documentation</p>
<h1>Everything Concord does, and how.</h1>
<p class="hero__lede">Peer-to-peer, end-to-end encrypted group chat. This manual covers installing it on every platform, guilds and direct messages, calls, devices and backups, what is private and from whom, and running the optional rendezvous.</p>
<div class="hero__actions"><a class="btn btn--primary" href="getting-started/">Get started</a><button class="btn" type="button" data-open-search><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="6.5"/><path d="m20 20-4.2-4.2"/></svg>Search<kbd>/</kbd></button></div>
</div>
<div class="hero__chat" aria-label="Common questions"><div class="hero__channel"><span aria-hidden="true">#</span>help</div>${asks}</div>
</div>
${cards}
`;
}

export function build({ check = false } = {}) {
  const problems = [];
  const files = new Map(); // repo-relative path → content
  const rendered = PAGES.map((p) => ({ page: p, ...renderPage(p, problems) }));
  const summaries = Object.fromEntries(rendered.map((r) => [r.page.slug, r.summary]));
  const search = [];
  rendered.forEach((r, i) => {
    for (const { page, hash } of r.links) {
      const ids = page ? rendered.find((x) => x.page === page).ids : r.ids;
      if (hash && !ids.has(hash) && !/-\d+$/.test(hash)) problems.push(`${r.page.file}: link to a missing heading ${page ? page.file : ""}#${hash}`);
    }
    if (!r.summary) problems.push(`${r.page.file}: no summary paragraph under the title`);
    files.set(`${OUT}/${r.page.slug}/index.html`, shell({
      title: r.title, body: r.body, toc: r.toc, current: r.page.slug, prev: PAGES[i - 1], next: PAGES[i + 1], file: r.page.file, summary: r.summary, base: "../",
    }));
    for (const chunk of r.body.split(/(?=<h[23] id=")/)) {
      const m = /^<h([23]) id="([^"]+)">(.*?)<\/h\1>/.exec(chunk);
      search.push({
        p: r.page.title,
        u: `${r.page.slug}/${m ? `#${m[2]}` : ""}`,
        h: m ? plain(m[3].replace(/<a class="anchor"[^>]*>#<\/a>/, "")) : "",
        t: plain(chunk.replace(/<pre[\s\S]*?<\/pre>/g, " ").replace(/<h[1-3][^>]*>[\s\S]*?<\/h[1-3]>/, "")).slice(0, 600),
      });
    }
  });
  files.set(`${OUT}/index.html`, shell({ title: "Overview", body: homeBody(summaries), toc: [], current: null, file: null, summary: "The Concord manual: installing, guilds, calls, devices, privacy and the rendezvous.", base: "" }));
  files.set(`${OUT}/search.json`, `${JSON.stringify(search)}\n`);

  let stale = [];
  for (const [path, content] of files) {
    const abs = join(ROOT, path);
    const old = existsSync(abs) ? readFileSync(abs, "utf8") : null;
    if (old === content) continue;
    if (check) stale.push(path);
    else { mkdirSync(dirname(abs), { recursive: true }); writeFileSync(abs, content); }
  }
  // A page directory no longer in the table is left behind by a rename.
  const known = new Set(PAGES.map((p) => p.slug));
  for (const d of readdirSync(join(ROOT, OUT), { withFileTypes: true })) {
    if (d.isDirectory() && existsSync(join(ROOT, OUT, d.name, "index.html")) && !known.has(d.name)) stale.push(`${OUT}/${d.name}/ (no longer a page; delete it)`);
  }
  for (const p of problems) console.error(`build-docs: ${p}`);
  if (check && stale.length) console.error(`build-docs: out of date, run make docs:\n  ${stale.join("\n  ")}`);
  if (!check) console.log(`build-docs: ${PAGES.length} pages → ${OUT}/`);
  if (problems.length || stale.length) process.exitCode = 1;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) build({ check: process.argv.includes("--check") });
