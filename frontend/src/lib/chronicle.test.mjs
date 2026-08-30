// Run with: node lib/chronicle.test.mjs
import {
  fmtBytes,
  fmtCount,
  rangeLabel,
  dateInput,
  nanoOfDate,
  policyFor,
  TIERS,
  DEFAULT_UI,
  historyBytes,
  estimateLine,
  leftBehind,
  histogramBars,
  channelRows,
  sortRows,
  phaseLabel,
  progressPct,
  resultLines,
  splitPlaceholders,
  foldName,
  channelTypeOf,
  landingFor,
} from "./chronicle.js";

let fails = 0;
const ok = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    fails++;
  }
};

// --- sizes: the same wording the backend's humanBytes produces ---
ok(fmtBytes(0) === "0 bytes", "zero bytes");
ok(fmtBytes(512) === "512 bytes", "bytes below a kilobyte stay bytes");
ok(fmtBytes(2048) === "2 KB", "kilobytes are whole");
ok(fmtBytes(5 << 20) === "5.0 MB", "megabytes carry one decimal");
ok(fmtBytes(3 * (1 << 30)) === "3.0 GB", "gigabytes carry one decimal");
ok(fmtBytes(undefined) === "0 bytes", "a missing size does not print NaN");
ok(fmtCount(1234567).includes("1"), "counts are grouped, not raw");

// --- ranges ---
const JAN2019 = Date.UTC(2019, 0, 15) * 1e6;
const AUG2026 = Date.UTC(2026, 7, 3) * 1e6;
ok(rangeLabel(JAN2019, AUG2026).includes("–"), "a real span reads as a range");
ok(rangeLabel(JAN2019, JAN2019).indexOf("–") === -1, "one month does not repeat itself");
ok(rangeLabel(0, 0) === "", "an empty channel has no range");
ok(rangeLabel(JAN2019, 0) !== "", "a half-known range still says what it knows");

// --- date inputs: local calendar days, and an EXCLUSIVE end ---
const day = nanoOfDate("2020-06-15");
ok(dateInput(day) === "2020-06-15", "a date round-trips through the input");
const end = nanoOfDate("2020-06-15", true);
ok(end - day === 86400 * 1e9, "the end of a day is midnight on the next one");
ok(nanoOfDate("") === 0, "an empty field means unbounded");
ok(nanoOfDate("not-a-date") === 0, "junk means unbounded, never NaN");
ok(dateInput(0) === "", "an unbounded edge shows an empty field");

// --- the policy the RPCs receive ---
const full = policyFor(DEFAULT_UI);
ok(full.includeImages && full.includeVideo && full.includeOther, "the default tier takes everything");
ok(full.maxAttachmentBytes === 5 << 20, "the default ceiling rides along");
ok(full.includeReactions === true && full.includeEmoji === true, "reactions and emoji default on");
ok(!("excludeChannels" in full), "an empty exclude list is not sent");
ok(!("fromNano" in full) && !("toNano" in full), "an unbounded range is not sent");

const imagesOnly = policyFor({ ...DEFAULT_UI, tier: "images" });
ok(imagesOnly.includeImages === true, "images-only keeps images");
ok(imagesOnly.includeVideo === false && imagesOnly.includeOther === false, "images-only drops the rest");

const noneTier = policyFor({ ...DEFAULT_UI, tier: "none" });
ok(!noneTier.includeImages && !noneTier.includeVideo && !noneTier.includeOther, "the none tier takes nothing");
ok(!("maxAttachmentBytes" in noneTier), "a ceiling on a tier that takes nothing is not sent");

const trimmed = policyFor({ ...DEFAULT_UI, source: "  Old place  ", description: "   " });
ok(trimmed.source === "Old place", "the source label is trimmed");
ok(!("description" in trimmed), "a blank description is not sent");

const excluded = policyFor({ ...DEFAULT_UI, exclude: ["c-voice"], reactions: false, emoji: false });
ok(excluded.excludeChannels.length === 1, "excluded channels are sent");
ok(excluded.includeReactions === false, "a switched-off toggle is sent as false, not omitted");
ok(policyFor({ ...DEFAULT_UI, exclude: ["a"] }).excludeChannels !== DEFAULT_UI.exclude, "the exclude list is copied, not aliased");
ok(TIERS.length === 3, "three tiers, and the wizard's radios enumerate them");

