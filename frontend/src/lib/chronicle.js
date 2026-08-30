// chronicle.js — the arithmetic and the wording behind the chat-archive import
// wizard, kept out of the components so it can be tested without a DOM.
//
// The wizard's whole promise is that it tells you what an import will cost
// BEFORE it costs it, so every number on screen comes from here and every one
// of them is derived from a backend report rather than guessed at. Two reports
// feed it: the scan (`ScanChatExport`, one pass over the directory) and the
// estimate (`EstimateChatImport`, pure arithmetic over that scan, cheap enough
// to re-run on every keystroke).
//
// The one rule worth stating: nothing here rounds in the flattering direction.
// A policy that leaves forty gigabytes behind says forty gigabytes.

// ---- sizes and counts -----------------------------------------------------

// fmtBytes matches the backend's humanBytes so the same file is never "1.4 MB"
// in the wizard and "1.5 MB" in the archive it produced.
export function fmtBytes(n) {
  const v = Number(n) || 0;
  if (v >= 1 << 30) return `${(v / (1 << 30)).toFixed(1)} GB`;
  if (v >= 1 << 20) return `${(v / (1 << 20)).toFixed(1)} MB`;
  if (v >= 1 << 10) return `${Math.round(v / (1 << 10))} KB`;
  return `${Math.round(v)} bytes`;
}

// fmtCount groups thousands. A ten-year export is a seven-digit number and an
// ungrouped one is unreadable at a glance, which is the only glance this screen
// gets.
export function fmtCount(n) {
  return (Number(n) || 0).toLocaleString();
}

// ---- dates ----------------------------------------------------------------

// Nanoseconds are what the manifest and the scan speak; JavaScript dates are
// milliseconds. A float64 loses about 256ns of precision at 2026 epochs, which
// is invisible in a month label and is why every cursor that round-trips
// through one is deduplicated by message id rather than trusted to be exact.
export const msOf = (nano) => (Number(nano) || 0) / 1e6;

export function monthLabel(nano) {
  if (!nano) return "";
  const d = new Date(msOf(nano));
  if (isNaN(d)) return "";
  return d.toLocaleDateString([], { month: "short", year: "numeric" });
}

export function dayLabel(nano) {
  if (!nano) return "";
  const d = new Date(msOf(nano));
  if (isNaN(d)) return "";
  return d.toLocaleDateString([], { year: "numeric", month: "short", day: "numeric" });
}

// rangeLabel is the span a channel or an archive covers: "Jan 2019 – Aug 2026",
// collapsed to one month when that is all it spans.
export function rangeLabel(firstNano, lastNano) {
  const a = monthLabel(firstNano);
  const b = monthLabel(lastNano);
  if (!a && !b) return "";
  if (!a || !b || a === b) return a || b;
  return `${a} – ${b}`;
}

