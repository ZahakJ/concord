// search.js — global message search: operator parsing, on-device filtering,
// and the shared open/close/refine state machine behind the header box and
// the results panel.
//
// The backend (api.searchMessages) substring-matches free text across ALL
// conversations; the search operators are parsed out here and applied
// on-device:
//   from:name  in:#channel  has:link|image|file  before:YYYY-MM-DD  after:…
import { S, flash, focusComposer } from "./state.svelte.js";
import { api } from "./api.js";

// parseQuery splits a raw query into free text, structured filters, and the
// chip list (one per recognised operator, keeping the raw token so removing
// a chip can splice it back out of the query string).
export function parseQuery(raw) {
  const f = { from: null, in: null, has: [], before: null, after: null };
  const chips = [];
  const text = raw
    .replace(/(\w+):("[^"]+"|\S+)/g, (m, key, val) => {
      val = val.replace(/^"|"$/g, "");
      const k = key.toLowerCase();
      switch (k) {
        case "from":
          f.from = val.toLowerCase();
          chips.push({ key: k, raw: m, label: `from:@${val}` });
          return "";
        case "in":
          f.in = val.replace(/^#/, "").toLowerCase();
          chips.push({ key: k, raw: m, label: `in:#${val.replace(/^#/, "")}` });
          return "";
        case "has":
          f.has.push(val.toLowerCase());
          chips.push({ key: k, raw: m, label: `has:${val.toLowerCase()}` });
          return "";
        case "before":
          f.before = new Date(val);
          chips.push({ key: k, raw: m, label: `before:${val}` });
          return "";
        case "after":
          f.after = new Date(val);
          chips.push({ key: k, raw: m, label: `after:${val}` });
          return "";
        default:
          return m; // unknown operator: keep as search text
      }
    })
    .trim();
  return { text, filters: f, chips };
}

function channelNameFor(chId) {
  for (const g of S.guilds) {
    const c = g.channels.find((x) => x.id === chId);
    if (c) return c.name;
  }
  return "";
}