// --- the estimate line ---
const est = {
  messages: 1880,
  chunkBytes: 300000,
  manifestBytes: 12000,
  attachmentBytes: 2 << 20,
};
ok(historyBytes(est) === 312000, "history is index plus pages");
const line = estimateLine(est);
ok(line.startsWith("Will import ~1,880 messages"), `estimate line counts messages: ${line}`);
ok(line.includes("history") && line.includes("attachments"), "estimate line names both halves");
ok(estimateLine({ messages: 1, chunkBytes: 10, manifestBytes: 0 }).includes("1 message,"), "one message is singular");
ok(!estimateLine({ messages: 5, chunkBytes: 10 }).includes("attachments"), "no attachments, no attachment clause");
ok(estimateLine({ messages: 0 }) === "Nothing to import under this policy", "an empty policy says so plainly");
ok(estimateLine(null) === "", "no estimate yet renders nothing");

// --- what is being left behind ---
const stats = { messages: 2000, attachmentBytes: 40 << 20 };
const left = leftBehind(est, stats);
ok(left.messages === 120, "left-behind messages are the difference");
ok(left.bytes === (40 << 20) - (2 << 20), "left-behind bytes are the difference");
ok(left.text.startsWith("Leaving 120 messages and "), `left-behind reads as a sentence: ${left.text}`);
ok(leftBehind({ messages: 2000, attachmentBytes: 40 << 20 }, stats).text === "", "a full import leaves nothing behind");
ok(leftBehind({ messages: 3000, attachmentBytes: 0 }, stats).messages === 0, "never negative when the estimate overshoots");
ok(leftBehind(est, null).text === "", "no scan, no claim");

// --- the histogram ---
const bars = histogramBars({ histogram: [10, 5, 4, 1], localHistogram: [10, 5, 0, 0] });
ok(bars.length === 4, "four size classes");
ok(bars[0].pct === 50, "percentages are of the count");
ok(bars[2].local === 0, "the local tally rides beside the total");
ok(histogramBars({}).every((b) => b.pct === 0), "an export with no attachments draws no bars");

// --- the channel table ---
const scan = {
  channels: [
    { id: "c-general", name: "general", messages: 1200, attachmentBytes: 900, firstNano: JAN2019, lastNano: AUG2026 },
    { id: "c-plans", name: "plans", messages: 800, attachmentBytes: 100, firstNano: JAN2019, lastNano: AUG2026 },
    { id: "c-voice", name: "lounge", type: "voice", messages: 0, attachmentBytes: 0 },
  ],
};
const rows = channelRows(scan, ["c-voice"]);
ok(rows.length === 3, "every channel is listed, included or not");
ok(rows.find((r) => r.id === "c-voice").included === false, "an excluded channel is listed as excluded");
ok(rows.find((r) => r.id === "c-general").included === true, "the rest are included by default");
ok(channelRows(null).length === 0, "no scan, no rows");

ok(foldName("  General  Chat ") === "general chat", "names fold the way the importer matches them");
ok(channelTypeOf("GUILD_VOICE") === "voice", "an export voice room is a voice room");
ok(channelTypeOf("news") === "announcement", "news maps to announcement");
ok(channelTypeOf("text") === "text", "unknown types become text");
{
  const guild = [
    { name: "general", type: "text", parent: "" },
    { name: "Lounge", type: "voice", parent: "" },
  ];
  const intoGeneral = landingFor({ name: "General", type: "text" }, guild);
  ok(intoGeneral.existing && intoGeneral.label === "#general (existing)", `general lands in existing: ${intoGeneral.label}`);
  const intoNew = landingFor({ name: "plans", type: "text" }, guild);
  ok(!intoNew.existing && intoNew.label === "#plans (new)", `plans is new: ${intoNew.label}`);
  const lounge = landingFor({ name: "lounge", type: "voice" }, guild);
  ok(lounge.existing && lounge.label === "Lounge (existing)", `voice lounge matches by name+type: ${lounge.label}`);
  const notVoice = landingFor({ name: "lounge", type: "text" }, guild);
  ok(!notVoice.existing, "a text lounge does not land in a voice Lounge");
}

