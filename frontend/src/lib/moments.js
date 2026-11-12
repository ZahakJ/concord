// moments.js — the order a guild's moments play in.
//
// A guild's stories arrive newest-first. The viewer plays them grouped by
// author (an author's moments read as one run), each author's oldest-first (a
// moment is a point in time; playback reads forward), and starts on the first
// one you have not seen — or on `fpr`'s first unseen when an author is named.

export function momentsPlaylist(stories, fpr = "") {
  const seen = new Set();
  const by = new Map();
  for (const s of stories || []) {
    if (!s || seen.has(s.id)) continue;
    seen.add(s.id);
    if (!by.has(s.author)) by.set(s.author, []);
    by.get(s.author).push(s);
  }
  const list = [];
  for (const run of by.values()) list.push(...run.reverse());
  let start = list.findIndex((s) => (!fpr || s.author === fpr) && !s.seen);
  if (start < 0) start = fpr ? list.findIndex((s) => s.author === fpr) : 0;
  return { list, start: Math.max(0, start) };
}