// dateInput / nanoOfDate convert between an <input type="date"> and the policy's
// nanoseconds. The input speaks local calendar days, so the conversion goes
// through local midnight — a person choosing "1 March" means their first of
// March, not UTC's.
export function dateInput(nano) {
  if (!nano) return "";
  const d = new Date(msOf(nano));
  if (isNaN(d)) return "";
  const p = (n) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

export function nanoOfDate(value, endOfDay = false) {
  if (!value) return 0;
  const [y, m, d] = value.split("-").map(Number);
  if (!y || !m || !d) return 0;
  // ToNano is exclusive, so "up to and including the 5th" is midnight on the
  // 6th. Getting this wrong loses a day off the end of every bounded import.
  const dt = new Date(y, m - 1, d + (endOfDay ? 1 : 0), 0, 0, 0, 0);
  return dt.getTime() * 1e6;
}

// ---- the attachment tiers -------------------------------------------------

// The three answers to "how much of the media do you want?", in the order they
// cost. Each one is a set of policy fields; the size ceiling is chosen
// separately and applies to whichever kinds are switched on.
export const TIERS = [
  {
    id: "all",
    label: "Everything under the size limit",
    hint: "Pictures, video and files the export brought with it.",
    fields: { includeImages: true, includeVideo: true, includeOther: true },
  },
  {
    id: "images",
    label: "Images only",
    hint: "Pictures come across; video and other files become a named placeholder.",
    fields: { includeImages: true, includeVideo: false, includeOther: false },
  },
  {
    id: "none",
    label: "No attachments",
    hint: "Text only. Every file becomes a line naming it and its size.",
    fields: { includeImages: false, includeVideo: false, includeOther: false },
  },
];

// The ceilings offered beside the tier. 5 MiB is what the live path caps an
// inline image at and is the backend's own default, so it leads.
export const CAPS = [
  { bytes: 1 << 20, label: "1 MB" },
  { bytes: 5 << 20, label: "5 MB" },
  { bytes: 25 << 20, label: "25 MB" },
  { bytes: 0, label: "No limit" },
];

export const DEFAULT_UI = {
  tier: "all",
  cap: 5 << 20,
  reactions: true,
  emoji: true,
  fromNano: 0,
  toNano: 0,
  exclude: [],
  source: "",
  description: "",
};

// policyFor turns the wizard's own state into the object the two RPCs take.
// Omitted fields are false on the wire, which is why every one the UI can turn
// OFF is written out explicitly rather than left to a default.
export function policyFor(ui) {
  const tier = TIERS.find((t) => t.id === ui.tier) || TIERS[0];
  const p = {
    includeImages: tier.fields.includeImages,
    includeVideo: tier.fields.includeVideo,
    includeOther: tier.fields.includeOther,
    includeReactions: !!ui.reactions,
    includeEmoji: !!ui.emoji,
  };
  // A ceiling on a tier that takes nothing is noise in the request and reads as
  // a contradiction in a log.
  if (tier.id !== "none" && ui.cap > 0) p.maxAttachmentBytes = ui.cap;
  if (ui.fromNano > 0) p.fromNano = ui.fromNano;
  if (ui.toNano > 0) p.toNano = ui.toNano;
  if (ui.exclude?.length) p.excludeChannels = [...ui.exclude];
  if (ui.source?.trim()) p.source = ui.source.trim();
  if (ui.description?.trim()) p.description = ui.description.trim();
  return p;
}

// ---- the estimate line ----------------------------------------------------

// historyBytes is what every member carries whether or not they ever scroll:
// the index plus the compressed pages. Attachments are separate because they
// are fetched lazily and are the part a policy can actually shrink.
export const historyBytes = (est) => (est?.chunkBytes || 0) + (est?.manifestBytes || 0);

// estimateLine is the sentence pinned to the bottom of the policy step. The
// tildes are not decoration: the chunk figure is a projection from a measured
// compression ratio and runs about 10% over on a real import.
export function estimateLine(est) {
  if (!est) return "";
  const msgs = est.messages || 0;
  if (msgs === 0) return "Nothing to import under this policy";
  const parts = [`~${fmtBytes(historyBytes(est))} history`];
  if (est.attachmentBytes > 0) parts.push(`${fmtBytes(est.attachmentBytes)} attachments`);
  return `Will import ~${fmtCount(msgs)} ${msgs === 1 ? "message" : "messages"}, ${parts.join(" + ")}`;
}

// leftBehind measures the policy against the whole export: what it is choosing
// not to take. Nothing is subtracted from the totals — a message excluded by a
// date range and a picture over the ceiling are both things the reader will not
// find later, and the screen says so.
export function leftBehind(est, stats) {
  if (!est || !stats) return { messages: 0, bytes: 0, text: "" };
  const messages = Math.max(0, (stats.messages || 0) - (est.messages || 0));
  const bytes = Math.max(0, (stats.attachmentBytes || 0) - (est.attachmentBytes || 0));
  const bits = [];
  if (messages > 0) bits.push(`${fmtCount(messages)} ${messages === 1 ? "message" : "messages"}`);
  if (bytes > 0) bits.push(fmtBytes(bytes));
  return { messages, bytes, text: bits.length ? `Leaving ${bits.join(" and ")} behind` : "" };
}

// ---- the bill -------------------------------------------------------------

// The four-bucket histogram as a readable bar row. Percentages are of the
// COUNT, not the bytes: the question it answers is "how many of these files are
// big?", which is what a size ceiling acts on.
export const SIZE_CLASSES = ["≤64 KB", "≤512 KB", "≤5 MB", "> 5 MB"];

export function histogramBars(stats) {
  const hist = stats?.histogram || [];
  const local = stats?.localHistogram || [];
  const total = hist.reduce((a, b) => a + (b || 0), 0);
  return SIZE_CLASSES.map((label, i) => ({
    label,
    count: hist[i] || 0,
    local: local[i] || 0,
    pct: total > 0 ? ((hist[i] || 0) / total) * 100 : 0,
  }));
}

// foldName matches the importer's comparison: case-insensitive, whitespace
// folded. "General" and "general" are the same room to everybody except a
// string comparison, and that is how the backend decides reuse.
export function foldName(s) {
  return String(s || "")
    .toLowerCase()
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .join(" ");
}

// channelTypeOf is the same mapping chronimport.ChannelTypeOf uses, so the
// "Lands in" column cannot disagree with what the import will actually do.
export function channelTypeOf(t) {
  const l = String(t || "").toLowerCase();
  if (l.includes("voice") || l.includes("stage")) return "voice";
  if (l.includes("forum")) return "forum";
  if (l.includes("news") || l.includes("announce")) return "announcement";
  return "text";
}

const hashPrefix = (type) => (type === "voice" ? "" : "#");

// landingFor answers "where will this row go in THIS guild": an existing
// channel of the same folded name and type, or a new one. The wizard used to
// say nothing until the completion screen mentioned "2 reused" — after 1,188
// strangers' messages had already landed in #general.
export function landingFor(scanCh, guildChannels = []) {
  const raw = String(scanCh?.name || scanCh?.id || "").trim() || "imported";
  const ctype = channelTypeOf(scanCh?.type);
  const want = foldName(raw);
  const match = (guildChannels || []).find(
    (ch) => !ch.parent && foldName(ch.name) === want && (ch.type || "text") === ctype,
  );
  if (match) {
    const type = match.type || ctype;
    return {
      existing: true,
      name: match.name,
      type,
      label: `${hashPrefix(type)}${match.name} (existing)`,
    };
  }
  return {
    existing: false,
    name: raw,
    type: ctype,
    label: `${hashPrefix(ctype)}${raw} (new)`,
  };
}

// channelRows flattens the scan into the table the bill draws, with the
// include-checkbox state folded in so the component renders one array.
export function channelRows(stats, exclude = [], guildChannels = []) {
  const out = [];
  for (const c of stats?.channels || []) {
    out.push({
      id: c.id,
      name: c.name || c.id,
      type: c.type || "",
      category: c.category || "",
      messages: c.messages || 0,
      firstNano: c.firstNano || 0,
      lastNano: c.lastNano || 0,
      attachmentBytes: c.attachmentBytes || 0,
      localAttachmentBytes: c.localAttachmentBytes || 0,
      included: !exclude.includes(c.id),
      landing: landingFor(c, guildChannels),
    });
  }
  return out;
}

// sortRows: one comparator, so the header cells all behave the same way. Name
// sorts alphabetically; everything else numerically, and every sort falls back
// to the name so the order is stable across re-renders.
export function sortRows(rows, key, dir = -1) {
  const sign = dir < 0 ? -1 : 1;
  const cmp = (a, b) => {
    if (key === "name") return sign * a.name.localeCompare(b.name);
    const av = a[key] ?? 0;
    const bv = b[key] ?? 0;
    if (av !== bv) return sign * (av < bv ? -1 : 1);
    return a.name.localeCompare(b.name);
  };
  return [...rows].sort(cmp);
}

// ---- files the archive names but does not hold ----------------------------

// The importer writes one of these lines per attachment it could not carry —
// a file the export only linked to, or one over the size ceiling the policy
// set. The exact wording is internal/app/chronimport.go's placeholderLine, and
// this is the only place that knows it: reading them back out of the body lets
// the feed draw a file-shaped stub instead of leaving square brackets in the
// middle of somebody's sentence.
//
// A line that does not match is left exactly where it was. Somebody typing
// "[attachment not exported: nice try]" gets it rendered as the text they wrote
// — the stub is a rendering of a known line, not a claim about the world.
const PLACEHOLDER_RE = /^\[attachment not exported: ([^\]]+?)(?:, ((?:\d+(?:\.\d+)? (?:bytes|KB|MB|GB))))?\]$/;