const byCount = sortRows(rows, "messages", -1);
ok(byCount[0].id === "c-general", "descending by count puts the biggest first");
ok(sortRows(rows, "messages", 1)[0].id === "c-voice", "ascending flips it");
ok(sortRows(rows, "name", 1)[0].name === "general", "name sorts alphabetically");
ok(sortRows(rows, "messages", -1) !== rows, "sorting returns a new array");
// Stability: two channels with the same count fall back to the name.
const tied = sortRows(
  [
    { id: "b", name: "beta", messages: 5 },
    { id: "a", name: "alpha", messages: 5 },
  ],
  "messages",
  -1,
);
ok(tied[0].name === "alpha", "a tie breaks on the name, so the order never jitters");

// --- files the archive names but does not hold ---
{
  const one = splitPlaceholders("look at this\n[attachment not exported: holiday.mov, 42.0 MB]");
  ok(one.text === "look at this", "the body loses the placeholder line");
  ok(one.files.length === 1 && one.files[0].name === "holiday.mov", "the file is named");
  ok(one.files[0].size === "42.0 MB", "and sized");

  const sizeless = splitPlaceholders("[attachment not exported: mystery.bin]");
  ok(sizeless.text === "", "a body that was nothing but a placeholder empties out");
  ok(sizeless.files[0].size === "", "a file of unknown size says nothing rather than 0");

  const many = splitPlaceholders(
    "three of them\n[attachment not exported: a.png, 2 KB]\n[attachment not exported: b.png, 2 KB]\n[attachment not exported: c.png]",
  );
  ok(many.files.length === 3, "every placeholder is picked up");
  ok(many.text === "three of them", "and none of them are left in the text");

  const imposter = splitPlaceholders("[attachment not exported: nice try] but with words after");
  ok(imposter.files.length === 0, "a line that only looks like one is not one");
  ok(imposter.text.includes("nice try"), "so it renders as the text somebody wrote");

  ok(splitPlaceholders("").text === "", "empty content is safe");
  ok(splitPlaceholders(null).files.length === 0, "missing content is safe");
  ok(splitPlaceholders("plain words").text === "plain words", "ordinary text is untouched");
}

// --- the running job ---
ok(phaseLabel("reading") === "Reading messages", "phases read as sentences");
ok(phaseLabel("who-knows") === "Working", "an unknown phase still says something");
ok(progressPct({ phase: "reading", done: 50, total: 200 }) === 25, "the bar fills by done/total");
ok(progressPct({ phase: "done" }) === 100, "done is full");
ok(progressPct({ phase: "attaching", done: 0, total: 0 }) === -1, "a phase with no total is indeterminate");
ok(progressPct(null) === -1, "no status is indeterminate");
ok(progressPct({ phase: "reading", done: 500, total: 200 }) === 100, "the bar never overflows");

// --- the completion summary ---
const lines = resultLines({
  imported: 1880,
  channels: 2,
  channelsCreated: 2,
  channelsReused: 0,
  attachmentsSealed: 3,
  attachmentBytesSealed: 1000,
  placeholders: 2,
  emojiImported: 0,
  skippedByPolicy: 120,
  skippedMalformed: 0,
  chunks: 4,
  chunkBytes: 300000,
  manifestBytes: 12000,
});
ok(lines[0].startsWith("1,880 messages imported"), "the count leads");
ok(lines.some((l) => l.includes("2 channels matched") && l.includes("2 created")), `structural matches add up: ${lines[1]}`);
ok(!lines.some((l) => l.includes("custom emoji")), "a zero is not printed as a line");
ok(lines.some((l) => l.includes("left out by the policy")), "what the policy dropped is reported");
ok(resultLines(null).length === 0, "no result, no lines");
{
  const split = resultLines({
    imported: 10,
    channels: 3,
    channelsCreated: 2,
    channelsReused: 2,
    chunks: 1,
    chunkBytes: 0,
    manifestBytes: 0,
  });
  ok(
    split[1].includes("4 channels matched") &&
      split[1].includes("2 created") &&
      split[1].includes("2 already existed") &&
      split[1].includes("1 received no messages"),
    `created+reused is the match count, not a parenthetical that disagrees: ${split[1]}`,
  );
}

console.log(fails ? `chronicle.js: ${fails} FAILED` : "chronicle.js: all tests passed");
process.exit(fails ? 1 : 0);
