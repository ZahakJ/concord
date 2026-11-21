// releasenotes.js — the release notes as the splash reads them.
//
// One file per release under src/releases/ (vX.Y.Z.md) is the single source:
// the same markdown is handed to scripts/publish-release.sh for the GitHub
// release, so the splash and the release page cannot say different things.
// This module only parses; nothing here fetches.
//
// The shape a release's notes take (docs/RELEASING.md):
//
//   Concord 0.62.0 lets … (the lead paragraph: the release in one breath)
//
//   ## A feature
//   <!-- hero: attach -->        optional: which drawing the deck shows
//   - a bullet
//   - another, which may wrap
//     onto the next line
//
//   ## Security / ## Also / ## Known limits / ## Before you downgrade
//   - fine print: never a slide of its own, always shown word for word in
//     the deck's full notes, and Known limits is never shortened.

// Sections that are fine print. Matched on the heading's start, case-blind.
const FINE = [
  ["security", /^security\b/i],
  ["limits", /^known (limits?|issues?)\b/i],
  ["downgrade", /^before you (downgrade|update|upgrade)\b/i],
  ["also", /^(also|and also|other changes|smaller changes|fixes|fixed)\b/i],
];

// The drawings the deck has (ReleaseHero.svelte). A hint naming anything else
// falls back to the keywords, then to the generic one.
export const HEROES = ["rejoin", "profile", "attach", "desktop", "call", "search", "bell", "shield", "spark"];

// Keyword fallback, first match wins, read against the heading then the
// section's text.
const KEYWORDS = [
  ["attach", /\b(attach|photo|picture|image|video|file|media|gallery|upload)/i],
  ["profile", /\b(profile|avatar|banner|bio|presence)/i],
  ["call", /\b(call|voice|ring|camera|screen ?share)/i],
  ["search", /\b(search|find|jump|switcher)/i],
  ["bell", /\b(notif|badge|mention|unread|inbox)/i],
  ["desktop", /\b(linux|windows|macos|desktop|install|plugin|gstreamer)/i],
  ["rejoin", /\b(device|guild|owner|member|sync|rejoin|removed)/i],
  ["shield", /\b(secur|safe|privacy|encrypt|passphrase|recovery)/i],
];

/** Which drawing a section wears: its hint if it names one, else a keyword
 *  guess from its heading, else from its text, else the generic spark. */
export function heroFor(title, hint, text = "") {
  const h = (hint || "").trim().toLowerCase();
  if (HEROES.includes(h)) return h;
  for (const src of [title || "", text || ""]) {
    for (const [kind, re] of KEYWORDS) if (re.test(src)) return kind;
  }
  return "spark";
}

/** A fine-print kind for a heading, or "" for a feature. */
export function fineKind(title) {
  for (const [kind, re] of FINE) if (re.test((title || "").trim())) return kind;
  return "";
}

/** Parse one release's markdown. `version` is the file's own version. */
export function parseNotes(md, version = "") {
  const lines = String(md || "").replace(/\r\n?/g, "\n").split("\n");
  const lead = []; // paragraphs before the first section
  const sections = [];
  let sec = null;
  let para = null; // the open paragraph or bullet: { kind, text }
  const close = () => {
    if (!para) return;
    para.text = para.text.trim();
    if (para.text) (sec ? sec.items : lead).push(para);
    para = null;
  };
  for (const raw of lines) {
    const line = raw.replace(/\s+$/, "");
    const hint = /^\s*<!--\s*hero:\s*([\w-]+)\s*-->\s*$/i.exec(line);
    if (hint) {
      if (sec) sec.hint = hint[1].toLowerCase();
      continue;
    }
    if (/^\s*<!--[\s\S]*-->\s*$/.test(line)) continue; // any other comment
    const h2 = /^##\s+(.+?)\s*#*$/.exec(line);
    if (h2) {
      close();
      sec = { title: h2[1].trim(), hint: "", items: [] };
      sections.push(sec);
      continue;
    }
    if (/^#\s/.test(line)) {
      // A top-level title ("# Concord 0.62.0") names the file; the deck has
      // its own title.
      close();
      continue;
    }
    if (!line.trim()) {
      close();
      continue;
    }
    const li = /^\s{0,3}[-*+]\s+(.*)$/.exec(line);
    if (li) {
      close();
      para = { kind: "li", text: li[1] };
      continue;
    }
    if (para) para.text += " " + line.trim();
    else para = { kind: "p", text: line.trim() };
  }
  close();
  const out = sections.map((s) => {
    const fine = fineKind(s.title);
    const text = s.items.map((i) => i.text).join(" ");
    return {
      title: s.title,
      items: s.items,
      fine,
      hero: fine ? "" : heroFor(s.title, s.hint, text),
    };
  });
  return {
    version,
    summary: lead[0]?.text || "",
    lead: lead.map((p) => p.text),
    features: out.filter((s) => !s.fine),
    fine: out.filter((s) => s.fine),
    sections: out,
  };
}