export function splitPlaceholders(content) {
  const files = [];
  const kept = [];
  for (const line of String(content || "").split("\n")) {
    const m = PLACEHOLDER_RE.exec(line);
    if (m) files.push({ name: m[1], size: m[2] || "" });
    else kept.push(line);
  }
  // Only trim the tail: the importer appends placeholders after the body, so
  // removing them can leave a trailing blank line and nothing else.
  return { text: kept.join("\n").replace(/\s+$/, ""), files };
}

// ---- the running job ------------------------------------------------------

// What each phase of an import is called where somebody can read it. The
// backend's phase names are identifiers; these are the sentences.
const PHASES = {
  scanning: "Reading the export",
  structure: "Creating channels",
  emoji: "Importing emoji",
  reading: "Reading messages",
  building: "Packing pages",
  attaching: "Signing and attaching",
  done: "Finished",
  failed: "Failed",
};

export function phaseLabel(phase) {
  return PHASES[phase] || "Working";
}

// progressPct is the bar's fill. A phase with no total of its own (signing) has
// nothing honest to report, so it returns -1 and the caller draws an
// indeterminate bar rather than inventing a percentage.
export function progressPct(status) {
  if (!status) return -1;
  if (status.phase === "done") return 100;
  const total = Number(status.total) || 0;
  if (total <= 0) return -1;
  return Math.max(0, Math.min(100, (Number(status.done) / total) * 100));
}