export function matchFilters(m, f) {
  if (f.from && !(m.senderName || m.name || "").toLowerCase().includes(f.from)) return false;
  if (f.in) {
    const cn = channelNameFor(m.channelId).toLowerCase();
    if (!cn.includes(f.in)) return false;
  }
  if (f.before && !isNaN(f.before) && new Date(m.sent) >= f.before) return false;
  if (f.after && !isNaN(f.after) && new Date(m.sent) <= f.after) return false;
  const c = m.content || "";
  for (const h of f.has) {
    if (h === "link" && !/https?:\/\//.test(c)) return false;
    if (h === "image" && !/concord:\/\/attach|data:image\//.test(c)) return false;
    if (h === "file" && !/concord:\/\/file/.test(c)) return false;
  }
  return true;
}

// The operator vocabulary, as data rather than as a sentence in a title=
// attribute. The panel renders one chip per entry and clicking it types the
// prefix for you — which is the only form of this documentation a phone user
// has ever been able to reach, native tooltips having no touch equivalent.
export const FILTERS = [
  { prefix: "from:", hint: "a person" },
  { prefix: "in:#", hint: "a channel" },
  { prefix: "has:", hint: "link, image, file" },
  { prefix: "before:", hint: "YYYY-MM-DD" },
  { prefix: "after:", hint: "YYYY-MM-DD" },
];

// The live input, so a chip click can put the caret back where the typing is.
let inputEl = null;
export function registerSearchInput(el) {
  inputEl = el;
  return () => {
    if (inputEl === el) inputEl = null;
  };
}

export function insertFilter(prefix) {
  const q = S.searchQuery.trim();
  S.searchQuery = (q ? q + " " : "") + prefix;
  const n = S.searchQuery.length;
  inputEl?.focus();
  inputEl?.setSelectionRange?.(n, n);
}

// Monotonic ticket so a slow response can't clobber a newer search (or a
// search the user has since closed).
let seq = 0;

// Live search. Enter used to be the only way to find out whether a query
// matched anything, which meant every refinement was a round trip through a
// key you had to know to press — and no feedback at all in between.
//
// Two guards keep it from being a keystroke firehose: the debounce, and a
// two-character floor. A one-letter query substring-matches most of the
// archive and answers with everything, which is slower AND less useful than
// waiting. Enter still forces the search whatever is typed.
let typeTimer = null;
export function queueSearch(delay = 200) {
  clearTimeout(typeTimer);
  const raw = S.searchQuery.trim();
  if (!raw) {
    closeSearch({ home: false });
    return;
  }
  const { text, chips } = parseQuery(raw);
  // A bare operator IS a query: the panel offers `from: a person` and
  // `has: link, image, file` as chips and types the prefix for you, so
  // following that affordance to "show me everything from Bilal" has to work.
  // It used to bail here — leaving S.searchLoading true from the keystroke
  // before, which is the "Searching" that never stopped — and then answer
  // 0 results on Enter, because an empty needle made the store return nil.
  if (text.trim().length < 2 && !chips.length) {
    // Half a word is not a query either, but saying so beats a spinner.
    S.searchLoading = false;
    S.searchHint = text.trim() ? "Keep typing — two characters at least." : "";
    return;
  }
  S.searchHint = "";
  S.searchLoading = true;
  typeTimer = setTimeout(runSearch, delay);
}

export async function runSearch(e) {
  e?.preventDefault();
  clearTimeout(typeTimer); // Enter overtakes whatever the debounce was about to send
  const raw = S.searchQuery.trim();
  if (!raw) {
    closeSearch({ home: false });
    return;
  }
  const { text, filters, chips } = parseQuery(raw);
  S.searchChips = chips;
  if (text.trim().length < 2 && !chips.length) {
    S.searchLoading = false;
    S.searchResults = [];
    S.searchHint = "Add a word to search for.";
    return;
  }
  S.searchHint = "";
  // Free-text terms drive <mark> highlighting in the results panel.
  S.searchTerms = text.split(/\s+/).filter(Boolean);
  S.searchLoading = true;
  const my = ++seq;
  try {
    // `from:` and `in:` go DOWN, into the SQL the scan is bounded by, so a query
    // that is only operators is answered from the whole history rather than
    // from whatever page an empty needle would otherwise have returned.
    const res = (await api.searchMessages(text, filters.from || "", filters.in || "")) || [];
    if (my !== seq) return;
    S.searchResults = res.filter((m) => matchFilters(m, filters));
  } catch (err) {
    if (my === seq) flash(err);
  } finally {
    if (my === seq) S.searchLoading = false;
  }
  // The archive is a second pass over a second store, so it is a second call —
  // and it runs AFTER the live one rather than beside it, because the live
  // results are what the reader is waiting for and the archive is the answer to
  // "and what about the old stuff?".
  //
  // The panel used to claim ALL CONVERSATIONS and mean "the messages table":
  // 1,981 imported messages, on the screen behind the panel, scrolling
  // perfectly, answered 0 results to three phrases read straight off them.
  searchArchive(text, my);
}

// searchArchive runs the archive pass for the guild on screen. Scoped to that
// guild because an archive belongs to one — and because that is the guild whose
// history the reader can see behind the panel and is asking about.
async function searchArchive(text, my) {
  const guildId = S.activeGuildId;
  S.searchArchive = null;
  if (!guildId || !S.chronicle?.messages) return;
  S.searchArchiveLoading = true;
  try {
    const res = await api.searchChronicle(guildId, text);
    if (my !== seq) return;
    S.searchArchive = res || null;
  } catch {
    // A failed archive pass must not take the live results down with it. The
    // panel simply says nothing about the archive, which is where it was.
    if (my === seq) S.searchArchive = null;
  } finally {
    if (my === seq) S.searchArchiveLoading = false;
  }
}

// removeChip drops one operator token from the query and re-runs the search;
// with nothing left to search for, it closes instead.
export function removeChip(chip) {
  const q = S.searchQuery.replace(chip.raw, " ").replace(/\s+/g, " ").trim();
  S.searchQuery = q;
  if (!q) closeSearch({ home: false });
  else runSearch();
}

export function closeSearch({ home = true } = {}) {
  seq++; // invalidate any in-flight response
  clearTimeout(typeTimer);
  S.searchArchive = null;
  S.searchArchiveLoading = false;
  S.searchResults = null;
  S.searchQuery = "";
  S.searchChips = [];
  S.searchTerms = [];
  S.searchLoading = false;
  S.searchHint = "";
  // Escape out of search used to abandon focus in the header. `home` is false
  // only for the internal calls that close because the field went empty — the
  // caret is already in the search box there, and stealing it mid-typing would
  // be worse than the bug.
  if (home) focusComposer();
}