// ---- versions -------------------------------------------------------------

/** "v0.62.0", "0.62.0" → [0, 62, 0]; anything else (dev, empty) → null. A
 *  suffix after the patch number ("0.62.0-rc1") is ignored. */
export function parseVersion(v) {
  const m = /^v?(\d+)\.(\d+)\.(\d+)/.exec(String(v || "").trim());
  return m ? [+m[1], +m[2], +m[3]] : null;
}

export function cmpVersion(a, b) {
  for (let i = 0; i < 3; i++) if (a[i] !== b[i]) return a[i] < b[i] ? -1 : 1;
  return 0;
}

/** The version a notes file speaks for, from its path ("./v0.62.0.md"). */
export function versionOfPath(p) {
  const m = /v?(\d+\.\d+\.\d+)\.md$/.exec(String(p || ""));
  return m ? m[1] : "";
}

/** Which release's notes to show.
 *
 *  `available` is the versions there are notes for; `app` is the running
 *  build's version (api.appVersion(): "v0.62.0" from a release, "dev" from a
 *  source build); `seen` is the version this device last ran, if known.
 *
 *  The newest notes not newer than the app. A build that names no version (a
 *  dev build) gets the newest notes there are. Notes this device already had
 *  the chance to see — not newer than `seen` — are not offered again: a 0.62.1
 *  with no notes of its own does not replay 0.62.0's deck. Returns the chosen
 *  version string, or "" for nothing to show. */
export function chooseNotes(available, app, seen = "") {
  const list = (available || [])
    .map((v) => [v, parseVersion(v)])
    .filter(([, p]) => p)
    .sort((a, b) => cmpVersion(b[1], a[1]));
  const mine = parseVersion(app);
  const pick = mine ? list.find(([, p]) => cmpVersion(p, mine) <= 0) : list[0];
  if (!pick) return "";
  const before = parseVersion(seen);
  if (before && cmpVersion(pick[1], before) <= 0) return "";
  return pick[0];
}

// ---- inline text ----------------------------------------------------------

/** Split a line of notes into runs the deck renders as text: plain, **strong**,
 *  *em* and `code`. A [link](url) keeps its words and drops the address: the
 *  splash opens nothing. Never HTML: every run is text. */
export function inlineRuns(text) {
  const s = String(text || "").replace(/\[([^\]]+)\]\([^)]*\)/g, "$1");
  const runs = [];
  const re = /\*\*(.+?)\*\*|`([^`]+)`|(?<![\w*])\*(?!\s)(.+?)(?<!\s)\*(?![\w*])/g;
  let at = 0;
  for (const m of s.matchAll(re)) {
    if (m.index > at) runs.push({ t: "text", v: s.slice(at, m.index) });
    if (m[1] !== undefined) runs.push({ t: "strong", v: m[1] });
    else if (m[2] !== undefined) runs.push({ t: "code", v: m[2] });
    else runs.push({ t: "em", v: m[3] });
    at = m.index + m[0].length;
  }
  if (at < s.length) runs.push({ t: "text", v: s.slice(at) });
  return runs;
}

/** The same line as plain words (for labels and announcements). */
export function plainText(text) {
  return inlineRuns(text)
    .map((r) => r.v)
    .join("");
}