// resultLines is the completion summary: the numbers worth reading, and only
// the ones that are not zero. An import that skipped nothing should not print
// three lines of zeroes to prove it.
//
// The channel line used to count two different things in one parenthetical —
// channels that received messages vs structural matches — and print
// "3 channels (2 created, 2 reused)", which does not add up. Count the
// matches, then say how they split, then mention any that carried no history.
export function resultLines(res) {
  if (!res) return [];
  const created = res.channelsCreated || 0;
  const reused = res.channelsReused || 0;
  const matched = created + reused;
  const withHistory = res.channels || 0;
  const empty = Math.max(0, matched - withHistory);
  const bits = [];
  if (created) bits.push(`${created} created`);
  if (reused) bits.push(`${reused} already existed`);
  let struct = `${fmtCount(matched)} ${matched === 1 ? "channel" : "channels"} matched`;
  if (bits.length) struct += ` — ${bits.join(", ")}`;
  if (empty > 0) struct += `; ${empty} received no messages`;
  const out = [
    `${fmtCount(res.imported)} ${res.imported === 1 ? "message" : "messages"} imported`,
    struct,
  ];
  if (res.attachmentsSealed > 0)
    out.push(`${fmtCount(res.attachmentsSealed)} attachments · ${fmtBytes(res.attachmentBytesSealed)}`);
  if (res.placeholders > 0) out.push(`${fmtCount(res.placeholders)} files named but not carried`);
  if (res.emojiImported > 0) out.push(`${fmtCount(res.emojiImported)} custom emoji`);
  if (res.skippedByPolicy > 0) out.push(`${fmtCount(res.skippedByPolicy)} left out by the policy`);
  if (res.skippedMalformed > 0) out.push(`${fmtCount(res.skippedMalformed)} unreadable entries skipped`);
  out.push(`${fmtCount(res.chunks)} pages · ${fmtBytes(res.chunkBytes + res.manifestBytes)} on every member's device`);
  return out;
}
