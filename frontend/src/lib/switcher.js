// switcher.js — what the command palette (Ctrl+K) can find, and in what order.
//
// Kept out of QuickSwitcher.svelte so the rules can be tested without a
// browser. Three things it did not do before:
//
//   • Recents only knew guild channels. The trail of visited conversations
//     (S.recentChannels) holds DM channel ids too, but they were looked up in
//     a map of guild channels only, so the DM you just came from was never
//     offered again.
//   • People were not searchable. A DM is listed under its partner's name, so
//     a DM that already existed could be found, but typing a member's name
//     found nobody you had not yet written to.
//   • The prefixes people type out of habit — "#" for a channel, "@" for a
//     person, "!" for a voice channel, "*" for a guild — matched nothing,
//     because they were taken as part of the name.

// parseQuery splits a leading mode prefix off what was typed.
export function parseQuery(raw) {
  const t = (raw || "").trimStart();
  const modes = { ">": "actions", "/": "actions", "#": "channels", "@": "people", "!": "voice", "*": "guilds" };
  const mode = modes[t[0]];
  if (mode) return { mode, q: t.slice(1).trim().toLowerCase() };
  return { mode: "all", q: t.trim().toLowerCase() };
}

// buildDests — every place and person the palette can take you to.
//   guilds: S.guilds; people: [{ fingerprint, name }] (contacts and members);
//   self: your own fingerprint, never offered.
export function buildDests(guilds, people = [], self = "") {
  const out = [];
  const dmWith = new Set();
  for (const g of guilds || []) {
    if (g.kind === "dm") {
      if (g.dmPeer) dmWith.add(g.dmPeer);
      out.push({ kind: "dm", key: `dm:${g.id}`, label: g.name, sub: "direct message", g, channelId: g.channels?.[0]?.id || "", icon: "edit" });
      continue;
    }
    out.push({ kind: "guild", key: `g:${g.id}`, label: g.name, sub: "guild", g, icon: "diamond" });
    for (const c of g.channels || []) {
      const icon = c.type === "voice" ? "speaker" : c.type === "announcement" ? "megaphone" : "hash";
      out.push({ kind: "channel", key: `c:${c.id}`, label: c.name, sub: g.name, c, g, channelId: c.id, icon });
    }
  }
  // People you have no one-to-one DM with yet: picking one opens (creates) it.
  const seen = new Set();
  for (const p of people || []) {
    const fpr = p?.fingerprint;
    if (!fpr || fpr === self || dmWith.has(fpr) || seen.has(fpr) || !p.name) continue;
    seen.add(fpr);
    out.push({ kind: "person", key: `p:${fpr}`, label: p.name, sub: "start a direct message", fpr, icon: "user" });
  }
  return out;
}

const IN_MODE = {
  all: () => true,
  actions: () => false,
  channels: (d) => d.kind === "channel" && d.c.type !== "voice",
  voice: (d) => d.kind === "channel" && d.c.type === "voice",
  people: (d) => d.kind === "dm" || d.kind === "person",
  guilds: (d) => d.kind === "guild",
};
export const inMode = (d, mode) => (IN_MODE[mode] || IN_MODE.all)(d);

// recentFirst — the empty-query list: conversations you visited, newest
// first and not the one you are in, then the rest of the channels so a fresh
// install still has something to show.
export function recentFirst(dests, recent, activeChannelId, mode = "all", cap = 6) {
  const byChannel = new Map();
  for (const d of dests) if (d.channelId && (d.kind === "channel" || d.kind === "dm")) byChannel.set(d.channelId, d);
  const trail = (recent || [])
    .filter((cid) => cid !== activeChannelId)
    .map((cid) => byChannel.get(cid))
    .filter((d) => d && inMode(d, mode));
  const seen = new Set(trail);
  const rest = dests.filter((d) => !seen.has(d) && d.channelId !== activeChannelId && (mode === "all" ? d.kind === "channel" : inMode(d, mode)));
  return [...trail, ...rest].slice(0, cap);
}

// fuzzy — lower score is better. Returns the matched character indices for
// highlighting, or null. The order of preference is the order a reader expects:
// the whole name, then the query as a run at the start of a word ("shop" in
// "workshop" is not a word start, "make" in "Lantern Makers" is), then the query
// as a run anywhere, and only then the letters scattered in order. The scattered
// match used to be the only one, and it took the FIRST occurrence of each
// letter — so "op" lit the "o" of "work" and the "p" of "shop" in "workshop",
// highlighting characters that were not the match the reader typed.
const WORD_EDGE = /[\s\-_#@.:/·]/;
export function fuzzy(text, q) {
  const lower = String(text || "").toLowerCase();
  q = String(q || "").toLowerCase();
  if (!q) return { score: 0, idx: [] };
  const run = (at) => Array.from({ length: q.length }, (_, k) => at + k);
  const tail = lower.length * 0.01;
  if (lower === q) return { score: -1000, idx: run(0) };
  for (let at = lower.indexOf(q); at >= 0; at = lower.indexOf(q, at + 1)) {
    if (at === 0 || WORD_EDGE.test(lower[at - 1])) return { score: -500 + at * 0.5 + tail, idx: run(at) };
  }
  const at = lower.indexOf(q);
  if (at >= 0) return { score: -200 + at * 0.5 + tail, idx: run(at) };
  let ti = 0;
  let score = 0;
  const idx = [];
  for (const c of q) {
    const found = lower.indexOf(c, ti);
    if (found < 0) return null;
    score += found - ti;
    idx.push(found);
    ti = found + 1;
  }
  // A match at the very start of the name beats the same match mid-word.
  return { score: score + (idx[0] || 0) * 0.5 + tail, idx };
}

export function rank(list, q, cap) {
  return list
    .map((i) => ({ i, m: fuzzy(i.label, q) }))
    .filter((x) => x.m)
    .sort((a, b) => a.m.score - b.m.score)
    .slice(0, cap)
    .map((x) => ({ ...x.i, idx: x.m.idx, score: x.m.score }));
}
