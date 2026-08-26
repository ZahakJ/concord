// search.js — global message search: operator parsing, on-device filtering,
// and the shared open/close/refine state machine behind the header box and
// the results panel.
//
// The backend (api.searchMessages) substring-matches free text across ALL
// conversations; the search operators are parsed out here and applied
// on-device:
//   from:name  in:#channel  has:link|image|file  before:YYYY-MM-DD  after:…
import { S, flash } from "./state.svelte.js";
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
    closeSearch();
    return;
  }
  // A bare operator ("from:") is half a thought, not a query.
  const { text } = parseQuery(raw);
  if (text.trim().length < 2) return;
  S.searchLoading = true;
  typeTimer = setTimeout(runSearch, delay);
}

export async function runSearch(e) {
  e?.preventDefault();
  clearTimeout(typeTimer); // Enter overtakes whatever the debounce was about to send
  const raw = S.searchQuery.trim();
  if (!raw) {
    closeSearch();
    return;
  }
  const { text, filters, chips } = parseQuery(raw);
  S.searchChips = chips;
  // Free-text terms drive <mark> highlighting in the results panel.
  S.searchTerms = text.split(/\s+/).filter(Boolean);
  S.searchLoading = true;
  const my = ++seq;
  try {
    const res = (await api.searchMessages(text)) || [];
    if (my !== seq) return;
    S.searchResults = res.filter((m) => matchFilters(m, filters));
  } catch (err) {
    if (my === seq) flash(err);
  } finally {
    if (my === seq) S.searchLoading = false;
  }
}

// removeChip drops one operator token from the query and re-runs the search;
// with nothing left to search for, it closes instead.
export function removeChip(chip) {
  const q = S.searchQuery.replace(chip.raw, " ").replace(/\s+/g, " ").trim();
  S.searchQuery = q;
  if (!q) closeSearch();
  else runSearch();
}

export function closeSearch() {
  seq++; // invalidate any in-flight response
  clearTimeout(typeTimer);
  S.searchResults = null;
  S.searchQuery = "";
  S.searchChips = [];
  S.searchTerms = [];
  S.searchLoading = false;
}
